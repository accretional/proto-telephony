package store

// AssignmentsDDL is the shared core table every source can emit (see
// research/schema.md): one row per MCC+MNC as that source states it, with
// the source's own values kept beside the normalized ones.
const AssignmentsDDL = `
CREATE TABLE assignments (
  source         TEXT NOT NULL,   -- ca | mx | jp | ...
  mcc            TEXT NOT NULL,
  mnc            TEXT NOT NULL,   -- as assigned (2 or 3 digits)
  plmn           TEXT NOT NULL,   -- mcc||mnc
  country        TEXT NOT NULL,   -- ISO 3166-1 alpha-2
  holder         TEXT,            -- normalized (NULL when unassigned)
  holder_raw     TEXT,            -- as published
  status         TEXT NOT NULL,   -- assigned | reserved | returned | reclaimed | withdrawn | available | test
  status_raw     TEXT,
  assigned_date  TEXT,            -- YYYY-MM-DD
  status_date    TEXT,
  reference      TEXT,            -- legal/document reference
  as_of          TEXT NOT NULL,   -- the source's "position on" date, else fetch date
  url            TEXT,
  PRIMARY KEY (source, plmn, country)
);
`

// Assignment is one row of the shared core table.
type Assignment struct {
	Source, MCC, MNC, Country string
	Holder, HolderRaw         string
	Status, StatusRaw         string
	AssignedDate, StatusDate  string
	Reference, AsOf, URL      string
}

// Statuses are the normalized status values.
var Statuses = []string{"assigned", "reserved", "returned", "reclaimed", "withdrawn", "available", "test"}

// AssignmentsTable lays out the core rows.
func AssignmentsTable(rows []Assignment) Table {
	t := Table{Name: "assignments",
		Cols: []string{"source", "mcc", "mnc", "plmn", "country", "holder", "holder_raw", "status", "status_raw",
			"assigned_date", "status_date", "reference", "as_of", "url"},
		Nullable: Nullables("holder", "holder_raw", "status_raw", "assigned_date", "status_date", "reference", "url")}
	for _, a := range rows {
		t.Add(a.Source, a.MCC, a.MNC, a.MCC+a.MNC, a.Country, Null(a.Holder), Null(a.HolderRaw), a.Status,
			Null(a.StatusRaw), Null(a.AssignedDate), Null(a.StatusDate), Null(a.Reference), a.AsOf, Null(a.URL))
	}
	return t
}

// AssignmentChecks are sanity checks on the core table, for any source.
var AssignmentChecks = []CheckSpec{
	{Name: "statuses are normalized", Query: `SELECT count(*) FROM assignments WHERE status NOT IN ('assigned','reserved','returned','reclaimed','withdrawn','available','test')`,
		OK: func(s string) bool { return s == "0" }},
	{Name: "assigned rows have holders", Query: `SELECT count(*) FROM assignments WHERE status='assigned' AND coalesce(holder,'')=''`,
		OK: func(s string) bool { return s == "0" }},
	{Name: "codes are digits", Query: `SELECT count(*) FROM assignments WHERE mcc GLOB '*[^0-9]*' OR mnc GLOB '*[^0-9]*' OR length(mcc)<>3 OR length(mnc) NOT IN (2,3)`,
		OK: func(s string) bool { return s == "0" }},
}

// SubAllocationsDDL is the shared table of blocks below an MNC: US CBRS
// IBNs, extraterritorial MSIN ranges, campus-network blocks.
const SubAllocationsDDL = `
CREATE TABLE sub_allocations (
  source   TEXT NOT NULL,
  plmn     TEXT NOT NULL,       -- the parent MCC+MNC
  kind     TEXT NOT NULL,       -- ibn | msin_range | campus_block
  block    TEXT NOT NULL,
  holder   TEXT,
  status   TEXT NOT NULL,       -- same vocabulary as assignments
  country  TEXT,               -- where the block is used, if not the parent's country
  as_of    TEXT NOT NULL,
  PRIMARY KEY (source, plmn, kind, block, country)
);
`

// SubAllocation is one row of the shared sub-allocation table.
type SubAllocation struct {
	Source, PLMN, Kind, Block string
	Holder, Status, Country   string
	AsOf                      string
}

// SubAllocationsTable lays out the sub-allocation rows.
func SubAllocationsTable(rows []SubAllocation) Table {
	t := Table{Name: "sub_allocations", Cols: []string{"source", "plmn", "kind", "block", "holder", "status", "country", "as_of"},
		Nullable: Nullables("holder", "country")}
	for _, r := range rows {
		t.Add(r.Source, r.PLMN, r.Kind, r.Block, Null(r.Holder), r.Status, Null(r.Country), r.AsOf)
	}
	return t
}
