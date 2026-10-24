package itu

import (
	"archive/zip"
	"bytes"
	"html"
	"strings"
	"testing"
)

// block is one body element of a synthetic .docx: a paragraph or a table.
type block struct {
	para  string
	table [][]string
}

func p(s string) block             { return block{para: s} }
func tbl(rows ...[]string) block   { return block{table: rows} }
func row(cells ...string) []string { return cells }

// paraXML renders text as a paragraph; "\n" becomes a line break, and a
// "|" splits the text into separate runs (as Word does mid-word).
func paraXML(s string) string {
	var b strings.Builder
	b.WriteString(`<w:p><w:pPr><w:pStyle w:val="Normal"/></w:pPr>`)
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteString(`<w:r><w:br/></w:r>`)
		}
		for _, run := range strings.Split(line, "|") {
			b.WriteString(`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">` + html.EscapeString(run) + `</w:t></w:r>`)
		}
	}
	b.WriteString(`</w:p>`)
	return b.String()
}

func tableXML(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr/>`)
	for _, r := range rows {
		b.WriteString(`<w:tr>`)
		for _, c := range r {
			b.WriteString(`<w:tc><w:tcPr/>`)
			for _, para := range strings.Split(c, "\n\n") {
				b.WriteString(paraXML(para))
			}
			b.WriteString(`</w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

func documentXML(blocks ...block) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, bl := range blocks {
		if bl.table != nil {
			b.WriteString(tableXML(bl.table))
		} else {
			b.WriteString(paraXML(bl.para))
		}
	}
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func zipFiles(t testing.TB, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// docxBytes builds a minimal .docx from blocks.
func docxBytes(t testing.TB, blocks ...block) []byte {
	return zipFiles(t, map[string][]byte{
		"[Content_Types].xml": []byte(`<Types/>`),
		"word/document.xml":   []byte(documentXML(blocks...)),
	})
}

func mustDoc(t testing.TB, blocks ...block) *Doc {
	t.Helper()
	d, err := ReadDocx(docxBytes(t, blocks...))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// baseListBlocks is a small E.212B annex in ITU's layout.
func baseListBlocks() []block {
	return []block{
		p("Annex to ITU Operational Bulletin \nNo. 1280 – 15.XI.2023"),
		p("Mobile Network Codes (MNC) for the international identification plan"),
		p("(POSITION ON 15 NOVEMBER 2023)"),
		tbl(
			row("Country or Geographical Area", "Networks", "MCC + MNC codes"),
			row("Afghanistan", "", ""),
			row("", "AWCC", "412 01"),
			row("", "Roshan", "412 20"),
			row("Estonia", "", ""),
			row("", "SkyTel OÜ", "248 29"),
			row("France", "", ""),
			row("", "Orange", "208 01"),
			row("Monaco", "", ""),
			row("", "Orange/Monaco Télécom", "208 01"),
			row("United States", "", ""),
			row("", "AT&T Mobility", "310 170"),
			row("", "T-Mobile USA", "310 160"),
			row("Country or Geographical Area", "Networks", "MCC + MNC codes"), // page-break header
			row("", "Verizon Wireless", "310 010"),
		),
		tbl(
			row("Networks", "MCC + MNC codes"),
			row("Iridium Communications Inc", "901 03"),
			row("HMD Global Oy", "901 95"),
		),
		tbl(
			row("", "Applicant/Network", "MCC + MNC codes"),
			row("regional and other international organizations (ROIO)/standards development organization (SDO)-specified networks, shared code", "", ""),
			row("", "MulteFire Alliance", "902 01"),
		),
		tbl(
			row("Country or Geographical Area", "MCC + MNC**", "MSIN range***", "Operator/Network"),
			row("France - Country A", "208 01", "0000000000-0099999999", "Orange"),
			row("Monaco - Country B", "208 01", "0100000000-0199999999", "Orange/Monaco Télécom"),
		),
		// Empty notification-form tables at the end of the annex.
		tbl(row("MCC + MNC", "Name of Network / Operator"), row("", "")),
	}
}
