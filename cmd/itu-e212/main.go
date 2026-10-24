// Command itu-e212 downloads the official ITU-T E.212 Mobile Network Code
// list — the E.212B base list, every amendment published in the ITU
// Operational Bulletins since, and ITU's live MCC 901 shared-code database —
// and writes the result to a SQLite database or a directory of CSV files.
//
//	itu-e212                         # -> data/carriers.sqlite
//	itu-e212 -format csv             # -> data/carriers/*.csv
//	itu-e212 -db /tmp/c.sqlite -workers 8
//	itu-e212 -offline                # rebuild from data/cache only
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/accretional/proto-telephony/pkg/carrier"
)

func main() {
	var (
		format   = flag.String("format", "sqlite", "output format: sqlite (-db) or csv (-csv-dir, one file per table)")
		dbPath   = flag.String("db", "data/carriers.sqlite", "SQLite database to write with -format sqlite (replaced atomically)")
		csvDir   = flag.String("csv-dir", "data/carriers", "directory of CSV files to write with -format csv (replaced atomically)")
		cacheDir = flag.String("cache", "data/cache", "cache for downloaded ITU documents (published bulletins never change); empty disables")
		workers  = flag.Int("workers", 4, "concurrent bulletin downloads")
		offline  = flag.Bool("offline", false, "build only from the cache, no network")
		skip901  = flag.Bool("skip-901", false, "skip ITU's live MCC 901 database")
		fromOB   = flag.Int("from-ob", 0, "first Operational Bulletin to scan (default: the base list's OB + 1)")
		toOB     = flag.Int("to-ob", 0, "last Operational Bulletin to scan (default: latest published)")
		verbose  = flag.Bool("v", false, "print every amendment change")
		verify   = flag.Bool("verify", false, "only run sanity checks on the existing output (-db or -csv-dir, per -format) and exit (non-zero if any fail)")
		dump     = flag.String("dump-docx", "", "print a .docx's text tokens (table/row/cell) and exit; for debugging the parsers")
	)
	flag.Parse()

	var out string
	switch *format {
	case "sqlite":
		out = *dbPath
	case "csv":
		out = *csvDir
	default:
		fmt.Fprintf(os.Stderr, "itu-e212: -format must be sqlite or csv, not %q\n", *format)
		os.Exit(2)
	}

	if *dump != "" {
		if err := dumpDocx(*dump); err != nil {
			fmt.Fprintf(os.Stderr, "itu-e212: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *verify {
		if !runVerify(out) {
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *format, out, *cacheDir, *workers, *offline, *skip901, *fromOB, *toOB, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "\nitu-e212: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, format, out, cacheDir string, workers int, offline, skip901 bool, fromOB, toOB int, verbose bool) error {
	start := time.Now()
	c := carrier.NewClient(cacheDir)
	c.Offline = offline
	rep := &reporter{w: os.Stdout, start: start, verbose: verbose}

	fmt.Printf("itu-e212: ITU-T E.212 MNC list -> %s (%s)\n", out, format)
	fmt.Printf("  source %s  cache %s  workers %d%s\n", carrier.BaseURL, orNone(cacheDir), workers, map[bool]string{true: "  OFFLINE"}[offline])

	res, err := carrier.Build(ctx, c, carrier.Options{Workers: workers, FromOB: fromOB, ToOB: toOB, Skip901: skip901}, rep)
	if err != nil {
		return err
	}

	rep.Stage(6, 6, "write "+out)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	t := time.Now()
	res.Meta["tool"] = "itu-e212"
	res.Meta["elapsed"] = time.Since(start).Round(time.Millisecond).String()
	write := carrier.WriteSQLite
	if format == "csv" {
		write = carrier.WriteCSV
	}
	if err := write(out, &res.Dataset); err != nil {
		return fmt.Errorf("writing %s: %w", out, err)
	}
	rep.Info("%d networks, %d amendment rows, %d extraterritorial, %d shared codes, %d sources  (%s, %s)",
		len(res.Networks), len(res.Amendments), len(res.ExtraTerritorial), len(res.SharedCodes), len(res.Sources),
		carrier.FmtBytes(diskSize(out)), time.Since(t).Round(time.Millisecond))

	summary(res, c, rep, time.Since(start))
	return nil
}

// diskSize is a file's size, or the total size of a directory's files.
func diskSize(path string) int64 {
	var n int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

func runVerify(out string) bool {
	checks, err := carrier.Verify(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "itu-e212: verify %s: %v\n", out, err)
		return false
	}
	fmt.Printf("verify %s\n", out)
	ok := true
	for _, c := range checks {
		mark := "ok  "
		if !c.OK {
			mark, ok = "FAIL", false
		}
		fmt.Printf("  %s %-26s %s\n", mark, c.Name, c.Got)
	}
	return ok
}

func summary(res *carrier.Result, c *carrier.Client, rep *reporter, elapsed time.Duration) {
	countries := map[string]bool{}
	kinds := map[carrier.Kind]int{}
	mccs := map[string]bool{}
	for _, n := range res.Networks {
		countries[n.Country] = true
		kinds[n.Kind]++
		mccs[n.MCC] = true
	}
	s := &c.Stats
	fmt.Println()
	fmt.Println("== summary")
	fmt.Printf("  base list      %s (annex to OB %d, position on %s)\n", res.Edition, res.Base.OB, res.Base.Position)
	fmt.Printf("  bulletins      %d scanned (OB %d..%d), %d missing\n", res.Bulletins, res.FirstOB, res.LastOB, len(res.Missing))
	fmt.Printf("  amendments     %d changes: %d applied, %d replaced, %d no_match, %d ambiguous\n", len(res.Amendments),
		res.Outcomes[carrier.OutcomeApplied], res.Outcomes[carrier.OutcomeReplaced],
		res.Outcomes[carrier.OutcomeNoMatch], res.Outcomes[carrier.OutcomeAmbiguous])
	fmt.Printf("  networks       %d (%d geographic, %d shared 901, %d ROIO 902) across %d MCCs, %d countries/groups\n",
		len(res.Networks), kinds[carrier.KindGeographic], kinds[carrier.KindShared], kinds[carrier.KindROIO], len(mccs), len(countries))
	fmt.Printf("  http           %d requests, %s downloaded, %d retries, %d not found\n",
		s.Requests.Load(), carrier.FmtBytes(s.Bytes.Load()), s.Retries.Load(), s.NotFound.Load())
	fmt.Printf("  cache          %d hits, %s\n", s.CacheHits.Load(), carrier.FmtBytes(s.CacheBytes.Load()))
	fmt.Printf("  warnings       %d\n", rep.warnings)
	var parts []string
	names := []string{"base", "discover", "bulletins", "replay", "901"}
	for i, d := range res.StageTimes {
		if i < len(names) {
			parts = append(parts, fmt.Sprintf("%s %s", names[i], d.Round(time.Millisecond)))
		}
	}
	fmt.Printf("  time           %s total (%s)\n", elapsed.Round(time.Millisecond), strings.Join(parts, ", "))
}

func dumpDocx(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc, err := carrier.ReadDocx(data)
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

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// reporter prints progress to w.
type reporter struct {
	mu       sync.Mutex
	w        io.Writer
	start    time.Time
	verbose  bool
	warnings int
	bytes    int64
	changes  int
}

func (r *reporter) ts() string {
	return fmt.Sprintf("[%6.1fs]", time.Since(r.start).Seconds())
}

func (r *reporter) Stage(n, of int, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.w, "\n%s ==> [%d/%d] %s\n", r.ts(), n, of, name)
}

func (r *reporter) Info(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.w, "%s     %s\n", r.ts(), fmt.Sprintf(format, args...))
}

func (r *reporter) Warn(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings++
	fmt.Fprintf(r.w, "%s   ! %s\n", r.ts(), fmt.Sprintf(format, args...))
}

func (r *reporter) Bulletin(done, total int, src carrier.Source, a *carrier.Amendment, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bytes += src.Bytes
	pct := 100 * done / max(total, 1)
	head := fmt.Sprintf("%s     [%3d/%d %3d%%]", r.ts(), done, total, pct)
	if err != nil {
		fmt.Fprintf(r.w, "%s %v\n", head, err)
		return
	}
	what := "no MNC amendment"
	if a != nil {
		r.changes += len(a.Changes)
		counts := map[carrier.Action]int{}
		for _, ch := range a.Changes {
			counts[ch.Action]++
		}
		var cs []string
		for _, act := range []carrier.Action{carrier.ActionAdd, carrier.ActionSup, carrier.ActionLIR} {
			if counts[act] > 0 {
				cs = append(cs, fmt.Sprintf("%d %s", counts[act], act))
			}
		}
		what = fmt.Sprintf("amendment %d: %d changes (%s)", a.No, len(a.Changes), strings.Join(cs, ", "))
	}
	tag := "cache"
	if !src.FromCache {
		tag = src.Elapsed.Round(time.Millisecond).String()
	}
	fmt.Fprintf(r.w, "%s %-18s %7s %7s  %s  | total %s, %d changes\n", head, src.Name, carrier.FmtBytes(src.Bytes), tag,
		what, carrier.FmtBytes(r.bytes), r.changes)
	if r.verbose && a != nil {
		for _, ch := range a.Changes {
			fmt.Fprintf(r.w, "                 %s %-30s %s %s  %s\n", ch.Action, ch.Country, ch.MCC, ch.MNC, ch.Operator)
		}
	}
}
