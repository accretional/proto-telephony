package merge

import (
	"strings"

	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// Schema is the merged output's DDL.
const Schema = store.MetaDDL + store.AssignmentsDDL + store.SubAllocationsDDL + `
-- MCC -> country and the source treated as authoritative for it.
CREATE TABLE mccs (
  mcc       TEXT PRIMARY KEY,
  country   TEXT NOT NULL,     -- ISO 3166-1 alpha-2 with the most codes ('ZZ' = international)
  countries TEXT NOT NULL,     -- every country with codes under it, comma-separated
  authority TEXT NOT NULL,     -- itu | us | ca | mx | jp ...
  sources   TEXT NOT NULL,     -- sources with rows under it
  codes     INTEGER NOT NULL
);

-- The best-known current row per code: the authoritative national
-- registry's, or ITU's. Codes a national registry marks free are left out.
CREATE TABLE current (
  plmn          TEXT NOT NULL,
  mcc           TEXT NOT NULL,
  mnc           TEXT NOT NULL,
  country       TEXT NOT NULL,
  holder        TEXT,
  status        TEXT NOT NULL,
  assigned_date TEXT,
  status_date   TEXT,
  reference     TEXT,
  source        TEXT NOT NULL,
  as_of         TEXT NOT NULL,
  url           TEXT,
  itu_holder    TEXT,          -- ITU's row for comparison
  itu_status    TEXT,
  flag          TEXT,          -- itu_only | not_in_itu | itu_stale
  PRIMARY KEY (plmn, country, source)
);

-- Where ITU and a national registry disagree.
CREATE TABLE conflicts (
  plmn            TEXT NOT NULL,
  mcc             TEXT NOT NULL,
  mnc             TEXT NOT NULL,
  country         TEXT,
  source          TEXT NOT NULL,   -- the national source
  kind            TEXT NOT NULL,   -- not_in_itu | itu_only | itu_stale | holder_differs
  national_status TEXT,
  national_holder TEXT,
  itu_status      TEXT,
  itu_holder      TEXT,
  note            TEXT,
  PRIMARY KEY (plmn, source, kind)
);
`

// Tables lays out the merged dataset.
func Tables(r *Result, meta map[string]string) []store.Table {
	var assigns []store.Assignment
	var subs []store.SubAllocation
	if r.Input != nil {
		assigns, subs = r.Input.Assignments, r.Input.SubAllocations
	}
	mccs := store.Table{Name: "mccs", Cols: []string{"mcc", "country", "countries", "authority", "sources", "codes"}}
	for _, m := range r.MCCs {
		mccs.Add(m.MCC, m.Country, strings.Join(m.Countries, ","), m.Authority, strings.Join(m.Sources, ","), m.Codes)
	}
	cur := store.Table{Name: "current", Cols: []string{"plmn", "mcc", "mnc", "country", "holder", "status", "assigned_date",
		"status_date", "reference", "source", "as_of", "url", "itu_holder", "itu_status", "flag"},
		Nullable: store.Nullables("holder", "assigned_date", "status_date", "reference", "url", "itu_holder", "itu_status", "flag")}
	for _, c := range r.Current {
		cur.Add(c.MCC+c.MNC, c.MCC, c.MNC, c.Country, store.Null(c.Holder), c.Status, store.Null(c.AssignedDate),
			store.Null(c.StatusDate), store.Null(c.Reference), c.Source, c.AsOf, store.Null(c.URL),
			store.Null(c.ITUHolder), store.Null(c.ITUStatus), store.Null(c.Flag))
	}
	conf := store.Table{Name: "conflicts", Cols: []string{"plmn", "mcc", "mnc", "country", "source", "kind", "national_status",
		"national_holder", "itu_status", "itu_holder", "note"},
		Nullable: store.Nullables("country", "national_status", "national_holder", "itu_status", "itu_holder", "note")}
	for _, c := range r.Conflicts {
		conf.Add(c.PLMN, c.MCC, c.MNC, store.Null(c.Country), c.Source, c.Kind, store.Null(c.NationalStatus),
			store.Null(c.NationalHolder), store.Null(c.ITUStatus), store.Null(c.ITUHolder), store.Null(c.Note))
	}
	return []store.Table{store.MetaTable(meta), store.AssignmentsTable(assigns), store.SubAllocationsTable(subs), mccs, cur, conf}
}

// Specs are the output tables' shapes, for reading CSV back.
func Specs() []store.Table { return Tables(&Result{}, map[string]string{}) }

// Checks are the sanity checks run by -verify.
var Checks = []store.CheckSpec{
	{Name: "sources merged", Query: `SELECT group_concat(source) FROM (SELECT DISTINCT source FROM assignments ORDER BY source)`,
		OK: func(s string) bool { return strings.Contains(s, "itu") }},
	{Name: "current rows >= 2000", Query: `SELECT count(*) FROM current`, OK: func(s string) bool { return store.Atoi(s) >= 2000 }},
	{Name: "every MCC is mapped", Query: `SELECT count(*) FROM (SELECT DISTINCT mcc FROM assignments) a LEFT JOIN mccs USING (mcc) WHERE mccs.mcc IS NULL`,
		OK: func(s string) bool { return s == "0" }},
	{Name: "310-170 is AT&T (from us)", Query: `SELECT holder || ' (' || source || ')' FROM current WHERE plmn='310170'`,
		OK: func(s string) bool { return strings.Contains(s, "AT&T") }},
	{Name: "no duplicate current rows", Query: `SELECT count(*) FROM (SELECT plmn, country FROM current GROUP BY plmn, country HAVING count(*) > 1)`,
		OK: func(s string) bool { return s == "0" }},
	{Name: "conflicts by kind", Query: `SELECT group_concat(kind || ':' || n) FROM (SELECT kind, count(*) n FROM conflicts GROUP BY kind ORDER BY kind)`,
		OK: func(string) bool { return true }},
}
