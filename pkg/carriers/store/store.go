// Package store writes downloader output as a SQLite database or a directory
// of CSV files (one per table) from the same table definitions, and verifies
// either.
package store

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/accretional/proto-telephony/pkg/carriers"
	_ "modernc.org/sqlite" // pure-Go SQLite driver ("sqlite")
)

// Table is one output table: its columns (nullable ones are written as NULL
// in SQLite and empty in CSV) and rows. A source package describes its output
// as a schema (CREATE statements) plus tables, and both writers use them.
type Table struct {
	Name     string
	Cols     []string
	Nullable map[string]bool
	Replace  bool // INSERT OR REPLACE (for sources that repeat rows)
	Rows     [][]any
}

// Nullables builds a Table.Nullable set.
func Nullables(cols ...string) map[string]bool {
	m := map[string]bool{}
	for _, c := range cols {
		m[c] = true
	}
	return m
}

// Add appends a row.
func (t *Table) Add(vals ...any) { t.Rows = append(t.Rows, vals) }

// Null maps zero values to NULL.
func Null[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

// MetaTable is the conventional key/value table, sorted by key.
func MetaTable(meta map[string]string) Table {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t := Table{Name: "meta", Cols: []string{"key", "value"}}
	for _, k := range keys {
		t.Add(k, meta[k])
	}
	return t
}

// MetaDDL creates the meta table.
const MetaDDL = `CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// SourcesDDL creates the sources table.
const SourcesDDL = `-- Every document the output was built from.
CREATE TABLE sources (
  kind       TEXT NOT NULL,
  name       TEXT NOT NULL,
  url        TEXT NOT NULL,
  bytes      INTEGER NOT NULL,
  sha256     TEXT,
  from_cache INTEGER NOT NULL
);
`

// SourcesTable lists the documents used.
func SourcesTable(srcs []carriers.Source) Table {
	t := Table{Name: "sources", Cols: []string{"kind", "name", "url", "bytes", "sha256", "from_cache"}, Nullable: Nullables("sha256")}
	for _, s := range srcs {
		t.Add(s.Kind, s.Name, s.URL, s.Bytes, Null(s.SHA256), s.FromCache)
	}
	return t
}

// Output is where and how to write.
type Output struct {
	Format string // "sqlite" or "csv"
	Path   string // the .sqlite file, or the CSV directory
}

// Write writes tables in o's format.
func (o Output) Write(ddl string, tbs []Table) error {
	if err := os.MkdirAll(filepath.Dir(o.Path), 0o755); err != nil {
		return err
	}
	switch o.Format {
	case "sqlite":
		return WriteSQLite(o.Path, ddl, tbs)
	case "csv":
		return WriteCSV(o.Path, tbs)
	}
	return fmt.Errorf("unknown format %q (want sqlite or csv)", o.Format)
}

// Size is the output's size on disk.
func (o Output) Size() int64 {
	var n int64
	filepath.WalkDir(o.Path, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// WriteSQLite writes a fresh database at path. It builds path+".tmp" and
// renames it over path, so readers never see a half-written file and a
// failed run leaves the previous database in place.
func WriteSQLite(path, ddl string, tbs []Table) error {
	tmp := path + ".tmp"
	for _, p := range []string{tmp, tmp + "-journal", tmp + "-wal", tmp + "-shm"} {
		_ = os.Remove(p)
	}
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return err
	}
	if err := load(db, ddl, tbs); err != nil {
		db.Close()
		os.Remove(tmp)
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// load creates the schema in db and inserts the tables.
func load(db *sql.DB, ddl string, tbs []Table) error {
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tb := range tbs {
		verb := "INSERT"
		if tb.Replace {
			verb = "INSERT OR REPLACE"
		}
		q := fmt.Sprintf("%s INTO %s (%s) VALUES (?%s)", verb, tb.Name, strings.Join(tb.Cols, ","),
			strings.Repeat(",?", len(tb.Cols)-1))
		st, err := tx.Prepare(q)
		if err != nil {
			return fmt.Errorf("%s: %w", tb.Name, err)
		}
		for i, r := range tb.Rows {
			if _, err := st.Exec(r...); err != nil {
				st.Close()
				return fmt.Errorf("%s: row %d: %w", tb.Name, i+1, err)
			}
		}
		st.Close()
	}
	return tx.Commit()
}

// WriteCSV writes one CSV file per table (with a header row) into dir. NULLs
// become empty fields. The files are built in dir+".tmp" and swapped in, so
// a failed run leaves the previous output in place.
func WriteCSV(dir string, tbs []Table) error {
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	for _, tb := range tbs {
		if err := writeCSVFile(filepath.Join(tmp, tb.Name+".csv"), tb); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	old := dir + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

func writeCSVFile(path string, tb Table) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write(tb.Cols)
	rec := make([]string, len(tb.Cols))
	for _, r := range tb.Rows {
		for i, v := range r {
			rec[i] = csvField(v)
		}
		w.Write(rec)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", path, err)
	}
	return f.Close()
}

func csvField(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "1"
		}
		return "0"
	}
	return fmt.Sprint(v)
}

// ReadCSV loads a directory written by WriteCSV into an in-memory SQLite
// database with schema ddl. specs gives each table's name, columns and
// nullable set (rows are ignored); empty fields in nullable columns become
// NULL.
func ReadCSV(dir, ddl string, specs []Table) (*sql.DB, error) {
	var tbs []Table
	for _, spec := range specs {
		spec.Rows = nil
		f, err := os.Open(filepath.Join(dir, spec.Name+".csv"))
		if err != nil {
			return nil, err
		}
		recs, err := csv.NewReader(f).ReadAll()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s.csv: %w", spec.Name, err)
		}
		if len(recs) == 0 || strings.Join(recs[0], ",") != strings.Join(spec.Cols, ",") {
			return nil, fmt.Errorf("%s.csv: header doesn't match columns %v", spec.Name, spec.Cols)
		}
		for _, rec := range recs[1:] {
			row := make([]any, len(rec))
			for i, v := range rec {
				if v == "" && spec.Nullable[spec.Cols[i]] {
					row[i] = nil
				} else {
					row[i] = v
				}
			}
			spec.Rows = append(spec.Rows, row)
		}
		tbs = append(tbs, spec)
	}
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one connection = one in-memory database
	if err := load(db, ddl, tbs); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// CheckSpec is a sanity check: a query returning one value, and a test on it.
type CheckSpec struct {
	Name  string
	Query string
	OK    func(string) bool
}

// Check is a CheckSpec's result.
type Check struct {
	Name string
	OK   bool
	Got  string
}

// Verify runs checks on output written by Output.Write. A CSV directory is
// loaded into an in-memory database first, which also checks that it
// round-trips.
func Verify(o Output, ddl string, specs []Table, checks []CheckSpec) ([]Check, error) {
	if _, err := os.Stat(o.Path); err != nil {
		return nil, err
	}
	var (
		db  *sql.DB
		err error
	)
	switch o.Format {
	case "csv":
		db, err = ReadCSV(o.Path, ddl, specs)
	case "sqlite":
		db, err = sql.Open("sqlite", "file:"+o.Path+"?mode=ro")
	default:
		err = fmt.Errorf("unknown format %q", o.Format)
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var out []Check
	for _, c := range checks {
		var v sql.NullString
		err := db.QueryRow(c.Query).Scan(&v)
		got := v.String
		if err != nil {
			got = err.Error()
		}
		out = append(out, Check{Name: c.Name, OK: err == nil && c.OK(got), Got: got})
	}
	return out, nil
}

// Atoi is strconv.Atoi without the error, for checks.
func Atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// PrintChecks prints Verify results and reports whether all passed.
func PrintChecks(w io.Writer, path string, checks []Check) bool {
	fmt.Fprintf(w, "verify %s\n", path)
	ok := true
	for _, c := range checks {
		mark := "ok  "
		if !c.OK {
			mark, ok = "FAIL", false
		}
		fmt.Fprintf(w, "  %s %-26s %s\n", mark, c.Name, c.Got)
	}
	return ok
}
