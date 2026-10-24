package imsiadmin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// fakeSite serves directories, the REST index (paged at perPage) and detail
// pages, with modified times the test can change.
type fakeSite struct {
	srv      *httptest.Server
	mu       sync.Mutex
	modified map[string]string // path -> modified_gmt
	hits     map[string]int
	nHNI     int
}

func newFakeSite(t *testing.T, nHNI int) *fakeSite {
	f := &fakeSite{modified: map[string]string{}, hits: map[string]int{}, nHNI: nHNI}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	for i := 0; i < nHNI; i++ {
		f.modified[hniPath(i)] = "2026-01-01T00:00:00"
	}
	f.modified["/cbrs-codes/westnetworks-315-010-0152/"] = "2026-01-01T00:00:00"
	return f
}

// Codes run 310-000, 310-010, ...; 310-170 is AT&T, the last one is returned.
func hniCode(i int) string { return fmt.Sprintf("31%d-%03d", i/100, (i%100)*10) }
func hniPath(i int) string { return "/hni-codes/entity-" + hniCode(i) + "/" }

func (f *fakeSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits[r.URL.Path]++
	base := f.srv.URL
	switch p := r.URL.Path; {
	case p == "/assignments/hni/":
		var b strings.Builder
		b.WriteString(`<table class="sortable"><tr><th>HNI-Code</th><th>Entity</th></tr>`)
		for i := 0; i < f.nHNI; i++ {
			entity := "Entity " + hniCode(i)
			switch {
			case hniCode(i) == "310-170":
				entity = "AT&amp;T Mobility"
			case i == f.nHNI-1:
				entity = "RETURNED 9/9/24 (" + hniCode(i) + ")"
			}
			fmt.Fprintf(&b, `<tr><td><a href="%s%s">%s</a></td><td>%s</td></tr>`, base, hniPath(i), hniCode(i), entity)
		}
		b.WriteString(`</table>`)
		fmt.Fprint(w, b.String())
	case p == "/assignments/cbrs/":
		fmt.Fprintf(w, `<table class="sortable"><tr><th>HNI-Code</th><th>IBN</th><th>Entity</th></tr>`+
			`<tr><td>315-010</td><td><a href="%s/cbrs-codes/westnetworks-315-010-0152/">0152</a></td><td>WestNetworks</td></tr></table>`, base)
	case strings.HasPrefix(p, "/wp-json/wp/v2/"):
		typ := strings.TrimPrefix(p, "/wp-json/wp/v2/")
		page := 1
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		var all []map[string]string
		for path, mod := range f.modified {
			if strings.HasPrefix(path, "/"+typ+"/") {
				all = append(all, map[string]string{"link": base + path, "modified_gmt": mod})
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i]["link"] < all[j]["link"] }) // stable pages
		lo, hi := min((page-1)*perPage, len(all)), min(page*perPage, len(all))
		json.NewEncoder(w).Encode(all[lo:hi])
	case strings.HasPrefix(p, "/hni-codes/"):
		code := strings.TrimSuffix(strings.TrimPrefix(p, "/hni-codes/entity-"), "/")
		name := "Entity " + code
		if code == "310-170" {
			name = "AT&amp;T Mobility"
		}
		fmt.Fprintf(w, `<h1 class="entry-title">%s (%s)</h1><div class="entry-content"><h2>Code Details</h2>`+
			`<div><strong>HNI Code:</strong> %s</div><div><strong>Wireless License Number:</strong> KNKA382</div>`+
			`<div><strong>Effective Date:</strong> June 7, 1995</div><div><strong>Protocol:</strong> LTE, 5G</div>`+
			`<h2>Entity Details</h2><div><strong>Entity Name:</strong> %s</div>`+
			`<div><strong>Entity Type:</strong> Public Network Operator</div><div><strong>Fax:</strong> 555</div></div></article>`,
			name, code, code, name)
	case strings.HasPrefix(p, "/cbrs-codes/"):
		fmt.Fprint(w, `<h1 class="entry-title">WestNetworks (315-010-0152)</h1><div class="entry-content"><h2>Code Details</h2>`+
			`<div><strong>HNI Code:</strong> 315-010</div><div><strong>IBN:</strong> 0152</div>`+
			`<div><strong>Application Date:</strong> 08/28/2026</div><h2>Entity Details</h2>`+
			`<div><strong>Entity Name:</strong> WestNetworks</div></div></article>`)
	default:
		http.NotFound(w, r)
	}
}

type testReporter struct {
	mu    sync.Mutex
	warns []string
}

func (*testReporter) Stage(int, int, string) {}
func (*testReporter) Info(string, ...any)    {}
func (r *testReporter) Warn(f string, a ...any) {
	r.mu.Lock()
	r.warns = append(r.warns, fmt.Sprintf(f, a...))
	r.mu.Unlock()
}
func (*testReporter) Code(int, int, *Record, carriers.Source, error) {}

func TestBuildEndToEnd(t *testing.T) {
	const n = 120 // > perPage, so the REST index pages
	site := newFakeSite(t, n)
	dir := t.TempDir()
	newClient := func() *Client {
		c := NewClient(filepath.Join(dir, "cache"))
		c.Base = site.srv.URL
		return c
	}
	rep := &testReporter{}
	res, err := Build(context.Background(), newClient(), Options{Workers: 3}, rep)
	if err != nil {
		t.Fatal(err)
	}
	// The fake detail pages carry one field we don't know ("Fax"): warned about, but kept.
	if len(rep.warns) != n || !strings.Contains(rep.warns[0], `unknown field "Fax"`) {
		t.Errorf("warnings (%d): %q", len(rep.warns), rep.warns[:min(3, len(rep.warns))])
	}
	if res.Kinds[KindHNI] != n || res.Kinds[KindCBRS] != 1 || res.Fetched != n+1 {
		t.Errorf("kinds %v fetched %d", res.Kinds, res.Fetched)
	}

	out := store.Output{Format: "csv", Path: filepath.Join(dir, "us")}
	if err := out.Write(Schema, Tables(&res.Dataset)); err != nil {
		t.Fatal(err)
	}
	db, err := store.ReadCSV(out.Path, Schema, Specs())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for query, want := range map[string]string{
		`SELECT count(*) FROM hni`: fmt.Sprint(n),
		`SELECT entity_name || '|' || effective_date || '|' || entity_category FROM hni WHERE hni='310-170'`:                                           "AT&T Mobility|1995-06-07|public_network_operator",
		`SELECT status || '|' || status_date || '|' || coalesce(related, 'NULL') || '|' || coalesce(entity_name, 'NULL') FROM hni WHERE hni='311-190'`: "returned|2024-09-09|NULL|Entity 311-190",
		`SELECT code || '|' || imsi_prefix || '|' || application_date FROM cbrs`:                                                                       "315-010-0152|3150100152|2026-08-28",
		`SELECT count(*) FROM details WHERE label='Fax'`:                                                                                               fmt.Sprint(n),
		`SELECT date FROM details WHERE code='310-170' AND label='Effective Date'`:                                                                     "1995-06-07",
		`SELECT group_concat(protocol) FROM (SELECT protocol FROM protocols WHERE code='310-170' ORDER BY protocol)`:                                   "5G,LTE/LTE Advanced",
		`SELECT type || ':' || value FROM identifiers WHERE code='310-170'`:                                                                            "call_sign:KNKA382",
	} {
		var got sql.NullString
		if err := db.QueryRow(query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if got.String != want {
			t.Errorf("%s = %q, want %q", query, got.String, want)
		}
	}
	checks, err := store.Verify(out, Schema, Specs(), Checks)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		// The fake site is smaller than the real one; only these must pass.
		if c.Name == "310-170 is AT&T" || c.Name == "every code has details" || c.Name == "MCCs are 310-316" {
			if !c.OK {
				t.Errorf("check %s failed: %s", c.Name, c.Got)
			}
		}
	}

	// Second run: nothing changed, so no detail page is refetched...
	site.mu.Lock()
	before := site.hits[hniPath(5)]
	site.modified[hniPath(7)] = "2026-02-02T00:00:00" // ...except one that was edited.
	site.mu.Unlock()
	res2, err := Build(context.Background(), newClient(), Options{}, &testReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Fetched != 1 || site.hits[hniPath(5)] != before || site.hits[hniPath(7)] != 2 {
		t.Errorf("incremental: fetched %d, unchanged page hits %d->%d, edited page hits %d",
			res2.Fetched, before, site.hits[hniPath(5)], site.hits[hniPath(7)])
	}

	// Offline: everything from the cache.
	c := newClient()
	c.Offline = true
	res3, err := Build(context.Background(), c, Options{}, &testReporter{})
	if err != nil {
		t.Fatalf("offline: %v", err)
	}
	if len(res3.Records) != n+1 || c.Stats.Requests.Load() != 0 {
		t.Errorf("offline: %d records, %d requests", len(res3.Records), c.Stats.Requests.Load())
	}
}
