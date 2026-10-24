# A shared schema and dataset

**Can one schema cover ITU and every national registry? Yes, if it's
layered:**

1. a small common core every source can fill;
2. sub-allocations (blocks under an MNC) in their own table;
3. everything source-specific kept as attributes;
4. provenance on every row.

Merging works by treating each national registry as authoritative for its
own MCCs and ITU as the fallback, with disagreements listed rather than
silently resolved.

## Why it works

Every source surveyed agrees on the unit of assignment: **one MNC under one
MCC, held by one entity.** E.212 defines that unit, so the core needs no
interpretation per country.

| Concept | ITU | US | CA | MX | JP | DE | IN |
|---|---|---|---|---|---|---|---|
| MCC + MNC | ✓ | ✓ (`310-170`) | ✓ | ✓ | ✓ (`44010`, one string) | ✓ | ✓ (per letter) |
| Holder | operator | entity (+ directory text) | company | PST (provider) | 事業者 (operator, Japanese) | Zuteilungsinhaber (holder) | addressee of the letter |
| Status | implicit (listed = assigned); SUP = withdrawn; 901 db has ASSIGNED/RECLAIMED/RETURNED | assigned/returned/reclaimed/reserved/test/available/unavailable | In Service/Assigned/Recovered-Aging | implicit (listed = assigned) | 使用中 (in use) / 未使用 (unused) | implicit | implicit (+ cancellations as letters) |
| Assigned date | amendment's OB date (approx.) | effective date | date assigned (partial) | fecha de asignación (assignment date) | — | — | letter date |
| Status date | — | return/reclaim date | — | — | — | — | — |
| Reference document | OB number + amendment | — | — | oficio (assignment letter) | — | — | letter |
| Sub-allocations | extraterritorial MSIN ranges | CBRS IBNs under 315-010 | — | — | — | 262 98 campus blocks (unpublished) | per-circle MNCs (each is a full MNC) |
| Extras | — | FCC licences, protocol, entity type | remarks ("recovered from…") | multiple letters per code | full code space | as-of date | circle, technology |

What doesn't align is extra detail. That goes in attributes, not core
columns.

## Proposed tables

```sql
-- One row per (source, MCC+MNC[, holder]): what each source says.
-- The merged view below picks one per code.
CREATE TABLE assignments (
  source         TEXT NOT NULL,   -- itu | us | ca | mx | jp | de | in ...
  mcc            TEXT NOT NULL,   -- '310'
  mnc            TEXT NOT NULL,   -- '170' (as assigned: 2 or 3 digits, zero-padded)
  plmn           TEXT NOT NULL,   -- '310170'
  country        TEXT NOT NULL,   -- ISO 3166-1 alpha-2 ('US'); 'XS' style codes for 901/902
  holder         TEXT,            -- normalized holder name (NULL when unassigned)
  holder_raw     TEXT,            -- exactly as published
  status         TEXT NOT NULL,   -- assigned | reserved | returned | reclaimed | withdrawn | available | test
  status_raw     TEXT,            -- source's own value ('In Service', '使用中', ...)
  assigned_date  TEXT,            -- YYYY-MM-DD when the source gives one
  status_date    TEXT,            -- date of return/reclaim/withdrawal
  reference      TEXT,            -- legal/document reference (oficio, OB+amendment, letter)
  as_of          TEXT NOT NULL,   -- the source's own "position on" date, else fetch date
  url            TEXT,            -- per-record page if any
  PRIMARY KEY (source, plmn, country)
);

-- Blocks under an MNC: CBRS IBNs, campus-network blocks, MSIN ranges.
CREATE TABLE sub_allocations (
  source   TEXT NOT NULL,
  plmn     TEXT NOT NULL,         -- the parent MCC+MNC, e.g. '315010'
  kind     TEXT NOT NULL,         -- ibn | msin_range | campus_block
  block    TEXT NOT NULL,         -- '0152', '0000099000-0000099099', ...
  holder   TEXT,
  status   TEXT NOT NULL,
  country  TEXT,                  -- for extraterritorial ranges: where it's used
  as_of    TEXT NOT NULL,
  PRIMARY KEY (source, plmn, kind, block)
);

-- Everything else, losslessly: FCC call signs, protocol, remarks, circle...
CREATE TABLE attributes (
  source TEXT NOT NULL, plmn TEXT NOT NULL, block TEXT NOT NULL DEFAULT '',
  key    TEXT NOT NULL, value TEXT NOT NULL,
  PRIMARY KEY (source, plmn, block, key, value)
);

-- What was fetched, when, and from where (already produced per source).
CREATE TABLE sources (source TEXT, kind TEXT, name TEXT, url TEXT, bytes INTEGER, sha256 TEXT, fetched_at TEXT, as_of TEXT);

-- MCC -> country, authority, and which source is authoritative.
CREATE TABLE mccs (
  mcc TEXT PRIMARY KEY, country TEXT NOT NULL, authority TEXT,
  authoritative_source TEXT       -- e.g. 310..316 -> 'us', 302 -> 'ca'; NULL -> 'itu'
);
```

### The merged view

```sql
-- The best-known current assignment per code.
CREATE VIEW current AS
SELECT a.* FROM assignments a JOIN mccs m USING (mcc)
WHERE a.source = coalesce(m.authoritative_source, 'itu')
UNION ALL
-- ITU rows for codes the national source doesn't mention at all
SELECT i.* FROM assignments i JOIN mccs m USING (mcc)
WHERE i.source = 'itu' AND m.authoritative_source IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM assignments n WHERE n.source = m.authoritative_source AND n.plmn = i.plmn);

-- Where ITU and the national registry disagree.
CREATE VIEW conflicts AS
SELECT n.plmn, n.source, n.status AS national_status, n.holder AS national_holder,
       i.status AS itu_status, i.holder AS itu_holder
FROM assignments n JOIN mccs m ON m.mcc = n.mcc AND m.authoritative_source = n.source
LEFT JOIN assignments i ON i.source = 'itu' AND i.plmn = n.plmn
WHERE i.plmn IS NULL OR (n.status = 'assigned') <> (i.status = 'assigned');
```

Rows that come only from ITU in `current` should be flagged. For example,
an ITU entry the national registry has never heard of may be stale. The
`conflicts` view is useful data in its own right: it shows how far ITU lags
each country.

## Problems a merge has to handle

- **MNC length.** MNCs are 2 or 3 digits, and `441 00` ≠ `441 000`. Japan
  moved 441 to 3 digits, and ITU still carries a 2-digit `441 00`. Keep the
  MNC exactly as assigned. Don't zero-pad 2-digit MNCs to 3. Compare
  carefully only where a country is known to have changed lengths.
- **MCC → country isn't 1:1.** One country can have several MCCs (US 310–316,
  IN 404–406, JP 440–441). Shared MCCs 901/902 have no country, and some
  codes are listed under two countries (France/Monaco 208 01, extraterritorial
  use). Hence `country` in the primary key, and the `mccs` table.
- **Status vocabularies.** Map each source's values onto a small enum, and
  keep the original in `status_raw`:

  | enum | ITU | US | CA | JP |
  |---|---|---|---|---|
  | assigned | listed / ASSIGNED | assigned | In Service, Assigned | 使用中 |
  | reserved | — (901 db has reservation dates, no observed RESERVED status) | reserved | — | — |
  | returned | RETURNED | returned | — | — |
  | reclaimed | RECLAIMED | reclaimed | Recovered/Aging | — |
  | withdrawn | SUP amendment | — | — | — |
  | available | — | available | — | 未使用 |

- **Holder names.** The same company is spelled many ways ("AT&T Mobility",
  "AT&T COMERCIALIZACIÓN MÓVIL", "ＫＤＤＩ株式会社").
  - First pass: normalize Unicode (NFKC turns full-width ＫＤＤＩ into KDDI),
    whitespace and case. Keep `holder_raw`.
  - An entity table that groups holders across countries (for example,
    "all AT&T codes") is a separate, harder problem. Leave it until the
    basic merge works.
- **Dates mean different things.** ITU's is when the amendment was published.
  The US has effective and deployment dates. Canada's and Mexico's are when
  the code was assigned. Japan has none. Keep them as `assigned_date` only
  where the meaning matches. Everything else goes in `attributes`.
- **Granularity.** India assigns an MNC per operator *per licence area*, so
  one operator holds dozens. That's still one row per MNC in the schema,
  with `circle` as an attribute.
- **Freshness.** Every row carries `as_of`: the source's own "position on"
  date where it has one (ITU, Japan, Germany, Canada's CSV), else the fetch
  date. Consumers can then see that a national row is newer than the ITU
  row it overrides.

## History

The per-source tables already keep history in different ways:
- ITU: the `amendments` table, every change since the base list.
- US: returned and reclaimed dates.
- Git: every committed CSV refresh.

A merged dataset can add an `assignment_events` table (source, plmn, event,
date, reference). It would be filled from ITU amendments, national status
dates, and differences between refreshes. That's enough to answer "who held
310-170 in 2019?" for the sources that record changes.

## How to build it

1. **Keep the per-source downloaders as they are.** Each owns its parsing and
   its rich, source-specific tables (`data/carriers/<source>/`). They're the
   lossless layer.
2. **Give each source a small mapping step** that emits the shared tables:
   `assignments`, `sub_allocations`, `attributes`. Store it as one more
   output table per source, or derive it in a `cmd/carriers-merge` that
   reads every `data/carriers/<source>/` and writes `data/carriers/merged/`.
   That keeps the sources independent: one failing doesn't block the others.
3. **Add a `mccs` table, committed and maintained by hand.** It maps each MCC
   to its country, its authority and its authoritative source. ITU's MCC list
   (E.212 Annex, `T-SP-E.212A`) could seed it.
4. **Publish `merged/current.csv` and `merged/conflicts.csv`** next to the
   per-source CSVs, using the same raw-URL scheme.
5. **Verify** with checks like the existing ones:
   - every MCC in `assignments` has a `mccs` row;
   - no code is `assigned` twice to different holders in the same source;
   - the size of `conflicts` per country stays within an expected band, so
     a parser regression shows up as a jump.

## What not to do

- **Don't force rich sources into the core.** US licence numbers, Mexican
  letters and Canadian remarks belong in `attributes` or the per-source
  tables.
- **Don't let a national source delete ITU rows silently.** Show them in
  `conflicts`, because the national list might be incomplete. Germany's PDF
  omits nothing ITU has, but India's per-letter data would.
- **Don't pick one canonical holder name across sources yet.** Normalized
  strings plus the raw value are enough to start with.
