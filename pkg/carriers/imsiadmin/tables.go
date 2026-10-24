package imsiadmin

import (
	"sort"
	"strings"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// Schema is the IMSI Admin output's DDL.
const Schema = store.MetaDDL + store.SourcesDDL + store.AssignmentsDDL + store.SubAllocationsDDL + `
-- US HNI (MCC+MNC) assignments: the directory row, its parsed status, and
-- the detail page's fields (dates as YYYY-MM-DD; raw text in details).
CREATE TABLE hni (
  hni                    TEXT PRIMARY KEY, -- 310-170
  mcc                    TEXT NOT NULL,
  mnc                    TEXT NOT NULL,
  plmn                   TEXT NOT NULL,    -- 310170
  entity                 TEXT NOT NULL,    -- directory text, as published
  status                 TEXT NOT NULL,    -- assigned | returned | reclaimed | reserved | test | available | unavailable
  status_date            TEXT,             -- date in a returned/reclaimed note
  related                TEXT,             -- code referenced in parentheses
  entity_name            TEXT,
  entity_type            TEXT,             -- as published
  entity_category        TEXT,             -- public_network_operator | service_profile_provider | service_provider (NULL: free text)
  protocol               TEXT,             -- as published; normalized in protocols
  wireless_license       TEXT,             -- as published; identifiers extracted to identifiers
  license_date           TEXT,
  application_date       TEXT,
  effective_date         TEXT,
  estimated_service_date TEXT,
  deployment_date        TEXT,
  modification_date      TEXT,
  return_date            TEXT,
  title                  TEXT,             -- detail page heading
  url                    TEXT
);
CREATE INDEX hni_plmn ON hni(plmn);

-- IMSI Block Numbers under the shared CBRS HNI 315-010.
CREATE TABLE cbrs (
  code                   TEXT PRIMARY KEY, -- 315-010-0152
  hni                    TEXT NOT NULL,
  ibn                    TEXT NOT NULL,
  imsi_prefix            TEXT NOT NULL,    -- 3150100152 (MCC+MNC+IBN)
  entity                 TEXT NOT NULL,
  status                 TEXT NOT NULL,
  status_date            TEXT,
  related                TEXT,
  entity_name            TEXT,
  entity_type            TEXT,
  entity_category        TEXT,
  protocol               TEXT,
  wireless_license       TEXT,
  license_date           TEXT,
  application_date       TEXT,
  effective_date         TEXT,
  estimated_service_date TEXT,
  deployment_date        TEXT,
  modification_date      TEXT,
  return_date            TEXT,
  title                  TEXT,
  url                    TEXT
);

-- Every field of every detail page, verbatim, in page order.
CREATE TABLE details (
  kind     TEXT NOT NULL,   -- hni | cbrs
  code     TEXT NOT NULL,
  seq      INTEGER NOT NULL,
  section  TEXT NOT NULL,   -- Code Details | Entity Details
  label    TEXT NOT NULL,
  value    TEXT NOT NULL,
  date     TEXT,            -- value as YYYY-MM-DD when it is a date
  PRIMARY KEY (kind, code, seq)
);

-- Identifiers found in the free-text fields (Wireless License Number,
-- Entity Type): FCC call signs, FRNs, ULS/experimental/cellular license file
-- numbers, spectrum lease IDs, filer IDs, satellite filings, FCC documents,
-- GSMA member numbers, market areas (CMA/BTA/MTA), and unclassified numbers.
CREATE TABLE identifiers (
  kind     TEXT NOT NULL,
  code     TEXT NOT NULL,
  field    TEXT NOT NULL,   -- the label it was found under
  type     TEXT NOT NULL,   -- call_sign | hni | frn | uls_file | experimental_file | cellular_file | lease | filer_id | satellite_filing | fcc_document | gsma_member | market | number
  value    TEXT NOT NULL,
  PRIMARY KEY (kind, code, field, type, value)
);
CREATE INDEX identifiers_value ON identifiers(type, value);

-- Technologies per code, normalized from Protocol.
CREATE TABLE protocols (
  kind     TEXT NOT NULL,
  code     TEXT NOT NULL,
  protocol TEXT NOT NULL,   -- GPRS | EDGE | W-CDMA | HSPA | 1xRTT | HRPD/EVDO | LTE/LTE Advanced | 5G
  PRIMARY KEY (kind, code, protocol)
);
`

// Dataset is everything written out.
type Dataset struct {
	Meta    map[string]string
	Sources []carriers.Source
	Records []*Record
}

var codeCols = []string{"status_date", "related", "entity_name", "entity_type", "entity_category", "protocol", "wireless_license",
	"license_date", "application_date", "effective_date", "estimated_service_date", "deployment_date",
	"modification_date", "return_date", "title", "url"}

// detailVals are the shared trailing columns of hni and cbrs.
func detailVals(r *Record) []any {
	d := r.Detail
	if d == nil {
		d = &Detail{}
	}
	date := func(label string) any { return store.Null(NormDate(d.Get(label))) }
	// For returned/reclaimed codes the site replaces the entity name with
	// the status note; that's not a name.
	name := d.Get("Entity Name")
	if name == "" {
		name = r.Status.Name
	}
	switch ParseStatus(name).State {
	case "returned", "reclaimed", "available", "unavailable":
		name = ""
	}
	return []any{store.Null(r.Status.Date), store.Null(r.Status.Related), store.Null(name),
		store.Null(d.Get("Entity Type")), store.Null(EntityCategory(d.Get("Entity Type"))), store.Null(d.Get("Protocol")), store.Null(d.Get("Wireless License Number")),
		date("License Date"), date("Application Date"), date("Effective Date"), date("Estimated Service Date"),
		date("Deployment Date"), date("Modification Date"), date("Return Date"), store.Null(d.Title), store.Null(r.URL)}
}

// Tables lays ds out as the output tables, sorted by code.
func Tables(ds *Dataset) []store.Table {
	recs := append([]*Record(nil), ds.Records...)
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Kind != recs[j].Kind {
			return recs[i].Kind > recs[j].Kind // hni before cbrs
		}
		return recs[i].Code() < recs[j].Code()
	})
	srcs := append([]carriers.Source(nil), ds.Sources...)
	sort.SliceStable(srcs, func(i, j int) bool {
		if srcs[i].Kind != srcs[j].Kind {
			return srcs[i].Kind < srcs[j].Kind
		}
		return srcs[i].Name < srcs[j].Name
	})

	hni := store.Table{Name: "hni", Cols: append([]string{"hni", "mcc", "mnc", "plmn", "entity", "status"}, codeCols...),
		Nullable: store.Nullables(codeCols...)}
	cbrs := store.Table{Name: "cbrs", Cols: append([]string{"code", "hni", "ibn", "imsi_prefix", "entity", "status"}, codeCols...),
		Nullable: store.Nullables(codeCols...)}
	details := store.Table{Name: "details", Cols: []string{"kind", "code", "seq", "section", "label", "value", "date"},
		Nullable: store.Nullables("date")}
	idents := store.Table{Name: "identifiers", Cols: []string{"kind", "code", "field", "type", "value"}}
	protocols := store.Table{Name: "protocols", Cols: []string{"kind", "code", "protocol"}}
	for _, r := range recs {
		mcc, mnc, _ := strings.Cut(r.HNI, "-")
		switch r.Kind {
		case KindHNI:
			hni.Add(append([]any{r.HNI, mcc, mnc, mcc + mnc, r.Entity, r.Status.State}, detailVals(r)...)...)
		case KindCBRS:
			cbrs.Add(append([]any{r.Code(), r.HNI, r.IBN, mcc + mnc + r.IBN, r.Entity, r.Status.State}, detailVals(r)...)...)
		}
		if r.Detail == nil {
			continue
		}
		for i, f := range r.Detail.Fields {
			var date any
			if strings.HasSuffix(f.Label, "Date") {
				date = store.Null(NormDate(f.Value))
			}
			details.Add(string(r.Kind), r.Code(), i+1, f.Section, f.Label, f.Value, date)
		}
		for _, field := range []string{"Wireless License Number", "Entity Type"} {
			for _, id := range ExtractIdentifiers(r.Detail.Get(field), field == "Wireless License Number") {
				idents.Add(string(r.Kind), r.Code(), field, id.Type, id.Value)
			}
		}
		for _, p := range ParseProtocols(r.Detail.Get("Protocol")) {
			protocols.Add(string(r.Kind), r.Code(), p)
		}
	}
	return []store.Table{store.MetaTable(ds.Meta), store.SourcesTable(srcs),
		store.AssignmentsTable(Assignments(recs, asOf(ds))), store.SubAllocationsTable(SubAllocations(recs, asOf(ds))),
		hni, cbrs, details, idents, protocols}
}

// Specs are the output tables' shapes (no rows), for reading CSV back.
func Specs() []store.Table { return Tables(&Dataset{Meta: map[string]string{}}) }

// Checks are the sanity checks run by -verify.
var Checks = []store.CheckSpec{
	{Name: "hni codes >= 500", Query: `SELECT count(*) FROM hni`, OK: func(s string) bool { return store.Atoi(s) >= 500 }},
	{Name: "cbrs codes >= 100", Query: `SELECT count(*) FROM cbrs`, OK: func(s string) bool { return store.Atoi(s) >= 100 }},
	{Name: "310-170 is AT&T", Query: `SELECT entity_name FROM hni WHERE hni='310-170'`, OK: func(s string) bool { return strings.Contains(s, "AT&T") }},
	{Name: "MCCs are 310-316", Query: `SELECT count(*) FROM hni WHERE mcc NOT BETWEEN '310' AND '316'`, OK: func(s string) bool { return s == "0" }},
	{Name: "every code has details", Query: `SELECT (SELECT count(*) FROM hni WHERE title IS NULL) + (SELECT count(*) FROM cbrs WHERE title IS NULL)`,
		OK: func(s string) bool { return s == "0" }},
	{Name: "detail fields", Query: `SELECT count(*) FROM details`, OK: func(s string) bool { return store.Atoi(s) > 2000 }},
	{Name: "assigned hni codes", Query: `SELECT count(*) FROM hni WHERE status='assigned'`, OK: func(s string) bool { return store.Atoi(s) > 300 }},
	{Name: "call signs", Query: `SELECT count(*) FROM identifiers WHERE type='call_sign'`, OK: func(s string) bool { return store.Atoi(s) > 100 }},
	{Name: "protocols", Query: `SELECT count(*) FROM protocols`, OK: func(s string) bool { return store.Atoi(s) > 500 }},
}

// asOf is the run's date: IMSI Admin's pages are live, with no "position
// on" date of their own.
func asOf(ds *Dataset) string {
	if w := ds.Meta["written_at"]; len(w) >= 10 {
		return w[:10]
	}
	return ds.Meta["fetched_at"]
}

// coreStatus maps a directory status onto the shared vocabulary.
func coreStatus(state string) string {
	switch state {
	case "unavailable": // "Not available for assignment" (310-000)
		return "reserved"
	case "":
		return "assigned"
	}
	return state
}

func coreHolder(r *Record) string {
	switch r.Status.State {
	case "returned", "reclaimed", "available", "unavailable":
		return ""
	}
	if r.Detail != nil {
		if n := r.Detail.Get("Entity Name"); n != "" && ParseStatus(n).State == "assigned" {
			return n
		}
	}
	return r.Status.Name
}

// Assignments maps the HNIs onto the shared core table: holder is the
// entity (NULL once returned/reclaimed), assigned_date the effective date,
// status_date the return/reclaim date.
func Assignments(recs []*Record, asOf string) []store.Assignment {
	var out []store.Assignment
	for _, r := range recs {
		if r.Kind != KindHNI {
			continue
		}
		mcc, mnc, _ := strings.Cut(r.HNI, "-")
		a := store.Assignment{Source: "us", MCC: mcc, MNC: mnc, Country: "US", Holder: coreHolder(r), HolderRaw: r.Entity,
			Status: coreStatus(r.Status.State), StatusRaw: r.Entity, StatusDate: r.Status.Date, AsOf: asOf, URL: r.URL}
		if r.Detail != nil {
			a.AssignedDate = NormDate(r.Detail.Get("Effective Date"))
		}
		out = append(out, a)
	}
	return out
}

// SubAllocations maps the CBRS IBNs onto the shared sub-allocation table,
// under their parent HNI 315-010.
func SubAllocations(recs []*Record, asOf string) []store.SubAllocation {
	var out []store.SubAllocation
	for _, r := range recs {
		if r.Kind != KindCBRS {
			continue
		}
		mcc, mnc, _ := strings.Cut(r.HNI, "-")
		out = append(out, store.SubAllocation{Source: "us", PLMN: mcc + mnc, Kind: "ibn", Block: r.IBN,
			Holder: coreHolder(r), Status: coreStatus(r.Status.State), AsOf: asOf})
	}
	return out
}

func init() { Checks = append(Checks, store.AssignmentChecks...) }
