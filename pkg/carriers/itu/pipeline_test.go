package itu

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
)

// fakeITU serves a tiny version of ITU's site: the E.212B landing page and
// annex, the OB index, per-bulletin landing pages (with the year quirks and a
// zip edition), and a two-page 901 database.
type fakeITU struct {
	t        *testing.T
	mu       sync.Mutex
	requests map[string]int
	files    map[string][]byte
}

func newFakeITU(t *testing.T) *fakeITU {
	f := &fakeITU{t: t, requests: map[string]int{}, files: map[string][]byte{}}
	f.files["/dms_pub/itu-t/opb/sp/T-SP-E.212B-2023-MSW-E.docx"] = docxBytes(t, baseListBlocks()...)
	// OB 1281: no MNC amendment.
	f.files["/dms_pub/itu-t/opb/sp/T-SP-OB.1281-2023-OAS-MSW-E.docx"] = docxBytes(t,
		append([]block{tbl(row("ITU Operational Bulletin"), row("No. 1281", "1.XII.2023"))}, otherLists()...)...)
	// OB 1282: amendment 1.
	f.files["/dms_pub/itu-t/opb/sp/T-SP-OB.1282-2023-OAS-MSW-E.docx"] = docxBytes(t,
		tbl(row("ITU Operational Bulletin"), row("No. 1282", "15.XII.2023", "(Information received by 1 December 2023)")),
		p("Mobile Network Codes (MNC) for the international identification plan"),
		p("(Annex to ITU Operational Bulletin No. 1280 - 15.XI.2023)(Amendment No. 1)"),
		tbl(
			row("Country/Geographical area", "MCC+MNC", "Operator/Network"),
			row("Estonia    ADD", "", ""), row("", "248 33", "Crowdfaster OÜ"),
			row("Estonia    SUP", "", ""), row("", "248 29", "SkyTel OÜ"),
		),
		p("____"),
	)
	// OB 1283 is dated 2024 and only published as a zip with an annex.
	f.files["/dms_pub/itu-t/opb/sp/T-SP-OB.1283-2024-OAS-ZIP-E.zip"] = zipFiles(t, map[string][]byte{
		"Annex-E_OB1283.docx": docxBytes(t, p("STATUS OF RADIOCOMMUNICATIONS BETWEEN AMATEUR STATIONS")),
		"1283E.docx": docxBytes(t,
			p("ITU Operational Bulletin No. 1283 (1 January 2024)"),
			p("Network Codes (MNC) for the international identification plan"),
			p("(Annex to ITU Operational Bulletin No. 1280 – 15.XI.2023)\n(Amendment No. 2)"),
			tbl(row("Country/Geographical area", "MCC+MNC", "Operator/Network"),
				row("Estonia    LIR", "", ""), row("", "248 33", "J-Mobile OÜ"),
				row("United States    ADD", "", ""), row("", "311 910", "Google LLC")),
			p("Extra-territorial use*** of MCC/MNC, Annex E to Recommendation ITU-T E.212"),
			tbl(row("Country", "MCC+MNC*", "MSIN** range to be used", "Operators Name"),
				row("Germany\n(Country A)", "262 74", "0-1", "Ericsson GmbH")),
			p("____"),
		),
	})
	return f
}

func (f *fakeITU) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests[r.Method+" "+r.URL.Path]++
	f.mu.Unlock()
	if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Mozilla/5.0 (compatible; proto-telephony") {
		f.t.Errorf("unexpected User-Agent %q", ua)
	}
	path := r.URL.Path
	switch {
	case path == "/pub/T-SP-E.212B":
		fmt.Fprint(w, `<a href="publications.aspx?parent=T-SP-E.212B-2018">x</a><a href="?parent=T-SP-E.212B-2023">y</a>`)
	case path == "/pub/T-SP-OB":
		fmt.Fprint(w, `<a href="/pub/T-SP-OB.1282-2023">1282</a><a href="/pub/T-SP-OB.1283-2024">1283</a>`)
	case strings.HasPrefix(path, "/pub/T-SP-OB."):
		name := strings.TrimPrefix(path, "/pub/")
		var links []string
		for k := range f.files {
			if strings.Contains(k, name+"-OAS-") {
				links = append(links, fmt.Sprintf(`<a href="%s">docx</a>`, k))
			}
		}
		// Like ITU: unknown publications get a 200 page with no files.
		fmt.Fprintf(w, `<html>%s<a href="/dms_pub/itu-s/opb/gen/S-GEN-CAT.OL-2025-PDF-E.pdf">cat</a></html>`, strings.Join(links, ""))
	case path == Shared901Path:
		page := 1
		if r.Method == http.MethodPost {
			r.ParseForm()
			if r.Form.Get("__VIEWSTATE") != "vs1" || r.Form.Get("__EVENTARGUMENT") != "Page$2" {
				http.Error(w, "bad postback", http.StatusBadRequest)
				return
			}
			page = 2
		}
		fmt.Fprint(w, grid901(page))
	default:
		data, ok := f.files[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}
}

func grid901(page int) string {
	type r struct{ code, op, status, assigned string }
	rows := map[int][]r{
		1: {{"901 03", "Iridium Communications Inc", "ASSIGNED", "1997-11-10"}, {"901 95", "HMD Global Oy", "RECLAIMED", "2016-01-01"}},
		2: {{"901 42", "Net Feasa Limited", "ASSIGNED", "2026-10-14"}},
	}[page]
	var b strings.Builder
	b.WriteString(`<input type="hidden" name="__VIEWSTATE" id="__VIEWSTATE" value="vs1" />`)
	b.WriteString(`<span id="ContentPlaceHolder1_lbl_total" style="font-weight:bold;">3</span>`)
	for i, x := range rows {
		fmt.Fprintf(&b, `<span id="ContentPlaceHolder1_GridView1_lblMCCMNC_%d">%s</span>`, i, x.code)
		fmt.Fprintf(&b, `<span id="ContentPlaceHolder1_GridView1_lblSPOperator_%d">%s</span>`, i, x.op)
		fmt.Fprintf(&b, `<span id="ContentPlaceHolder1_GridView1_lblStatut_%d">%s</span>`, i, x.status)
		fmt.Fprintf(&b, `<span id="ContentPlaceHolder1_GridView1_lblStartReservation_%d">-</span>`, i)
		fmt.Fprintf(&b, `<span id="ContentPlaceHolder1_GridView1_lblAssignment_%d">%s</span>`, i, x.assigned)
		fmt.Fprintf(&b, `<span id="ContentPlaceHolder1_GridView1_lblReclaim_%d">-</span>`, i)
	}
	if page == 1 {
		b.WriteString(`<a href="javascript:__doPostBack(&#39;ctl00$ContentPlaceHolder1$GridView1&#39;,&#39;Page$2&#39;)">2</a>`)
	}
	return b.String()
}

type nopReporter struct {
	warns []string
}

func (*nopReporter) Stage(int, int, string)                                {}
func (*nopReporter) Info(string, ...any)                                   {}
func (r *nopReporter) Warn(f string, a ...any)                             { r.warns = append(r.warns, fmt.Sprintf(f, a...)) }
func (*nopReporter) Bulletin(int, int, carriers.Source, *Amendment, error) {}

func TestBuildEndToEnd(t *testing.T) {
	fake := newFakeITU(t)
	srv := httptest.NewServer(fake)
	defer srv.Close()

	dir := t.TempDir()
	c := NewClient(filepath.Join(dir, "cache"))
	c.Base = srv.URL
	rep := &nopReporter{}
	res, err := Build(context.Background(), c, Options{Workers: 2}, rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.warns) != 0 {
		t.Errorf("warnings: %q", rep.warns)
	}
	if res.FirstOB != 1281 || res.LastOB != 1283 || res.Bulletins != 3 || len(res.Missing) != 0 {
		t.Errorf("bulletins: first %d last %d scanned %d missing %v", res.FirstOB, res.LastOB, res.Bulletins, res.Missing)
	}
	if res.Outcomes[OutcomeApplied] != 5 || len(res.Amendments) != 5 {
		t.Errorf("outcomes %v, %d changes", res.Outcomes, len(res.Amendments))
	}
	if len(res.SharedCodes) != 3 {
		t.Errorf("shared codes: %+v", res.SharedCodes)
	}

	// The same queries must hold for the SQLite file and for the CSV
	// directory loaded back (which also checks NULLs survive as NULLs).
	dbPath := filepath.Join(dir, "carriers.sqlite")
	if err := store.WriteSQLite(dbPath, Schema, Tables(&res.Dataset)); err != nil {
		t.Fatal(err)
	}
	sdb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	csvDir := filepath.Join(dir, "carriers")
	if err := store.WriteCSV(csvDir, Tables(&res.Dataset)); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteCSV(csvDir, Tables(&res.Dataset)); err != nil { // rewriting replaces it
		t.Fatalf("rewrite: %v", err)
	}
	cdb, err := store.ReadCSV(csvDir, Schema, Specs())
	if err != nil {
		t.Fatal(err)
	}
	defer cdb.Close()
	for name, db := range map[string]*sql.DB{"sqlite": sdb, "csv": cdb} {
		for query, want := range map[string]string{
			`SELECT operator FROM networks WHERE plmn='310170'`:                                                            "AT&T Mobility",
			`SELECT operator FROM networks WHERE plmn='24833'`:                                                             "J-Mobile OÜ",
			`SELECT added_amendment || '/' || changed_amendment FROM networks WHERE plmn='24833'`:                          "1/2",
			`SELECT count(*) FROM networks WHERE added_amendment IS NULL`:                                                  "10",
			`SELECT count(*) FROM networks WHERE plmn='24829'`:                                                             "0",
			`SELECT operator || '/' || source_ob FROM networks WHERE plmn='311910'`:                                        "Google LLC/1283",
			`SELECT count(*) FROM networks`:                                                                                "12",
			`SELECT count(*) FROM extraterritorial`:                                                                        "3",
			`SELECT msin_range FROM extraterritorial WHERE amendment=2`:                                                    "0-1",
			`SELECT count(*) FROM amendments WHERE outcome='applied'`:                                                      "5",
			`SELECT group_concat(status) FROM (SELECT status FROM shared_codes ORDER BY mnc)`:                              "ASSIGNED,ASSIGNED,RECLAIMED",
			`SELECT count(*) FROM sources`:                                                                                 "5",
			`SELECT value FROM meta WHERE key='last_amendment'`:                                                            "2",
			`SELECT country FROM mcc WHERE mcc='901'`:                                                                      SharedGroup,
			`SELECT group_concat(DISTINCT ob_date) FROM amendments`:                                                        "2023-12-15,2024-01-01",
			`SELECT count(*) FROM assignments`:                                                                             "13",
			`SELECT assigned_date || '|' || country || '|' || as_of FROM assignments WHERE plmn='311910'`:                  "2024-01-01|US|2024-01-01",
			`SELECT country || '|' || coalesce(assigned_date,'NULL') FROM assignments WHERE plmn='20801' AND country='MC'`: "MC|NULL",
			`SELECT status || '|' || country FROM assignments WHERE plmn='90142'`:                                          "assigned|ZZ",
			`SELECT count(*) FROM sub_allocations WHERE kind='msin_range'`:                                                 "3",
		} {
			var got sql.NullString
			if err := db.QueryRow(query).Scan(&got); err != nil {
				t.Fatalf("%s: %s: %v", name, query, err)
			}
			if got.String != want {
				t.Errorf("%s: %s = %q, want %q", name, query, got.String, want)
			}
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 3 { // cache, carriers.sqlite, carriers/
		t.Errorf("leftover temp output: %v", entries)
	}

	// A second run reads every immutable document from the cache.
	before := fake.requests["GET /dms_pub/itu-t/opb/sp/T-SP-OB.1282-2023-OAS-MSW-E.docx"]
	c2 := NewClient(filepath.Join(dir, "cache"))
	c2.Base = srv.URL
	if _, err := Build(context.Background(), c2, Options{Skip901: true}, rep); err != nil {
		t.Fatal(err)
	}
	if after := fake.requests["GET /dms_pub/itu-t/opb/sp/T-SP-OB.1282-2023-OAS-MSW-E.docx"]; after != before {
		t.Errorf("bulletin refetched despite cache (%d -> %d)", before, after)
	}
	if hits := c2.Stats.CacheHits.Load(); hits != 4 {
		t.Errorf("cache hits = %d, want 4 (base + 3 bulletins)", hits)
	}

	// Offline runs work from the cache alone.
	c3 := NewClient(filepath.Join(dir, "cache"))
	c3.Base, c3.Offline = "http://127.0.0.1:1", true
	res3, err := Build(context.Background(), c3, Options{}, rep)
	if err != nil {
		t.Fatalf("offline: %v", err)
	}
	if len(res3.Networks) != 12 || c3.Stats.Requests.Load() != 0 {
		t.Errorf("offline: %d networks, %d requests", len(res3.Networks), c3.Stats.Requests.Load())
	}
}

func TestFetchOBNotFound(t *testing.T) {
	srv := httptest.NewServer(newFakeITU(t))
	defer srv.Close()
	c := NewClient("")
	c.Base = srv.URL
	if _, _, err := c.FetchOB(context.Background(), 1999, []int{2026, 2027}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found, got %v", err)
	}
}
