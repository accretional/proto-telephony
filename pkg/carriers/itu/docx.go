package itu

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Token is one unit of text from a Word document: a table cell (its direct
// paragraphs joined by "\n") or a paragraph outside any table. Tokens are in
// document order. Cells of a table nested inside another cell become their
// own tokens; the outer cell's text before the nested table is flushed first
// so order is preserved.
type Token struct {
	Text  string
	Table int // innermost table id (in order of appearance), -1 outside tables
	Row   int // row index within Table
	Cell  int // cell index within Row
}

// Row is the text of one table row, cell by cell.
type Row []string

// Table is a table's rows, rebuilt from tokens.
type Table struct {
	ID   int
	Rows []Row
}

// Doc is a parsed .docx body.
type Doc struct {
	Tokens []Token
}

// ReadDocx parses word/document.xml out of a .docx archive.
func ReadDocx(data []byte) (*Doc, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("docx: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return parseDocumentXML(rc)
	}
	return nil, fmt.Errorf("docx: no word/document.xml")
}

type cellState struct {
	table, row, cell int
	paras            []string
}

func parseDocumentXML(r io.Reader) (*Doc, error) {
	const w = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	dec := xml.NewDecoder(r)
	doc := &Doc{}
	var (
		tables   []int // stack of open table ids
		rows     []int // current row index per open table
		cellIdx  []int // current cell index per open table
		cells    []*cellState
		nextID   int
		para     strings.Builder
		inPara   int
		inText   bool
		emitCell = func(c *cellState) {
			text := strings.TrimSpace(strings.Join(c.paras, "\n"))
			doc.Tokens = append(doc.Tokens, Token{Text: text, Table: c.table, Row: c.row, Cell: c.cell})
			c.paras = nil
		}
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("docx: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != w {
				continue
			}
			switch t.Name.Local {
			case "tbl":
				// Flush the enclosing cell's text so far so order is kept.
				if n := len(cells); n > 0 && len(cells[n-1].paras) > 0 {
					emitCell(cells[n-1])
				}
				tables = append(tables, nextID)
				rows = append(rows, -1)
				cellIdx = append(cellIdx, -1)
				nextID++
			case "tr":
				if n := len(tables); n > 0 {
					rows[n-1]++
					cellIdx[n-1] = -1
				}
			case "tc":
				if n := len(tables); n > 0 {
					cellIdx[n-1]++
					cells = append(cells, &cellState{table: tables[n-1], row: rows[n-1], cell: cellIdx[n-1]})
				}
			case "p":
				inPara++
				if inPara == 1 {
					para.Reset()
				}
			case "t":
				inText = true
			case "tab":
				para.WriteByte('\t')
			case "br", "cr":
				para.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Space != w {
				continue
			}
			switch t.Name.Local {
			case "tbl":
				if n := len(tables); n > 0 {
					tables, rows, cellIdx = tables[:n-1], rows[:n-1], cellIdx[:n-1]
				}
			case "tc":
				if n := len(cells); n > 0 {
					emitCell(cells[n-1])
					cells = cells[:n-1]
				}
			case "p":
				inPara--
				if inPara > 0 {
					continue
				}
				text := strings.TrimSpace(para.String())
				if n := len(cells); n > 0 {
					cells[n-1].paras = append(cells[n-1].paras, text)
				} else if text != "" {
					doc.Tokens = append(doc.Tokens, Token{Text: text, Table: -1})
				}
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText && inPara > 0 {
				para.Write(t)
			}
		}
	}
	return doc, nil
}

// Tables groups the document's table tokens back into tables of rows.
func (d *Doc) Tables() []Table {
	var out []Table
	idx := map[int]int{}
	for _, t := range d.Tokens {
		if t.Table < 0 {
			continue
		}
		i, ok := idx[t.Table]
		if !ok {
			i = len(out)
			idx[t.Table] = i
			out = append(out, Table{ID: t.Table})
		}
		tb := &out[i]
		for len(tb.Rows) <= t.Row {
			tb.Rows = append(tb.Rows, nil)
		}
		r := tb.Rows[t.Row]
		for len(r) <= t.Cell {
			r = append(r, "")
		}
		// A cell flushed around a nested table yields several tokens; join them.
		if r[t.Cell] != "" && t.Text != "" {
			r[t.Cell] += "\n" + t.Text
		} else if t.Text != "" {
			r[t.Cell] = t.Text
		}
		tb.Rows[t.Row] = r
	}
	return out
}

// Text is the whole document's text, one token per line.
func (d *Doc) Text() string {
	var b strings.Builder
	for _, t := range d.Tokens {
		b.WriteString(t.Text)
		b.WriteByte('\n')
	}
	return b.String()
}
