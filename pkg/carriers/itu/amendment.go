package itu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	amendNoRE = regexp.MustCompile(`Amendment\s*No\.?\s*(\d+)`)
	// "Australia     ADD", "International Mobile, shared code    SUP*"
	countryActionRE = regexp.MustCompile(`^(.*?)\s*\b(ADD|SUP|LIR)\b\s*\**\s*$`)
	// A code cell, optionally followed by the operator in the same cell.
	codeCellRE = regexp.MustCompile(`^(\d{3})\s?(\d{2,3})\s*\**\s*(?:\n(.*))?$`)
	obNoRE     = regexp.MustCompile(`Operational Bulletin\s*No\.\s*(\d+)`)
)

// ParseAmendment finds the MNC-list amendment in an Operational Bulletin and
// returns it, or nil if this bulletin has none. ob is the bulletin's number.
//
// Bulletins are laid out inconsistently (the heading is sometimes a
// paragraph, sometimes table cells; country/action lines are sometimes
// paragraphs between rows), so this works on the flat token stream: find the
// MNC heading followed closely by "(Amendment No. N)", then read
// country/action lines, codes and operator names until the section ends.
func ParseAmendment(doc *Doc, ob int) *Amendment {
	toks := doc.Tokens
	for i := 0; i < len(toks); i++ {
		if !strings.Contains(toks[i].Text, "Network Codes (MNC)") {
			continue
		}
		// The heading's "(Annex to ... No. 1280 ...)(Amendment No. N)" follows
		// within a few tokens (or in the same token).
		for j := i; j < len(toks) && j <= i+8; j++ {
			t := toks[j].Text
			if j > i && isListTitle(t) {
				break
			}
			m := amendNoRE.FindStringSubmatch(t)
			if m == nil {
				continue
			}
			a := &Amendment{OB: ob}
			a.No, _ = strconv.Atoi(m[1])
			for k := i; k <= j; k++ {
				if mm := obNoRE.FindStringSubmatch(toks[k].Text); mm != nil {
					a.BaseOB, _ = strconv.Atoi(mm[1])
				}
			}
			a.Changes = parseChanges(toks[j+1:], a)
			return a
		}
	}
	return nil
}

// isListTitle reports whether a token starts another service publication's
// amendment section.
func isListTitle(t string) bool {
	return strings.HasPrefix(t, "List of ") || strings.HasPrefix(t, "National Numbering Plan")
}

func isSectionEnd(t string) bool {
	return strings.HasPrefix(t, "___") || strings.HasPrefix(t, "MCC:") || strings.HasPrefix(t, "MCC :") ||
		strings.HasPrefix(t, "* ") && strings.Contains(t, "Operational Bulletin") ||
		isListTitle(t) || amendNoRE.MatchString(t) || strings.Contains(t, "Annex to ITU Operational Bulletin")
}

func isHeader(t string) bool {
	l := strings.ToLower(t)
	return strings.HasPrefix(l, "country/geographical area") || strings.HasPrefix(l, "mcc+mnc") ||
		strings.HasPrefix(l, "mcc + mnc") || strings.HasPrefix(l, "operator/network") ||
		strings.HasPrefix(l, "operator / network")
}

func parseChanges(toks []Token, a *Amendment) []Change {
	var (
		out     []Change
		country string
		action  Action
		pending *Change // code seen, waiting for its operator
		et      bool    // inside an "Extra-territorial use" table
		etRow   []string
		etKey   [2]int // table, row of etRow
	)
	flush := func() {
		if pending != nil {
			out = append(out, *pending)
			pending = nil
		}
	}
	flushET := func() {
		if len(etRow) == 4 {
			if mcc, mnc, err := SplitCode(normSpace(etRow[1])); err == nil {
				act := action
				if act == "" {
					act = ActionAdd
				}
				out = append(out, Change{
					Amendment: a.No, OB: a.OB, Seq: len(out) + 1, ExtraTerritorial: true,
					Country: etCountry(etRow[0]), Action: act, MCC: mcc, MNC: mnc,
					MSINRange: normSpace(etRow[2]), Operator: normSpace(etRow[3]),
				})
			}
		}
		etRow = nil
	}
	for _, tk := range toks {
		raw := strings.TrimSpace(strings.ReplaceAll(tk.Text, "\u00a0", " "))
		if isSectionEnd(raw) {
			break
		}
		if et {
			// Rows of [country (A/B), MCC+MNC, MSIN range, operator].
			if k := [2]int{tk.Table, tk.Row}; tk.Table < 0 || k != etKey {
				flushET()
				etKey = k
			}
			if tk.Table >= 0 {
				etRow = append(etRow, raw)
				continue
			}
		}
		if raw == "" || isHeader(raw) {
			continue
		}
		t := normSpace(raw)
		if strings.HasPrefix(strings.ToLower(t), "extra-territorial use") {
			flush()
			et = true
			// The table's own action, if the heading carries one.
			if m := countryActionRE.FindStringSubmatch(t); m != nil {
				action = Action(m[2])
			} else {
				action = ""
			}
			continue
		}
		if m := countryActionRE.FindStringSubmatch(t); m != nil && !codeRE.MatchString(t) {
			flush()
			country, action = strings.TrimSpace(m[1]), Action(m[2])
			continue
		}
		if et {
			continue
		}
		if m := codeCellRE.FindStringSubmatch(raw); m != nil && action != "" {
			flush()
			pending = &Change{
				Amendment: a.No, OB: a.OB, Seq: len(out) + 1,
				Country: country, Action: action, MCC: m[1], MNC: m[2],
				Operator: normSpace(m[3]),
			}
			if pending.Operator != "" {
				flush()
			}
			continue
		}
		if pending != nil {
			pending.Operator = t
			flush()
		}
		// Anything else (country repeated in its own cell, footnote marks)
		// carries no change.
	}
	flush()
	flushET()
	return out
}

var etCountryRE = regexp.MustCompile(`(?s)^(.*?)\s*\(\s*(Country [AB])\b.*$`)

// etCountry turns "France\n(Country B – Where the MCC+MNC is ...)" into
// "France - Country B", the base list's spelling.
func etCountry(s string) string {
	if m := etCountryRE.FindStringSubmatch(s); m != nil {
		return normSpace(m[1]) + " - " + m[2]
	}
	return normSpace(s)
}

var (
	romanDateRE = regexp.MustCompile(`^(\d{1,2})\.([IVX]+)\.(\d{4})$`)
	longDateRE  = regexp.MustCompile(`Operational Bulletin\s*No\.\s*\d+\s*\((\d{1,2}\s+[A-Za-z]+\s+\d{4})\)`)
	romanMonths = map[string]int{"I": 1, "II": 2, "III": 3, "IV": 4, "V": 5, "VI": 6, "VII": 7, "VIII": 8, "IX": 9, "X": 10, "XI": 11, "XII": 12}
)

// RomanDate parses ITU's "15.XI.2023" dates to YYYY-MM-DD ("" if not one).
func RomanDate(s string) string {
	m := romanDateRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil || romanMonths[m[2]] == 0 {
		return ""
	}
	d, _ := strconv.Atoi(m[1])
	return fmt.Sprintf("%s-%02d-%02d", m[3], romanMonths[m[2]], d)
}

// BulletinDate finds an Operational Bulletin's publication date on its
// cover: a "1.X.2026" cell next to "No. 1349", or "No. 1350 (15 October
// 2026)". "" if neither is found.
func BulletinDate(doc *Doc) string {
	toks := doc.Tokens
	if len(toks) > 60 {
		toks = toks[:60]
	}
	for _, t := range toks {
		if d := RomanDate(t.Text); d != "" {
			return d
		}
		if m := longDateRE.FindStringSubmatch(normSpace(t.Text)); m != nil {
			if tm, err := time.Parse("2 January 2006", m[1]); err == nil {
				return tm.Format("2006-01-02")
			}
		}
	}
	return ""
}
