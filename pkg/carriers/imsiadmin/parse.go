// Package imsiadmin downloads the US Home Network Identity (HNI = MCC+MNC)
// assignments from IMSI Admin (imsiadmin.com), the US IMSI Administrator
// run by ATIS for the IMSI Oversight Council, which assigns MNCs under the
// US MCCs 310–316. It reads both directories — HNI codes, and the shared
// CBRS HNI 315-010's IMSI Block Numbers (IBNs) — and every code's detail
// page, extracting all of the structured fields on it.
package imsiadmin

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/accretional/proto-telephony/pkg/carriers"
)

// Kind is which directory a code is from.
type Kind string

const (
	KindHNI  Kind = "hni"  // an HNI (MCC+MNC) assignment
	KindCBRS Kind = "cbrs" // an IBN under the shared CBRS HNI 315-010
)

// Entry is one directory row.
type Entry struct {
	Kind   Kind
	HNI    string // "310-170"
	IBN    string // CBRS only, "0152"
	Entity string // directory text: an entity name or a status note
	URL    string // detail page
}

// Code is "310-170" or, for CBRS, "315-010-0152".
func (e Entry) Code() string {
	if e.IBN != "" {
		return e.HNI + "-" + e.IBN
	}
	return e.HNI
}

var (
	tableRE  = regexp.MustCompile(`(?s)<table[^>]*class="sortable"[^>]*>(.*?)</table>`)
	rowRE    = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRE   = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	hrefRE   = regexp.MustCompile(`href="([^"]+)"`)
	tagRE    = regexp.MustCompile(`<[^>]+>`)
	hniRE    = regexp.MustCompile(`^(\d{3})-(\d{3})$`)
	ibnRE    = regexp.MustCompile(`^\d{4}$`)
	h1RE     = regexp.MustCompile(`(?s)<h1[^>]*class="entry-title"[^>]*>(.*?)</h1>`)
	h2RE     = regexp.MustCompile(`(?s)<h2[^>]*>(.*?)</h2>`)
	fieldRE  = regexp.MustCompile(`(?s)<strong>([^<]+?):\s*</strong>(.*?)</div>`)
	contentS = `class="entry-content`
)

func cellText(s string) string {
	return carriers.NormSpace(html.UnescapeString(tagRE.ReplaceAllString(s, " ")))
}

// ParseDirectory reads the HNI or CBRS directory table.
func ParseDirectory(kind Kind, page []byte) ([]Entry, error) {
	m := tableRE.FindSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("%s directory: no table found", kind)
	}
	want := 2
	if kind == KindCBRS {
		want = 3
	}
	var out []Entry
	for i, r := range rowRE.FindAllSubmatch(m[1], -1) {
		cells := cellRE.FindAllSubmatch(r[1], -1)
		if len(cells) != want {
			return nil, fmt.Errorf("%s directory row %d: %d cells, want %d", kind, i+1, len(cells), want)
		}
		if i == 0 {
			continue // header
		}
		e := Entry{Kind: kind, HNI: cellText(string(cells[0][1])), Entity: cellText(string(cells[want-1][1]))}
		if kind == KindCBRS {
			e.IBN = cellText(string(cells[1][1]))
			if !ibnRE.MatchString(e.IBN) {
				return nil, fmt.Errorf("cbrs directory row %d: bad IBN %q", i+1, e.IBN)
			}
		}
		if !hniRE.MatchString(e.HNI) {
			return nil, fmt.Errorf("%s directory row %d: bad HNI %q", kind, i+1, e.HNI)
		}
		if h := hrefRE.FindSubmatch(r[1]); h != nil {
			e.URL = html.UnescapeString(string(h[1]))
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s directory: empty table", kind)
	}
	return out, nil
}

// Field is one "Label: value" line of a detail page.
type Field struct {
	Section string // "Code Details", "Entity Details"
	Label   string // "Effective Date"
	Value   string
}

// Detail is a parsed detail page.
type Detail struct {
	Title  string
	Fields []Field
}

// Get returns the first value for label ("" if absent).
func (d *Detail) Get(label string) string {
	for _, f := range d.Fields {
		if strings.EqualFold(f.Label, label) {
			return f.Value
		}
	}
	return ""
}

// ParseDetail extracts every field from a code's detail page, in order,
// with the section heading it appears under.
func ParseDetail(page []byte) (*Detail, error) {
	s := string(page)
	i := strings.Index(s, contentS)
	if i < 0 {
		return nil, fmt.Errorf("detail page: no entry content")
	}
	d := &Detail{}
	if m := h1RE.FindStringSubmatch(s); m != nil {
		d.Title = cellText(m[1])
	}
	body := s[i:]
	if j := strings.Index(body, "</article>"); j >= 0 {
		body = body[:j]
	}
	// Walk sections: each <h2> names the fields until the next one.
	heads := h2RE.FindAllStringSubmatchIndex(body, -1)
	for k := range heads {
		end := len(body)
		if k+1 < len(heads) {
			end = heads[k+1][0]
		}
		section := cellText(body[heads[k][2]:heads[k][3]])
		for _, f := range fieldRE.FindAllStringSubmatch(body[heads[k][1]:end], -1) {
			d.Fields = append(d.Fields, Field{Section: section, Label: cellText(f[1]), Value: cellText(f[2])})
		}
	}
	if len(heads) == 0 {
		for _, f := range fieldRE.FindAllStringSubmatch(body, -1) {
			d.Fields = append(d.Fields, Field{Label: cellText(f[1]), Value: cellText(f[2])})
		}
	}
	if len(d.Fields) == 0 {
		return nil, fmt.Errorf("detail page: no fields")
	}
	return d, nil
}

// Status is what a directory entry's text says about the code.
type Status struct {
	State   string // assigned | returned | reclaimed | reserved | test | available | unavailable
	Date    string // ISO date of the return/reclamation, if given
	Related string // code in parentheses, e.g. "316-700" or "315-010-0043"
	Name    string // the entity name with any status/reference removed
}

var (
	returnedRE = regexp.MustCompile(`(?i)^(returned|reclaimed)\b[\s\-–]*(?:on\s+)?(\d{1,2}/\d{1,2}/\d{2,4})?\s*(?:\((.+?)\))?\s*$`)
	refRE      = regexp.MustCompile(`^(.*?)\s*\((\d{3}\s*-\s*\d{3}(?:\s*[-–]\s*\d{4})?)\)\s*$`)
	refPartsRE = regexp.MustCompile(`\d+`)
)

// ParseStatus interprets a directory entity text.
func ParseStatus(text string) Status {
	t := carriers.NormSpace(text)
	if m := returnedRE.FindStringSubmatch(t); m != nil {
		return Status{State: strings.ToLower(m[1]), Date: NormDate(m[2]), Related: normRef(m[3])}
	}
	l := strings.ToLower(t)
	switch {
	case strings.HasPrefix(l, "reserved"):
		return Status{State: "reserved", Name: t}
	case strings.HasPrefix(l, "not available"):
		return Status{State: "unavailable"}
	case strings.HasPrefix(l, "available"):
		return Status{State: "available"}
	case strings.HasPrefix(l, "test imsi"):
		return Status{State: "test", Name: t}
	}
	st := Status{State: "assigned", Name: t}
	if m := refRE.FindStringSubmatch(t); m != nil {
		st.Name, st.Related = m[1], normRef(m[2])
	}
	return st
}

// normRef turns "315-010 – 0044" into "315-010-0044".
func normRef(s string) string {
	return strings.Join(refPartsRE.FindAllString(s, -1), "-")
}

var dateLayouts = []string{"January 2, 2006", "Jan 2, 2006", "1/2/2006", "01/02/2006", "1/2/06", "01/02/06", "2006-01-02"}

// NormDate parses the site's date spellings ("June 7, 1995", "08/28/2026",
// "3/16/16") to YYYY-MM-DD; "" if it isn't a date.
func NormDate(s string) string {
	s = carriers.NormSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}
