// Package carrier builds a database of mobile network operators from the
// official ITU-T E.212 Mobile Network Code (MNC) list.
//
// ITU publishes the list as an annex to an Operational Bulletin (OB) — the
// current base edition is T-SP-E.212B-2023, the annex to OB 1280 (position
// on 15 November 2023) — and then updates it through numbered amendments
// printed in every later OB. The list of shared-code (MCC 901) assignments
// is also kept live in ITU's online database. This package fetches all of
// those, parses them, and replays the amendments over the base list.
package carrier

import (
	"fmt"
	"regexp"
	"strings"
)

// Kind classifies an MCC range.
type Kind string

const (
	KindGeographic Kind = "geographic" // MCC assigned to a country/area
	KindShared     Kind = "shared"     // MCC 901, international shared code
	KindROIO       Kind = "roio"       // MCC 902, regional/international organizations
)

// Network is one MCC+MNC assignment.
type Network struct {
	MCC      string
	MNC      string
	Country  string // country/area, or the shared-code group name
	Operator string
	Kind     Kind
	// Provenance: the OB the record comes from and, if it was added or
	// changed by an amendment, that amendment's number.
	SourceOB         int
	AddedAmendment   int // 0 = in the base list
	ChangedAmendment int // last LIR (change) amendment, 0 = none
}

// PLMN is MCC+MNC as one string, e.g. "310170".
func (n Network) PLMN() string { return n.MCC + n.MNC }

// ExtraTerritorial is a row of the base list's table of MCC+MNCs used outside
// their home country (MSIN ranges split between Country A and Country B).
type ExtraTerritorial struct {
	Country   string
	MCC, MNC  string
	MSINRange string
	Operator  string
	Amendment int // 0 = in the base list
}

// Action is an amendment verb as printed by ITU.
type Action string

const (
	ActionAdd Action = "ADD" // add an assignment
	ActionSup Action = "SUP" // suppress (withdraw) an assignment
	ActionLIR Action = "LIR" // "lire" — replace the entry's details
)

// Change is one line of an MNC amendment.
type Change struct {
	Amendment int
	OB        int
	Seq       int // order within the amendment
	Country   string
	Action    Action
	MCC, MNC  string
	Operator  string
	// ExtraTerritorial changes come from an amendment's "Extra-territorial
	// use" table and apply to the extraterritorial list, not the networks.
	ExtraTerritorial bool
	MSINRange        string
}

// Amendment is the MNC-list section of one Operational Bulletin.
type Amendment struct {
	No      int
	OB      int
	BaseOB  int // the OB whose annex it amends (e.g. 1280)
	Changes []Change
}

var codeRE = regexp.MustCompile(`^(\d{3})\s*(\d{2,3})$`)

// SplitCode parses "412 01" / "41201" into MCC "412" and MNC "01".
func SplitCode(s string) (mcc, mnc string, err error) {
	s = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s), "*"))
	m := codeRE.FindStringSubmatch(s)
	if m == nil {
		return "", "", fmt.Errorf("not an MCC+MNC code: %q", s)
	}
	return m[1], m[2], nil
}

// normSpace collapses runs of whitespace (including NBSP) to one space.
func normSpace(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

// kindFor classifies an MCC.
func kindFor(mcc string) Kind {
	switch mcc {
	case "901":
		return KindShared
	case "902":
		return KindROIO
	}
	return KindGeographic
}
