// Command national-mnc downloads the national MNC registries that publish
// a single machine-readable file — Canada (CNAC CSV), Mexico (IFT/CRT
// XLSX) and Japan (MIC XLSX) — each into its own data/carriers/<cc>/ (CSV)
// or data/carriers/<cc>.sqlite. Every source also writes the shared
// assignments table from research/schema.md. A failing source doesn't stop
// the others; the exit status is non-zero if any failed.
//
//	national-mnc -format csv            # all of ca, mx, jp
//	national-mnc -only mx -v
//	national-mnc -verify -format csv    # check existing output
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/cnac"
	"github.com/accretional/proto-telephony/pkg/carriers/ift"
	"github.com/accretional/proto-telephony/pkg/carriers/mic"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// source adapts one registry package.
type source struct {
	cc, title, base string
	schema          string
	specs           func() []store.Table
	checks          []store.CheckSpec
	build           func(ctx context.Context, c *carriers.Client, rep carriers.Reporter) ([]store.Table, map[string]string, error)
}

var sources = []source{
	{cc: "ca", title: "Canada — CNAC (MCC 302)", base: cnac.BaseURL, schema: cnac.Schema, specs: cnac.Specs, checks: cnac.Checks,
		build: func(ctx context.Context, c *carriers.Client, rep carriers.Reporter) ([]store.Table, map[string]string, error) {
			ds, err := cnac.Build(ctx, c, rep)
			if err != nil {
				return nil, nil, err
			}
			return cnac.Tables(ds), ds.Meta, nil
		}},
	{cc: "mx", title: "Mexico — IFT/CRT (MCC 334)", base: ift.BaseURL, schema: ift.Schema, specs: ift.Specs, checks: ift.Checks,
		build: func(ctx context.Context, c *carriers.Client, rep carriers.Reporter) ([]store.Table, map[string]string, error) {
			ds, err := ift.Build(ctx, c, rep)
			if err != nil {
				return nil, nil, err
			}
			return ift.Tables(ds), ds.Meta, nil
		}},
	{cc: "jp", title: "Japan — MIC 総務省 (MCC 440/441)", base: mic.BaseURL, schema: mic.Schema, specs: mic.Specs, checks: mic.Checks,
		build: func(ctx context.Context, c *carriers.Client, rep carriers.Reporter) ([]store.Table, map[string]string, error) {
			ds, err := mic.Build(ctx, c, rep)
			if err != nil {
				return nil, nil, err
			}
			return mic.Tables(ds), ds.Meta, nil
		}},
}

func main() {
	var (
		only     = flag.String("only", "", "comma-separated country codes to run (default: all of "+codes()+")")
		format   = flag.String("format", "sqlite", "output format: sqlite (data/carriers/<cc>.sqlite) or csv (data/carriers/<cc>/)")
		outDir   = flag.String("out-dir", filepath.Join("data", "carriers"), "parent directory of the outputs")
		cacheDir = flag.String("cache-dir", filepath.Join("data", "cache"), "parent directory of the per-country caches; empty disables")
		offline  = flag.Bool("offline", false, "build only from the caches, no network")
		verify   = flag.Bool("verify", false, "only run sanity checks on the existing outputs and exit (non-zero if any fail)")
	)
	flag.Parse()
	if *format != "sqlite" && *format != "csv" {
		fmt.Fprintf(os.Stderr, "national-mnc: -format must be sqlite or csv, not %q\n", *format)
		os.Exit(2)
	}
	selected, err := pick(*only)
	if err != nil {
		fmt.Fprintf(os.Stderr, "national-mnc: %v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	var failed []string
	type result struct {
		cc, status string
	}
	var results []result
	for _, s := range selected {
		out := store.Output{Format: *format, Path: filepath.Join(*outDir, s.cc)}
		if *format == "sqlite" {
			out.Path += ".sqlite"
		}
		var err error
		if *verify {
			err = runVerify(s, out)
		} else {
			cache := ""
			if *cacheDir != "" {
				cache = filepath.Join(*cacheDir, s.cc)
			}
			err = run(ctx, s, out, cache, *offline)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nnational-mnc: %s: %v\n", s.cc, err)
			failed = append(failed, s.cc)
			results = append(results, result{s.cc, "FAILED: " + err.Error()})
		} else {
			results = append(results, result{s.cc, "ok -> " + out.Path})
		}
		if ctx.Err() != nil {
			break
		}
	}
	fmt.Println()
	fmt.Printf("== national-mnc (%s)\n", time.Since(start).Round(time.Millisecond))
	for _, r := range results {
		fmt.Printf("  %-3s %s\n", r.cc, r.status)
	}
	if len(failed) > 0 {
		os.Exit(1)
	}
}

func codes() string {
	var cs []string
	for _, s := range sources {
		cs = append(cs, s.cc)
	}
	return strings.Join(cs, ",")
}

func pick(only string) ([]source, error) {
	if only == "" {
		return sources, nil
	}
	var out []source
	for _, cc := range strings.Split(only, ",") {
		cc = strings.ToLower(strings.TrimSpace(cc))
		found := false
		for _, s := range sources {
			if s.cc == cc {
				out, found = append(out, s), true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown source %q (have %s)", cc, codes())
		}
	}
	return out, nil
}

func run(ctx context.Context, s source, out store.Output, cache string, offline bool) error {
	c := carriers.NewClient(s.base, cache)
	c.Offline = offline
	rep := carriers.NewConsole(os.Stdout)
	fmt.Printf("\n######## %s -> %s (%s)\n", s.title, out.Path, out.Format)

	tbs, meta, err := s.build(ctx, c, rep)
	if err != nil {
		return err
	}
	rep.Stage(2, 2, "write "+out.Path)
	meta["tool"] = "national-mnc"
	meta["elapsed"] = time.Since(rep.Start).Round(time.Millisecond).String()
	meta["written_at"] = time.Now().UTC().Format(time.RFC3339)
	tbs[0] = store.MetaTable(meta) // the meta table is always first
	if err := out.Write(s.schema, tbs); err != nil {
		return fmt.Errorf("writing %s: %w", out.Path, err)
	}
	var counts []string
	for _, tb := range tbs {
		counts = append(counts, fmt.Sprintf("%d %s", len(tb.Rows), tb.Name))
	}
	rep.Info("%s  (%s)", strings.Join(counts, ", "), carriers.FmtBytes(out.Size()))
	carriers.PrintStats(os.Stdout, &c.Stats, rep.Warnings)
	return runVerify(s, out)
}

func runVerify(s source, out store.Output) error {
	checks, err := store.Verify(out, s.schema, s.specs(), s.checks)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if !store.PrintChecks(os.Stdout, out.Path, checks) {
		return fmt.Errorf("verify: checks failed")
	}
	return nil
}
