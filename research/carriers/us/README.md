# United States: IMSI Admin (MCC 310–316)

**Status: built.** Code is in `pkg/carriers/imsiadmin` and `cmd/us-hni`.
Output goes to `data/carriers/us/` and is refreshed by `./LET_IT_RIP.sh`.

## Authority

US MNCs, called **HNIs** (Home Network Identities, i.e. MCC+MNC), are
assigned by the **US IMSI Administrator**, [imsiadmin.com](https://imsiadmin.com).
ATIS runs it for the **IMSI Oversight Council (IOC)** under the IOC's
assignment guidelines. The FCC doesn't assign HNIs, but applicants
generally cite FCC licences. robots.txt allows everything used here (it
only disallows `/invoice/`, `/entity/` and the dashboards).

## What's published

| Page | Content |
|---|---|
| `/assignments/hni/` | One HTML table (`class="sortable"`) of every HNI: `HNI-Code` (`310-170`) and `Entity`, with each row linking to a detail page. **557 codes** across 310 (123), 311 (133), 312 (99), 313 (98), 314 (94) and 316 (10). |
| `/assignments/cbrs/` | The same for the **shared CBRS HNI 315-010**: `HNI-Code`, `IBN` (4-digit IMSI Block Number) and `Entity`. **151 IBNs.** |
| `/hni-codes/<slug>/`, `/cbrs-codes/<slug>/` | One detail page per code. Under "Code Details" and "Entity Details" headings, each field is a `<strong>Label:</strong> value` line. |
| `/wp-json/wp/v2/{hni-codes,cbrs-codes}` | The WordPress REST API. It has **no field data** (ACF isn't exposed), but it lists every post's `link` and `modified_gmt`, 100 per page. We use it only for change detection. |

Detail-page fields: 13 labels across all 708 pages. Counts show how many
pages have each.

| Label | Count | Notes |
|---|---|---|
| HNI Code | 708 | |
| IBN | 151 | CBRS only |
| Protocol | 582 | Free text over the form's checklist: GPRS, EDGE, W-CDMA, HSPA, 1xRTT, HRPD/EVDO, LTE/LTE Advanced, 5G. Misspellings like "LTE/LTE Advances". |
| Wireless License Number | 440 | Free text: FCC call signs, FRNs, ULS/experimental/cellular file numbers, lease IDs, notes ("N/A", "see attached"). The site **truncates values at 25 characters**. |
| License / Application / Effective / Estimated Service / Deployment / Modification / Return Date | 388 / 60 / 597 / 149 / 324 / 172 / 46 | "June 7, 1995" and "08/28/2026" styles, plus placeholders (2001-01-01, 1930-10-11, 2086-02-07) |
| Entity Name | 708 | For returned codes, replaced by the status note |
| Entity Type | 566 | Usually one of three form categories; older records have free-text service descriptions, often naming call signs and markets (CMA/BTA) |

**Directory entity text carries the status.** Examples:
- "RETURNED 10/20/25 (316-700)", "Returned on 10/2/18", "RECLAIMED 12/15/25 (310-790)";
- "RESERVED FOR PUBLIC SAFETY", "TEST IMSI HNI", "Not available for assignment";
- "XF Wireless Investments (314-200)", which is a cross-reference.

Parsed statuses: HNI 439 assigned, 87 returned, 15 reserved, 12 reclaimed,
2 test, 1 available, 1 unavailable. CBRS 111 assigned, 24 reclaimed, 16
returned.

**Public safety block.** 313-100–190 and 314-100–190 are reserved for
public safety. 313-100–140 went to AT&T FirstNet; 313-100 is the live one,
with licence WQQE234, deployed 2014. The rest carry placeholder data.

## Output tables

- `hni` and `cbrs`: one row per code, with:
  - the directory text, and the status, date and related code parsed from it;
  - every detail field, with dates as YYYY-MM-DD;
  - `entity_category`;
  - the title and URL.
- `details`: every field verbatim, in page order, with section, label, value
  and a parsed date.
- `identifiers`: 433 typed identifiers extracted from the free text:
  - 286 `call_sign`, 40 `frn`, 20 `lease`, 18 `market`, 8 `hni` references, 42 `number`;
  - plus `uls_file`, `experimental_file`, `cellular_file`, `filer_id`, `gsma_member`, `fcc_document` and `satellite_filing`.
- `protocols`: 819 rows, normalized.
- `sources`, `meta`.

## Download / refresh / sync

How the built tool works:

1. **Directories:** 2 requests, always refetched.
2. **Change index:** the REST API's `link` + `modified_gmt` for both post
   types, 8 requests.
3. **Detail pages:** cached next to their `modified_gmt` stamp, and
   refetched only when it changes. With no stamp (the index is down), all
   are refetched. 4 workers.
4. **Extract:**
   - warn on any field label not in the known list (it's kept in `details`
     anyway);
   - warn on a date that won't parse;
   - warn on a detail page that disagrees with the directory.
5. **Write** atomically, then `-verify` (≥500 HNIs, 310-170 is AT&T, every
   code has details, and so on).

Cost: a run from scratch takes about **6 minutes, 718 requests (62 MB)**,
because the site is slow at about 2 seconds per page. A rerun takes about
**5 seconds and 10 requests**.

Risks:
- The site is a WordPress theme. A redesign would break the table and
  detail regexes; the `-verify` checks catch that as a hard failure.
- The REST index is a convenience. If it goes away, the tool falls back to
  fetching every page.

## Compared with ITU

On 2026-10-24, for MCC 310–316, from `data/carriers/merged/conflicts.csv`:
- **25** assigned HNIs are missing from ITU's list (`not_in_itu`).
- **16** codes ITU still lists as assigned have been returned or reclaimed
  here (`itu_stale`). For example, ITU lists 310-790 as PinPoint
  Communications, but it was reclaimed on 2025-12-15.
- **1** ITU code isn't in the US directory at all (`itu_only`): 314-620,
  Memphis Light, Gas and Water.
- **4** holders differ (`holder_differs`). For example, 310-040 is Minute
  Wireless LLC here and "Mobi" in ITU.

ITU also carries the 15 public-safety reservations and the 2 test codes.
Both sources agree on those, so they aren't conflicts. (An earlier count of
"35 ITU codes not assigned here" lumped them together with the 16 stale
codes, 1 unknown and a code reclaimed on both sides.)

Some codes are in neither list as assigned. 311-910, which Twilio Lookup
showed for a Google Voice number, was returned on 2020-01-22 and isn't
listed by ITU. Prefer this source for US codes.

## Notes for the shared schema

- **Statuses** map directly onto the enum.
- **CBRS IBNs** become `sub_allocations` (`kind = ibn`, parent `315010`).
- **Licences, protocol and entity type** become `attributes`.
- **`assigned_date`:** use the effective date, which is the closest match to
  "assigned". Keep the others as attributes.
