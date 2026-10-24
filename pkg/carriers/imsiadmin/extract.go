package imsiadmin

import (
	"regexp"
	"sort"
	"strings"

	"github.com/accretional/proto-telephony/pkg/carriers"
)

// Identifier is an FCC (or other) identifier found in a free-text field.
type Identifier struct {
	Type  string // see identifierPatterns
	Value string // normalized
}

// identifierPatterns are tried in order; each match is removed from the
// text before the next pattern runs, so a span is classified once.
var identifierPatterns = []struct {
	typ string
	re  *regexp.Regexp
	fn  func(m []string) string // builds the value from the submatches
}{
	// Experimental license file numbers: 0124-EX-ST-2016.
	{"experimental_file", regexp.MustCompile(`(?i)\b(\d{4}-EX-[A-Z]{2}-\d{4})\b`), upper1},
	// Legacy cellular license file numbers: 00197-CW-L-96, 07989-CL-MR-95.
	{"cellular_file", regexp.MustCompile(`(?i)\b(\d{5}-C[A-Z]-[A-Z]{1,2}-\d{2})\b`), upper1},
	// Satellite filings: SAT-LOA-20210511-00064.
	{"satellite_filing", regexp.MustCompile(`(?i)\b(SAT-[A-Z]{3}-\d{8}-\d{5})\b`), upper1},
	// FCC documents: DA 24-1193.
	{"fcc_document", regexp.MustCompile(`\b(DA)\s+(\d{2}-\d{1,4})\b`), func(m []string) string { return "DA " + m[2] }},
	// Spectrum lease IDs: L000012016.
	{"lease", regexp.MustCompile(`(?i)\b(L\d{8,9})\b`), upper1},
	// FCC Registration Numbers (CORES): "FRN 0001723410", "FRN#0004-3414-26", "CORES 0034742809".
	{"frn", regexp.MustCompile(`(?i)\b(?:FCC\s+)?(?:FRN|CORES)\s*[:#-]*\s*(\d[\d-]{5,12}\d)`), digits1},
	// ULS file numbers: "File #000210939", "File No. 0011357477".
	{"uls_file", regexp.MustCompile(`(?i)\bFile\s*(?:#|No\.?|Number|:)?\s*(\d{9,10})\b`), digits1},
	// FCC filer IDs: "filer id 827537", "FCC 837077".
	{"filer_id", regexp.MustCompile(`(?i)\b(?:filer\s*id|FCC)\s*[:#]?\s*(\d{6})\b`), digits1},
	// GSMA membership numbers.
	{"gsma_member", regexp.MustCompile(`(?i)\bGSMA(?:\s+member)?\s*#?\s*(\d{4,6})\b`), digits1},
	// FCC call signs, with "WPOJ826/827" shorthand for a second sign.
	{"call_sign", regexp.MustCompile(`(?i)\b([KW][A-Z]{2,3})[\s-]?(\d{3})((?:/\d{3})*)\b`), nil},
	// Experimental call signs: WH2XMI, WA9XGL.
	{"call_sign", regexp.MustCompile(`(?i)\b([KW][A-Z]?\d[A-Z]{2,3})\b`), upper1},
	// References to other US HNIs: "previously provided for 313-460".
	{"hni", regexp.MustCompile(`\b(31[0-6])[\s-](\d{3})\b`), func(m []string) string { return m[1] + "-" + m[2] }},
	// Market areas named alongside licenses: CMA375, BTA 267, MTA 2.
	{"market", regexp.MustCompile(`(?i)\b(CMA|BTA|MTA)\s*#?\s*(\d{1,3})\b`), func(m []string) string { return strings.ToUpper(m[1]) + m[2] }},
	// A bare 10-digit number starting 00 is an FRN.
	{"frn", regexp.MustCompile(`\b(00\d{8})\b`), digits1},
	// Any other bare number is kept, unclassified (license fields only).
	{"number", regexp.MustCompile(`\b(\d{6,10})\b`), digits1},
}

func upper1(m []string) string { return strings.ToUpper(m[1]) }

func digits1(m []string) string { return strings.ReplaceAll(m[1], "-", "") }

// ExtractIdentifiers finds the identifiers in free text (a "Wireless
// License Number" or "Entity Type" value). Notes such as "N/A" or "see
// attached" yield nothing; the raw text is kept elsewhere. Unclassified bare
// numbers are kept only if numbers is set (they mean something in a license
// field, not in a description).
func ExtractIdentifiers(s string, numbers bool) []Identifier {
	text := carriers.NormSpace(s)
	var out []Identifier
	seen := map[Identifier]bool{}
	add := func(id Identifier) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, p := range identifierPatterns {
		if p.typ == "number" && !numbers {
			continue
		}
		text = p.re.ReplaceAllStringFunc(text, func(match string) string {
			m := p.re.FindStringSubmatch(match)
			if p.typ == "call_sign" && p.fn == nil {
				prefix := strings.ToUpper(m[1])
				add(Identifier{"call_sign", prefix + m[2]})
				for _, more := range strings.Split(strings.TrimPrefix(m[3], "/"), "/") {
					if more != "" {
						add(Identifier{"call_sign", prefix + more})
					}
				}
			} else {
				add(Identifier{p.typ, p.fn(m)})
			}
			return " "
		})
	}
	return out
}

// protocolNames are the technologies on IMSI Admin's application form.
var protocolNames = map[string]string{
	"gprs": "GPRS", "edge": "EDGE", "w-cdma": "W-CDMA", "wcdma": "W-CDMA", "hspa": "HSPA", "1xrtt": "1xRTT",
	"hrpd/evdo": "HRPD/EVDO", "evdo": "HRPD/EVDO", "lte": "LTE/LTE Advanced", "lte advanced": "LTE/LTE Advanced",
	"lte/lte advanced": "LTE/LTE Advanced", "lte/lte advances": "LTE/LTE Advanced", "5g": "5G",
}

var (
	fiveGRE      = regexp.MustCompile(`(?i)\b5G\b`)
	other5GRE    = regexp.MustCompile(`(?i)\bother\s*\(\s*5G\s*\)|\b5G\b`)
	protocolList = []string{"GPRS", "EDGE", "W-CDMA", "HSPA", "1xRTT", "HRPD/EVDO", "LTE/LTE Advanced", "5G"}
)

// ParseProtocols normalizes a Protocol value into the form's technology
// names: "LTE/LTE Advances, 5G" → [LTE/LTE Advanced, 5G]. Unrecognized
// parts are returned as written.
func ParseProtocols(s string) []string {
	s = carriers.NormSpace(s)
	if s == "" {
		return nil
	}
	// "LTE 5G" and "Other (5G)" both mean 5G alongside anything else.
	has5G := fiveGRE.MatchString(s)
	s = other5GRE.ReplaceAllString(s, "")
	set := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, ok := protocolNames[strings.ToLower(p)]; ok {
			set[n] = true
		} else {
			set[p] = true
		}
	}
	if has5G {
		set["5G"] = true
	}
	var out []string
	for _, n := range protocolList {
		if set[n] {
			out = append(out, n)
			delete(set, n)
		}
	}
	var rest []string
	for n := range set {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// EntityCategory maps an Entity Type to the application form's category,
// or "" for the free-text descriptions on older records.
func EntityCategory(s string) string {
	l := strings.ToLower(carriers.NormSpace(s))
	switch {
	case strings.HasPrefix(l, "public network operator"):
		return "public_network_operator"
	case strings.HasPrefix(l, "provider of a service profile management system"):
		return "service_profile_provider"
	case strings.HasPrefix(l, "provider of service"):
		return "service_provider"
	}
	return ""
}
