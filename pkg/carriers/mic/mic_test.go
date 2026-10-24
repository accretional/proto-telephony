package mic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/japanese"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
	"github.com/accretional/proto-telephony/pkg/carriers/xlsx/xlsxtest"
)

// workbook is a full 440/441 code space in MIC's layout.
func workbook() []byte {
	rows := [][]any{
		{"", "", ""},
		{"", xlsxtest.Phonetic{Text: "電気通信番号指定状況", Reading: "デンキツウシンバンゴウシテイジョウキョウ"}, ""},
		{"・IMSI", "", ""},
		{"", xlsxtest.Phonetic{Text: "（令和８年９月１日現在）", Reading: "レイワネンガツニチゲンザイ"}, ""},
		{"番号", "事業者", "使用状況"},
	}
	add := func(code, op, st string) { rows = append(rows, []any{code, op, st}) }
	for i := 0; i < 100; i++ {
		switch i {
		case 1:
			add("44001", "ＫＤＤＩ株式会社", "使用中")
		case 10:
			rows = append(rows, []any{"44010", "株式会社ＮＴＴドコモ", xlsxtest.Phonetic{Text: "使用中", Reading: "シヨウチュウ"}})
		default:
			add(fmt.Sprintf("440%02d", i), "", "未使用")
		}
	}
	for i := 0; i < 900; i++ {
		if i == 5 {
			add("441005", "Ｗｉｒｅｌｅｓｓ　Ｃｉｔｙ　Ｐｌａｎｎｉｎｇ株式会社", "使用中")
			continue
		}
		add(fmt.Sprintf("441%03d", i), "", "未使用")
	}
	return xlsxtest.Workbook("LST", rows)
}

func sjis(t *testing.T, s string) []byte {
	b, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const index = `<html><body>
<table><tr><td>IMSI</td><td>441214</td><td>ヴルーヴ株式会社</td><td>令和8年8月10日</td></tr>
<tr><td>050</td><td>05012</td><td>某社</td><td>令和8年8月1日</td></tr></table>
<p>9. IMSI [<a href="/main_content/001090875.pdf">PDF形式</a> 、<a href="/main_content/001090876.xlsx">Excel形式</a>]</p>
<p>10. 事業者設備識別番号 [<a href="/main_content/000697542.xlsx">Excel形式</a>]</p>
</body></html>`

func TestFindFileAndRecent(t *testing.T) {
	page, err := DecodeIndex(sjis(t, index))
	if err != nil {
		t.Fatal(err)
	}
	u, err := FindFile(page, BaseURL+IndexPath)
	if err != nil || u != BaseURL+"/main_content/001090876.xlsx" {
		t.Fatalf("FindFile = %q, %v", u, err)
	}
	rec := RecentDesignations(page)
	if len(rec) != 1 || rec[0] != (Designation{MCC: "441", MNC: "214", Operator: "ヴルーヴ株式会社", Date: "2026-08-10"}) {
		t.Errorf("recent: %+v", rec)
	}
	if _, err := FindFile(`<p>IMSI</p><table><tr><td>IMSI</td></tr></table><a href="/main_content/1.xlsx">x</a>`, BaseURL); err == nil {
		t.Error("want error when no IMSI item has links")
	}
}

func TestReiwaAndNorm(t *testing.T) {
	for in, want := range map[string]string{"（令和８年９月１日現在）": "2026-09-01", "令和元年": "", "令和1年5月1日": "2019-05-01", "x": ""} {
		if got := ReiwaDate(in); got != want {
			t.Errorf("ReiwaDate(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Norm("Ｗｉｒｅｌｅｓｓ　Ｃｉｔｙ　Ｐｌａｎｎｉｎｇ株式会社"); got != "Wireless City Planning株式会社" {
		t.Errorf("Norm = %q", got)
	}
}

func TestParse(t *testing.T) {
	l, err := Parse(workbook())
	if err != nil {
		t.Fatal(err)
	}
	if l.AsOf != "2026-09-01" || len(l.Codes) != 1000 {
		t.Fatalf("as of %q, %d codes", l.AsOf, len(l.Codes))
	}
	byCode := map[string]Code{}
	for _, c := range l.Codes {
		byCode[c.MCC+c.MNC] = c
	}
	if c := byCode["44010"]; c.Operator != "株式会社NTTドコモ" || c.OperatorRaw != "株式会社ＮＴＴドコモ" || c.StatusRaw != "使用中" || c.Status() != "assigned" {
		t.Errorf("44010: %+v", c)
	}
	if c := byCode["441005"]; c.MNC != "005" || c.Operator != "Wireless City Planning株式会社" {
		t.Errorf("441005: %+v", c)
	}
	if c := byCode["441899"]; c.Status() != "available" || c.Operator != "" {
		t.Errorf("441899: %+v", c)
	}
}

type quiet struct{ warns []string }

func (*quiet) Stage(int, int, string)    {}
func (*quiet) Info(string, ...any)       {}
func (q *quiet) Warn(f string, a ...any) { q.warns = append(q.warns, fmt.Sprintf(f, a...)) }

func TestBuildAndVerify(t *testing.T) {
	book, page := workbook(), sjis(t, index)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case IndexPath:
			w.Header().Set("Content-Type", "text/html; charset=Shift_JIS")
			w.Write(page)
		case "/main_content/001090876.xlsx":
			w.Write(book)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := carriers.NewClient(srv.URL, filepath.Join(dir, "cache"))
	rep := &quiet{}
	ds, err := Build(context.Background(), c, rep)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.warns) != 0 {
		t.Errorf("warnings: %q", rep.warns)
	}
	out := store.Output{Format: "csv", Path: filepath.Join(dir, "jp")}
	if err := out.Write(Schema, Tables(ds)); err != nil {
		t.Fatal(err)
	}
	checks, err := store.Verify(out, Schema, Specs(), Checks)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range checks {
		if ch.Name != "in use >= 50" && !ch.OK {
			t.Errorf("check %s failed: %s", ch.Name, ch.Got)
		}
	}
	// The recent designation's date flows into the core table.
	for _, r := range Tables(ds)[2].Rows {
		if r[3] == "441214" && r[9] != "2026-08-10" {
			t.Errorf("441214 assigned_date = %v", r[9])
		}
	}
}
