// Package cnac downloads Canada's MNC assignments (MCC 302) from the
// Canadian Numbering Administrator (CNAC), which publishes them as one CSV:
// MCC, MNC, Status (In Service / Assigned / Recovered/Aging), Company, Date
// Assigned, Remarks, preceded by an as-of date row. See
// research/carriers/ca/README.md.
package cnac

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// BaseURL is CNAC's site; the CSV is at CSVPath.
const (
	BaseURL = "https://cnac.ca"
	CSVPath = "/data/MNC_Codes.csv"
)

var header = []string{"MCC", "MNC", "Status", "Company", "Date Assigned", "Remarks"}

// MNC is one row of the CNAC list.
type MNC struct {
	MCC, MNC      string
	StatusRaw     string // In Service | Assigned | Recovered/Aging
	Company       string
	DateAssigned  string // YYYY-MM-DD or ""
	Remarks       string
	RecoveredFrom string // from "Recovered from X"
	Formerly      string // from "Formerly X"
	Experimental  bool   // "Experimental Purposes ..."
}

// Status maps CNAC's status onto the shared vocabulary.
func (m MNC) Status() string {
	switch strings.ToLower(m.StatusRaw) {
	case "in service", "assigned":
		return "assigned"
	case "recovered/aging", "recovered":
		return "reclaimed"
	}
	return strings.ToLower(m.StatusRaw)
}

// List is the parsed CSV.
type List struct {
	AsOf string // YYYY-MM-DD
	MNCs []MNC
}

var (
	recoveredRE = regexp.MustCompile(`(?i)\brecovered from\s+(.+?)\s*$`)
	formerlyRE  = regexp.MustCompile(`(?i)\bformerly\s+(.+?)\s*$`)
	isoDateRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Parse reads the CNAC CSV.
func Parse(data []byte) (*List, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\ufeff"))))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("cnac csv: %w", err)
	}
	if len(recs) == 0 || strings.Join(recs[0], "|") != strings.Join(header, "|") {
		return nil, fmt.Errorf("cnac csv: unexpected header %q (want %q)", firstOr(recs), header)
	}
	l := &List{}
	for i, rec := range recs[1:] {
		for len(rec) < len(header) {
			rec = append(rec, "")
		}
		for j := range rec {
			rec[j] = carriers.NormSpace(rec[j])
		}
		if rec[0] == "" && rec[1] == "" {
			// The as-of row: only the last field is set.
			if d := rec[len(rec)-1]; isoDateRE.MatchString(d) {
				l.AsOf = d
			}
			continue
		}
		if len(rec[0]) != 3 || len(rec[1]) < 2 || len(rec[1]) > 3 {
			return nil, fmt.Errorf("cnac csv row %d: bad code %q %q", i+2, rec[0], rec[1])
		}
		m := MNC{MCC: rec[0], MNC: rec[1], StatusRaw: rec[2], Company: rec[3], Remarks: rec[5]}
		if isoDateRE.MatchString(rec[4]) {
			m.DateAssigned = rec[4]
		}
		if x := recoveredRE.FindStringSubmatch(m.Remarks); x != nil {
			m.RecoveredFrom = x[1]
		}
		if x := formerlyRE.FindStringSubmatch(m.Remarks); x != nil {
			m.Formerly = x[1]
		}
		m.Experimental = strings.Contains(strings.ToLower(m.Remarks), "experimental")
		l.MNCs = append(l.MNCs, m)
	}
	if len(l.MNCs) == 0 {
		return nil, fmt.Errorf("cnac csv: no rows")
	}
	return l, nil
}

func firstOr(recs [][]string) []string {
	if len(recs) == 0 {
		return nil
	}
	return recs[0]
}

// Dataset is everything written out.
type Dataset struct {
	Meta    map[string]string
	Sources []carriers.Source
	List    *List
}

// Build downloads and parses the list.
func Build(ctx context.Context, c *carriers.Client, rep carriers.Reporter) (*Dataset, error) {
	rep.Stage(1, 2, "CNAC MNC list (CSV)")
	data, src, err := c.Fresh(ctx, "cnac", "MNC_Codes.csv", c.Base+CSVPath)
	if err != nil {
		return nil, err
	}
	src.Kind, src.Name = "csv", "MNC_Codes.csv"
	rep.Info("%s  %s  %s", src.URL, carriers.FmtBytes(src.Bytes), tag(src))
	l, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if l.AsOf == "" {
		rep.Warn("no as-of date row in the CSV; using today")
		l.AsOf = time.Now().UTC().Format("2006-01-02")
	}
	counts := map[string]int{}
	for _, m := range l.MNCs {
		counts[m.StatusRaw]++
	}
	rep.Info("%d MNCs as of %s: %v", len(l.MNCs), l.AsOf, counts)
	return &Dataset{Meta: map[string]string{"source": BaseURL + CSVPath, "as_of": l.AsOf, "mncs": fmt.Sprint(len(l.MNCs))},
		Sources: []carriers.Source{src}, List: l}, nil
}

func tag(s carriers.Source) string {
	if s.FromCache {
		return "cache"
	}
	return s.Elapsed.Round(time.Millisecond).String()
}

// Schema is the output's DDL.
const Schema = store.MetaDDL + store.SourcesDDL + store.AssignmentsDDL + `
-- CNAC's list as published, with fields parsed from Remarks.
CREATE TABLE mnc (
  mcc            TEXT NOT NULL,
  mnc            TEXT NOT NULL,
  plmn           TEXT NOT NULL,
  status         TEXT NOT NULL,   -- shared vocabulary
  status_raw     TEXT NOT NULL,   -- In Service | Assigned | Recovered/Aging
  company        TEXT,
  date_assigned  TEXT,
  remarks        TEXT,
  recovered_from TEXT,
  formerly       TEXT,
  experimental   INTEGER NOT NULL,
  PRIMARY KEY (mcc, mnc)
);
`

// Tables lays ds out as the output tables.
func Tables(ds *Dataset) []store.Table {
	mnc := store.Table{Name: "mnc", Cols: []string{"mcc", "mnc", "plmn", "status", "status_raw", "company", "date_assigned",
		"remarks", "recovered_from", "formerly", "experimental"},
		Nullable: store.Nullables("company", "date_assigned", "remarks", "recovered_from", "formerly")}
	var core []store.Assignment
	if ds.List != nil {
		for _, m := range ds.List.MNCs {
			mnc.Add(m.MCC, m.MNC, m.MCC+m.MNC, m.Status(), m.StatusRaw, store.Null(m.Company), store.Null(m.DateAssigned),
				store.Null(m.Remarks), store.Null(m.RecoveredFrom), store.Null(m.Formerly), m.Experimental)
			core = append(core, store.Assignment{Source: "ca", MCC: m.MCC, MNC: m.MNC, Country: "CA",
				Holder: m.Company, HolderRaw: m.Company, Status: m.Status(), StatusRaw: m.StatusRaw,
				AssignedDate: m.DateAssigned, AsOf: ds.List.AsOf})
		}
	}
	return []store.Table{store.MetaTable(ds.Meta), store.SourcesTable(ds.Sources), store.AssignmentsTable(core), mnc}
}

// Specs are the output tables' shapes, for reading CSV back.
func Specs() []store.Table { return Tables(&Dataset{Meta: map[string]string{}}) }

// Checks are the sanity checks run by -verify.
var Checks = append([]store.CheckSpec{
	{Name: "mncs >= 60", Query: `SELECT count(*) FROM mnc`, OK: func(s string) bool { return store.Atoi(s) >= 60 }},
	{Name: "302-220 is Telus", Query: `SELECT company FROM mnc WHERE plmn='302220'`, OK: func(s string) bool { return strings.Contains(s, "Telus") }},
	{Name: "302-720 is Rogers", Query: `SELECT company FROM mnc WHERE plmn='302720'`, OK: func(s string) bool { return strings.Contains(s, "Rogers") }},
	{Name: "all MCC 302", Query: `SELECT count(*) FROM mnc WHERE mcc<>'302'`, OK: func(s string) bool { return s == "0" }},
}, store.AssignmentChecks...)
