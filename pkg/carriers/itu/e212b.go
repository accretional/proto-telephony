package itu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BaseList is the parsed E.212B annex.
type BaseList struct {
	OB               int    // Operational Bulletin it is annexed to, e.g. 1280
	Date             string // that bulletin's date, YYYY-MM-DD
	Position         string // "15 NOVEMBER 2023"
	Networks         []Network
	ExtraTerritorial []ExtraTerritorial
	Skipped          []string // rows that looked like data but didn't parse
}

var (
	annexOBRE  = regexp.MustCompile(`Annex to ITU Operational Bulletin\s*No\.\s*(\d+)`)
	positionRE = regexp.MustCompile(`(?i)\(POSITION ON ([^)]+)\)`)
	baseDateRE = regexp.MustCompile(`^Annex to ITU Operational Bulletin\s*No\.\s*\d+\s*[–-]\s*(\d{1,2}\.[IVX]+\.\d{4})`)
)

// SharedGroup is the Country value given to MCC 901 rows, matching how the
// amendments name them.
const SharedGroup = "International Mobile, shared code"

// ParseBaseList parses the MNC list annex (T-SP-E.212B-*.docx).
func ParseBaseList(doc *Doc) (*BaseList, error) {
	bl := &BaseList{}
	text := doc.Text()
	if m := annexOBRE.FindStringSubmatch(text); m != nil {
		bl.OB, _ = strconv.Atoi(m[1])
		if d := baseDateRE.FindStringSubmatch(text[strings.Index(text, m[0]):]); d != nil {
			bl.Date = RomanDate(d[1])
		}
	} else {
		return nil, fmt.Errorf("base list: no 'Annex to ITU Operational Bulletin No.' header")
	}
	if m := positionRE.FindStringSubmatch(text); m != nil {
		bl.Position = normSpace(m[1])
	}
	for _, tb := range doc.Tables() {
		if len(tb.Rows) == 0 {
			continue
		}
		head := strings.ToLower(strings.Join(tb.Rows[0], "|"))
		switch {
		case strings.Contains(head, "msin range") && strings.Contains(head, "operator") && len(tb.Rows[0]) == 4:
			bl.parseExtraTerritorial(tb)
		case strings.Contains(head, "mcc + mnc codes"):
			bl.parseNetworks(tb)
		}
	}
	if len(bl.Networks) == 0 {
		return nil, fmt.Errorf("base list: no MNC tables found")
	}
	return bl, nil
}

// parseNetworks handles the three MNC tables:
//
//	[Country, Network, Code] with country rows [Country, "", ""]  (geographic)
//	[Network, Code]                                               (901 shared)
//	["", Applicant, Code] with group rows [Group, "", ""]         (902 ROIO)
func (bl *BaseList) parseNetworks(tb Table) {
	group := ""
	for _, r := range tb.Rows[1:] {
		cells := make([]string, len(r))
		for i, c := range r {
			cells[i] = normSpace(c)
		}
		if len(cells) == 0 || strings.Join(cells, "") == "" {
			continue
		}
		code := cells[len(cells)-1]
		var name string
		switch len(cells) {
		case 2:
			name = cells[0]
		case 3:
			if cells[1] == "" && code == "" {
				group = cells[0]
				continue
			}
			name = cells[1]
		default:
			bl.Skipped = append(bl.Skipped, strings.Join(cells, " | "))
			continue
		}
		if strings.EqualFold(code, "MCC + MNC codes") {
			continue // repeated header row on a page break
		}
		mcc, mnc, err := SplitCode(code)
		if err != nil {
			bl.Skipped = append(bl.Skipped, strings.Join(cells, " | "))
			continue
		}
		country := group
		if len(cells) == 2 || mcc == "901" {
			country = SharedGroup
		}
		bl.Networks = append(bl.Networks, Network{
			MCC: mcc, MNC: mnc, Country: country, Operator: name,
			Kind: kindFor(mcc), SourceOB: bl.OB,
		})
	}
}

func (bl *BaseList) parseExtraTerritorial(tb Table) {
	for _, r := range tb.Rows[1:] {
		if len(r) != 4 {
			continue
		}
		mcc, mnc, err := SplitCode(normSpace(strings.TrimRight(r[1], "*")))
		if err != nil {
			continue
		}
		bl.ExtraTerritorial = append(bl.ExtraTerritorial, ExtraTerritorial{
			Country: normSpace(r[0]), MCC: mcc, MNC: mnc,
			MSINRange: normSpace(r[2]), Operator: normSpace(r[3]),
		})
	}
}
