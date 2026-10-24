package itu

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
)

// Reporter receives progress events from Build.
type Reporter interface {
	carriers.Reporter
	// Bulletin reports one fetched Operational Bulletin (done of total).
	Bulletin(done, total int, src carriers.Source, a *Amendment, err error)
}

// Options configure Build.
type Options struct {
	Workers int  // concurrent bulletin downloads
	FromOB  int  // first bulletin to scan; 0 = the base list's OB + 1
	ToOB    int  // last bulletin; 0 = latest ITU lists
	Skip901 bool // skip the live 901 database
}

// Result is the built dataset plus run metrics.
type Result struct {
	Dataset
	Base       *BaseList
	Edition    string
	FirstOB    int
	LastOB     int
	Bulletins  int // bulletins scanned
	Missing    []int
	Outcomes   map[Outcome]int
	Actions    map[Action]int
	StageTimes []time.Duration
}

// stages counts Build's stages plus the caller's final write.
const stages = 6

// Build fetches the ITU sources and assembles the dataset.
func Build(ctx context.Context, c *Client, opt Options, rep Reporter) (*Result, error) {
	if opt.Workers <= 0 {
		opt.Workers = 4
	}
	res := &Result{Outcomes: map[Outcome]int{}, Actions: map[Action]int{}}
	res.Meta = map[string]string{}
	t0 := time.Now()
	lap := func() {
		res.StageTimes = append(res.StageTimes, time.Since(t0))
		t0 = time.Now()
	}

	// 1. Base list.
	rep.Stage(1, stages, "base MNC list (ITU-T E.212B)")
	edition, err := c.LatestBaseEdition(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding E.212B edition: %w", err)
	}
	rep.Info("latest edition: %s", edition)
	doc, src, err := c.FetchBaseList(ctx, edition)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", edition, err)
	}
	rep.Info("%s  %s  %s", src.URL, carriers.FmtBytes(src.Bytes), cacheTag(src))
	base, err := ParseBaseList(doc)
	if err != nil {
		return nil, err
	}
	res.Base, res.Edition = base, edition
	res.Sources = append(res.Sources, src)
	countries := map[string]bool{}
	for _, n := range base.Networks {
		countries[n.Country] = true
	}
	rep.Info("annex to OB %d, position on %s: %d networks in %d countries/groups, %d extraterritorial rows",
		base.OB, base.Position, len(base.Networks), len(countries), len(base.ExtraTerritorial))
	for _, s := range base.Skipped {
		rep.Warn("base list: skipped row %q", s)
	}
	lap()

	// 2. Which bulletins can carry amendments.
	rep.Stage(2, stages, "discover Operational Bulletins")
	res.FirstOB = opt.FromOB
	if res.FirstOB == 0 {
		res.FirstOB = base.OB + 1
	}
	latestYear := 0
	res.LastOB = opt.ToOB
	if res.LastOB == 0 {
		res.LastOB, latestYear, err = c.LatestOB(ctx)
		if err != nil {
			return nil, fmt.Errorf("finding latest OB: %w", err)
		}
	}
	if latestYear == 0 {
		latestYear = time.Now().Year()
	}
	baseYear, _ := strconv.Atoi(edition[len(edition)-4:])
	n := res.LastOB - res.FirstOB + 1
	if n < 0 {
		n = 0
	}
	rep.Info("bulletins %d..%d (%d to scan, latest published %d)", res.FirstOB, res.LastOB, n, latestYear)
	lap()

	// 3. Fetch bulletins concurrently, parse each one's MNC amendment.
	rep.Stage(3, stages, "fetch bulletins and parse MNC amendments")
	type item struct {
		ob  int
		a   *Amendment
		src carriers.Source
		err error
	}
	items := make([]item, n)
	jobs := make(chan int)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
	)
	for w := 0; w < opt.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				ob := res.FirstOB + i
				// Bulletins run ~24 a year; estimate the year, then try neighbours.
				est := baseYear + (ob-base.OB)/24
				years := uniq(est, est+1, est-1, latestYear)
				doc, src, err := c.FetchOB(ctx, ob, years)
				it := item{ob: ob, src: src, err: err}
				if err == nil {
					it.a = ParseAmendment(doc, ob)
				}
				items[i] = it
				mu.Lock()
				done++
				rep.Bulletin(done, n, src, it.a, err)
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < n; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var amendments []*Amendment
	for _, it := range items {
		switch {
		case errors.Is(it.err, carriers.ErrNotFound):
			res.Missing = append(res.Missing, it.ob)
			continue
		case it.err != nil:
			return nil, fmt.Errorf("OB %d: %w", it.ob, it.err)
		}
		res.Bulletins++
		res.Sources = append(res.Sources, it.src)
		if it.a == nil {
			continue
		}
		if it.a.BaseOB != 0 && it.a.BaseOB != base.OB {
			rep.Warn("OB %d: amendment %d is to OB %d's list, not %d; skipped", it.ob, it.a.No, it.a.BaseOB, base.OB)
			continue
		}
		amendments = append(amendments, it.a)
	}
	for _, ob := range res.Missing {
		rep.Warn("OB %d not found on ITU's site", ob)
	}
	// Report gaps in the amendment numbering.
	sort.Slice(amendments, func(i, j int) bool { return amendments[i].No < amendments[j].No })
	for i := 1; i < len(amendments); i++ {
		for gap := amendments[i-1].No + 1; gap < amendments[i].No; gap++ {
			rep.Warn("amendment No. %d not found in any bulletin", gap)
		}
	}
	lap()

	// 4. Replay amendments over the base list.
	rep.Stage(4, stages, "replay amendments")
	reg := NewRegistry(base.Networks, base.ExtraTerritorial)
	for _, a := range amendments {
		for _, ch := range a.Changes {
			ap := reg.Apply(ch)
			res.Amendments = append(res.Amendments, ap)
			res.Outcomes[ap.Outcome]++
			res.Actions[ap.Action]++
			if ap.Outcome != OutcomeApplied {
				rep.Warn("amendment %d (OB %d): %s %s %s %s %q: %s", a.No, a.OB, ch.Action, ch.Country,
					ch.MCC, ch.MNC, ch.Operator, ap.Outcome)
			}
		}
	}
	res.Networks = reg.Networks()
	res.ExtraTerritorial = reg.ExtraTerritorial()
	first, last := 0, 0
	if len(amendments) > 0 {
		first, last = amendments[0].No, amendments[len(amendments)-1].No
	}
	rep.Info("%d amendments (No. %d..%d), %d changes: %d ADD, %d SUP, %d LIR",
		len(amendments), first, last, len(res.Amendments), res.Actions[ActionAdd], res.Actions[ActionSup], res.Actions[ActionLIR])
	rep.Info("outcomes: %d applied, %d replaced, %d no_match, %d ambiguous",
		res.Outcomes[OutcomeApplied], res.Outcomes[OutcomeReplaced], res.Outcomes[OutcomeNoMatch], res.Outcomes[OutcomeAmbiguous])
	rep.Info("networks: %d in base list -> %d current", len(base.Networks), len(res.Networks))
	lap()

	// 5. Live 901 database.
	rep.Stage(5, stages, "live shared-code (MCC 901) database")
	if opt.Skip901 || c.Offline {
		rep.Info("skipped")
		res.Meta["shared901"] = "skipped"
	} else {
		codes, src, err := c.FetchShared901(ctx, func(page, rows, total int) {
			rep.Info("page %d: %d/%d rows", page, rows, total)
		})
		if err != nil {
			return nil, fmt.Errorf("901 database: %w", err)
		}
		res.SharedCodes = codes
		res.Sources = append(res.Sources, src)
		status := map[string]int{}
		for _, s := range codes {
			status[s.Status]++
		}
		rep.Info("%d codes %v  %s", len(codes), status, carriers.FmtBytes(src.Bytes))
		listed := map[string]bool{}
		for _, n := range res.Networks {
			listed[n.PLMN()] = true
		}
		for _, s := range codes {
			if s.Status == "ASSIGNED" && !listed[s.MCC+s.MNC] {
				rep.Info("%s %s %q assigned %s, not yet in any bulletin", s.MCC, s.MNC, s.Assignee, s.Assigned)
			}
		}
	}
	lap()

	res.Meta["base_edition"] = edition
	res.Meta["base_ob"] = strconv.Itoa(base.OB)
	res.Meta["base_position"] = base.Position
	res.Meta["first_ob"] = strconv.Itoa(res.FirstOB)
	res.Meta["last_ob"] = strconv.Itoa(res.LastOB)
	if len(amendments) > 0 {
		res.Meta["last_amendment"] = strconv.Itoa(last)
		res.Meta["last_amendment_ob"] = strconv.Itoa(amendments[len(amendments)-1].OB)
	}
	return res, nil
}

func uniq(ys ...int) []int {
	var out []int
	seen := map[int]bool{}
	for _, y := range ys {
		if y > 0 && !seen[y] {
			seen[y] = true
			out = append(out, y)
		}
	}
	return out
}

func cacheTag(s carriers.Source) string {
	if s.FromCache {
		return "cache"
	}
	return fmt.Sprintf("downloaded in %s", s.Elapsed.Round(time.Millisecond))
}
