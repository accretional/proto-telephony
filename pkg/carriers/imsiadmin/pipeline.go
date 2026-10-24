package imsiadmin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
)

// Reporter receives progress events from Build.
type Reporter interface {
	carriers.Reporter
	// Code reports one fetched detail page (done of total).
	Code(done, total int, r *Record, src carriers.Source, err error)
}

// Options configure Build.
type Options struct {
	Workers int // concurrent detail-page downloads
}

// Record is a directory entry with its status and detail page.
type Record struct {
	Entry
	Status Status
	Detail *Detail
}

// Result is the built dataset plus run metrics.
type Result struct {
	Dataset
	Kinds      map[Kind]int
	States     map[string]int
	Labels     map[string]int // detail field label -> occurrences
	Fetched    int            // detail pages downloaded (not from cache)
	StageTimes []time.Duration
}

const stages = 4 // plus the caller's write

// KnownLabels are the detail fields with their own columns; every field,
// known or not, is also kept in the details table.
var KnownLabels = []string{
	"HNI Code", "IBN", "Protocol", "Wireless License Number", "License Date", "Application Date",
	"Effective Date", "Estimated Service Date", "Deployment Date", "Modification Date", "Return Date",
	"Entity Name", "Entity Type",
}

func known(label string) bool {
	for _, k := range KnownLabels {
		if strings.EqualFold(k, label) {
			return true
		}
	}
	return false
}

// Build fetches both directories and every detail page.
func Build(ctx context.Context, c *Client, opt Options, rep Reporter) (*Result, error) {
	if opt.Workers <= 0 {
		opt.Workers = 4
	}
	res := &Result{Kinds: map[Kind]int{}, States: map[string]int{}, Labels: map[string]int{}}
	res.Meta = map[string]string{}
	t0 := time.Now()
	lap := func() {
		res.StageTimes = append(res.StageTimes, time.Since(t0))
		t0 = time.Now()
	}

	// 1. Directories.
	rep.Stage(1, stages+1, "directories (HNI codes, CBRS IBNs)")
	var entries []Entry
	for _, k := range []Kind{KindHNI, KindCBRS} {
		es, src, err := c.Directory(ctx, k)
		if err != nil {
			return nil, fmt.Errorf("%s directory: %w", k, err)
		}
		res.Sources = append(res.Sources, src)
		res.Kinds[k] = len(es)
		rep.Info("%-4s %3d codes  %s  %s", k, len(es), src.URL, cacheTag(src))
		entries = append(entries, es...)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Code()] {
			rep.Warn("duplicate code %s in the %s directory", e.Code(), e.Kind)
		}
		seen[e.Code()] = true
		if e.URL == "" {
			rep.Warn("%s %s has no detail page link", e.Kind, e.Code())
		}
	}
	lap()

	// 2. Last-modified times, so unchanged detail pages come from the cache.
	rep.Stage(2, stages+1, "modification index (WordPress REST API)")
	modified := map[string]string{}
	for _, k := range []Kind{KindHNI, KindCBRS} {
		m, srcs, err := c.Modified(ctx, k)
		res.Sources = append(res.Sources, srcs...)
		if err != nil {
			if c.Offline {
				rep.Info("%s: offline, using cached pages", k)
			} else {
				rep.Warn("%s modification index unavailable (%v); refetching every %s page", k, err, k)
			}
			continue
		}
		for u, t := range m {
			modified[u] = t
		}
		rep.Info("%-4s %3d posts in %d page(s)", k, len(m), len(srcs))
	}
	missing := 0
	for _, e := range entries {
		if _, ok := modified[e.URL]; !ok {
			missing++
		}
	}
	if missing > 0 && len(modified) > 0 {
		rep.Warn("%d directory entries not in the REST index (fetched regardless)", missing)
	}
	lap()

	// 3. Detail pages.
	rep.Stage(3, stages+1, "detail pages")
	recs := make([]*Record, len(entries))
	jobs := make(chan int)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		done int
		errs []error
	)
	for w := 0; w < opt.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				e := entries[i]
				r := &Record{Entry: e, Status: ParseStatus(e.Entity)}
				if r.Status.Related == e.Code() {
					r.Status.Related = "" // "RETURNED 10/20/25 (316-700)" on 316-700 itself
				}
				var (
					src carriers.Source
					err error
				)
				if e.URL != "" {
					var page []byte
					page, src, err = c.Detail(ctx, e, modified[e.URL])
					if err == nil {
						r.Detail, err = ParseDetail(page)
						if err != nil {
							err = fmt.Errorf("%s: %w", e.URL, err)
						}
					}
				}
				recs[i] = r
				mu.Lock()
				done++
				if err != nil {
					errs = append(errs, err)
				} else if src.URL != "" {
					res.Sources = append(res.Sources, src)
					if !src.FromCache {
						res.Fetched++
					}
				}
				rep.Code(done, len(entries), r, src, err)
				mu.Unlock()
			}
		}()
	}
	for i := range entries {
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
	if len(errs) > 0 {
		return nil, fmt.Errorf("%d detail pages failed, first: %w", len(errs), errs[0])
	}
	lap()

	// 4. Check what we got.
	rep.Stage(4, stages+1, "extract fields")
	for _, r := range recs {
		res.States[r.Status.State]++
		if r.Detail == nil {
			continue
		}
		for _, f := range r.Detail.Fields {
			res.Labels[f.Label]++
			if !known(f.Label) {
				rep.Warn("%s %s: unknown field %q = %q (kept in details)", r.Kind, r.Code(), f.Label, f.Value)
			}
		}
		if got := r.Detail.Get("HNI Code"); got != "" && got != r.HNI {
			rep.Warn("%s %s: detail page says HNI Code %s", r.Kind, r.Code(), got)
		}
		if got := r.Detail.Get("IBN"); r.Kind == KindCBRS && got != "" && got != r.IBN {
			rep.Warn("%s %s: detail page says IBN %s", r.Kind, r.Code(), got)
		}
		for _, f := range r.Detail.Fields {
			if strings.HasSuffix(f.Label, "Date") && f.Value != "" && NormDate(f.Value) == "" {
				rep.Warn("%s %s: unparsed date %s = %q", r.Kind, r.Code(), f.Label, f.Value)
			}
		}
	}
	labels := make([]string, 0, len(res.Labels))
	for l := range res.Labels {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(i, j int) bool { return res.Labels[labels[i]] > res.Labels[labels[j]] })
	var parts []string
	for _, l := range labels {
		parts = append(parts, fmt.Sprintf("%s %d", l, res.Labels[l]))
	}
	rep.Info("fields: %s", strings.Join(parts, ", "))
	rep.Info("states: %v", res.States)
	lap()

	res.Records = recs
	res.Meta["source"] = BaseURL
	res.Meta["fetched_at"] = time.Now().UTC().Format("2006-01-02")
	res.Meta["hni_codes"] = fmt.Sprint(res.Kinds[KindHNI])
	res.Meta["cbrs_codes"] = fmt.Sprint(res.Kinds[KindCBRS])
	return res, nil
}

func cacheTag(s carriers.Source) string {
	if s.FromCache {
		return "cache"
	}
	return fmt.Sprintf("%s in %s", carriers.FmtBytes(s.Bytes), s.Elapsed.Round(time.Millisecond))
}
