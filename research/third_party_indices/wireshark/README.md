# Wireshark: `packet-e212.c`

**Status: fetched** (`scripts/third_party_indices/wireshark.py`, output
`data/vendored/third_party_indices/wireshark/`). A name lookup table for
packet dissection, not a registry. It's broad and easy to parse, but it's a
snapshot that's updated now and then by contributors.

## What it is

Wireshark labels IMSIs, PLMN IDs and MCC/MNC fields in decoded packets
(GTP, Diameter, NAS, S1AP, ...) with country and operator names from three C
`value_string` tables in
[`epan/dissectors/packet-e212.c`](https://gitlab.com/wireshark/wireshark/-/blob/master/epan/dissectors/packet-e212.c):

```c
static const value_string E212_codes[] = {             /* MCC -> country */
    {  202, "Greece" },
    {  203, "Unassigned" },
static const value_string mcc_mnc_2digits_codes[] = {  /* MCC*100 + MNC */
    { 20201, "Cosmote AE" },
static const value_string mcc_mnc_3digits_codes[] = {  /* MCC*1000 + MNC */
    { 208500, "EDF" },
```

- **License:** GPL-2.0-or-later (`SPDX-License-Identifier` in the file).
- **Maintainer:** whoever sends a merge request. There's no owner and no
  update schedule.

## Contents (2026-10-24)

From master at `94d2ba2` (2026-07-23):
- **MCC table:** 800 slots in total. 243 are named and 557 are
  "Unassigned" placeholders.
- **Network tables:** 3,063 codes in total, 2,201 with 2-digit MNCs and
  862 with 3-digit MNCs. There are no duplicates.
- **What each row has:** code and name only. No status, dates, country per
  network, brand vs. legal entity, or provenance per row. Country comes
  only from the MCC, so codes used in several territories (for example
  310-260 in the US, Puerto Rico and the US Virgin Islands) can't be told
  apart.
- **Comments:** only 2 entries carry one, both "Annex E" notes (MSIN
  ranges under 208-01 and 274-02).
- **Names:** a mix of legal entities ("Og fjarskipti hf (Vodafone
  Iceland)") and brands ("Orange"). India's codes are named by licence area
  ("Andhra Pradesh and Telangana") rather than operator.

## Provenance and freshness

The file header lists its sources:
- the ITU Operational Bulletin annexes (MCC list from OB 1117, 2017; MNC
  list from OB 1280, 2023);
- amendments through 34 (OB 1198, 2020) only;
- mcc-mnc.com, for Chinese, Iranian and Kazakh codes;
- Russian Wikipedia and htmlweb.ru, for Kazakhstan;
- a bulk addition dated 2026-02-09 ("added 600+ missing operators using
  info from http://www.mcc-mnc.com").

The last data change was that 2026-02-09 commit (`c441f60d75`, "Updating
with all currently documented operators"). The commits since then
(April and July 2026) changed code only. A large part of the table is
therefore copied from mcc-mnc.com, itself a third-party aggregator, and
nothing assigned after early 2026 is likely to be there.

## Compared with the official data

Against `data/carriers/merged/current.csv` (ITU plus national registries),
from `comparison/summary.csv`:

| | |
|---|---|
| Officially active codes (2,514) listed | **2,284 (90.9%)** |
| Officially active codes missing | 230. ITU 135, US 51, Japan 24, Canada 14, Mexico 6. Most are recent: US 314 (44), Hong Kong 454 (23), Japan 441/440 |
| Listed codes the registry marks returned/reclaimed | 121: returned 86, reclaimed 35. Mostly US 311/312/313 and the shared 901 |
| Listed codes not in the official data at all | 657. Of these, 15 exist officially with the other MNC length |

The 657 not-in-official codes are mostly **historical codes the current ITU
list has dropped**:
- India 405 has 76, mostly 2-digit legacy codes such as 405-01 and 405-03.
  The current list uses 3-digit codes under 405.
- Others: Russia 250 (20), Finland 244 (19), Taiwan 466 (19), Italy 222
  (18), Poland 260 (15).

Wireshark never removes a code, so stale entries pile up. Holder names also
drift from other lists. For example, `244-20` is "Telia Finland Oyj" here
but Elisa on Wikipedia, and `244-29` is "SCNL Truphone" here but VTT on
Wikipedia.

## How the fetcher reads it

1. **Resolve the ref** (default `master`) to the last commit touching the
   file, using the GitLab API. Download the raw file at that commit, so
   `sources.csv` is pinned.
2. **Parse** the three tables with a line regex: `{ key, "name" }, /*
   comment */`. Any line inside a table that doesn't match is reported on
   stderr, so format changes are visible.
3. **Split keys:** `key // 100` and `key % 100` for the 2-digit table,
   `// 1000` and `% 1000` for the 3-digit table. Zero-pad to keep the MNC
   length.
4. **Fail** if there are fewer than 2,500 networks or 200 named MCCs.

## Usefulness

- **Good:** a quick offline name lookup with no dependencies, and broad
  historical coverage. That helps label old traces that use retired codes.
- **Not good for:** whether a code is currently assigned, who holds it
  legally, or anything recent. Prefer `merged/` and use Wireshark only as a
  fallback label.
