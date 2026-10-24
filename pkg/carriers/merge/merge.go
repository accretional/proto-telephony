// Package merge combines every source's shared `assignments` table (see
// research/schema.md) into one dataset: all rows, the MCC → country and
// authoritative-source map, the best-known current row per code, and the
// conflicts between ITU and each national registry.
package merge

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// Authority says which national source is authoritative for which MCCs.
// A source only takes over its MCCs if its data was loaded.
var Authority = map[string][]string{
	"us": {"310", "311", "312", "313", "314", "315", "316"},
	"ca": {"302"},
	"mx": {"334"},
	"jp": {"440", "441"},
}

// ITU is the fallback source's name.
const ITU = "itu"

// Input is what Load found on disk.
type Input struct {
	Assignments    []store.Assignment
	SubAllocations []store.SubAllocation
	Files          []string // CSV files read, relative to the root
}

// Load reads <root>/<source>/assignments.csv (and sub_allocations.csv) for
// every source directory that has one.
func Load(root string) (*Input, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	in := &Input{}
	for _, e := range ents {
		if !e.IsDir() || e.Name() == "merged" {
			continue
		}
		path := filepath.Join(root, e.Name(), "assignments.csv")
		recs, err := readCSV(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		in.Files = append(in.Files, filepath.Join(e.Name(), "assignments.csv"))
		for _, r := range recs {
			in.Assignments = append(in.Assignments, store.Assignment{
				Source: r["source"], MCC: r["mcc"], MNC: r["mnc"], Country: r["country"],
				Holder: r["holder"], HolderRaw: r["holder_raw"], Status: r["status"], StatusRaw: r["status_raw"],
				AssignedDate: r["assigned_date"], StatusDate: r["status_date"], Reference: r["reference"],
				AsOf: r["as_of"], URL: r["url"]})
		}
		sub, err := readCSV(filepath.Join(root, e.Name(), "sub_allocations.csv"))
		if err == nil {
			in.Files = append(in.Files, filepath.Join(e.Name(), "sub_allocations.csv"))
			for _, r := range sub {
				in.SubAllocations = append(in.SubAllocations, store.SubAllocation{Source: r["source"], PLMN: r["plmn"],
					Kind: r["kind"], Block: r["block"], Holder: r["holder"], Status: r["status"], Country: r["country"], AsOf: r["as_of"]})
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if len(in.Assignments) == 0 {
		return nil, fmt.Errorf("no <source>/assignments.csv under %s", root)
	}
	return in, nil
}

func readCSV(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("%s: empty", path)
	}
	var out []map[string]string
	for _, rec := range recs[1:] {
		m := map[string]string{}
		for i, h := range recs[0] {
			if i < len(rec) {
				m[h] = rec[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// MCC is one row of the MCC map.
type MCC struct {
	MCC, Country string   // the primary country (most rows)
	Countries    []string // every country with codes under it
	Authority    string   // authoritative source ("itu" if none loaded)
	Sources      []string
	Codes        int // distinct MCC+MNCs across sources
}

// Current is the best-known row for one code.
type Current struct {
	store.Assignment
	ITUHolder, ITUStatus string
	Flag                 string // "" | itu_only | not_in_itu | itu_stale
}

// Conflict is a disagreement between ITU and a national registry.
type Conflict struct {
	PLMN, MCC, MNC, Country, Source, Kind string
	NationalStatus, NationalHolder        string
	ITUStatus, ITUHolder                  string
	Note                                  string
}

// Result is the merged dataset.
type Result struct {
	Input     *Input
	MCCs      []MCC
	Current   []Current
	Conflicts []Conflict
}

// inactive statuses mean a national registry doesn't consider the code held.
func inactive(status string) bool {
	switch status {
	case "returned", "reclaimed", "withdrawn", "available":
		return true
	}
	return false
}

// Merge builds the MCC map, the current view and the conflicts.
func Merge(in *Input) *Result {
	loaded := map[string]bool{}
	for _, a := range in.Assignments {
		loaded[a.Source] = true
	}
	auth := map[string]string{} // mcc -> source
	for src, mccs := range Authority {
		if loaded[src] {
			for _, m := range mccs {
				auth[m] = src
			}
		}
	}

	bySrc := map[string]map[string][]store.Assignment{} // source -> plmn -> rows
	mccCountries := map[string]map[string]int{}
	mccSources := map[string]map[string]bool{}
	mccCodes := map[string]map[string]bool{}
	for _, a := range in.Assignments {
		p := a.MCC + a.MNC
		if bySrc[a.Source] == nil {
			bySrc[a.Source] = map[string][]store.Assignment{}
		}
		bySrc[a.Source][p] = append(bySrc[a.Source][p], a)
		if mccCountries[a.MCC] == nil {
			mccCountries[a.MCC], mccSources[a.MCC], mccCodes[a.MCC] = map[string]int{}, map[string]bool{}, map[string]bool{}
		}
		if a.Status != "available" {
			mccCountries[a.MCC][a.Country]++
			mccCodes[a.MCC][p] = true
		}
		mccSources[a.MCC][a.Source] = true
	}

	res := &Result{Input: in}
	for mcc, cs := range mccCountries {
		m := MCC{MCC: mcc, Authority: ITU, Codes: len(mccCodes[mcc])}
		if s, ok := auth[mcc]; ok {
			m.Authority = s
		}
		best := 0
		for c, n := range cs {
			m.Countries = append(m.Countries, c)
			if n > best || n == best && c < m.Country {
				m.Country, best = c, n
			}
		}
		sort.Strings(m.Countries)
		for s := range mccSources[mcc] {
			m.Sources = append(m.Sources, s)
		}
		sort.Strings(m.Sources)
		res.MCCs = append(res.MCCs, m)
	}
	sort.Slice(res.MCCs, func(i, j int) bool { return res.MCCs[i].MCC < res.MCCs[j].MCC })

	itu := bySrc[ITU]
	first := func(rows []store.Assignment) (store.Assignment, bool) {
		if len(rows) == 0 {
			return store.Assignment{}, false
		}
		return rows[0], true
	}
	// National MNC length per MCC, to explain 2- vs 3-digit mismatches.
	natLen := map[string]map[int]int{}
	for src, codes := range bySrc {
		if src == ITU {
			continue
		}
		for _, rows := range codes {
			for _, a := range rows {
				if auth[a.MCC] == src {
					if natLen[a.MCC] == nil {
						natLen[a.MCC] = map[int]int{}
					}
					natLen[a.MCC][len(a.MNC)]++
				}
			}
		}
	}

	// Codes under an authoritative national source.
	for mcc, src := range auth {
		nat := bySrc[src]
		for p, rows := range nat {
			if rows[0].MCC != mcc {
				continue
			}
			for _, a := range rows {
				i, inITU := first(itu[p])
				cur := Current{Assignment: a, ITUHolder: i.Holder, ITUStatus: i.Status}
				switch {
				case !inITU && a.Status == "assigned":
					cur.Flag = "not_in_itu"
					res.Conflicts = append(res.Conflicts, conflict(a, i, "not_in_itu", ""))
				case inITU && i.Status == "assigned" && inactive(a.Status):
					cur.Flag = "itu_stale"
					res.Conflicts = append(res.Conflicts, conflict(a, i, "itu_stale", ""))
				case inITU && a.Status == "assigned" && i.Status == "assigned" && holdersDiffer(a.Holder, i.Holder):
					res.Conflicts = append(res.Conflicts, conflict(a, i, "holder_differs", ""))
				}
				if a.Status != "available" { // a free code isn't an assignment
					res.Current = append(res.Current, cur)
				}
			}
		}
		// ITU rows the national source doesn't mention at all.
		for p, rows := range itu {
			if rows[0].MCC != mcc || len(nat[p]) > 0 {
				continue
			}
			i := rows[0]
			note := ""
			if l := natLen[mcc]; l != nil && l[len(i.MNC)] == 0 {
				note = fmt.Sprintf("national registry uses %s-digit MNCs under %s; ITU's %d-digit code may be stale", lengths(l), mcc, len(i.MNC))
			}
			res.Conflicts = append(res.Conflicts, Conflict{PLMN: p, MCC: i.MCC, MNC: i.MNC, Country: i.Country, Source: src,
				Kind: "itu_only", ITUStatus: i.Status, ITUHolder: i.Holder, Note: note})
			for _, r := range rows {
				res.Current = append(res.Current, Current{Assignment: r, ITUHolder: r.Holder, ITUStatus: r.Status, Flag: "itu_only"})
			}
		}
	}
	// Every other MCC: ITU.
	for _, rows := range itu {
		if _, ok := auth[rows[0].MCC]; ok {
			continue
		}
		for _, r := range rows {
			res.Current = append(res.Current, Current{Assignment: r, ITUHolder: r.Holder, ITUStatus: r.Status})
		}
	}
	sort.Slice(res.Current, func(i, j int) bool {
		a, b := res.Current[i], res.Current[j]
		if a.MCC+a.MNC != b.MCC+b.MNC {
			return a.MCC+a.MNC < b.MCC+b.MNC
		}
		return a.Country < b.Country
	})
	sort.Slice(res.Conflicts, func(i, j int) bool {
		a, b := res.Conflicts[i], res.Conflicts[j]
		if a.PLMN != b.PLMN {
			return a.PLMN < b.PLMN
		}
		return a.Kind < b.Kind
	})
	return res
}

func conflict(nat, itu store.Assignment, kind, note string) Conflict {
	return Conflict{PLMN: nat.MCC + nat.MNC, MCC: nat.MCC, MNC: nat.MNC, Country: nat.Country, Source: nat.Source, Kind: kind,
		NationalStatus: nat.Status, NationalHolder: nat.Holder, ITUStatus: itu.Status, ITUHolder: itu.Holder, Note: note}
}

func lengths(l map[int]int) string {
	var ks []string
	for k := range l {
		ks = append(ks, fmt.Sprint(k))
	}
	sort.Strings(ks)
	return strings.Join(ks, "/")
}

var (
	wordRE    = regexp.MustCompile(`[\p{L}\p{N}]+`)
	stopWords = map[string]bool{"inc": true, "llc": true, "ltd": true, "limited": true, "gmbh": true, "co": true, "corp": true,
		"corporation": true, "company": true, "the": true, "of": true, "and": true, "de": true, "s": true, "a": true, "c": true,
		"v": true, "r": true, "l": true, "sa": true, "cv": true, "sab": true, "dba": true, "d": true, "b": true, "plc": true,
		"ag": true, "oy": true, "ab": true, "as": true, "bv": true, "nv": true, "spa": true, "srl": true, "kk": true,
		"mobile": true, "mobility": true, "wireless": true, "communications": true, "telecom": true, "networks": true,
		"network": true, "services": true, "canada": true, "usa": true, "us": true, "méxico": true, "mexico": true}
)

func latin(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			return false
		}
	}
	return true
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRE.FindAllString(strings.ToLower(s), -1) {
		if !stopWords[w] && len(w) > 1 {
			out[w] = true
		}
	}
	return out
}

// fold lowercases and strips diacritics ("TELÉFONOS" -> "telefonos").
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// compact is a name's significant words run together ("Sask Tel Mobility"
// and "SaskTel Mobility" both -> "sasktel").
func compact(s string) string {
	var ws []string
	for _, w := range wordRE.FindAllString(fold(s), -1) {
		if !stopWords[w] && len(w) > 1 {
			ws = append(ws, w)
		}
	}
	return strings.Join(ws, "")
}

// dice is the Sørensen–Dice similarity of two strings' character bigrams.
func dice(a, b string) float64 {
	bg := func(s string) map[string]int {
		m := map[string]int{}
		r := []rune(s)
		for i := 0; i+1 < len(r); i++ {
			m[string(r[i:i+2])]++
		}
		return m
	}
	ma, mb := bg(a), bg(b)
	na, nb, common := 0, 0, 0
	for k, v := range ma {
		na += v
		common += min(v, mb[k])
	}
	for _, v := range mb {
		nb += v
	}
	if na+nb == 0 {
		return 0
	}
	return 2 * float64(common) / float64(na+nb)
}

// holdersDiffer is a deliberately loose test that two Latin-script names
// are different holders: no significant word in common (accents folded),
// and their run-together forms neither contain one another nor look alike
// (character-bigram similarity under 0.6, which tolerates spelling variants
// such as Keewaytinook/Keewatinook). Different scripts (Japanese vs ITU's
// English) and empty names are never flagged.
func holdersDiffer(a, b string) bool {
	if a == "" || b == "" || !latin(a) || !latin(b) {
		return false
	}
	wa, wb := words(fold(a)), words(fold(b))
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	for w := range wa {
		if wb[w] {
			return false
		}
	}
	ca, cb := compact(a), compact(b)
	if strings.Contains(ca, cb) || strings.Contains(cb, ca) || dice(ca, cb) >= 0.6 {
		return false
	}
	return true
}
