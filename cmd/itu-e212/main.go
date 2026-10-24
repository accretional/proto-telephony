// Command itu-e212 downloads the official ITU-T E.212 Mobile Network Code
// list — the E.212B base list, every amendment published in the ITU
// Operational Bulletins since, and ITU's live MCC 901 shared-code database —
// and writes the result to a SQLite database or a directory of CSV files.
//
//	itu-e212                         # -> data/carriers/itu.sqlite
//	itu-e212 -format csv             # -> data/carriers/itu/*.csv
//	itu-e212 -offline                # rebuild from data/cache/itu only
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/cli"
	"github.com/accretional/proto-telephony/pkg/carriers/itu"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

func main() {
	var common cli.Common
	common.Register(flag.CommandLine, "itu-e212", "itu")
	var (
		skip901 = flag.Bool("skip-901", false, "skip ITU's live MCC 901 database")
		fromOB  = flag.Int("from-ob", 0, "first Operational Bulletin to scan (default: the base list's OB + 1)")
		toOB    = flag.Int("to-ob", 0, "last Operational Bulletin to scan (default: latest published)")
		dump    = flag.String("dump-docx", "", "print a .docx's text tokens (table/row/cell) and exit; for debugging the parsers")
	)
	flag.Parse()

	if *dump != "" {
		if err := dumpDocx(*dump); err != nil {
			fmt.Fprintf(os.Stderr, "itu-e212: %v\n", err)
			os.Exit(1)
		}
		return
	}
	opt := itu.Options{Workers: common.Workers, FromOB: *fromOB, ToOB: *toOB, Skip901: *skip901}
	common.Main(itu.Schema, itu.Specs(), itu.Checks, func(ctx context.Context, out store.Output) error {
		return run(ctx, &common, opt, out)
	})
}

func run(ctx context.Context, common *cli.Common, opt itu.Options, out store.Output) error {
	c := itu.NewClient(common.Cache)
	c.Offline = common.Offline
	rep := &reporter{Console: carriers.NewConsole(os.Stdout), verbose: common.Verbose}

	fmt.Printf("itu-e212: ITU-T E.212 MNC list -> %s (%s)\n", out.Path, out.Format)
	fmt.Printf("  source %s  cache %s  workers %d%s\n", itu.BaseURL, cli.OrNone(common.Cache), opt.Workers,
		map[bool]string{true: "  OFFLINE"}[common.Offline])

	res, err := itu.Build(ctx, c, opt, rep)
	if err != nil {
		return err
	}

	rep.Stage(6, 6, "write "+out.Path)
	t := time.Now()
	res.Meta["tool"] = "itu-e212"
	res.Meta["elapsed"] = time.Since(rep.Start).Round(time.Millisecond).String()
	res.Meta["written_at"] = time.Now().UTC().Format(time.RFC3339)
	if err := out.Write(itu.Schema, itu.Tables(&res.Dataset)); err != nil {
		return fmt.Errorf("writing %s: %w", out.Path, err)
	}
	rep.Info("%d networks, %d amendment rows, %d extraterritorial, %d shared codes, %d sources  (%s, %s)",
		len(res.Networks), len(res.Amendments), len(res.ExtraTerritorial), len(res.SharedCodes), len(res.Sources),
		carriers.FmtBytes(out.Size()), time.Since(t).Round(time.Millisecond))

	summary(res, c, rep)
	return nil
}

func summary(res *itu.Result, c *itu.Client, rep *reporter) {
	countries := map[string]bool{}
	kinds := map[itu.Kind]int{}
	mccs := map[string]bool{}
	for _, n := range res.Networks {
		countries[n.Country] = true
		kinds[n.Kind]++
		mccs[n.MCC] = true
	}
	fmt.Println()
	fmt.Println("== summary")
	fmt.Printf("  base list      %s (annex to OB %d, position on %s)\n", res.Edition, res.Base.OB, res.Base.Position)
	fmt.Printf("  bulletins      %d scanned (OB %d..%d), %d missing\n", res.Bulletins, res.FirstOB, res.LastOB, len(res.Missing))
	fmt.Printf("  amendments     %d changes: %d applied, %d replaced, %d no_match, %d ambiguous\n", len(res.Amendments),
		res.Outcomes[itu.OutcomeApplied], res.Outcomes[itu.OutcomeReplaced],
		res.Outcomes[itu.OutcomeNoMatch], res.Outcomes[itu.OutcomeAmbiguous])
	fmt.Printf("  networks       %d (%d geographic, %d shared 901, %d ROIO 902) across %d MCCs, %d countries/groups\n",
		len(res.Networks), kinds[itu.KindGeographic], kinds[itu.KindShared], kinds[itu.KindROIO], len(mccs), len(countries))
	carriers.PrintStats(os.Stdout, &c.Stats, rep.Warnings)
	var parts []string
	names := []string{"base", "discover", "bulletins", "replay", "901"}
	for i, d := range res.StageTimes {
		if i < len(names) {
			parts = append(parts, fmt.Sprintf("%s %s", names[i], d.Round(time.Millisecond)))
		}
	}
	fmt.Printf("  time           %s total (%s)\n", time.Since(rep.Start).Round(time.Millisecond), strings.Join(parts, ", "))
}

func dumpDocx(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := itu.ReadDocx(data)
	if err != nil {
		return err
	}
	for i, t := range doc.Tokens {
		loc := "para"
		if t.Table >= 0 {
			loc = fmt.Sprintf("t%d r%d c%d", t.Table, t.Row, t.Cell)
		}
		fmt.Printf("%5d %-14s %q\n", i, loc, t.Text)
	}
	return nil
}

// reporter adds per-bulletin progress lines to the console reporter.
type reporter struct {
	*carriers.Console
	verbose bool
	bytes   int64
	changes int
}

func (r *reporter) Bulletin(done, total int, src carriers.Source, a *itu.Amendment, err error) {
	r.Mu.Lock()
	r.bytes += src.Bytes
	what := "no MNC amendment"
	if a != nil {
		r.changes += len(a.Changes)
		counts := map[itu.Action]int{}
		for _, ch := range a.Changes {
			counts[ch.Action]++
		}
		var cs []string
		for _, act := range []itu.Action{itu.ActionAdd, itu.ActionSup, itu.ActionLIR} {
			if counts[act] > 0 {
				cs = append(cs, fmt.Sprintf("%d %s", counts[act], act))
			}
		}
		what = fmt.Sprintf("amendment %d: %d changes (%s)", a.No, len(a.Changes), strings.Join(cs, ", "))
	}
	sum, changes := r.bytes, r.changes
	r.Mu.Unlock()
	if err != nil {
		r.Progress(done, total, "%v", err)
		return
	}
	tag := "cache"
	if !src.FromCache {
		tag = src.Elapsed.Round(time.Millisecond).String()
	}
	r.Progress(done, total, "%-18s %7s %7s  %s  | total %s, %d changes", src.Name, carriers.FmtBytes(src.Bytes), tag,
		what, carriers.FmtBytes(sum), changes)
	if r.verbose && a != nil {
		r.Mu.Lock()
		for _, ch := range a.Changes {
			fmt.Fprintf(r.W, "                 %s %-30s %s %s  %s\n", ch.Action, ch.Country, ch.MCC, ch.MNC, ch.Operator)
		}
		r.Mu.Unlock()
	}
}
