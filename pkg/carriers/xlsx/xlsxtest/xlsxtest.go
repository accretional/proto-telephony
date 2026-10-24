// Package xlsxtest builds small .xlsx workbooks for tests: one sheet,
// strings stored as shared strings (optionally with <rPh> phonetic runs),
// numbers as numeric cells, and empty cells omitted like Excel does.
package xlsxtest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"
)

// Phonetic marks a cell whose shared string carries a phonetic guide
// (furigana) that readers must drop.
type Phonetic struct{ Text, Reading string }

// Workbook returns an .xlsx with one sheet. Cells are string (shared),
// Phonetic (shared, with <rPh>), int/float64 (numeric), or "" (omitted).
func Workbook(sheet string, rows [][]any) []byte {
	var (
		shared []string
		sheetX strings.Builder
	)
	sheetX.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for i, r := range rows {
		fmt.Fprintf(&sheetX, `<row r="%d">`, i+1)
		for j, v := range r {
			ref := colName(j) + strconv.Itoa(i+1)
			switch x := v.(type) {
			case string:
				if x == "" {
					continue
				}
				shared = append(shared, `<si><t xml:space="preserve">`+html.EscapeString(x)+`</t></si>`)
				fmt.Fprintf(&sheetX, `<c r="%s" t="s"><v>%d</v></c>`, ref, len(shared)-1)
			case Phonetic:
				shared = append(shared, `<si><r><t>`+html.EscapeString(x.Text)+`</t></r><rPh sb="0" eb="1"><t>`+
					html.EscapeString(x.Reading)+`</t></rPh><phoneticPr fontId="1"/></si>`)
				fmt.Fprintf(&sheetX, `<c r="%s" t="s"><v>%d</v></c>`, ref, len(shared)-1)
			case int:
				fmt.Fprintf(&sheetX, `<c r="%s"><v>%d</v></c>`, ref, x)
			case float64:
				fmt.Fprintf(&sheetX, `<c r="%s"><v>%v</v></c>`, ref, x)
			}
		}
		sheetX.WriteString(`</row>`)
	}
	sheetX.WriteString(`</sheetData></worksheet>`)

	files := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			`<sheet name="` + html.EscapeString(sheet) + `" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":     `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + strings.Join(shared, "") + `</sst>`,
		"xl/worksheets/sheet1.xml": sheetX.String(),
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}
