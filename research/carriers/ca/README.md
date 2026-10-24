# Canada: CNAC (MCC 302)

**Status: built** (`pkg/carriers/cnac`, run by `cmd/national-mnc`, output `data/carriers/ca/`). Originally assessed as Easy. One CSV file, and the richest
lifecycle data per row of the sources surveyed.

## Authority

The **Canadian Numbering Administrator (CNAC)** administers Canadian
numbering resources, including IMSI/MNC, under CRTC oversight and Canadian
assignment guidelines. The remarks cite "the Guideline" for experimental
codes.

## What's published

- **HTML:** <https://cnac.ca/data/MNC_Codes.htm>, "IMSI / MNC Status". One
  table, 85 rows.
- **CSV:** <https://cnac.ca/data/MNC_Codes.csv>, the same data, about 4.8 KB.

```csv
"MCC","MNC","Status","Company","Date Assigned","Remarks"
,,,,,2026-10-23                                   <- as-of date row
302,060,In Service,"Karrier One Inc.","2024-12-23",
302,100,Recovered/Aging,,,"Recovered from Data on Tap Inc."
302,131,In Service,"Xplore Inc.","",
302,991,In Service,"Halton Regional Police Service","2019-04-16","Experimental Purposes iaw the Guideline"
```

- **MNCs** are 3 digits.
- **Status:** In Service (69), Recovered/Aging (14), Assigned (2).
  "Recovered/Aging" means taken back and held before reuse. It has an empty
  company, and the remark says whom it was recovered from.
- **Date Assigned** is filled in on 29 of 85 rows, as ISO dates.
- **Remarks** are free text: "Recovered from X", "Formerly Freedom Mobile",
  "Experimental Purposes iaw the Guideline".
- **The as-of date** is the second row of the CSV.

## Compared with ITU

On 2026-10-24: 71 active nationally (In Service + Assigned), 69 in ITU's
list. **5** are national-only, e.g. 302-310, 302-520, 302-750, 302-820 and
302-991 (Halton Regional Police, experimental). **3** ITU-only codes
(302-390, 302-560, 302-670) appear to be recovered nationally.

## The downloader (as built)

Package `pkg/carriers/cnac`, run by `cmd/national-mnc -only ca`, output
`data/carriers/ca/`:

1. **Fetch** the CSV with `Client.Fresh`: always refetch, keep a cached copy
   for `-offline`. Fall back to parsing the HTML table if the CSV ever
   disappears; the columns are the same.
2. **Parse** with `encoding/csv`:
   - the row with only a 6th field is the as-of date, which goes in `meta`;
   - check the header exactly, and fail loudly if columns change.
3. **Derive:**
   - `status` enum: In Service and Assigned → `assigned`, Recovered/Aging →
     `reclaimed`;
   - `recovered_from` / `formerly`, parsed from remarks;
   - `experimental` (bool), from remarks.
4. **Tables:**
   - `mnc`: mcc, mnc, plmn, status_raw, status, company, date_assigned,
     remarks, recovered_from, experimental;
   - `sources`, `meta`.
5. **Checks:**
   - ≥ 60 in service;
   - 302-220 (Telus) and 302-720 (Rogers) are in service;
   - the as-of date is within 90 days.

Cost: 1 request; a rerun is the same. **Refresh daily or weekly.** The
as-of date shows CNAC updates the file at least as often.

**Change detection:** the file is tiny, so just diff the committed CSV.
`git log -p data/carriers/ca/mnc.csv` then becomes the history, which the
source itself doesn't keep beyond the remarks.

## Notes for the shared schema

- Map directly onto `assignments`.
- `Recovered/Aging` rows → `status = reclaimed`, holder NULL, with
  `recovered_from` in attributes. The source has no status date.
- Experimental assignments → attribute `experimental = true`.

**As built (2026-10-24):** the tool follows this plan. Every source also
writes the shared `assignments` table from `research/schema.md`. It runs as
part of `./LET_IT_RIP.sh`, and the first live run passed all `-verify`
checks. One difference: the freshness check (as-of date age) isn't
enforced, because it would fail offline rebuilds from an old cache. The
as-of date is in `meta` and `assignments.as_of` instead.
