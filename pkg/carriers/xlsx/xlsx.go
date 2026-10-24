// Package xlsx reads the cell text of .xlsx workbooks with the standard
// library: enough for the registries published as spreadsheets (Mexico's
// IFT, Japan's MIC). It returns every sheet as rows of strings, with cells
// placed by their column reference (Excel omits empty cells), shared and
// inline strings resolved, and phonetic guides (<rPh>, Japanese furigana)
// dropped.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
)

// Sheet is one worksheet's cell text.
type Sheet struct {
	Name string
	Rows [][]string
}

// Read parses a workbook.
func Read(data []byte) ([]Sheet, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("xlsx: no %s", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}

	var shared []string
	if b, err := read("xl/sharedStrings.xml"); err == nil {
		if shared, err = parseSharedStrings(b); err != nil {
			return nil, err
		}
	}

	// Sheet names and their part paths, via the workbook relationships.
	wb, err := read("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(wb, &book); err != nil {
		return nil, fmt.Errorf("xlsx: workbook: %w", err)
	}
	rels := map[string]string{}
	if b, err := read("xl/_rels/workbook.xml.rels"); err == nil {
		var r struct {
			Rel []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if err := xml.Unmarshal(b, &r); err == nil {
			for _, x := range r.Rel {
				t := strings.TrimPrefix(x.Target, "/")
				if !strings.HasPrefix(t, "xl/") {
					t = path.Join("xl", t)
				}
				rels[x.ID] = t
			}
		}
	}
	var out []Sheet
	for i, s := range book.Sheets {
		part := rels[s.RID]
		if part == "" {
			part = fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		}
		b, err := read(part)
		if err != nil {
			return nil, err
		}
		rows, err := parseSheet(b, shared)
		if err != nil {
			return nil, fmt.Errorf("xlsx: sheet %q: %w", s.Name, err)
		}
		out = append(out, Sheet{Name: s.Name, Rows: rows})
	}
	return out, nil
}

// richText collects <t> text from an <si> or <is> element, skipping <rPh>
// (phonetic guide) runs.
func richText(d *xml.Decoder, end xml.Name) (string, error) {
	var (
		b     strings.Builder
		inT   bool
		inRPh int
	)
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "rPh":
				inRPh++
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch {
			case t.Name == end:
				return b.String(), nil
			case t.Name.Local == "rPh":
				inRPh--
			case t.Name.Local == "t":
				inT = false
			}
		case xml.CharData:
			if inT && inRPh == 0 {
				b.Write(t)
			}
		}
	}
}

func parseSharedStrings(b []byte) ([]string, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var out []string
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("xlsx: shared strings: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "si" {
			s, err := richText(d, se.Name)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
	}
}

func parseSheet(b []byte, shared []string) ([][]string, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	var (
		rows   [][]string
		row    []string
		rowNum int
	)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "row":
			n := len(rows) + 1
			if r := attr(se, "r"); r != "" {
				n, _ = strconv.Atoi(r)
			}
			for len(rows) < n-1 { // rows Excel skipped
				rows = append(rows, nil)
			}
			row, rowNum = nil, n
			rows = append(rows, nil)
		case "c":
			col := len(row)
			if ref := attr(se, "r"); ref != "" {
				col = colIndex(ref)
			}
			v, err := cellValue(d, se, shared)
			if err != nil {
				return nil, err
			}
			for len(row) <= col {
				row = append(row, "")
			}
			row[col] = v
			rows[rowNum-1] = row
		}
	}
}

func cellValue(d *xml.Decoder, c xml.StartElement, shared []string) (string, error) {
	typ := attr(c, "t")
	var v string
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "v":
				var s string
				if err := d.DecodeElement(&s, &t); err != nil {
					return "", err
				}
				v = s
			case "is":
				s, err := richText(d, t.Name)
				if err != nil {
					return "", err
				}
				v = s
			}
		case xml.EndElement:
			if t.Name.Local == "c" {
				if typ == "s" {
					i, err := strconv.Atoi(strings.TrimSpace(v))
					if err != nil || i < 0 || i >= len(shared) {
						return "", fmt.Errorf("bad shared string index %q", v)
					}
					return shared[i], nil
				}
				return v, nil
			}
		}
	}
}

func attr(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// colIndex turns a cell reference ("C12") into a 0-based column index.
func colIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

// SerialDate converts an Excel serial day number (1900 date system) to
// YYYY-MM-DD; ok is false if s isn't one. Correct from 1900-03-01 (serial
// 61) on, which covers every registry date.
func SerialDate(s string) (string, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 61 || f > 2958465 {
		return "", false
	}
	// Excel counts a fictitious 1900-02-29, so from serial 61 on, day 0 is 1899-12-30.
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(f)).Format("2006-01-02"), true
}
