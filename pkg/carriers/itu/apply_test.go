package itu

import "testing"

func TestRegistryApply(t *testing.T) {
	bl, err := ParseBaseList(mustDoc(t, baseListBlocks()...))
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(bl.Networks, bl.ExtraTerritorial)
	ch := func(action Action, country, code, op string) Change {
		mcc, mnc, _ := SplitCode(code)
		return Change{Amendment: 7, OB: 1290, Action: action, Country: country, MCC: mcc, MNC: mnc, Operator: op}
	}
	for _, tc := range []struct {
		c      Change
		want   Outcome
		before string
	}{
		{ch(ActionAdd, "Estonia", "248 33", "Crowdfaster OÜ"), OutcomeApplied, ""},
		{ch(ActionLIR, "Estonia", "248 33", "J-Mobile OÜ (formerly Crowdfaster OÜ)"), OutcomeApplied, "Crowdfaster OÜ"},
		{ch(ActionSup, "Estonia", "248 29", "SkyTel OÜ"), OutcomeApplied, "SkyTel OÜ"},
		{ch(ActionSup, "Estonia", "248 29", "SkyTel OÜ"), OutcomeNoMatch, ""},
		// Shared codes: the amendment's group name differs from nothing in particular.
		{ch(ActionSup, "International Mobile, shared code", "901 95", "HMD Global Oy"), OutcomeApplied, "HMD Global Oy"},
		// 208 01 is under France and Monaco: pick by country, ignoring "(Rep. of)" style suffixes.
		{ch(ActionLIR, "Monaco (Principality of)", "208 01", "Monaco Telecom"), OutcomeApplied, "Orange/Monaco Télécom"},
		{ch(ActionSup, "Andorra", "208 01", ""), OutcomeAmbiguous, ""},
		// Re-adding an existing code replaces it.
		{ch(ActionAdd, "United States", "310 170", "AT&T Mobility LLC"), OutcomeReplaced, "AT&T Mobility"},
		// A change to an entry we never saw is kept, flagged.
		{ch(ActionLIR, "Nauru", "536 03", "Nauru Telikom"), OutcomeNoMatch, ""},
	} {
		got := r.Apply(tc.c)
		if got.Outcome != tc.want || got.Before != tc.before {
			t.Errorf("%s %s %s%s: got %s (before %q), want %s (before %q)", tc.c.Action, tc.c.Country,
				tc.c.MCC, tc.c.MNC, got.Outcome, got.Before, tc.want, tc.before)
		}
	}
	nets := map[string]Network{}
	for _, n := range r.Networks() {
		nets[n.PLMN()+"/"+n.Country] = n
	}
	if n := nets["24833/Estonia"]; n.Operator != "J-Mobile OÜ (formerly Crowdfaster OÜ)" || n.AddedAmendment != 7 || n.ChangedAmendment != 7 {
		t.Errorf("248 33: %+v", n)
	}
	if _, ok := nets["24829/Estonia"]; ok {
		t.Error("248 29 should be suppressed")
	}
	if _, ok := nets["90195/"+SharedGroup]; ok {
		t.Error("901 95 should be suppressed")
	}
	if n := nets["20801/Monaco"]; n.Operator != "Monaco Telecom" {
		t.Errorf("208 01 Monaco: %+v", n)
	}
	if n := nets["20801/France"]; n.Operator != "Orange" {
		t.Errorf("208 01 France changed: %+v", n)
	}
	if n := nets["53603/Nauru"]; n.Operator != "Nauru Telikom" {
		t.Errorf("536 03: %+v", n)
	}
	// Sorted by MCC, MNC.
	ns := r.Networks()
	for i := 1; i < len(ns); i++ {
		a, b := ns[i-1], ns[i]
		if a.MCC > b.MCC || a.MCC == b.MCC && a.MNC > b.MNC {
			t.Fatalf("not sorted at %d: %s, %s", i, ns[i-1].PLMN(), ns[i].PLMN())
		}
	}
}

func TestRegistryExtraTerritorial(t *testing.T) {
	r := NewRegistry(nil, []ExtraTerritorial{
		{Country: "France - Country A", MCC: "208", MNC: "01", MSINRange: "0-1", Operator: "Orange"},
	})
	et := func(action Action, country, msin string) Change {
		return Change{Amendment: 13, Action: action, Country: country, MCC: "208", MNC: "01",
			MSINRange: msin, Operator: "Orange", ExtraTerritorial: true}
	}
	for _, tc := range []struct {
		c    Change
		want Outcome
	}{
		{et(ActionAdd, "Monaco - Country B", "2-3"), OutcomeApplied},
		{et(ActionLIR, "France - Country A", "0-0"), OutcomeApplied},
		{et(ActionAdd, "Monaco - Country B", "2-4"), OutcomeReplaced},
		{et(ActionSup, "Andorra - Country B", ""), OutcomeNoMatch},
	} {
		if got := r.Apply(tc.c); got.Outcome != tc.want {
			t.Errorf("%s %s: got %s want %s", tc.c.Action, tc.c.Country, got.Outcome, tc.want)
		}
	}
	got := r.ExtraTerritorial()
	if len(got) != 2 || got[0].MSINRange != "0-0" || got[1].MSINRange != "2-4" || got[1].Amendment != 13 {
		t.Fatalf("extraterritorial: %+v", got)
	}
	if len(r.Networks()) != 0 {
		t.Error("extraterritorial changes must not touch networks")
	}
}
