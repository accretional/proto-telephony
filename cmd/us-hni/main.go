// Command us-hni downloads the US HNI (MCC+MNC) assignments from IMSI Admin
// (imsiadmin.com), the US IMSI Administrator — the HNI and CBRS directories
// and every code's detail page — and writes them to a SQLite database or a
// directory of CSV files.
//
//	us-hni                     # -> data/carriers/us.sqlite
//	us-hni -format csv         # -> data/carriers/us/*.csv
//	us-hni -offline            # rebuild from data/cache/us only
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
	"github.com/accretional/proto-telephony/pkg/carriers/imsiadmin"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

func main() {
	var common cli.Common
	common.Register(flag.CommandLine, "us-hni", "us")
	flag.Parse()
	opt := imsiadmin.Options{Workers: common.Workers}
	common.Main(imsiadmin.Schema, imsiadmin.Specs(), imsiadmin.Checks, func(ctx context.Context, out store.Output) error {
		return run(ctx, &common, opt, out)
	})
}

func run(ctx context.Context, common *cli.Common, opt imsiadmin.Options, out store.Output) error {
	c := imsiadmin.NewClient(common.Cache)
	c.Offline = common.Offline
	rep := &reporter{Console: carriers.NewConsole(os.Stdout), verbose: common.Verbose}

	fmt.Printf("us-hni: US HNI assignments (IMSI Admin) -> %s (%s)\n", out.Path, out.Format)
	fmt.Printf("  source %s  cache %s  workers %d%s\n", imsiadmin.BaseURL, cli.OrNone(common.Cache), opt.Workers,
		map[bool]string{true: "  OFFLINE"}[common.Offline])

	res, err := imsiadmin.Build(ctx, c, opt, rep)
	if err != nil {
		return err
	}

	rep.Stage(5, 5, "write "+out.Path)
	t := time.Now()
	res.Meta["tool"] = "us-hni"
	res.Meta["elapsed"] = time.Since(rep.Start).Round(time.Millisecond).String()
	res.Meta["written_at"] = time.Now().UTC().Format(time.RFC3339)
	tbs := imsiadmin.Tables(&res.Dataset)
	if err := out.Write(imsiadmin.Schema, tbs); err != nil {
		return fmt.Errorf("writing %s: %w", out.Path, err)
	}
	var counts []string
	for _, tb := range tbs {
		counts = append(counts, fmt.Sprintf("%d %s", len(tb.Rows), tb.Name))
	}
	rep.Info("%s  (%s, %s)", strings.Join(counts, ", "), carriers.FmtBytes(out.Size()), time.Since(t).Round(time.Millisecond))

	fmt.Println()
	fmt.Println("== summary")
	fmt.Printf("  codes          %d HNI, %d CBRS IBNs\n", res.Kinds[imsiadmin.KindHNI], res.Kinds[imsiadmin.KindCBRS])
	fmt.Printf("  status         %v\n", res.States)
	fmt.Printf("  detail pages   %d downloaded, %d from cache\n", res.Fetched, len(res.Records)-res.Fetched)
	fmt.Printf("  fields         %d distinct labels, %d values\n", len(res.Labels), sum(res.Labels))
	carriers.PrintStats(os.Stdout, &c.Stats, rep.Warnings)
	var parts []string
	for i, name := range []string{"directories", "index", "details", "extract"} {
		if i < len(res.StageTimes) {
			parts = append(parts, fmt.Sprintf("%s %s", name, res.StageTimes[i].Round(time.Millisecond)))
		}
	}
	fmt.Printf("  time           %s total (%s)\n", time.Since(rep.Start).Round(time.Millisecond), strings.Join(parts, ", "))
	return nil
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// reporter adds per-code progress lines to the console reporter.
type reporter struct {
	*carriers.Console
	verbose bool
	fetched int
}

func (r *reporter) Code(done, total int, rec *imsiadmin.Record, src carriers.Source, err error) {
	if err != nil {
		r.Progress(done, total, "%-4s %-13s %v", rec.Kind, rec.Code(), err)
		return
	}
	if !src.FromCache {
		r.fetched++
	}
	// Downloads are always shown; cache hits only every 50 (or with -v).
	if src.FromCache && !r.verbose && done%50 != 0 && done != total {
		return
	}
	tag := "cache"
	if !src.FromCache {
		tag = fmt.Sprintf("%s %s", carriers.FmtBytes(src.Bytes), src.Elapsed.Round(time.Millisecond))
	}
	fields := 0
	if rec.Detail != nil {
		fields = len(rec.Detail.Fields)
	}
	r.Progress(done, total, "%-4s %-13s %-10s %2d fields  %-14s %.50s  | %d downloaded", rec.Kind, rec.Code(), rec.Status.State,
		fields, tag, rec.Entity, r.fetched)
}
