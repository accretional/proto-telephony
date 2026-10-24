# Wikipedia: "Mobile network codes in ITU region 2xx–7xx"

**Status: fetched** (`scripts/third_party_indices/wikipedia.py`, output
`data/vendored/third_party_indices/wikipedia/`). The largest and most
actively edited free list, and the only one with operational status, radio
bands and notes. Quality depends on the country, and every edit is
crowd-sourced.

## What it is

English Wikipedia splits the list over six region pages plus the
[Mobile country code](https://en.wikipedia.org/wiki/Mobile_country_code)
article:

| Page | Last edit (2026-10-24) | Rows |
|---|---|---|
| [2xx (Europe)](https://en.wikipedia.org/wiki/Mobile_network_codes_in_ITU_region_2xx_(Europe)) | 2026-10-05 | 1,121 |
| [3xx (North America)](https://en.wikipedia.org/wiki/Mobile_network_codes_in_ITU_region_3xx_(North_America)) | 2026-10-02 | 806 |
| [4xx (Asia)](https://en.wikipedia.org/wiki/Mobile_network_codes_in_ITU_region_4xx_(Asia)) | 2026-10-17 | 693 |
| [5xx (Oceania)](https://en.wikipedia.org/wiki/Mobile_network_codes_in_ITU_region_5xx_(Oceania)) | 2026-09-29 | 213 |
| [6xx (Africa)](https://en.wikipedia.org/wiki/Mobile_network_codes_in_ITU_region_6xx_(Africa)) | 2026-10-16 | 339 |
| [7xx (South America)](https://en.wikipedia.org/wiki/Mobile_network_codes_in_ITU_region_7xx_(South_America)) | 2026-10-16 | 194 |
| [Mobile country code](https://en.wikipedia.org/wiki/Mobile_country_code): international 901/902 and test networks (001, 999) | 2026-10-04 | 109 |

`wikipedia/pages.csv` records the exact revision IDs used.

- **Format:** every list is a wikitext table using the `{{MNC}}` header
  template with 7 columns:

  ```
  ==== [[Anguilla]] (United Kingdom) – AI ====
  {| {{MNC}}
  |-
  | 365 || 840 || [[Flow (brand)|FLOW]] || Cable & Wireless || Operational || UMTS 850 / UMTS 1900 / LTE 700 || <ref .../> GSM shut down Apr 2024
  ```

  MCC | MNC | Brand | Operator | Status | Bands | References and notes.
  Country and ISO code come from the heading above each table.
- **The MCC table:** the "Mobile country code" article also has an MCC →
  country table (`mccs`, 252 rows over 238 MCCs). It includes ISO codes,
  the national authority (filled for 70 rows) and remarks. It uses
  `rowspan` where one MCC covers several territories, e.g. 647 → RE and YT.
- **License:** CC BY-SA 4.0. Derived data needs attribution (the
  permalinks in `pages.csv`) and must be shared alike.

## Contents (2026-10-24)

- **Rows:** 3,475, covering **3,427 unique codes** in 238 MCCs.
  - 38 codes appear under several territories. For example, 310-260 is
    listed under the US, Puerto Rico and the US Virgin Islands; Wikipedia
    repeats US codes for US territories.
  - 32 rows are **nonstandard codes**:
    - 31 are France's 5-digit blocks under 208 50, such as `208 50244`
      (EDF) and `208 51075` (RATP), private and industrial networks
      sub-allocated by ARCEP;
    - 1 is a range row, "314 100 - 190, Reserved for Public Safety".

    These are kept with `code_format = nonstandard` and left out of the
    comparison.
- **Status:** free text in several spellings, normalized to lowercase in
  `status_norm`:
  - operational 1,587, unknown 1,042, not operational 824, reserved 13;
  - one-offs: allocated, planned, testing, upcoming, "implement / design",
    "temporary operational", "test network", "ongoing".
- **Other columns:**
  - Brand is blank on 1,226 rows, and operator on 23. Operator is usually
    the legal entity, brand the consumer name.
  - Bands are a slash-separated list ("GSM 900 / LTE 800 / 5G 3500").
  - Notes carry history ("Former Cook Inlet West Wireless", "MNC
    withdrawn", "GSM shut down Apr 2024") and inline citations, often to
    ITU Operational Bulletins.
- **Beyond ITU:** it includes codes ITU doesn't publish, e.g. Abkhazia
  (289, "MCC is not listed by ITU") and Kosovo.

## Compared with the official data

From `comparison/summary.csv` (standard codes only):

| | |
|---|---|
| Officially active codes (2,514) listed | **2,388 (95.0%)**, the best of the three |
| Officially active codes missing | 126. ITU 87, US 27, Japan 11, Mexico 1. Mostly recent or niche: India 405 (26; 13 of these appear in 2-digit form, e.g. 405-05 for ITU's 405-005), Hong Kong 454 (20), US 314 (19), Kuwait 418 (16), Japan 441 (11) |
| Listed codes the registry marks returned/reclaimed | 130: returned 88, reclaimed 42. Mostly US 311/312/313, the shared 901, and Canada 302 |
| Listed codes not in the official data at all | 877. Of these, 15 exist officially with the other MNC length |

- **Wikipedia doubles as a history.** Of the 877 extra codes, 421 are
  marked "not operational", 310 "operational" and 141 "unknown". Most are
  former assignments the current ITU list has dropped: India 405 (143),
  Russia 250 (32), Finland 244 (26), Poland 260 (25), Italy 222 (22).
- **Status contradicts the registry in both directions.**
  - 271 codes that are officially assigned are "not operational" here.
    That's not necessarily an error, since an assigned code may be unused
    on air.
  - 3 codes are "operational" here but officially taken back: 311-690 and
    313-870 reclaimed, 314-830 returned.
- **Holders disagree with other lists.** For example, 244-20 is Elisa here
  and Telia in Wireshark; 244-16 is Digita here and Tele2 in Wireshark and
  AOSP.

## How the fetcher reads it

1. **Fetch** each page's current wikitext and revision ID/timestamp with
   one MediaWiki API `query` call per page (7 requests).
2. **Clean the wikitext:**
   - drop HTML comments and `<ref>` elements first, since they can contain
     pipes and newlines;
   - read tables row by row: `|-` starts a row, `||`/`!!` separate cells,
     multi-line cells are joined, and `rowspan` is honoured;
   - strip markup: links become their label; `{{TOC tab}}`, `{{ill}}`,
     `{{Proper name}}` and `{{usurped}}` keep their first argument; other
     templates (citation markers) are dropped.
3. **Find the country** from the nearest `Name – XX` heading. For the
   international and test tables, the section name is used.
4. **Report** rows with fewer than 6 or more than 7 cells on stderr. A
   6-cell row, with the notes cell omitted, is accepted.
5. **Fail** if there are fewer than 2,500 network rows or 200 MCC rows.

The latest run had 0 parse problems.

## Usefulness

- **Good:**
  - the widest free coverage of active codes;
  - status and bands;
  - history and notes (former holders, shutdown dates);
  - unofficial territories;
  - ISO country per code.
- **Not good for:**
  - authoritative status: "operational" means on air, not assigned;
  - reproducibility without pinning revisions (the fetcher records them).

  Edits can be wrong or vandalized; diff `networks.csv` between runs to
  spot changes.
