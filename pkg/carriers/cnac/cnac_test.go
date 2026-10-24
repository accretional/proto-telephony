package cnac

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

const sample = "\ufeff" + `"MCC","MNC","Status","Company","Date Assigned","Remarks"
,,,,,2026-10-23
302,060,In Service,"Karrier One Inc.","2024-12-23",
302,100,Recovered/Aging,,,"Recovered from Data on Tap Inc."
302,220,In Service,"Telus Mobility",,
302,490,In Service,"Freedom Mobile Inc.",,"Formerly Wind Mobile"
302,720,In Service,"Rogers Communications Canada Inc. (Wireless)",,
302,991,In Service,"Halton Regional Police Service","2019-04-16","Experimental Purposes iaw the Guideline"
302,999,Assigned,"Some  Co",,
`

func TestParse(t *testing.T) {
	l, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if l.AsOf != "2026-10-23" || len(l.MNCs) != 7 {
		t.Fatalf("as of %q, %d rows", l.AsOf, len(l.MNCs))
	}
	got := map[string]MNC{}
	for _, m := range l.MNCs {
		got[m.MNC] = m
	}
	for mnc, want := range map[string]MNC{
		"060": {MCC: "302", MNC: "060", StatusRaw: "In Service", Company: "Karrier One Inc.", DateAssigned: "2024-12-23"},
		"100": {MCC: "302", MNC: "100", StatusRaw: "Recovered/Aging", Remarks: "Recovered from Data on Tap Inc.", RecoveredFrom: "Data on Tap Inc."},
		"490": {MCC: "302", MNC: "490", StatusRaw: "In Service", Company: "Freedom Mobile Inc.", Remarks: "Formerly Wind Mobile", Formerly: "Wind Mobile"},
		"991": {MCC: "302", MNC: "991", StatusRaw: "In Service", Company: "Halton Regional Police Service", DateAssigned: "2019-04-16",
			Remarks: "Experimental Purposes iaw the Guideline", Experimental: true},
		"999": {MCC: "302", MNC: "999", StatusRaw: "Assigned", Company: "Some Co"},
	} {
		if got[mnc] != want {
			t.Errorf("%s:\n got %+v\nwant %+v", mnc, got[mnc], want)
		}
	}
	if got["100"].Status() != "reclaimed" || got["999"].Status() != "assigned" || got["220"].Status() != "assigned" {
		t.Error("status mapping")
	}
	for _, bad := range []string{`"MCC","MNC","State"` + "\n", `"MCC","MNC","Status","Company","Date Assigned","Remarks"` + "\n" + `30,1,x,y,,` + "\n"} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("want error for %q", bad)
		}
	}
}

type quiet struct{ warns []string }

func (*quiet) Stage(int, int, string)    {}
func (*quiet) Info(string, ...any)       {}
func (q *quiet) Warn(f string, a ...any) { q.warns = append(q.warns, fmt.Sprintf(f, a...)) }

func TestBuildAndVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != CSVPath {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, sample)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := carriers.NewClient(srv.URL, filepath.Join(dir, "cache"))
	ds, err := Build(context.Background(), c, &quiet{})
	if err != nil {
		t.Fatal(err)
	}
	out := store.Output{Format: "csv", Path: filepath.Join(dir, "ca")}
	if err := out.Write(Schema, Tables(ds)); err != nil {
		t.Fatal(err)
	}
	checks, err := store.Verify(out, Schema, Specs(), Checks)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range checks {
		// The sample is smaller than the real list; the rest must pass.
		if ch.Name != "mncs >= 60" && !ch.OK {
			t.Errorf("check %s failed: %s", ch.Name, ch.Got)
		}
	}
	// Offline rebuild from the cache.
	c2 := carriers.NewClient("http://127.0.0.1:1", filepath.Join(dir, "cache"))
	c2.Offline = true
	if ds2, err := Build(context.Background(), c2, &quiet{}); err != nil || len(ds2.List.MNCs) != 7 {
		t.Fatalf("offline: %v", err)
	}
}
