// Package mic downloads Japan's IMSI (MCC+MNC) designations (MCC 440 with
// 2-digit MNCs, 441 with 3-digit) from the Ministry of Internal Affairs and
// Communications (MIC, 総務省). Its numbering page (電気通信番号指定状況,
// Shift_JIS) links a spreadsheet listing the whole code space — every code
// with its operator and 使用中 (in use) / 未使用 (unused) — whose URL changes
// with each edition, and a table of recent designations. See
// research/carriers/jp/README.md.
package mic

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/unicode/norm"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
	"github.com/accretional/proto-telephony/pkg/carriers/xlsx"
)

// BaseURL is MIC's site and IndexPath its number-designation page.
const (
	BaseURL   = "https://www.soumu.go.jp"
	IndexPath = "/main_sosiki/joho_tsusin/top/tel_number/number_shitei.html"
)

// Code is one IMSI code (MCC+MNC) and its designation.
type Code struct {
	MCC, MNC    string
	Operator    string // NFKC-normalized
	OperatorRaw string
	StatusRaw   string // 使用中 | 未使用
}

// Status maps MIC's status onto the shared vocabulary.
func (c Code) Status() string {
	switch c.StatusRaw {
	case "使用中": // in use
		return "assigned"
	case "未使用": // unused
		return "available"
	}
	return c.StatusRaw
}

// Designation is a row of the index page's recent-designations table.
type Designation struct {
	MCC, MNC string
	Operator string
	Date     string // YYYY-MM-DD
}

// List is the parsed spreadsheet plus the index page's recent designations.
type List struct {
	AsOf   string
	File   string
	Codes  []Code
	Recent []Designation
}

// Norm applies NFKC (full-width → ASCII, ideographic space → space) and
// collapses whitespace.
func Norm(s string) string { return carriers.NormSpace(norm.NFKC.String(s)) }

var reiwaRE = regexp.MustCompile(`令和\s*(\d+)\s*年\s*(\d+)\s*月\s*(\d+)\s*日`)

// ReiwaDate parses a 令和N年M月D日 date (Reiwa 1 = 2019), with full-width
// digits allowed; "" if there is none.
func ReiwaDate(s string) string {
	m := reiwaRE.FindStringSubmatch(Norm(s))
	if m == nil {
		return ""
	}
	n, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	return fmt.Sprintf("%04d-%02d-%02d", 2018+n, mo, d)
}

// DecodeIndex converts the Shift_JIS index page to UTF-8.
func DecodeIndex(b []byte) (string, error) {
	out, err := io.ReadAll(japanese.ShiftJIS.NewDecoder().Reader(bytes.NewReader(b)))
	return string(out), err
}

var (
	itemRE   = regexp.MustCompile(`IMSI`)
	xlsxRE   = regexp.MustCompile(`href="([^"]*/main_content/\d+\.xlsx)"`)
	trRE     = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	tdRE     = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRE    = regexp.MustCompile(`<[^>]+>`)
	codeRE   = regexp.MustCompile(`^(44[01])(\d{2,3})$`)
	imsiCell = regexp.MustCompile(`^IMSI$`)
)

func cellText(s string) string { return Norm(html.UnescapeString(tagRE.ReplaceAllString(s, " "))) }

// FindFile returns the IMSI spreadsheet URL. The page lists number types
// as "9. IMSI [ PDF形式 、 Excel形式 ]" ([PDF format, Excel format]), so
// it takes the first "IMSI" followed, within the same item, by a PDF
// mention and then an .xlsx link. Table rows that merely mention IMSI
// (recent designations) end with </tr> before any such link.
func FindFile(page, base string) (string, error) {
	for _, loc := range itemRE.FindAllStringIndex(page, -1) {
		rest := page[loc[1]:]
		if len(rest) > 1500 {
			rest = rest[:1500]
		}
		m := xlsxRE.FindStringSubmatchIndex(rest)
		if m == nil {
			continue
		}
		between := rest[:m[0]]
		if strings.Contains(strings.ToLower(between), "</tr>") || !strings.Contains(between, "PDF") {
			continue
		}
		ref, err := url.Parse(rest[m[2]:m[3]])
		if err != nil {
			return "", err
		}
		b, err := url.Parse(base)
		if err != nil {
			return "", err
		}
		return b.ResolveReference(ref).String(), nil
	}
	return "", fmt.Errorf("no IMSI .xlsx link on the index page")
}

// RecentDesignations reads index-page table rows of the form
// IMSI | 441214 | operator | 令和8年8月10日.
func RecentDesignations(page string) []Designation {
	var out []Designation
	for _, tr := range trRE.FindAllStringSubmatch(page, -1) {
		var cells []string
		for _, td := range tdRE.FindAllStringSubmatch(tr[1], -1) {
			cells = append(cells, cellText(td[1]))
		}
		if len(cells) < 4 || !imsiCell.MatchString(cells[0]) {
			continue
		}
		m := codeRE.FindStringSubmatch(cells[1])
		if m == nil {
			continue
		}
		out = append(out, Designation{MCC: m[1], MNC: m[2], Operator: cells[2], Date: ReiwaDate(cells[3])})
	}
	return out
}

// Parse reads the IMSI spreadsheet.
func Parse(data []byte) (*List, error) {
	sheets, err := xlsx.Read(data)
	if err != nil {
		return nil, err
	}
	if len(sheets) == 0 {
		return nil, fmt.Errorf("mic: no sheets")
	}
	l := &List{}
	header := false
	for i, r := range sheets[0].Rows {
		for len(r) < 3 {
			r = append(r, "")
		}
		if l.AsOf == "" {
			for _, c := range r {
				if d := ReiwaDate(c); d != "" && strings.Contains(c, "現在") { // "as of"
					l.AsOf = d
				}
			}
		}
		n := Norm(r[0])
		if n == "番号" { // header: number | operator | status
			header = true
			continue
		}
		if !header {
			continue
		}
		m := codeRE.FindStringSubmatch(n)
		if m == nil {
			if n != "" {
				return nil, fmt.Errorf("mic row %d: bad code %q", i+1, r[0])
			}
			continue
		}
		l.Codes = append(l.Codes, Code{MCC: m[1], MNC: m[2], OperatorRaw: strings.TrimSpace(r[1]),
			Operator: Norm(r[1]), StatusRaw: Norm(r[2])})
	}
	if len(l.Codes) == 0 {
		return nil, fmt.Errorf("mic: no codes (header 番号 not found?)")
	}
	return l, nil
}

// Dataset is everything written out.
type Dataset struct {
	Meta    map[string]string
	Sources []carriers.Source
	List    *List
}

// Build finds, downloads and parses the current IMSI spreadsheet.
func Build(ctx context.Context, c *carriers.Client, rep carriers.Reporter) (*Dataset, error) {
	rep.Stage(1, 2, "MIC IMSI designations (総務省)")
	raw, isrc, err := c.Fresh(ctx, "mic", "number_shitei.html", c.Base+IndexPath)
	if err != nil {
		return nil, fmt.Errorf("index page: %w", err)
	}
	isrc.Kind, isrc.Name = "index", "number_shitei"
	page, err := DecodeIndex(raw)
	if err != nil {
		return nil, fmt.Errorf("index page: shift_jis: %w", err)
	}
	fileURL, err := FindFile(page, c.Base+IndexPath)
	if err != nil {
		return nil, err
	}
	name := path.Base(fileURL)
	rep.Info("index %s -> %s", isrc.URL, name)
	// Each edition gets a new main_content ID, so the file is cached by name.
	data, src, err := c.Cached(ctx, "mic", name, fileURL)
	if err != nil {
		return nil, err
	}
	src.Kind, src.Name = "xlsx", name
	rep.Info("%s  %s  %s", src.URL, carriers.FmtBytes(src.Bytes), tag(src))
	l, err := Parse(data)
	if err != nil {
		return nil, err
	}
	l.File = name
	if l.AsOf == "" {
		rep.Warn("no 令和…現在 as-of date in the spreadsheet; using today")
		l.AsOf = time.Now().UTC().Format("2006-01-02")
	}
	l.Recent = RecentDesignations(page)
	counts := map[string]int{}
	byCode := map[string]Code{}
	for _, cd := range l.Codes {
		counts[cd.Status()]++
		byCode[cd.MCC+cd.MNC] = cd
	}
	rep.Info("%d codes as of %s: %v", len(l.Codes), l.AsOf, counts)
	for _, d := range l.Recent {
		cd, ok := byCode[d.MCC+d.MNC]
		state := "missing from the spreadsheet"
		if ok {
			state = "spreadsheet: " + cd.StatusRaw
		}
		rep.Info("recent designation %s-%s %s %s (%s)", d.MCC, d.MNC, d.Operator, d.Date, state)
	}
	return &Dataset{Meta: map[string]string{"source": fileURL, "as_of": l.AsOf, "file": name,
		"codes": fmt.Sprint(len(l.Codes)), "in_use": fmt.Sprint(counts["assigned"])},
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
-- MIC's IMSI list: every code under 440/441, used and unused.
CREATE TABLE imsi (
  mcc          TEXT NOT NULL,
  mnc          TEXT NOT NULL,   -- 2 digits under 440, 3 under 441
  plmn         TEXT NOT NULL,
  operator     TEXT,            -- NFKC-normalized
  operator_raw TEXT,            -- as published (full-width characters)
  status       TEXT NOT NULL,   -- assigned | available
  status_raw   TEXT NOT NULL,   -- 使用中 (in use) | 未使用 (unused)
  PRIMARY KEY (mcc, mnc)
);

-- The index page's recent designations (with dates, which the list lacks).
CREATE TABLE recent_designations (
  mcc      TEXT NOT NULL,
  mnc      TEXT NOT NULL,
  operator TEXT NOT NULL,
  date     TEXT,
  PRIMARY KEY (mcc, mnc, operator)
);
`

// Tables lays ds out as the output tables.
func Tables(ds *Dataset) []store.Table {
	imsi := store.Table{Name: "imsi", Cols: []string{"mcc", "mnc", "plmn", "operator", "operator_raw", "status", "status_raw"},
		Nullable: store.Nullables("operator", "operator_raw")}
	recent := store.Table{Name: "recent_designations", Cols: []string{"mcc", "mnc", "operator", "date"},
		Nullable: store.Nullables("date"), Replace: true}
	var core []store.Assignment
	if ds.List != nil {
		dates := map[string]string{}
		for _, d := range ds.List.Recent {
			recent.Add(d.MCC, d.MNC, d.Operator, store.Null(d.Date))
			dates[d.MCC+d.MNC] = d.Date
		}
		for _, c := range ds.List.Codes {
			imsi.Add(c.MCC, c.MNC, c.MCC+c.MNC, store.Null(c.Operator), store.Null(c.OperatorRaw), c.Status(), c.StatusRaw)
			core = append(core, store.Assignment{Source: "jp", MCC: c.MCC, MNC: c.MNC, Country: "JP",
				Holder: c.Operator, HolderRaw: c.OperatorRaw, Status: c.Status(), StatusRaw: c.StatusRaw,
				AssignedDate: dates[c.MCC+c.MNC], AsOf: ds.List.AsOf})
		}
	}
	return []store.Table{store.MetaTable(ds.Meta), store.SourcesTable(ds.Sources), store.AssignmentsTable(core), imsi, recent}
}

// Specs are the output tables' shapes, for reading CSV back.
func Specs() []store.Table { return Tables(&Dataset{Meta: map[string]string{}}) }

// Checks are the sanity checks run by -verify.
var Checks = append([]store.CheckSpec{
	{Name: "full code space (1000)", Query: `SELECT count(*) FROM imsi`, OK: func(s string) bool { return s == "1000" }},
	{Name: "in use >= 50", Query: `SELECT count(*) FROM imsi WHERE status='assigned'`, OK: func(s string) bool { return store.Atoi(s) >= 50 }},
	{Name: "440-10 is NTT Docomo", Query: `SELECT operator FROM imsi WHERE plmn='44010'`, OK: func(s string) bool { return strings.Contains(s, "ドコモ") }},
	{Name: "statuses known", Query: `SELECT count(*) FROM imsi WHERE status_raw NOT IN ('使用中','未使用')`, OK: func(s string) bool { return s == "0" }},
	{Name: "MNC lengths (440:2, 441:3)", Query: `SELECT count(*) FROM imsi WHERE (mcc='440' AND length(mnc)<>2) OR (mcc='441' AND length(mnc)<>3)`,
		OK: func(s string) bool { return s == "0" }},
}, store.AssignmentChecks...)
