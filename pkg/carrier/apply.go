package carrier

import (
	"sort"
	"strings"
)

// Outcome of replaying one Change.
type Outcome string

const (
	OutcomeApplied   Outcome = "applied"
	OutcomeReplaced  Outcome = "replaced"  // ADD for a code that already existed
	OutcomeNoMatch   Outcome = "no_match"  // SUP/LIR for a code not in the list
	OutcomeAmbiguous Outcome = "ambiguous" // several countries share the code and none matched by name
)

// Applied is a Change with what happened when it was replayed.
type Applied struct {
	Change
	Outcome Outcome
	Before  string // operator before the change, if any
}

// Registry is the current MNC list: the base list plus replayed amendments.
type Registry struct {
	byCode map[string][]*Network // PLMN -> entries (a code can appear under several countries)
	et     []ExtraTerritorial
}

// NewRegistry seeds a registry from the base list.
func NewRegistry(base []Network, et []ExtraTerritorial) *Registry {
	r := &Registry{byCode: map[string][]*Network{}, et: append([]ExtraTerritorial(nil), et...)}
	for i := range base {
		n := base[i]
		r.byCode[n.PLMN()] = append(r.byCode[n.PLMN()], &n)
	}
	return r
}

// countryKey normalizes a country name for matching amendment rows to base
// rows ("Korea (Rep. of)" vs "Korea (Republic of)", trailing footnote marks).
func countryKey(s string) string {
	s = strings.ToLower(strings.TrimRight(normSpace(s), "* "))
	if i := strings.IndexAny(s, "(,"); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// find picks the entry for code that the change refers to.
func (r *Registry) find(c Change) (idx int, ok, ambiguous bool) {
	list := r.byCode[c.MCC+c.MNC]
	if len(list) == 0 {
		return -1, false, false
	}
	key := countryKey(c.Country)
	for i, n := range list {
		if countryKey(n.Country) == key {
			return i, true, false
		}
	}
	if len(list) == 1 {
		return 0, true, false
	}
	return -1, false, true
}

// Apply replays one change and reports what it did.
func (r *Registry) Apply(c Change) Applied {
	if c.ExtraTerritorial {
		return r.applyET(c)
	}
	plmn := c.MCC + c.MNC
	country := c.Country
	if c.MCC == "901" {
		country = SharedGroup
	}
	a := Applied{Change: c}
	i, ok, amb := r.find(c)
	switch c.Action {
	case ActionAdd:
		n := &Network{MCC: c.MCC, MNC: c.MNC, Country: country, Operator: c.Operator,
			Kind: kindFor(c.MCC), SourceOB: c.OB, AddedAmendment: c.Amendment}
		if ok {
			a.Before = r.byCode[plmn][i].Operator
			r.byCode[plmn][i] = n
			a.Outcome = OutcomeReplaced
		} else {
			r.byCode[plmn] = append(r.byCode[plmn], n)
			a.Outcome = OutcomeApplied
		}
	case ActionSup:
		switch {
		case ok:
			a.Before = r.byCode[plmn][i].Operator
			r.byCode[plmn] = append(r.byCode[plmn][:i], r.byCode[plmn][i+1:]...)
			if len(r.byCode[plmn]) == 0 {
				delete(r.byCode, plmn)
			}
			a.Outcome = OutcomeApplied
		case amb:
			a.Outcome = OutcomeAmbiguous
		default:
			a.Outcome = OutcomeNoMatch
		}
	case ActionLIR:
		switch {
		case ok:
			n := r.byCode[plmn][i]
			a.Before = n.Operator
			if c.Operator != "" {
				n.Operator = c.Operator
			}
			n.SourceOB, n.ChangedAmendment = c.OB, c.Amendment
			a.Outcome = OutcomeApplied
		case amb:
			a.Outcome = OutcomeAmbiguous
		default:
			// A change to an entry we don't have: record it as an addition
			// so the list reflects ITU's current position.
			r.byCode[plmn] = append(r.byCode[plmn], &Network{MCC: c.MCC, MNC: c.MNC, Country: country,
				Operator: c.Operator, Kind: kindFor(c.MCC), SourceOB: c.OB, ChangedAmendment: c.Amendment})
			a.Outcome = OutcomeNoMatch
		}
	}
	return a
}

// applyET replays a change to the extraterritorial list. Entries are keyed by
// country (A/B side) and MCC+MNC; ADD of an existing key replaces it.
func (r *Registry) applyET(c Change) Applied {
	a := Applied{Change: c}
	idx := -1
	for i, e := range r.et {
		if e.MCC == c.MCC && e.MNC == c.MNC && strings.EqualFold(normSpace(e.Country), normSpace(c.Country)) {
			idx = i
			break
		}
	}
	e := ExtraTerritorial{Country: c.Country, MCC: c.MCC, MNC: c.MNC, MSINRange: c.MSINRange,
		Operator: c.Operator, Amendment: c.Amendment}
	switch {
	case c.Action == ActionSup && idx >= 0:
		a.Before = r.et[idx].MSINRange
		r.et = append(r.et[:idx], r.et[idx+1:]...)
		a.Outcome = OutcomeApplied
	case c.Action == ActionSup:
		a.Outcome = OutcomeNoMatch
	case idx >= 0:
		a.Before = r.et[idx].MSINRange
		r.et[idx] = e
		a.Outcome = OutcomeApplied
		if c.Action == ActionAdd {
			a.Outcome = OutcomeReplaced
		}
	default:
		r.et = append(r.et, e)
		a.Outcome = OutcomeApplied
		if c.Action == ActionLIR {
			a.Outcome = OutcomeNoMatch
		}
	}
	return a
}

// ExtraTerritorial returns the current extraterritorial list.
func (r *Registry) ExtraTerritorial() []ExtraTerritorial {
	return append([]ExtraTerritorial(nil), r.et...)
}

// Networks returns the current list sorted by MCC, MNC, country.
func (r *Registry) Networks() []Network {
	var out []Network
	for _, l := range r.byCode {
		for _, n := range l {
			out = append(out, *n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MCC != out[j].MCC {
			return out[i].MCC < out[j].MCC
		}
		if out[i].MNC != out[j].MNC {
			return out[i].MNC < out[j].MNC
		}
		return out[i].Country < out[j].Country
	})
	return out
}
