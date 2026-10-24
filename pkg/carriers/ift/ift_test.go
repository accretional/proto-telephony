package ift

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/accretional/proto-telephony/pkg/carriers"
	"github.com/accretional/proto-telephony/pkg/carriers/store"
	"github.com/accretional/proto-telephony/pkg/carriers/xlsx/xlsxtest"
)

func workbook() []byte {
	return xlsxtest.Workbook("MNC", [][]any{
		{"MCC\r\nINDICATIVO DE PAÍS", "MNC\r\nCÓDIGO DE RED MÓVIL", "PST\r\nPROVEEDOR", "OFICIO DE ASIGNACIÓN", "FECHA DE ASIGNACIÓN"},
		{"334", "020", "RADIOMÓVIL DIPSA, S.A. DE C.V.", "CFT/DO4/AGIT/DGPFT/3089/01", 36934},
		{"334", "090", "AT&T COMERCIALIZACIÓN MÓVIL, S. DE R.L. DE C.V.",
			"CFT/D03/USI/DGA/1782/10\r\nIFT/223/UCS/DG-AUSE/6966/17", "11/10/2010\r\n15/12/2017"},
		{"334", "240", "PEGASO PCS, S.A. DE C.V. ", "IFT/223/UCS/2881/2025", 45744},
		{"334", "250", "WAL-MART INNOVACIÓN, S. DE R.L. DE C.V.", "CRT/DG-CAR/8514/2026", 46148, ""},
	})
}

func TestParse(t *testing.T) {
	l, err := Parse(workbook())
	if err != nil {
		t.Fatal(err)
	}
	want := []MNC{
		{MCC: "334", MNC: "020", Holder: "RADIOMÓVIL DIPSA, S.A. DE C.V.", HolderRaw: "RADIOMÓVIL DIPSA, S.A. DE C.V.",
			Letters: []Letter{{"CFT/DO4/AGIT/DGPFT/3089/01", "2001-02-12", "CFT"}}},
		{MCC: "334", MNC: "090", Holder: "AT&T COMERCIALIZACIÓN MÓVIL, S. DE R.L. DE C.V.", HolderRaw: "AT&T COMERCIALIZACIÓN MÓVIL, S. DE R.L. DE C.V.",
			Letters: []Letter{{"CFT/D03/USI/DGA/1782/10", "2010-10-11", "CFT"}, {"IFT/223/UCS/DG-AUSE/6966/17", "2017-12-15", "IFT"}}},
		{MCC: "334", MNC: "240", Holder: "PEGASO PCS, S.A. DE C.V.", HolderRaw: "PEGASO PCS, S.A. DE C.V. ",
			Letters: []Letter{{"IFT/223/UCS/2881/2025", "2025-03-28", "IFT"}}},
		{MCC: "334", MNC: "250", Holder: "WAL-MART INNOVACIÓN, S. DE R.L. DE C.V.", HolderRaw: "WAL-MART INNOVACIÓN, S. DE R.L. DE C.V.",
			Letters: []Letter{{"CRT/DG-CAR/8514/2026", "2026-05-06", "CRT"}}},
	}
	if !reflect.DeepEqual(l.MNCs, want) {
		t.Errorf("rows:\n got %+v\nwant %+v", l.MNCs, want)
	}
	if _, err := Parse(xlsxtest.Workbook("x", [][]any{{"nothing"}})); err == nil {
		t.Error("want error without header")
	}
}

func TestFindFileAndDate(t *testing.T) {
	page := []byte(`<a href="/x.docx">x</a><a href="https://www.ift.org.mx/sites/default/files/mncasignados_06_05_26.xlsx">Consulta de Código de Red Móvil (MNC).</a>`)
	u, err := FindFile(page, BaseURL+IndexPath)
	if err != nil || u != "https://www.ift.org.mx/sites/default/files/mncasignados_06_05_26.xlsx" {
		t.Fatalf("FindFile = %q, %v", u, err)
	}
	if u, _ := FindFile([]byte(`<a href="/sites/default/files/mncasignados_01_12_2027.xlsx">`), "https://crt.example/a/b"); u != "https://crt.example/sites/default/files/mncasignados_01_12_2027.xlsx" {
		t.Errorf("relative link: %q", u)
	}
	if _, err := FindFile([]byte(`<p>moved</p>`), BaseURL); err == nil {
		t.Error("want error without link")
	}
	for in, want := range map[string]string{"mncasignados_06_05_26.xlsx": "2026-05-06", "mncasignados_01_12_2027.xlsx": "2027-12-01", "mnc.xlsx": ""} {
		if got := FileDate(in); got != want {
			t.Errorf("FileDate(%q) = %q, want %q", in, got, want)
		}
	}
}

type quiet struct{ warns []string }

func (*quiet) Stage(int, int, string)    {}
func (*quiet) Info(string, ...any)       {}
func (q *quiet) Warn(f string, a ...any) { q.warns = append(q.warns, fmt.Sprintf(f, a...)) }

func TestBuildAndVerify(t *testing.T) {
	book := workbook()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case IndexPath:
			fmt.Fprint(w, `<a href="/sites/default/files/mncasignados_06_05_26.xlsx">Consulta de Código de Red Móvil (MNC).</a>`)
		case "/sites/default/files/mncasignados_06_05_26.xlsx":
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
	if len(rep.warns) != 0 || ds.List.AsOf != "2026-05-06" {
		t.Errorf("warns %q, as of %q", rep.warns, ds.List.AsOf)
	}
	out := store.Output{Format: "sqlite", Path: filepath.Join(dir, "mx.sqlite")}
	if err := out.Write(Schema, Tables(ds)); err != nil {
		t.Fatal(err)
	}
	checks, err := store.Verify(out, Schema, Specs(), Checks)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range checks {
		if ch.Name != "mncs >= 20" && !ch.OK {
			t.Errorf("check %s failed: %s", ch.Name, ch.Got)
		}
	}
	core := Tables(ds)[2]
	if core.Name != "assignments" || len(core.Rows) != 4 || core.Rows[1][9] != "2010-10-11" || core.Rows[1][11] != "IFT/223/UCS/DG-AUSE/6966/17" {
		t.Errorf("assignments: %v", core.Rows)
	}
}
