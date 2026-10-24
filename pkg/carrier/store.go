package carrier

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver ("sqlite")
)

const schema = `
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Every document the database was built from.
CREATE TABLE sources (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL,          -- e212b | ob | shared901
  name       TEXT NOT NULL,          -- T-SP-E.212B-2023, T-SP-OB.1305-2024, e212_901
  url        TEXT NOT NULL,
  bytes      INTEGER NOT NULL,
  sha256     TEXT,
  from_cache INTEGER NOT NULL
);

-- The current MNC list: base list with all amendments replayed.
CREATE TABLE networks (
  mcc               TEXT NOT NULL,
  mnc               TEXT NOT NULL,
  plmn              TEXT NOT NULL,   -- mcc||mnc, e.g. 310170
  country           TEXT NOT NULL,   -- country/area or shared-code group
  operator          TEXT NOT NULL,
  kind              TEXT NOT NULL,   -- geographic | shared | roio
  source_ob         INTEGER NOT NULL,-- OB of the record's latest version
  added_amendment   INTEGER,         -- NULL = in the base list
  changed_amendment INTEGER,         -- last LIR amendment, NULL = none
  PRIMARY KEY (mcc, mnc, country)
);
CREATE INDEX networks_plmn ON networks(plmn);
CREATE INDEX networks_operator ON networks(operator);

-- Countries/areas per MCC, derived from networks.
CREATE VIEW mcc AS
  SELECT mcc, country, kind, COUNT(*) AS networks
  FROM networks GROUP BY mcc, country, kind;

-- Every amendment line, in order, with what replaying it did.
CREATE TABLE amendments (
  amendment  INTEGER NOT NULL,
  ob         INTEGER NOT NULL,
  seq        INTEGER NOT NULL,
  country    TEXT NOT NULL,
  action     TEXT NOT NULL,          -- ADD | SUP | LIR
  mcc        TEXT NOT NULL,
  mnc        TEXT NOT NULL,
  operator   TEXT NOT NULL,
  msin_range TEXT,                   -- set for extraterritorial changes
  outcome    TEXT NOT NULL,          -- applied | replaced | no_match | ambiguous
  before     TEXT,                   -- operator (or MSIN range) before the change
  PRIMARY KEY (amendment, seq)
);

-- MCC+MNCs used outside their home country (base list table).
CREATE TABLE extraterritorial (
  country    TEXT NOT NULL,
  mcc        TEXT NOT NULL,
  mnc        TEXT NOT NULL,
  msin_range TEXT NOT NULL,
  operator   TEXT NOT NULL,
  amendment  INTEGER                 -- NULL = in the base list
);

-- ITU's live MCC 901 database (includes reserved / reclaimed codes).
CREATE TABLE shared_codes (
  mcc               TEXT NOT NULL,
  mnc               TEXT NOT NULL,
  assignee          TEXT NOT NULL,
  status            TEXT NOT NULL,
  reservation_start TEXT,
  reservation_end   TEXT,
  assigned          TEXT,
  reclaimed         TEXT,
  PRIMARY KEY (mcc, mnc, status, assignee)
);
`

// Dataset is everything written to the database.
type Dataset struct {
	Meta             map[string]string
	Sources          []Source
	Networks         []Network
	Amendments       []Applied
	ExtraTerritorial []ExtraTerritorial
	SharedCodes      []SharedCode
}

// WriteSQLite writes ds to a fresh database at path. It builds path+".tmp"
// and renames it over path, so readers never see a half-written file and a
// failed run leaves the previous database in place.
func WriteSQLite(path string, ds *Dataset) error {
	tmp := path + ".tmp"
	for _, p := range []string{tmp, tmp + "-journal", tmp + "-wal", tmp + "-shm"} {
		_ = os.Remove(p)
	}
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return err
	}
	if err := write(db, ds); err != nil {
		db.Close()
		os.Remove(tmp)
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// table is one output table: its columns (nullable ones can be written as
// NULL / empty) and rows. Both the SQLite and CSV writers use these.
type table struct {
	name     string
	cols     []string
	nullable map[string]bool
	replace  bool // INSERT OR REPLACE (the 901 database repeats some rows)
	rows     [][]any
}

func nullables(cols ...string) map[string]bool {
	m := map[string]bool{}
	for _, c := range cols {
		m[c] = true
	}
	return m
}

// tables lays ds out as the output tables, in a stable order.
func tables(ds *Dataset) []table {
	keys := make([]string, 0, len(ds.Meta))
	for k := range ds.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	meta := table{name: "meta", cols: []string{"key", "value"}}
	for _, k := range keys {
		meta.rows = append(meta.rows, []any{k, ds.Meta[k]})
	}
	sources := table{name: "sources", cols: []string{"kind", "name", "url", "bytes", "sha256", "from_cache"}, nullable: nullables("sha256")}
	for _, s := range ds.Sources {
		sources.rows = append(sources.rows, []any{s.Kind, s.Name, s.URL, s.Bytes, nullStr(s.SHA256), s.FromCache})
	}
	networks := table{name: "networks", cols: []string{"mcc", "mnc", "plmn", "country", "operator", "kind", "source_ob",
		"added_amendment", "changed_amendment"}, nullable: nullables("added_amendment", "changed_amendment")}
	for _, n := range ds.Networks {
		networks.rows = append(networks.rows, []any{n.MCC, n.MNC, n.PLMN(), n.Country, n.Operator, string(n.Kind), n.SourceOB,
			nullInt(n.AddedAmendment), nullInt(n.ChangedAmendment)})
	}
	amendments := table{name: "amendments", cols: []string{"amendment", "ob", "seq", "country", "action", "mcc", "mnc", "operator",
		"msin_range", "outcome", "before"}, nullable: nullables("msin_range", "before")}
	for _, a := range ds.Amendments {
		amendments.rows = append(amendments.rows, []any{a.Amendment, a.OB, a.Seq, a.Country, string(a.Action), a.MCC, a.MNC, a.Operator,
			nullStr(a.MSINRange), string(a.Outcome), nullStr(a.Before)})
	}
	et := table{name: "extraterritorial", cols: []string{"country", "mcc", "mnc", "msin_range", "operator", "amendment"},
		nullable: nullables("amendment")}
	for _, e := range ds.ExtraTerritorial {
		et.rows = append(et.rows, []any{e.Country, e.MCC, e.MNC, e.MSINRange, e.Operator, nullInt(e.Amendment)})
	}
	shared := table{name: "shared_codes", cols: []string{"mcc", "mnc", "assignee", "status", "reservation_start",
		"reservation_end", "assigned", "reclaimed"}, replace: true,
		nullable: nullables("reservation_start", "reservation_end", "assigned", "reclaimed")}
	for _, c := range ds.SharedCodes {
		shared.rows = append(shared.rows, []any{c.MCC, c.MNC, c.Assignee, c.Status, nullStr(c.ReservationStart),
			nullStr(c.ReservationEnd), nullStr(c.Assigned), nullStr(c.Reclaimed)})
	}
	return []table{meta, sources, networks, amendments, et, shared}
}

func write(db *sql.DB, ds *Dataset) error {
	ds.Meta["written_at"] = time.Now().UTC().Format(time.RFC3339)
	return load(db, tables(ds))
}

// load creates the schema in db and inserts the tables.
func load(db *sql.DB, tbs []table) error {
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tb := range tbs {
		verb := "INSERT"
		if tb.replace {
			verb = "INSERT OR REPLACE"
		}
		q := fmt.Sprintf("%s INTO %s (%s) VALUES (?%s)", verb, tb.name, strings.Join(tb.cols, ","),
			strings.Repeat(",?", len(tb.cols)-1))
		st, err := tx.Prepare(q)
		if err != nil {
			return fmt.Errorf("%s: %w", tb.name, err)
		}
		for i, r := range tb.rows {
			if _, err := st.Exec(r...); err != nil {
				st.Close()
				return fmt.Errorf("%s: row %d: %w", tb.name, i+1, err)
			}
		}
		st.Close()
	}
	return tx.Commit()
}

// Check is one sanity check on a written database.
type Check struct {
	Name string
	OK   bool
	Got  string
}

// Verify runs sanity checks on a database written by WriteSQLite. The
// path may also be a directory written by WriteCSV; it is loaded into an
// in-memory database first, which also checks that the CSV round-trips.
func Verify(path string) ([]Check, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var db *sql.DB
	if fi.IsDir() {
		db, err = ReadCSV(path)
	} else {
		db, err = sql.Open("sqlite", "file:"+path+"?mode=ro")
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var out []Check
	for _, c := range []struct {
		name, query string
		ok          func(string) bool
	}{
		{"networks >= 2000", `SELECT count(*) FROM networks`, func(s string) bool { return atoi(s) >= 2000 }},
		{"countries >= 200", `SELECT count(DISTINCT country) FROM networks WHERE kind='geographic'`, func(s string) bool { return atoi(s) >= 200 }},
		{"310 170 is AT&T", `SELECT operator FROM networks WHERE plmn='310170'`, func(s string) bool { return strings.Contains(s, "AT&T") }},
		{"no empty operators", `SELECT count(*) FROM networks WHERE operator='' OR country=''`, func(s string) bool { return s == "0" }},
		{"amendments replayed", `SELECT count(*) FROM amendments`, func(s string) bool { return atoi(s) > 0 }},
		{"no unmatched amendments", `SELECT count(*) FROM amendments WHERE outcome IN ('no_match','ambiguous')`, func(s string) bool { return s == "0" }},
		{"shared codes (901 db)", `SELECT coalesce((SELECT value FROM meta WHERE key='shared901'), count(*)) FROM shared_codes`,
			func(s string) bool { return s == "skipped" || atoi(s) > 0 }},
		{"base edition recorded", `SELECT value FROM meta WHERE key='base_edition'`, func(s string) bool { return strings.HasPrefix(s, "T-SP-E.212B-") }},
		{"last amendment", `SELECT value || ' (OB ' || (SELECT value FROM meta WHERE key='last_amendment_ob') || ')' FROM meta WHERE key='last_amendment'`, func(s string) bool { return s != "" }},
	} {
		var v sql.NullString
		err := db.QueryRow(c.query).Scan(&v)
		got := v.String
		if err != nil {
			got = err.Error()
		}
		out = append(out, Check{Name: c.name, OK: err == nil && c.ok(got), Got: got})
	}
	return out, nil
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
