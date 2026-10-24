package itu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// Schema is the ITU output's DDL.
const Schema = store.MetaDDL + store.SourcesDDL + store.AssignmentsDDL + store.SubAllocationsDDL + `

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
  ob_date    TEXT,                   -- the bulletin's publication date
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

// Dataset is everything written out.
type Dataset struct {
	Meta             map[string]string
	Sources          []carriers.Source
	Networks         []Network
	Amendments       []Applied
	ExtraTerritorial []ExtraTerritorial
	SharedCodes      []SharedCode
}

// Tables lays ds out as the output tables, in a stable order.
func Tables(ds *Dataset) []store.Table {
	networks := store.Table{Name: "networks", Cols: []string{"mcc", "mnc", "plmn", "country", "operator", "kind", "source_ob",
		"added_amendment", "changed_amendment"}, Nullable: store.Nullables("added_amendment", "changed_amendment")}
	for _, n := range ds.Networks {
		networks.Add(n.MCC, n.MNC, n.PLMN(), n.Country, n.Operator, string(n.Kind), n.SourceOB,
			store.Null(n.AddedAmendment), store.Null(n.ChangedAmendment))
	}
	amendments := store.Table{Name: "amendments", Cols: []string{"amendment", "ob", "seq", "country", "action", "mcc", "mnc", "operator",
		"msin_range", "ob_date", "outcome", "before"}, Nullable: store.Nullables("msin_range", "ob_date", "before")}
	for _, a := range ds.Amendments {
		amendments.Add(a.Amendment, a.OB, a.Seq, a.Country, string(a.Action), a.MCC, a.MNC, a.Operator,
			store.Null(a.MSINRange), store.Null(a.OBDate), string(a.Outcome), store.Null(a.Before))
	}
	et := store.Table{Name: "extraterritorial", Cols: []string{"country", "mcc", "mnc", "msin_range", "operator", "amendment"},
		Nullable: store.Nullables("amendment")}
	for _, e := range ds.ExtraTerritorial {
		et.Add(e.Country, e.MCC, e.MNC, e.MSINRange, e.Operator, store.Null(e.Amendment))
	}
	shared := store.Table{Name: "shared_codes", Cols: []string{"mcc", "mnc", "assignee", "status", "reservation_start",
		"reservation_end", "assigned", "reclaimed"}, Replace: true,
		Nullable: store.Nullables("reservation_start", "reservation_end", "assigned", "reclaimed")}
	for _, c := range ds.SharedCodes {
		shared.Add(c.MCC, c.MNC, c.Assignee, c.Status, store.Null(c.ReservationStart),
			store.Null(c.ReservationEnd), store.Null(c.Assigned), store.Null(c.Reclaimed))
	}
	return []store.Table{store.MetaTable(ds.Meta), store.SourcesTable(ds.Sources),
		store.AssignmentsTable(Assignments(ds)), store.SubAllocationsTable(SubAllocations(ds)), networks, amendments, et, shared}
}

// Checks are the sanity checks run by -verify.
var Checks = []store.CheckSpec{
	{Name: "networks >= 2000", Query: `SELECT count(*) FROM networks`, OK: func(s string) bool { return store.Atoi(s) >= 2000 }},
	{Name: "countries >= 200", Query: `SELECT count(DISTINCT country) FROM networks WHERE kind='geographic'`, OK: func(s string) bool { return store.Atoi(s) >= 200 }},
	{Name: "310 170 is AT&T", Query: `SELECT operator FROM networks WHERE plmn='310170'`, OK: func(s string) bool { return strings.Contains(s, "AT&T") }},
	{Name: "no empty operators", Query: `SELECT count(*) FROM networks WHERE operator='' OR country=''`, OK: func(s string) bool { return s == "0" }},
	{Name: "amendments replayed", Query: `SELECT count(*) FROM amendments`, OK: func(s string) bool { return store.Atoi(s) > 0 }},
	{Name: "no unmatched amendments", Query: `SELECT count(*) FROM amendments WHERE outcome IN ('no_match','ambiguous')`, OK: func(s string) bool { return s == "0" }},
	{Name: "shared codes (901 db)", Query: `SELECT coalesce((SELECT value FROM meta WHERE key='shared901'), count(*)) FROM shared_codes`,
		OK: func(s string) bool { return s == "skipped" || store.Atoi(s) > 0 }},
	{Name: "base edition recorded", Query: `SELECT value FROM meta WHERE key='base_edition'`, OK: func(s string) bool { return strings.HasPrefix(s, "T-SP-E.212B-") }},
	{Name: "every country has an ISO code", Query: `SELECT count(*) FROM assignments WHERE country=''`, OK: func(s string) bool { return s == "0" }},
	{Name: "as_of is a date", Query: `SELECT count(*) FROM assignments WHERE as_of NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'`, OK: func(s string) bool { return s == "0" }},
	{Name: "last amendment", Query: `SELECT value || ' (OB ' || (SELECT value FROM meta WHERE key='last_amendment_ob') || ')' FROM meta WHERE key='last_amendment'`,
		OK: func(s string) bool { return s != "" }},
}

func init() { Checks = append(Checks, store.AssignmentChecks...) }

// Specs are the output tables' shapes (no rows), for reading CSV back.
func Specs() []store.Table { return Tables(&Dataset{Meta: map[string]string{}}) }

// Assignments maps the list onto the shared core table: every network, plus
// live-901-database codes no bulletin has listed yet (assigned) or that the
// database shows reclaimed/returned. Status is "reserved" for entries whose
// operator says so; assigned_date is the adding bulletin's date (unknown
// for the base list); as_of is the newest bulletin's date.
func Assignments(ds *Dataset) []store.Assignment {
	asOf := ds.Meta["as_of"]
	var out []store.Assignment
	listed := map[string]bool{}
	for _, n := range ds.Networks {
		status, statusDate, holder := operatorStatus(n.Operator)
		ref := fmt.Sprintf("OB %d", n.SourceOB)
		switch {
		case n.ChangedAmendment > 0:
			ref = fmt.Sprintf("OB %d, amendment %d", n.SourceOB, n.ChangedAmendment)
		case n.AddedAmendment > 0:
			ref = fmt.Sprintf("OB %d, amendment %d", n.SourceOB, n.AddedAmendment)
		}
		out = append(out, store.Assignment{Source: "itu", MCC: n.MCC, MNC: n.MNC, Country: ISO(n.Country, n.MCC),
			Holder: holder, HolderRaw: n.Operator, Status: status, StatusDate: statusDate,
			AssignedDate: n.AddedOBDate, Reference: ref, AsOf: asOf})
		listed[n.PLMN()] = true
	}
	for _, c := range ds.SharedCodes {
		if listed[c.MCC+c.MNC] {
			continue
		}
		a := store.Assignment{Source: "itu", MCC: c.MCC, MNC: c.MNC, Country: International,
			Holder: c.Assignee, HolderRaw: c.Assignee, StatusRaw: c.Status, AssignedDate: c.Assigned,
			Reference: "ITU 901 database", AsOf: ds.Meta["written_at"][:min(10, len(ds.Meta["written_at"]))]}
		switch c.Status {
		case "ASSIGNED":
			a.Status = "assigned"
		case "RECLAIMED":
			a.Status, a.StatusDate = "reclaimed", c.Reclaimed
		case "RETURNED":
			a.Status, a.StatusDate = "returned", c.Reclaimed
		default:
			a.Status = "reserved"
		}
		if a.AsOf == "" {
			a.AsOf = asOf
		}
		out = append(out, a)
		listed[c.MCC+c.MNC] = true
	}
	return out
}

var statusNoteRE = regexp.MustCompile(`(?i)^(reclaimed|returned|withdrawn)\b\s*(?:on\s+)?(\d{1,2})/(\d{1,2})/(\d{4})?`)

// operatorStatus reads the status notes ITU sometimes prints in place of an
// operator ("RESERVED FOR PUBLIC SAFETY", "TEST IMSI HNI", "Reclaimed
// 06/21/2016"): it returns the status, its date (YYYY-MM-DD, US-style
// month/day as in the US-supplied entries) and the holder ("" for notes).
func operatorStatus(op string) (status, date, holder string) {
	u := strings.ToUpper(strings.TrimSpace(op))
	switch {
	case strings.HasPrefix(u, "RESERVED"):
		return "reserved", "", op
	case u == "TEST" || strings.HasPrefix(u, "TEST IMSI"):
		return "test", "", op
	}
	if m := statusNoteRE.FindStringSubmatch(strings.TrimSpace(op)); m != nil {
		if m[4] != "" {
			mo, _ := strconv.Atoi(m[2])
			d, _ := strconv.Atoi(m[3])
			date = fmt.Sprintf("%s-%02d-%02d", m[4], mo, d)
		}
		return strings.ToLower(m[1]), date, ""
	}
	return "assigned", "", op
}

// SubAllocations maps the extraterritorial MSIN ranges onto the shared
// sub-allocation table (the parent code is the MCC+MNC, the block is the
// range, country is where that range is used).
func SubAllocations(ds *Dataset) []store.SubAllocation {
	var out []store.SubAllocation
	for _, e := range ds.ExtraTerritorial {
		out = append(out, store.SubAllocation{Source: "itu", PLMN: e.MCC + e.MNC, Kind: "msin_range", Block: e.MSINRange,
			Holder: e.Operator, Status: "assigned", Country: ISO(e.Country, e.MCC), AsOf: ds.Meta["as_of"]})
	}
	return out
}
