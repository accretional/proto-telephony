# proto-telephony

## itu-e212: the official MCC/MNC carrier list as CSV or SQLite

`cmd/itu-e212` builds the ITU-T E.212 Mobile Network Code list, the registry
behind every MCC+MNC (e.g. `310 170` → AT&T Mobility, United States), from
ITU's own publications. It writes either CSV files (one per table) or a
SQLite database.

### Download the data

The CSVs are committed in [`data/carriers/`](data/carriers) and can be
fetched directly:

| File | URL |
|---|---|
| networks (the MCC/MNC list) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/networks.csv |
| amendments (change history) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/amendments.csv |
| extraterritorial | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/extraterritorial.csv |
| shared_codes (live MCC 901 db) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/shared_codes.csv |
| sources | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/sources.csv |
| meta | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/meta.csv |

```bash
curl -sL https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/networks.csv | grep ',310170,'
# 310,170,310170,United States,AT&T Mobility,geographic,1280,,
```

### Rebuild it

```bash
./LET_IT_RIP.sh                  # setup + build + test, then download from ITU and rewrite data/carriers/*.csv
./LET_IT_RIP.sh -format sqlite   # ... or write data/carriers.sqlite (gitignored)
```

Each script calls the previous one, and every step is idempotent:

| Script | Does |
|---|---|
| `setup.sh` | Checks for Go (>= 1.21) and downloads and verifies the modules. No cgo is needed, because SQLite is the pure-Go `modernc.org/sqlite`. |
| `build.sh` | Runs `setup.sh`, then builds `bin/itu-e212`. |
| `test.sh` | Runs `build.sh`, then gofmt, `go vet` and `go test`. The tests are hermetic: synthetic ITU documents served by a fake ITU site. If `data/cache` exists, it also rebuilds both formats offline and verifies them. |
| `LET_IT_RIP.sh` | Runs `test.sh`, then `bin/itu-e212 -format csv` against itu.int, then `-verify`. Extra args are passed through. |

### Where the data comes from

ITU doesn't publish the MNC list as one downloadable file. It comes in three
parts, and the tool fetches and combines all of them:

1. **The base list.** This is the annex *Mobile Network Codes (MNC) for the
   international identification plan*, publication `T-SP-E.212B-<year>`.
   The newest edition is found automatically: currently 2023, the annex to
   Operational Bulletin 1280, position on 15 November 2023. The English
   `.docx` is parsed. It has four tables: geographic MNCs, shared MCC 901,
   ROIO MCC 902, and extraterritorial MSIN ranges.
2. **Every amendment since.** Changes are published as numbered amendments
   (ADD / SUP / LIR) inside each later Operational Bulletin (`T-SP-OB.<n>-<year>`,
   about 24 a year). The tool scans every bulletin from the base list's OB+1
   to the latest one ITU lists, extracts the MNC section, and replays the
   changes in order. Extraterritorial changes are included.
3. **The live MCC 901 database** (`/net/ITU-T/inrdb/e212_901.aspx`). It
   includes reservations, reclaimed codes, and assignments too new for a
   bulletin.

ITU's per-country MCC database is behind a TIES login and isn't used.
Countries assign their own MNCs and only some of them notify ITU, so a few
national assignments aren't in this list. For example, US code `311 910` is
missing even though `310 170` is present. For complete US data, see the
national IMSI administrator.

### Running

```
bin/itu-e212 [-format sqlite|csv] [-db data/carriers.sqlite] [-csv-dir data/carriers]
             [-cache data/cache] [-workers 4] [-offline] [-skip-901]
             [-from-ob N] [-to-ob N] [-v]
bin/itu-e212 -verify [-format ...]   # sanity-check existing output (CSV is loaded into SQLite to check)
bin/itu-e212 -dump-docx FILE.docx    # print a document's text tokens (parser debugging)
```

The tool prints each stage with timestamps. It reports each bulletin as it
arrives: its size, download time, the amendment found, and running totals.
It warns about anything off, such as a missing amendment number or a change
that matches no entry. It ends with a summary: counts, outcomes, HTTP
requests and bytes, cache hits, and time per stage. A run from scratch makes
about 210 requests (about 26 MB) and takes about 1.5 minutes.

Published bulletins never change, so downloads are cached in `data/cache`. A
rerun only fetches the index pages, the 901 database, and any new bulletins,
which takes seconds. `-offline` rebuilds entirely from the cache.

Output is written to `<db>.tmp` or `<csv-dir>.tmp` and swapped into place,
so a failed run leaves the previous output intact.

### Schema

The CSV files and the SQLite tables have the same columns. In CSV, an empty
field in a nullable column (`added_amendment`, `changed_amendment`,
`msin_range`, `before`, the 901 dates) means NULL.

| Table | Contents |
|---|---|
| `networks` | Current list: `mcc`, `mnc`, `plmn` (`310170`), `country`, `operator`, `kind` (`geographic`/`shared`/`roio`), `source_ob`, `added_amendment`, `changed_amendment`. The key is `(mcc, mnc, country)`, because a code can be listed under two countries. |
| `mcc` (view) | Country/area for each MCC, with network counts |
| `amendments` | Every amendment line: `amendment`, `ob`, `seq`, `country`, `action`, `mcc`, `mnc`, `operator`, `msin_range`, `outcome` (`applied`/`replaced`/`no_match`/`ambiguous`), `before` |
| `extraterritorial` | MCC+MNCs used outside their home country, with MSIN ranges (Country A/B) |
| `shared_codes` | Live MCC 901 database: assignee, status, reservation, assignment and reclamation dates |
| `sources` | Every document used: URL, bytes, sha256, whether it came from the cache |
| `meta` | Base edition, OB range, last amendment, timings |

```sql
SELECT operator, country FROM networks WHERE plmn = '310170';
SELECT * FROM amendments WHERE mcc = '248' AND mnc = '33';   -- an entry's history
```

### Layout

- `pkg/carrier`: the library.
  - `docx.go`: `.docx` → ordered text tokens
  - `e212b.go`: base-list parser
  - `amendment.go`: bulletin amendment parser
  - `apply.go`: amendment replay
  - `itu.go`: fetching, discovery and caching
  - `shared901.go`: 901 database
  - `pipeline.go`: `Build`
  - `store.go`: SQLite and `Verify`
  - `csv.go`: CSV output, and loading it back
- `cmd/itu-e212`: the CLI and its progress reporter.
- `data/carriers/`: the CSV output (committed). `data/carriers.sqlite` and the download cache `data/cache/` are gitignored.
