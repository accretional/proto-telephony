// Package ift downloads Mexico's MNC assignments (MCC 334) from the
// telecom regulator's "Consulta de Código de Red Móvil (MNC)" spreadsheet:
// MCC, MNC, provider (PST), assignment letters (oficios) and dates. The
// spreadsheet's file name carries its date (mncasignados_DD_MM_YY.xlsx), so
// it is found from the index page each run. The regulator is moving from IFT
// to the CRT; the index URL is configurable. See
// research/carriers/mx/README.md.
package ift

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
	"github.com/accretional/proto-telephony/pkg/carriers/xlsx"
)

// BaseURL is the regulator's site and IndexPath the page linking the file.
const (
	BaseURL   = "https://www.ift.org.mx"
	IndexPath = "/usuarios-telefonia-fija/marcacion-y-zonas"
)

// Letter is one assignment letter (oficio) with its date.
type Letter struct {
	Ref       string // CFT/D03/USI/DGA/1602/10
	Date      string // YYYY-MM-DD
	Regulator string // CFT (COFETEL) | IFT | CRT, from the reference prefix
}

// MNC is one row of the spreadsheet.
type MNC struct {
	MCC, MNC  string
	Holder    string // trimmed
	HolderRaw string
	Letters   []Letter // in published order
}

// List is the parsed spreadsheet.
type List struct {
	AsOf string // from the file name
	File string
	MNCs []MNC
}

var (
	fileLinkRE = regexp.MustCompile(`(?i)href="([^"]*mncasignados[^"]*\.xlsx)"`)
	fileDateRE = regexp.MustCompile(`(\d{2})_(\d{2})_(\d{2,4})\.xlsx$`)
	textDateRE = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`)
)

// FindFile returns the spreadsheet URL linked from the index page.
func FindFile(index []byte, base string) (string, error) {
	m := fileLinkRE.FindSubmatch(index)
	if m == nil {
		return "", fmt.Errorf("no mncasignados*.xlsx link on the index page")
	}
	ref, err := url.Parse(html.UnescapeString(string(m[1])))
	if err != nil {
		return "", err
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(ref).String(), nil
}

// FileDate parses the DD_MM_YY in the file name.
func FileDate(name string) string {
	m := fileDateRE.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	y := m[3]
	if len(y) == 2 {
		y = "20" + y
	}
	return fmt.Sprintf("%s-%s-%s", y, m[2], m[1])
}

func parseDate(s string) string {
	s = strings.TrimSpace(s)
	if d, ok := xlsx.SerialDate(s); ok {
		return d
	}
	if m := textDateRE.FindStringSubmatch(s); m != nil { // DD/MM/YYYY
		d, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		return fmt.Sprintf("%s-%02d-%02d", m[3], mo, d)
	}
	return ""
}

func regulator(ref string) string {
	if i := strings.IndexByte(ref, '/'); i > 0 {
		return strings.ToUpper(ref[:i])
	}
	return ""
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Parse reads the spreadsheet.
func Parse(data []byte) (*List, error) {
	sheets, err := xlsx.Read(data)
	if err != nil {
		return nil, err
	}
	if len(sheets) == 0 {
		return nil, fmt.Errorf("ift: no sheets")
	}
	rows := sheets[0].Rows
	// Find the header: MCC | MNC | PST | OFICIO | FECHA.
	hdr := -1
	for i, r := range rows {
		if len(r) >= 5 && strings.HasPrefix(strings.ToUpper(r[0]), "MCC") && strings.HasPrefix(strings.ToUpper(r[1]), "MNC") {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		return nil, fmt.Errorf("ift: no MCC/MNC header row")
	}
	l := &List{}
	for i, r := range rows[hdr+1:] {
		for len(r) < 5 {
			r = append(r, "")
		}
		mcc, mnc := strings.TrimSpace(r[0]), strings.TrimSpace(r[1])
		if mcc == "" && mnc == "" {
			continue
		}
		if len(mcc) != 3 || len(mnc) < 2 || len(mnc) > 3 {
			return nil, fmt.Errorf("ift row %d: bad code %q %q", hdr+i+2, mcc, mnc)
		}
		m := MNC{MCC: mcc, MNC: mnc, HolderRaw: r[2], Holder: carriers.NormSpace(r[2])}
		refs, dates := lines(r[3]), lines(r[4])
		for j, ref := range refs {
			lt := Letter{Ref: ref, Regulator: regulator(ref)}
			if j < len(dates) {
				lt.Date = parseDate(dates[j])
			}
			m.Letters = append(m.Letters, lt)
		}
		l.MNCs = append(l.MNCs, m)
	}
	if len(l.MNCs) == 0 {
		return nil, fmt.Errorf("ift: no rows")
	}
	return l, nil
}

// Dataset is everything written out.
type Dataset struct {
	Meta    map[string]string
	Sources []carriers.Source
	List    *List
}

// Build finds, downloads and parses the current spreadsheet.
func Build(ctx context.Context, c *carriers.Client, rep carriers.Reporter) (*Dataset, error) {
	rep.Stage(1, 2, "IFT/CRT MNC spreadsheet")
	index, isrc, err := c.Fresh(ctx, "ift", "index.html", c.Base+IndexPath)
	if err != nil {
		return nil, fmt.Errorf("index page: %w", err)
	}
	isrc.Kind, isrc.Name = "index", "marcacion-y-zonas"
	fileURL, err := FindFile(index, c.Base+IndexPath)
	if err != nil {
		return nil, err
	}
	name := path.Base(fileURL)
	rep.Info("index %s -> %s", isrc.URL, name)
	// A dated file never changes, so it's cached by name.
	data, src, err := c.Cached(ctx, "ift", name, fileURL)
	if err != nil {
		return nil, err
	}
	src.Kind, src.Name = "xlsx", name
	rep.Info("%s  %s  %s", src.URL, carriers.FmtBytes(src.Bytes), tag(src))
	l, err := Parse(data)
	if err != nil {
		return nil, err
	}
	l.File, l.AsOf = name, FileDate(name)
	if l.AsOf == "" {
		rep.Warn("no date in file name %q; using today", name)
		l.AsOf = time.Now().UTC().Format("2006-01-02")
	}
	letters, regs := 0, map[string]int{}
	for _, m := range l.MNCs {
		letters += len(m.Letters)
		for _, lt := range m.Letters {
			regs[lt.Regulator]++
			if lt.Date == "" {
				rep.Warn("334-%s: letter %s has no parseable date", m.MNC, lt.Ref)
			}
		}
		if len(m.Letters) == 0 {
			rep.Warn("334-%s: no assignment letter", m.MNC)
		}
	}
	rep.Info("%d MNCs as of %s, %d assignment letters by issuer %v", len(l.MNCs), l.AsOf, letters, regs)
	return &Dataset{Meta: map[string]string{"source": fileURL, "as_of": l.AsOf, "file": name, "mncs": fmt.Sprint(len(l.MNCs))},
		Sources: []carriers.Source{isrc, src}, List: l}, nil
}

func tag(s carriers.Source) string {
	if s.FromCache {
		return "cache"
	}
	return s.Elapsed.Round(time.Millisecond).String()
}

// Schema is the output's DDL.
const Schema = store.MetaDDL + store.SourcesDDL + store.AssignmentsDDL + `
-- The regulator's list: one row per MNC.
CREATE TABLE mnc (
  mcc            TEXT NOT NULL,
  mnc            TEXT NOT NULL,
  plmn           TEXT NOT NULL,
  holder         TEXT NOT NULL,
  first_assigned TEXT,           -- date of the first letter
  last_letter    TEXT,           -- latest assignment letter (oficio)
  last_date      TEXT,
  letters        INTEGER NOT NULL,
  PRIMARY KEY (mcc, mnc)
);

-- Every assignment letter (oficio), in published order.
CREATE TABLE assignment_letters (
  mcc       TEXT NOT NULL,
  mnc       TEXT NOT NULL,
  seq       INTEGER NOT NULL,
  letter    TEXT NOT NULL,
  date      TEXT,
  regulator TEXT,                -- CFT | IFT | CRT
  PRIMARY KEY (mcc, mnc, seq)
);
`

// Tables lays ds out as the output tables.
func Tables(ds *Dataset) []store.Table {
	mnc := store.Table{Name: "mnc", Cols: []string{"mcc", "mnc", "plmn", "holder", "first_assigned", "last_letter", "last_date", "letters"},
		Nullable: store.Nullables("first_assigned", "last_letter", "last_date")}
	letters := store.Table{Name: "assignment_letters", Cols: []string{"mcc", "mnc", "seq", "letter", "date", "regulator"},
		Nullable: store.Nullables("date", "regulator")}
	var core []store.Assignment
	if ds.List != nil {
		for _, m := range ds.List.MNCs {
			var first, last Letter
			if n := len(m.Letters); n > 0 {
				first, last = m.Letters[0], m.Letters[n-1]
			}
			mnc.Add(m.MCC, m.MNC, m.MCC+m.MNC, m.Holder, store.Null(first.Date), store.Null(last.Ref), store.Null(last.Date), len(m.Letters))
			for i, lt := range m.Letters {
				letters.Add(m.MCC, m.MNC, i+1, lt.Ref, store.Null(lt.Date), store.Null(lt.Regulator))
			}
			core = append(core, store.Assignment{Source: "mx", MCC: m.MCC, MNC: m.MNC, Country: "MX",
				Holder: m.Holder, HolderRaw: m.HolderRaw, Status: "assigned",
				AssignedDate: first.Date, Reference: last.Ref, AsOf: ds.List.AsOf})
		}
	}
	return []store.Table{store.MetaTable(ds.Meta), store.SourcesTable(ds.Sources), store.AssignmentsTable(core), mnc, letters}
}

// Specs are the output tables' shapes, for reading CSV back.
func Specs() []store.Table { return Tables(&Dataset{Meta: map[string]string{}}) }

// Checks are the sanity checks run by -verify.
var Checks = append([]store.CheckSpec{
	{Name: "mncs >= 20", Query: `SELECT count(*) FROM mnc`, OK: func(s string) bool { return store.Atoi(s) >= 20 }},
	{Name: "334-020 is Radiomóvil Dipsa", Query: `SELECT holder FROM mnc WHERE plmn='334020'`, OK: func(s string) bool { return strings.Contains(strings.ToUpper(s), "DIPSA") }},
	{Name: "MNCs are 3 digits", Query: `SELECT count(*) FROM mnc WHERE length(mnc)<>3`, OK: func(s string) bool { return s == "0" }},
	{Name: "letters have dates", Query: `SELECT count(*) FROM assignment_letters WHERE date IS NULL`, OK: func(s string) bool { return s == "0" }},
}, store.AssignmentChecks...)
