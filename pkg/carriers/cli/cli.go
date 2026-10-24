// Package cli holds the flags and run loop shared by the downloader commands
// (cmd/itu-e212, cmd/us-hni).
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// Common are the flags every downloader takes. Output defaults are derived
// from the source's name: data/carriers/<name>/ (CSV) or
// data/carriers/<name>.sqlite, caching under data/cache/<name>/.
type Common struct {
	Cmd     string // command name, for messages
	Format  string
	DB      string
	CSVDir  string
	Cache   string
	Workers int
	Offline bool
	Verify  bool
	Verbose bool
}

// Register defines the common flags on fs.
func (c *Common) Register(fs *flag.FlagSet, cmd, name string) {
	c.Cmd = cmd
	fs.StringVar(&c.Format, "format", "sqlite", "output format: sqlite (-db) or csv (-csv-dir, one file per table)")
	fs.StringVar(&c.DB, "db", filepath.Join("data", "carriers", name+".sqlite"), "SQLite database to write with -format sqlite (replaced atomically)")
	fs.StringVar(&c.CSVDir, "csv-dir", filepath.Join("data", "carriers", name), "directory of CSV files to write with -format csv (replaced atomically)")
	fs.StringVar(&c.Cache, "cache", filepath.Join("data", "cache", name), "cache for downloaded documents; empty disables")
	fs.IntVar(&c.Workers, "workers", 4, "concurrent downloads")
	fs.BoolVar(&c.Offline, "offline", false, "build only from the cache, no network")
	fs.BoolVar(&c.Verify, "verify", false, "only run sanity checks on the existing output (-db or -csv-dir, per -format) and exit (non-zero if any fail)")
	fs.BoolVar(&c.Verbose, "v", false, "print more detail")
}

// Output is where the flags say to write.
func (c *Common) Output() (store.Output, error) {
	switch c.Format {
	case "sqlite":
		return store.Output{Format: "sqlite", Path: c.DB}, nil
	case "csv":
		return store.Output{Format: "csv", Path: c.CSVDir}, nil
	}
	return store.Output{}, fmt.Errorf("-format must be sqlite or csv, not %q", c.Format)
}

// Main runs a downloader: with -verify it checks the existing output,
// otherwise it calls run with a context cancelled on SIGINT/SIGTERM. It
// exits non-zero on failure.
func (c *Common) Main(ddl string, specs []store.Table, checks []store.CheckSpec,
	run func(ctx context.Context, out store.Output) error) {
	out, err := c.Output()
	if err != nil {
		c.fail(err, 2)
	}
	if c.Verify {
		res, err := store.Verify(out, ddl, specs, checks)
		if err != nil {
			c.fail(fmt.Errorf("verify %s: %w", out.Path, err), 1)
		}
		if !store.PrintChecks(os.Stdout, out.Path, res) {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, out); err != nil {
		c.fail(err, 1)
	}
}

func (c *Common) fail(err error, code int) {
	fmt.Fprintf(os.Stderr, "\n%s: %v\n", c.Cmd, err)
	os.Exit(code)
}

// OrNone prints "" as "(none)".
func OrNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
