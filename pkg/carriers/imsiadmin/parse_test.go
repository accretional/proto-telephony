package imsiadmin

import (
	"reflect"
	"testing"
)

const hniDirectory = `<html><body><h2>HNI Codes Directory</h2>
<table id="hniTable6adcb" class="sortable" style="width:100%">
<thead><tr><th class="hni" style="x">HNI-Code &#8645;</th><th class="entity">Entity &#8645;</th></tr></thead>
<tbody>
<tr><td style="p"><a href="https://imsiadmin.com/hni-codes/att-mobility-310-170/" style="c">
   310-170 </a></td><td style="p">
      AT&amp;T Mobility </td></tr>
<tr><td><a href="https://imsiadmin.com/hni-codes/mile-high-networks-llc-316-700/">316-700</a></td><td>RETURNED 10/20/25 (316-700)</td></tr>
<tr><td><a href="https://imsiadmin.com/hni-codes/x-314-190/">314-190</a></td><td>RESERVED FOR PUBLIC SAFETY</td></tr>
</tbody></table></body></html>`

const cbrsDirectory = `<table id="cbrsTable1" class="sortable">
<tr><th class="hni">HNI-Code</th><th class="ibn">IBN &#8645;</th><th class="entity">Entity &#8645;</th></tr>
<tr><td>315-010</td><td><a href="https://imsiadmin.com/cbrs-codes/westnetworks-315-010-0152/">0152</a></td><td>WestNetworks</td></tr>
</table>`

const attDetail = `<html><main><article><header class="entry-header"><h1 class="entry-title">AT&#038;T Mobility (310-170)</h1></header>
<div class="entry-content single-content"><div>
<!-- Code Details --><h2 style="x">Code Details</h2><div>
<div style="m"><strong>HNI Code:</strong> 310-170</div><div><strong>Protocol:</strong> LTE/LTE Advances, 5G</div><div><strong>Wireless License Number:</strong> KNKA382, WPOJ826/827</div><div><strong>License Date:</strong> February 7, 2086</div><div><strong>Effective Date:</strong> 08/28/2026</div>
</div>
<!-- Entity Details --><h2>Entity Details</h2><div>
<div><strong>Entity Name:</strong> AT&#038;T Mobility</div><div><strong>Entity Type:</strong> Public Network Operator (commercial or government)</div>
</div></div></div></article></main>
<footer><div><strong>Contact:</strong> not a field</div></footer></html>`

func TestParseDirectory(t *testing.T) {
	got, err := ParseDirectory(KindHNI, []byte(hniDirectory))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Kind: KindHNI, HNI: "310-170", Entity: "AT&T Mobility", URL: "https://imsiadmin.com/hni-codes/att-mobility-310-170/"},
		{Kind: KindHNI, HNI: "316-700", Entity: "RETURNED 10/20/25 (316-700)", URL: "https://imsiadmin.com/hni-codes/mile-high-networks-llc-316-700/"},
		{Kind: KindHNI, HNI: "314-190", Entity: "RESERVED FOR PUBLIC SAFETY", URL: "https://imsiadmin.com/hni-codes/x-314-190/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hni:\n got %+v\nwant %+v", got, want)
	}
	cb, err := ParseDirectory(KindCBRS, []byte(cbrsDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(cb) != 1 || cb[0].Code() != "315-010-0152" || cb[0].Entity != "WestNetworks" {
		t.Errorf("cbrs: %+v", cb)
	}
	for _, bad := range []string{
		`<p>no table</p>`,
		`<table class="sortable"><tr><th>a</th><th>b</th></tr><tr><td>31-170</td><td>x</td></tr></table>`,
		`<table class="sortable"><tr><th>a</th></tr><tr><td>310-170</td></tr></table>`,
	} {
		if _, err := ParseDirectory(KindHNI, []byte(bad)); err == nil {
			t.Errorf("want error for %q", bad)
		}
	}
}

func TestParseDetail(t *testing.T) {
	d, err := ParseDetail([]byte(attDetail))
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "AT&T Mobility (310-170)" {
		t.Errorf("title %q", d.Title)
	}
	want := []Field{
		{"Code Details", "HNI Code", "310-170"},
		{"Code Details", "Protocol", "LTE/LTE Advances, 5G"},
		{"Code Details", "Wireless License Number", "KNKA382, WPOJ826/827"},
		{"Code Details", "License Date", "February 7, 2086"},
		{"Code Details", "Effective Date", "08/28/2026"},
		{"Entity Details", "Entity Name", "AT&T Mobility"},
		{"Entity Details", "Entity Type", "Public Network Operator (commercial or government)"},
	}
	if !reflect.DeepEqual(d.Fields, want) {
		t.Errorf("fields:\n got %+v\nwant %+v", d.Fields, want)
	}
	if _, err := ParseDetail([]byte(`<html>nothing</html>`)); err == nil {
		t.Error("want error without content")
	}
}

func TestParseStatus(t *testing.T) {
	for in, want := range map[string]Status{
		"AT&T Mobility":                       {State: "assigned", Name: "AT&T Mobility"},
		"XF Wireless Investments (314-200)":   {State: "assigned", Name: "XF Wireless Investments", Related: "314-200"},
		"RETURNED 10/20/25 (316-700)":         {State: "returned", Date: "2025-10-20", Related: "316-700"},
		"RETURNED - 04/26/2016":               {State: "returned", Date: "2016-04-26"},
		"Returned on 10/2/18":                 {State: "returned", Date: "2018-10-02"},
		"Returned":                            {State: "returned"},
		"RECLAIMED 12/15/25 (310-790)":        {State: "reclaimed", Date: "2025-12-15", Related: "310-790"},
		"RETURNED 12/3/2024 (315-010 – 0044)": {State: "returned", Date: "2024-12-03", Related: "315-010-0044"},
		"RESERVED FOR PUBLIC SAFETY":          {State: "reserved", Name: "RESERVED FOR PUBLIC SAFETY"},
		"Not available for assignment":        {State: "unavailable"},
		"Available for Assignment":            {State: "available"},
		"TEST IMSI-HNI":                       {State: "test", Name: "TEST IMSI-HNI"},
	} {
		if got := ParseStatus(in); got != want {
			t.Errorf("ParseStatus(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestNormDate(t *testing.T) {
	for in, want := range map[string]string{
		"June 7, 1995": "1995-06-07", "September 3, 2026": "2026-09-03", "08/28/2026": "2026-08-28",
		"3/16/16": "2016-03-16", "5/4/2018": "2018-05-04", "": "", "TBC": "",
	} {
		if got := NormDate(in); got != want {
			t.Errorf("NormDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractIdentifiers(t *testing.T) {
	id := func(typ, v string) Identifier { return Identifier{typ, v} }
	for _, tc := range []struct {
		in      string
		numbers bool
		want    []Identifier
	}{
		{"FCC Call Sign: WQRN480", true, []Identifier{id("call_sign", "WQRN480")}},
		{"WQIZ525, WQIZ526, wqiz527", true, []Identifier{id("call_sign", "WQIZ525"), id("call_sign", "WQIZ526"), id("call_sign", "WQIZ527")}},
		{"WPOJ826/827", true, []Identifier{id("call_sign", "WPOJ826"), id("call_sign", "WPOJ827")}},
		{"KNLH 443 File:0007714142", true, []Identifier{id("uls_file", "0007714142"), id("call_sign", "KNLH443")}},
		{"MVNO (FCC 837077, CORES 0034742809)", true, []Identifier{id("frn", "0034742809"), id("filer_id", "837077")}},
		{"FRN#0004-3414-26", true, []Identifier{id("frn", "0004341426")}},
		{"L00005723, L00153782, L00", true, []Identifier{id("lease", "L00005723"), id("lease", "L00153782")}},
		{"0407-EX-ST-2024, 00197-CW-L-96, SAT-LOA-20210511-00064, DA 24-1193", true, []Identifier{
			id("experimental_file", "0407-EX-ST-2024"), id("cellular_file", "00197-CW-L-96"),
			id("satellite_filing", "SAT-LOA-20210511-00064"), id("fcc_document", "DA 24-1193")}},
		{"FCC Experimental WG2XHO", true, []Identifier{id("call_sign", "WG2XHO")}},
		{"GSMA member 22358", true, []Identifier{id("gsma_member", "22358")}},
		{"0024415002/WQVK773", true, []Identifier{id("call_sign", "WQVK773"), id("frn", "0024415002")}},
		{"Georgia RSA 5-B2 (CMA375, call sign KNKN934)", false, []Identifier{id("call_sign", "KNKN934"), id("market", "CMA375")}},
		{"previously provided for 313-460", true, []Identifier{id("hni", "313-460")}},
		{"2915533", true, []Identifier{id("number", "2915533")}},
		{"Reassignment of IMSI HNI ,311 470, 12345678", false, []Identifier{id("hni", "311-470")}},
		{"N/A", true, nil},
		{"Please see the attached list of licenses", true, nil},
	} {
		if got := ExtractIdentifiers(tc.in, tc.numbers); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ExtractIdentifiers(%q) =\n %v, want\n %v", tc.in, got, tc.want)
		}
	}
}

func TestParseProtocols(t *testing.T) {
	for in, want := range map[string][]string{
		"LTE/LTE Advances, 5G": {"LTE/LTE Advanced", "5G"},
		"LTE 5G":               {"LTE/LTE Advanced", "5G"},
		"Other (5G)":           {"5G"},
		"GPRS, EDGE, W-CDMA, HSPA, 1xRTT, HRPD/EVDO, LTE, LTE ADVANCED": {
			"GPRS", "EDGE", "W-CDMA", "HSPA", "1xRTT", "HRPD/EVDO", "LTE/LTE Advanced"},
		"LTE, Satellite": {"LTE/LTE Advanced", "Satellite"},
		"":               nil,
	} {
		if got := ParseProtocols(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseProtocols(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntityCategory(t *testing.T) {
	for in, want := range map[string]string{
		"Public Network Operator":                                     "public_network_operator",
		"Provider of a service profile management system (e.g., HLR)": "service_profile_provider",
		"Provider of Service":                                         "service_provider",
		"GSM based service in Iowa":                                   "",
	} {
		if got := EntityCategory(in); got != want {
			t.Errorf("EntityCategory(%q) = %q, want %q", in, got, want)
		}
	}
}
