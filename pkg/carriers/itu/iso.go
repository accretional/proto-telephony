package itu

import "strings"

// International is the country code used for ITU's shared, non-geographic
// MCCs (901, 902, 991): ISO 3166's user-assigned "ZZ".
const International = "ZZ"

// isoByName maps ITU's country/area names (as the E.212 list spells them) to
// ISO 3166-1 alpha-2 codes. Kosovo uses the widely adopted user-assigned XK;
// "French Departments and Territories in the Indian Ocean" (MCC 647) is
// filed under Réunion.
var isoByName = map[string]string{
	"Afghanistan": "AF", "Albania": "AL", "Algeria": "DZ", "Andorra": "AD", "Angola": "AO", "Anguilla": "AI",
	"Antigua and Barbuda": "AG", "Argentina": "AR", "Armenia": "AM", "Aruba": "AW", "Australia": "AU", "Austria": "AT",
	"Azerbaijan": "AZ", "Bahamas": "BS", "Bahrain": "BH", "Bangladesh": "BD", "Barbados": "BB", "Belarus": "BY",
	"Belgium": "BE", "Belize": "BZ", "Benin": "BJ", "Bermuda": "BM", "Bhutan": "BT",
	"Bolivia (Plurinational State of)": "BO", "Bosnia and Herzegovina": "BA", "Botswana": "BW", "Brazil": "BR",
	"British Virgin Islands": "VG", "Brunei Darussalam": "BN", "Bulgaria": "BG", "Burkina Faso": "BF", "Burundi": "BI",
	"Cabo Verde": "CV", "Cambodia": "KH", "Cameroon": "CM", "Canada": "CA", "Cayman Islands": "KY",
	"Central African Rep.": "CF", "Chad": "TD", "Chile": "CL", "China": "CN", "Colombia": "CO", "Comoros": "KM",
	"Congo": "CG", "Cook Islands": "CK", "Costa Rica": "CR", "Croatia": "HR", "Cuba": "CU", "Curaçao": "CW",
	"Cyprus": "CY", "Czech Rep.": "CZ", "Côte d'Ivoire": "CI", "Dem. Rep. of the Congo": "CD", "Denmark": "DK",
	"Djibouti": "DJ", "Dominica": "DM", "Dominican Rep.": "DO", "Ecuador": "EC", "Egypt": "EG", "El Salvador": "SV",
	"Equatorial Guinea": "GQ", "Eritrea": "ER", "Estonia": "EE", "Eswatini": "SZ", "Ethiopia": "ET",
	"Falkland Islands (Malvinas)": "FK", "Faroe Islands": "FO", "Fiji": "FJ", "Finland": "FI", "France": "FR",
	"French Departments and Territories in the Indian Ocean": "RE", "French Guiana": "GF", "French Polynesia": "PF",
	"Gabon": "GA", "Gambia": "GM", "Georgia": "GE", "Germany": "DE", "Ghana": "GH", "Gibraltar": "GI", "Greece": "GR",
	"Greenland": "GL", "Grenada": "GD", "Guadeloupe": "GP", "Guam": "GU", "Guatemala": "GT", "Guinea": "GN",
	"Guinea-Bissau": "GW", "Guyana": "GY", "Haiti": "HT", "Honduras": "HN", "Hong Kong, China": "HK", "Hungary": "HU",
	"Iceland": "IS", "India": "IN", "Indonesia": "ID", "Iran (Islamic Republic of)": "IR", "Iraq": "IQ", "Ireland": "IE",
	"Israel": "IL", "Italy": "IT", "Jamaica": "JM", "Japan": "JP", "Jordan": "JO", "Kazakhstan": "KZ", "Kenya": "KE",
	"Kiribati": "KI", "Korea (Rep. of)": "KR", "Kosovo": "XK", "Kuwait": "KW", "Kyrgyzstan": "KG", "Lao P.D.R.": "LA",
	"Latvia": "LV", "Lebanon": "LB", "Lesotho": "LS", "Liberia": "LR", "Libya": "LY", "Liechtenstein": "LI",
	"Lithuania": "LT", "Luxembourg": "LU", "Macao, China": "MO", "Madagascar": "MG", "Malawi": "MW", "Malaysia": "MY",
	"Maldives": "MV", "Mali": "ML", "Malta": "MT", "Marshall Islands": "MH", "Martinique": "MQ", "Mauritania": "MR",
	"Mauritius": "MU", "Mexico": "MX", "Micronesia": "FM", "Moldova (Republic of)": "MD", "Monaco": "MC",
	"Mongolia": "MN", "Montenegro": "ME", "Montserrat": "MS", "Morocco": "MA", "Mozambique": "MZ", "Myanmar": "MM",
	"Namibia": "NA", "Nauru": "NR", "Nepal": "NP", "Netherlands": "NL", "New Caledonia": "NC", "New Zealand": "NZ",
	"Nicaragua": "NI", "Niger": "NE", "Nigeria": "NG", "Niue": "NU", "North Macedonia": "MK", "Norway": "NO",
	"Oman": "OM", "Pakistan": "PK", "Palau": "PW", "Palestine": "PS", "Panama": "PA", "Papua New Guinea": "PG",
	"Paraguay": "PY", "Peru": "PE", "Philippines": "PH", "Poland": "PL", "Portugal": "PT", "Puerto Rico": "PR",
	"Qatar": "QA", "Romania": "RO", "Russian Federation": "RU", "Rwanda": "RW",
	"Saint Helena, Ascension and Tristan da Cunha": "SH", "Saint Kitts and Nevis": "KN", "Saint Lucia": "LC",
	"Saint Pierre and Miquelon": "PM", "Saint Vincent and the Grenadines": "VC", "Samoa": "WS", "San Marino": "SM",
	"Sao Tome and Principe": "ST", "Saudi Arabia": "SA", "Senegal": "SN", "Serbia": "RS", "Seychelles": "SC",
	"Sierra Leone": "SL", "Singapore": "SG", "Slovakia": "SK", "Slovenia": "SI", "Solomon Islands": "SB",
	"Somalia": "SO", "South Africa": "ZA", "South Sudan": "SS", "Spain": "ES", "Sri Lanka": "LK", "Sudan": "SD",
	"Suriname": "SR", "Sweden": "SE", "Switzerland": "CH", "Syrian Arab Republic": "SY", "Tajikistan": "TJ",
	"Tanzania": "TZ", "Thailand": "TH", "Timor-Leste": "TL", "Togo": "TG", "Tokelau": "TK", "Tonga": "TO",
	"Trinidad and Tobago": "TT", "Tunisia": "TN", "Turkmenistan": "TM", "Turks and Caicos Islands": "TC",
	"Tuvalu": "TV", "Türkiye": "TR", "Uganda": "UG", "Ukraine": "UA", "United Arab Emirates": "AE",
	"United Kingdom": "GB", "United States": "US", "Uruguay": "UY", "Uzbekistan": "UZ", "Vanuatu": "VU",
	"Vatican": "VA", "Venezuela (Bolivarian Republic of)": "VE", "Viet Nam": "VN", "Wallis and Futuna": "WF",
	"Yemen": "YE", "Zambia": "ZM", "Zimbabwe": "ZW",
}

// ISO returns the ISO 3166-1 alpha-2 code for an ITU country name, the
// International code for the shared MCCs, and "" if the name is unknown.
// Footnote marks ("Kosovo*") and the extraterritorial table's
// " - Country A/B" suffix are ignored.
func ISO(country, mcc string) string {
	switch mcc {
	case "901", "902", "991":
		return International
	}
	name := strings.TrimRight(strings.TrimSpace(country), "*")
	if i := strings.Index(name, " - Country "); i > 0 {
		name = name[:i]
	}
	if iso, ok := isoByName[name]; ok {
		return iso
	}
	// Tolerate spelling drift in amendments: case and spacing.
	for n, iso := range isoByName {
		if strings.EqualFold(normSpace(n), normSpace(name)) {
			return iso
		}
	}
	return ""
}
