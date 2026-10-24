// Command carriers-merge combines every downloader's shared `assignments`
// (and `sub_allocations`) table under data/carriers/<source>/ into one
// dataset in data/carriers/merged/: all rows, the MCC map, the best-known
// current row per code (national registry where authoritative, else ITU),
// and the conflicts between ITU and each national registry. It reads only
// local CSV output; run the downloaders first.
//
//	carriers-merge -format csv
//	carriers-merge -verify -format csv
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/merge"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

func main() {
	var (
		in     = flag.String("in", filepath.Join("data", "carriers"), "directory holding <source>/assignments.csv")
		format = flag.String("format", "sqlite", "output format: sqlite (-db) or csv (-csv-dir)")
		db     = flag.String("db", filepath.Join("data", "carriers", "merged.sqlite"), "SQLite output with -format sqlite")
		csvDir = flag.String("csv-dir", filepath.Join("data", "carriers", "merged"), "CSV output directory with -format csv")
		verify = flag.Bool("verify", false, "only run sanity checks on the existing output and exit")
	)
	flag.Parse()
	out := store.Output{Format: *format, Path: *db}
	switch *format {
	case "sqlite":
	case "csv":
		out.Path = *csvDir
	default:
		fail(fmt.Errorf("-format must be sqlite or csv, not %q", *format))
	}
	if *verify {
		checks, err := store.Verify(out, merge.Schema, merge.Specs(), merge.Checks)
		if err != nil {
			fail(err)
		}
		if !store.PrintChecks(os.Stdout, out.Path, checks) {
			os.Exit(1)
		}
		return
	}

	rep := carriers.NewConsole(os.Stdout)
	fmt.Printf("carriers-merge: %s/*/assignments.csv -> %s (%s)\n", *in, out.Path, out.Format)
	rep.Stage(1, 3, "load")
	input, err := merge.Load(*in)
	if err != nil {
		fail(err)
	}
	perSource := map[string]int{}
	for _, a := range input.Assignments {
		perSource[a.Source]++
	}
	rep.Info("%d files: %s", len(input.Files), strings.Join(input.Files, ", "))
	rep.Info("%d assignment rows %v, %d sub-allocations", len(input.Assignments), perSource, len(input.SubAllocations))

	rep.Stage(2, 3, "merge")
	res := merge.Merge(input)
	auth := map[string][]string{}
	for _, m := range res.MCCs {
		if m.Authority != merge.ITU {
			auth[m.Authority] = append(auth[m.Authority], m.MCC)
		}
	}
	for _, s := range sortedKeys(auth) {
		rep.Info("authoritative: %s for MCC %s", s, strings.Join(auth[s], ","))
	}
	rep.Info("%d MCCs, %d current rows", len(res.MCCs), len(res.Current))
	kinds := map[string]map[string]int{}
	for _, c := range res.Conflicts {
		if kinds[c.Source] == nil {
			kinds[c.Source] = map[string]int{}
		}
		kinds[c.Source][c.Kind]++
	}
	for _, s := range sortedKeys(kinds) {
		rep.Info("conflicts with ITU, %s: %v", s, kinds[s])
	}

	rep.Stage(3, 3, "write "+out.Path)
	meta := map[string]string{"tool": "carriers-merge", "written_at": time.Now().UTC().Format(time.RFC3339),
		"inputs": strings.Join(input.Files, ",")}
	for s, n := range perSource {
		meta["rows_"+s] = fmt.Sprint(n)
	}
	tbs := merge.Tables(res, meta)
	if err := out.Write(merge.Schema, tbs); err != nil {
		fail(err)
	}
	var counts []string
	for _, tb := range tbs {
		counts = append(counts, fmt.Sprintf("%d %s", len(tb.Rows), tb.Name))
	}
	rep.Info("%s  (%s)", strings.Join(counts, ", "), carriers.FmtBytes(out.Size()))
	checks, err := store.Verify(out, merge.Schema, merge.Specs(), merge.Checks)
	if err != nil {
		fail(err)
	}
	if !store.PrintChecks(os.Stdout, out.Path, checks) {
		os.Exit(1)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "carriers-merge: %v\n", err)
	os.Exit(1)
}
