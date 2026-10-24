package xlsx_test

import (
	"reflect"
	"testing"

	"github.com/accretional/proto-telephony/pkg/carriers/xlsx"
	"github.com/accretional/proto-telephony/pkg/carriers/xlsx/xlsxtest"
)

func TestRead(t *testing.T) {
	data := xlsxtest.Workbook("LST", [][]any{
		{"番号", "事業者", "使用状況"},
		{"44010", xlsxtest.Phonetic{Text: "株式会社ＮＴＴドコモ", Reading: "カブシキガイシャ"}, "使用中"},
		{"", "", ""}, // an empty row
		{"441005", "", xlsxtest.Phonetic{Text: "未使用", Reading: "ミシヨウ"}}, // B omitted
		{36934, 46148.0, "a\nb"},
	})
	sheets, err := xlsx.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 1 || sheets[0].Name != "LST" {
		t.Fatalf("sheets: %+v", sheets)
	}
	want := [][]string{
		{"番号", "事業者", "使用状況"},
		{"44010", "株式会社ＮＴＴドコモ", "使用中"},
		nil,
		{"441005", "", "未使用"},
		{"36934", "46148", "a\nb"},
	}
	if !reflect.DeepEqual(sheets[0].Rows, want) {
		t.Errorf("rows:\n got %q\nwant %q", sheets[0].Rows, want)
	}
	if _, err := xlsx.Read([]byte("not a zip")); err == nil {
		t.Error("want error for non-zip")
	}
}

func TestSerialDate(t *testing.T) {
	for in, want := range map[string]string{"36934": "2001-02-12", "46148": "2026-05-06", "45744.0": "2025-03-28"} {
		if got, ok := xlsx.SerialDate(in); !ok || got != want {
			t.Errorf("SerialDate(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "15/12/2017", "-3", "abc"} {
		if _, ok := xlsx.SerialDate(bad); ok {
			t.Errorf("SerialDate(%q): want !ok", bad)
		}
	}
}
