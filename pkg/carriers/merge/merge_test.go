package merge

import (
	"path/filepath"
	"testing"

	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

func a(src, mcc, mnc, country, holder, status string) store.Assignment {
	return store.Assignment{Source: src, MCC: mcc, MNC: mnc, Country: country, Holder: holder, Status: status, AsOf: "2026-10-01"}
}

// writeSource writes a source's assignments (and sub-allocations) as the
// downloaders do.
func writeSource(t *testing.T, root, src string, rows []store.Assignment, subs []store.SubAllocation) {
	t.Helper()
	tbs := []store.Table{store.AssignmentsTable(rows)}
	if subs != nil {
		tbs = append(tbs, store.SubAllocationsTable(subs))
	}
	if err := store.WriteCSV(filepath.Join(root, src), tbs); err != nil {
		t.Fatal(err)
	}
}

func TestMergeEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "itu", []store.Assignment{
		a("itu", "310", "170", "US", "AT&T Mobility", "assigned"),
		a("itu", "311", "910", "US", "Verizon", "assigned"),       // returned nationally
		a("itu", "314", "620", "US", "Memphis Light", "assigned"), // unknown nationally
		a("itu", "313", "150", "US", "RESERVED FOR PUBLIC SAFETY", "reserved"),
		a("itu", "441", "00", "JP", "Wireless City Planning Inc.", "assigned"), // stale 2-digit form
		a("itu", "302", "680", "CA", "Sask Tel Mobility", "assigned"),
		a("itu", "302", "760", "CA", "Public Mobile Inc", "assigned"),
		a("itu", "208", "01", "FR", "Orange", "assigned"),
		a("itu", "208", "01", "MC", "Orange/Monaco Télécom", "assigned"),
		a("itu", "262", "01", "DE", "Telekom Deutschland GmbH", "assigned"),
	}, []store.SubAllocation{{Source: "itu", PLMN: "20801", Kind: "msin_range", Block: "0-1", Status: "assigned", Country: "MC", AsOf: "x"}})
	writeSource(t, root, "us", []store.Assignment{
		a("us", "310", "170", "US", "AT&T Mobility", "assigned"),
		a("us", "311", "910", "US", "", "returned"),
		a("us", "313", "150", "US", "RESERVED FOR PUBLIC SAFETY", "reserved"),
		a("us", "316", "080", "US", "Talus Communications", "assigned"), // new, not in ITU
	}, []store.SubAllocation{{Source: "us", PLMN: "315010", Kind: "ibn", Block: "0152", Holder: "WestNetworks", Status: "assigned", AsOf: "x"}})
	writeSource(t, root, "jp", []store.Assignment{
		a("jp", "441", "000", "JP", "Wireless City Planning株式会社", "assigned"),
		a("jp", "441", "899", "JP", "", "available"),
	}, nil)
	writeSource(t, root, "ca", []store.Assignment{
		a("ca", "302", "680", "CA", "SaskTel Mobility", "assigned"),
		a("ca", "302", "760", "CA", "Telus Mobility", "assigned"),
	}, nil)

	in, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Assignments) != 18 || len(in.SubAllocations) != 2 || len(in.Files) != 6 {
		t.Fatalf("loaded %d rows, %d subs, files %v", len(in.Assignments), len(in.SubAllocations), in.Files)
	}
	res := Merge(in)

	auth := map[string]string{}
	for _, m := range res.MCCs {
		auth[m.MCC] = m.Authority
	}
	for mcc, want := range map[string]string{"310": "us", "441": "jp", "302": "ca", "208": "itu", "262": "itu"} {
		if auth[mcc] != want {
			t.Errorf("authority %s = %q, want %q", mcc, auth[mcc], want)
		}
	}

	type key struct{ plmn, kind string }
	got := map[key]Conflict{}
	for _, c := range res.Conflicts {
		got[key{c.PLMN, c.Kind}] = c
	}
	want := []key{
		{"311910", "itu_stale"}, {"314620", "itu_only"}, {"316080", "not_in_itu"},
		{"44100", "itu_only"}, {"441000", "not_in_itu"}, {"302760", "holder_differs"},
	}
	if len(res.Conflicts) != len(want) {
		t.Errorf("conflicts: %+v", res.Conflicts)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing conflict %v", k)
		}
	}
	if n := got[key{"44100", "itu_only"}].Note; n == "" {
		t.Error("441 00 should carry an MNC-length note")
	}

	cur := map[string][]Current{}
	for _, c := range res.Current {
		cur[c.MCC+c.MNC] = append(cur[c.MCC+c.MNC], c)
	}
	check := func(plmn, source, flag string) {
		t.Helper()
		rows := cur[plmn]
		if len(rows) == 0 || rows[0].Source != source || rows[0].Flag != flag {
			t.Errorf("current %s = %+v, want source %s flag %q", plmn, rows, source, flag)
		}
	}
	check("310170", "us", "")
	check("311910", "us", "itu_stale")
	check("314620", "itu", "itu_only")
	check("316080", "us", "not_in_itu")
	check("26201", "itu", "")
	if len(cur["20801"]) != 2 {
		t.Errorf("208 01 under both FR and MC: %+v", cur["20801"])
	}
	if len(cur["441899"]) != 0 {
		t.Error("free (available) codes don't belong in current")
	}

	// Through the writer and verifier.
	out := store.Output{Format: "csv", Path: filepath.Join(root, "merged")}
	if err := out.Write(Schema, Tables(res, map[string]string{"tool": "test"})); err != nil {
		t.Fatal(err)
	}
	checks, err := store.Verify(out, Schema, Specs(), Checks)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Name != "current rows >= 2000" && !c.OK {
			t.Errorf("check %s failed: %s", c.Name, c.Got)
		}
	}
	// The merged dir itself is skipped on the next load.
	if in2, err := Load(root); err != nil || len(in2.Assignments) != 18 {
		t.Errorf("reload: %v, %d rows", err, len(in2.Assignments))
	}
}

func TestHoldersDiffer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"SaskTel Mobility", "Sask Tel Mobility", false},
		{"MetroPCS Wireless, Inc", "Metro PCS Wireless Inc", false},
		{"TELEFONOS DE MÉXICO, S.A.B. DE C.V.", "TELÉFONOS DE MÉXICO, S.A.B. DE C.V.", false},
		{"Keewaytinook Okimakinak", "Keewatinook Okimacinac", false},
		{"AT&T Mobility", "AT&T Mobility LLC", false},
		{"Telus Mobility", "Public Mobile Inc", true},
		{"Fido Solutions Inc.", "Microcell", true},
		{"CELMAX MOVIL, S.A. DE C.V.", "MAXCOM TELECOMUNICACIONES, S.A.B. DE C.V.", true},
		{"株式会社NTTドコモ", "NTT DOCOMO, INC.", false}, // different scripts: not compared
		{"", "Anything", false},
	} {
		if got := holdersDiffer(tc.a, tc.b); got != tc.want {
			t.Errorf("holdersDiffer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
