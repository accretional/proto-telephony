package itu

import (
	"reflect"
	"testing"
)

func TestReadDocxTokens(t *testing.T) {
	d := mustDoc(t,
		p("Mobile |Network Codes"),
		tbl(row("a", "b\n\nc")),
		p(""), // empty paragraphs outside tables are dropped
		p("line1\nline2"),
	)
	want := []Token{
		{Text: "Mobile Network Codes", Table: -1},
		{Text: "a", Table: 0, Row: 0, Cell: 0},
		{Text: "b\nc", Table: 0, Row: 0, Cell: 1},
		{Text: "line1\nline2", Table: -1},
	}
	if !reflect.DeepEqual(d.Tokens, want) {
		t.Fatalf("tokens:\n got %+v\nwant %+v", d.Tokens, want)
	}
}

func TestReadDocxNestedTable(t *testing.T) {
	inner := tableXML([][]string{{"x", "y"}})
	doc := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:tbl><w:tr><w:tc>` + paraXML("before") + inner + paraXML("after") + `</w:tc></w:tr></w:tbl></w:body></w:document>`
	d, err := ReadDocx(zipFiles(t, map[string][]byte{"word/document.xml": []byte(doc)}))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, tk := range d.Tokens {
		texts = append(texts, tk.Text)
	}
	if want := []string{"before", "x", "y", "after"}; !reflect.DeepEqual(texts, want) {
		t.Fatalf("order: got %q want %q", texts, want)
	}
	// Tables() rejoins the outer cell's pieces.
	tbs := d.Tables()
	if len(tbs) != 2 || tbs[0].Rows[0][0] != "before\nafter" {
		t.Fatalf("tables: %+v", tbs)
	}
}

func TestReadDocxErrors(t *testing.T) {
	if _, err := ReadDocx([]byte("not a zip")); err == nil {
		t.Error("want error for non-zip")
	}
	if _, err := ReadDocx(zipFiles(t, map[string][]byte{"x.xml": nil})); err == nil {
		t.Error("want error for zip without word/document.xml")
	}
}

func TestSplitCode(t *testing.T) {
	for in, want := range map[string][2]string{
		"412 01": {"412", "01"}, "310 170": {"310", "170"}, "41201": {"412", "01"},
		" 228 73 *": {"228", "73"}, "901\u00a003": {"901", "03"},
	} {
		in = normSpace(in)
		mcc, mnc, err := SplitCode(in)
		if err != nil || mcc != want[0] || mnc != want[1] {
			t.Errorf("SplitCode(%q) = %q %q %v, want %v", in, mcc, mnc, err, want)
		}
	}
	for _, bad := range []string{"", "MCC + MNC codes", "12 34", "4120", "412 0001"} {
		if _, _, err := SplitCode(bad); err == nil {
			t.Errorf("SplitCode(%q): want error", bad)
		}
	}
}

func TestParseBaseList(t *testing.T) {
	bl, err := ParseBaseList(mustDoc(t, baseListBlocks()...))
	if err != nil {
		t.Fatal(err)
	}
	if bl.OB != 1280 || bl.Position != "15 NOVEMBER 2023" {
		t.Errorf("header: OB %d position %q", bl.OB, bl.Position)
	}
	if len(bl.Skipped) != 0 {
		t.Errorf("skipped: %q", bl.Skipped)
	}
	got := map[string]Network{}
	for _, n := range bl.Networks {
		got[n.PLMN()+"/"+n.Country] = n
	}
	if len(bl.Networks) != 11 {
		t.Errorf("got %d networks, want 11: %+v", len(bl.Networks), bl.Networks)
	}
	for key, want := range map[string]Network{
		"41201/Afghanistan":    {MCC: "412", MNC: "01", Country: "Afghanistan", Operator: "AWCC", Kind: KindGeographic, SourceOB: 1280},
		"310170/United States": {MCC: "310", MNC: "170", Country: "United States", Operator: "AT&T Mobility", Kind: KindGeographic, SourceOB: 1280},
		"310010/United States": {MCC: "310", MNC: "010", Country: "United States", Operator: "Verizon Wireless", Kind: KindGeographic, SourceOB: 1280},
		"20801/Monaco":         {MCC: "208", MNC: "01", Country: "Monaco", Operator: "Orange/Monaco Télécom", Kind: KindGeographic, SourceOB: 1280},
		"90103/" + SharedGroup: {MCC: "901", MNC: "03", Country: SharedGroup, Operator: "Iridium Communications Inc", Kind: KindShared, SourceOB: 1280},
	} {
		if g := got[key]; g != want {
			t.Errorf("%s:\n got %+v\nwant %+v", key, g, want)
		}
	}
	if n := got["90201/regional and other international organizations (ROIO)/standards development organization (SDO)-specified networks, shared code"]; n.Kind != KindROIO || n.Operator != "MulteFire Alliance" {
		t.Errorf("902 01: %+v", n)
	}
	wantET := []ExtraTerritorial{
		{Country: "France - Country A", MCC: "208", MNC: "01", MSINRange: "0000000000-0099999999", Operator: "Orange"},
		{Country: "Monaco - Country B", MCC: "208", MNC: "01", MSINRange: "0100000000-0199999999", Operator: "Orange/Monaco Télécom"},
	}
	if !reflect.DeepEqual(bl.ExtraTerritorial, wantET) {
		t.Errorf("extraterritorial:\n got %+v\nwant %+v", bl.ExtraTerritorial, wantET)
	}
}

func TestParseBaseListRejectsOtherDocs(t *testing.T) {
	if _, err := ParseBaseList(mustDoc(t, p("hello"))); err == nil {
		t.Error("want error without annex header")
	}
	if _, err := ParseBaseList(mustDoc(t, p("Annex to ITU Operational Bulletin No. 1280"))); err == nil {
		t.Error("want error without MNC tables")
	}
}

// otherLists are neighbouring amendment sections that must not be read as MNCs.
func otherLists() []block {
	return []block{
		p("Lists annexed to the ITU Operational Bulletin"),
		p("1280\tMobile Network Codes (MNC) for the international identification plan (Position on 15 November 2023)"),
		p("1251\tStatus of Radiocommunications between Amateur Stations"),
		p("List of ITU Carrier Codes (According to Recommendation ITU-T M.1400 (03/2013))"),
		p("(Annex to ITU Operational Bulletin No. 1060 – 15.IX.2014)(Amendment No. 161)"),
		p("Germany (Federal Republic of) / DEU\tADD"),
		tbl(row("NET@vision GmbH", "NAV001", "Mr X")),
	}
}

func TestParseAmendmentParagraphLayout(t *testing.T) {
	// OB 1282 style: heading paragraphs, country/action rows in the table,
	// footnote after "____".
	blocks := append(otherLists(),
		p("Mobile Network Codes (MNC) for the international identification plan for public networks and subscriptions(According to Recommendation ITU-T E.212 (09/2016))(Position on 15 November 2023)"),
		p("(Annex to ITU Operational Bulletin No. 1280 - 15.XI.2023)"),
		p("(Amendment No. 1)"),
		tbl(
			row("Country/Geographical area", "MCC+MNC", "Operator/Network"),
			row("Australia     ADD", "", ""),
			row("", "505 58", "Wi-Sky (NSW) Pty Ltd"),
			row("International Mobile, shared code    SUP*", "", ""),
			row("", "901 95", "HMD Global Oy"),
			row("Korea (Rep. of)  LIR", "", ""),
			row("", "450 08", "KT\n\nCorporation"),
		),
		p("____________"),
		p("MCC:  Mobile Country Code"),
		p("* See the present ITU Operational Bulletin No. 1282 of 15.XII.2023, page 5."),
		p("List of International Signalling Point Codes (ISPC)"),
		p("(Annex to ITU Operational Bulletin No. 1199 – 1.VII.2020)(Amendment No. 63)"),
		p("Ireland    SUP"),
		tbl(row("2-146-5", "5269", "East Wall Dublin")),
	)
	a := ParseAmendment(mustDoc(t, blocks...), 1282)
	if a == nil {
		t.Fatal("no amendment found")
	}
	if a.No != 1 || a.OB != 1282 || a.BaseOB != 1280 {
		t.Errorf("header: %+v", a)
	}
	want := []Change{
		{Amendment: 1, OB: 1282, Seq: 1, Country: "Australia", Action: ActionAdd, MCC: "505", MNC: "58", Operator: "Wi-Sky (NSW) Pty Ltd"},
		{Amendment: 1, OB: 1282, Seq: 2, Country: "International Mobile, shared code", Action: ActionSup, MCC: "901", MNC: "95", Operator: "HMD Global Oy"},
		{Amendment: 1, OB: 1282, Seq: 3, Country: "Korea (Rep. of)", Action: ActionLIR, MCC: "450", MNC: "08", Operator: "KT Corporation"},
	}
	if !reflect.DeepEqual(a.Changes, want) {
		t.Errorf("changes:\n got %+v\nwant %+v", a.Changes, want)
	}
}

func TestParseAmendmentCellLayoutAndTypo(t *testing.T) {
	// OB 1329 style: heading inside table cells. OB 1285 style: the word
	// "Mobile" missing from the heading. OB 1348 style: code and name in
	// separate rows, "Annex to" line before "Amendment No.".
	blocks := []block{
		tbl(
			row("Network Codes (MNC) for the international identification plan \nfor public networks and subscriptions"),
			row("Annex to ITU Operational Bulletin No. 1280 – 15.XI.2023"),
			row("Amendment No. 64"),
		),
		tbl(
			row("Country / Geographical area"),
			row("MCC + MNC", "Name of assignee"),
			row("Australia   ADD"),
			row("505 65", "Fear Enterprises Pty Ltd"),
			row("Costa Rica   ADD"),
			row("712 10", "COOPERATIVA DE ELECTRIFICACIÓN RURAL DE GUANACASTE R.L."),
		),
		p("Extra-territorial use*** of MCC/MNC, Annex E to Recommendation ITU-T E.212"),
		tbl(
			row("Country \nor Geographical Area", "MCC+MNC*", "MSIN** range to be used", "Name of assignee"),
			row("Germany\n(Country A)", "262 74", "0000099000 - 0000099099", "Ericsson GmbH"),
			row("Sweden\n(Country B – Where the MCC+MNC is to be used extraterritorially)", "262 74", "0000099100 - 0000099199", "Ericsson GmbH"),
		),
		p("____________"),
	}
	a := ParseAmendment(mustDoc(t, blocks...), 1348)
	if a == nil || a.No != 64 || a.BaseOB != 1280 {
		t.Fatalf("amendment: %+v", a)
	}
	want := []Change{
		{Amendment: 64, OB: 1348, Seq: 1, Country: "Australia", Action: ActionAdd, MCC: "505", MNC: "65", Operator: "Fear Enterprises Pty Ltd"},
		{Amendment: 64, OB: 1348, Seq: 2, Country: "Costa Rica", Action: ActionAdd, MCC: "712", MNC: "10", Operator: "COOPERATIVA DE ELECTRIFICACIÓN RURAL DE GUANACASTE R.L."},
		{Amendment: 64, OB: 1348, Seq: 3, Country: "Germany - Country A", Action: ActionAdd, MCC: "262", MNC: "74",
			Operator: "Ericsson GmbH", ExtraTerritorial: true, MSINRange: "0000099000 - 0000099099"},
		{Amendment: 64, OB: 1348, Seq: 4, Country: "Sweden - Country B", Action: ActionAdd, MCC: "262", MNC: "74",
			Operator: "Ericsson GmbH", ExtraTerritorial: true, MSINRange: "0000099100 - 0000099199"},
	}
	if !reflect.DeepEqual(a.Changes, want) {
		t.Errorf("changes:\n got %+v\nwant %+v", a.Changes, want)
	}
}

func TestParseAmendmentNone(t *testing.T) {
	// Only the TOC / annexed-lists mentions: no amendment.
	blocks := append(otherLists(),
		p("AMENDMENTS  TO  SERVICE  PUBLICATIONS"),
		p("Mobile Network Codes (MNC) for the international identification plan for public networks and subscriptions\t16"),
		p("List of ITU Carrier Codes\t17"),
	)
	if a := ParseAmendment(mustDoc(t, blocks...), 1284); a != nil {
		t.Fatalf("want nil, got %+v", a)
	}
}
