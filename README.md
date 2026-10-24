# proto-telephony

## Carrier MCC/MNC lists

Downloaders that build mobile network code (MCC+MNC) registries from their
official sources and write them as CSV (committed here) or SQLite:

| Source | Command | Package | Output |
|---|---|---|---|
| ITU-T E.212 MNC list (worldwide) | `bin/itu-e212` | `pkg/carriers/itu` | [`data/carriers/itu/`](data/carriers/itu) |
| US HNI assignments, [IMSI Admin](https://imsiadmin.com) (MCCs 310–316) | `bin/us-hni` | `pkg/carriers/imsiadmin` | [`data/carriers/us/`](data/carriers/us) |
| Canada, [CNAC](https://cnac.ca/data/MNC_Codes.htm) (MCC 302) | `bin/national-mnc` | `pkg/carriers/cnac` | [`data/carriers/ca/`](data/carriers/ca) |
| Mexico, IFT/CRT (MCC 334) | `bin/national-mnc` | `pkg/carriers/ift` | [`data/carriers/mx/`](data/carriers/mx) |
| Japan, MIC 総務省 (MCC 440/441) | `bin/national-mnc` | `pkg/carriers/mic` | [`data/carriers/jp/`](data/carriers/jp) |

For the source survey (ITU, US, Canada, Mexico, Japan, Germany, India) and
the plan for one shared, merged dataset, see [`research/`](research/README.md).

```bash
./LET_IT_RIP.sh                  # setup + build + test, then rebuild data/carriers/*/ from the live sources
./LET_IT_RIP.sh -format sqlite   # ... or write data/carriers/{itu,us}.sqlite (gitignored)
```

### Download the data

Raw files: `https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/<source>/<table>.csv`

| Table | URL |
|---|---|
| ITU networks (the worldwide MCC/MNC list) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/itu/networks.csv |
| ITU amendments (change history) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/itu/amendments.csv |
| ITU extraterritorial | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/itu/extraterritorial.csv |
| ITU shared_codes (live MCC 901 db) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/itu/shared_codes.csv |
| US hni (every US MCC+MNC with its details) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/us/hni.csv |
| US cbrs (IBNs under shared HNI 315-010) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/us/cbrs.csv |
| US details (every detail-page field, verbatim) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/us/details.csv |
| US identifiers (call signs, FRNs, ...) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/us/identifiers.csv |
| US protocols | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/us/protocols.csv |
| Canada mnc (status, date assigned, remarks) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/ca/mnc.csv |
| Mexico mnc / assignment_letters (oficios + dates) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/mx/mnc.csv, https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/mx/assignment_letters.csv |
| Japan imsi (whole 440/441 code space, in use / unused) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/jp/imsi.csv |
| Shared core format, per country (`research/schema.md`) | https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/ca/assignments.csv, https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/mx/assignments.csv, https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/jp/assignments.csv |

Each source also has `meta.csv` (run info) and `sources.csv` (every
document used, with sha256).

```bash
curl -sL https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/itu/networks.csv | grep ',310170,'
# 310,170,310170,United States,AT&T Mobility,geographic,1280,,
curl -sL https://raw.githubusercontent.com/accretional/proto-telephony/main/data/carriers/us/hni.csv | grep '^310-170,'
```

For US codes, prefer `us/`. Countries assign their own MNCs and don't
always tell ITU. Compared with IMSI Admin, ITU's list is missing 25 of the
439 assigned US HNIs and still lists 35 codes the US registry shows as
returned, reclaimed, or not assigned. For example, `311-910` was returned on
2020-01-22.

### Scripts

Each script calls the previous one, and every step is idempotent:

| Script | Does |
|---|---|
| `setup.sh` | Checks for Go (>= 1.21) and downloads and verifies the modules. No cgo is needed, because SQLite is the pure-Go `modernc.org/sqlite`. |
| `build.sh` | Runs `setup.sh`, then builds `bin/itu-e212`, `bin/us-hni` and `bin/national-mnc`. |
| `test.sh` | Runs `build.sh`, then gofmt, `go vet` and `go test`. The tests are hermetic: synthetic documents served by fake sites. For each source with a cache in `data/cache/<source>`, it also rebuilds both formats offline and verifies them. |
| `LET_IT_RIP.sh` | Runs `test.sh`, then each downloader with `-format csv` against its live source, then `-verify`. Extra args go to `itu-e212` and `us-hni`; `national-mnc` gets `-format`/`-offline`. |

### Shared flags

```
-format sqlite|csv   -db data/carriers/<source>.sqlite   -csv-dir data/carriers/<source>
-cache data/cache/<source>   -workers 4   -offline   -verify   -v
```

Every run prints its stages with timestamps and a progress line per
document. It warns about anything unexpected, and ends with a summary of
counts, HTTP requests and bytes, cache hits, and time per stage. Output is
written to `<path>.tmp` and swapped into place, so a failed run leaves the
previous output intact. `-verify` runs sanity checks. For CSV output, it
first loads the files into an in-memory SQLite, which also checks the round
trip. In CSV, an empty field in a nullable column means NULL.

## itu-e212: ITU-T E.212

ITU publishes the MNC list in three parts, and the tool combines all of them:

1. **The base list.** This is the annex *Mobile Network Codes (MNC) for the
   international identification plan*, publication `T-SP-E.212B-<year>`.
   The newest edition is found automatically: currently 2023, the annex to
   Operational Bulletin 1280. The English `.docx` is parsed. It has four
   tables: geographic MNCs, shared MCC 901, ROIO MCC 902, and
   extraterritorial MSIN ranges.
2. **Every amendment since.** Changes are numbered amendments (ADD / SUP /
   LIR) inside each later Operational Bulletin, about 24 a year. The tool
   scans every bulletin from the base list's OB+1 to the latest, extracts
   the MNC section, and replays the changes in order. Extraterritorial
   changes are included.
3. **The live MCC 901 database** (`/net/ITU-T/inrdb/e212_901.aspx`). It
   includes reservations, reclaimed codes, and assignments too new for a
   bulletin.

Published bulletins never change, so they're cached for good. A rerun takes
about 6 seconds and 4 requests. A run from scratch takes about 1.5 minutes
and 210 requests. Extra flags: `-skip-901`, `-from-ob N`, `-to-ob N`, and
`-dump-docx FILE` (prints a document's text tokens, for parser debugging).

| Table | Contents |
|---|---|
| `networks` | Current list: `mcc`, `mnc`, `plmn`, `country`, `operator`, `kind` (`geographic`/`shared`/`roio`), `source_ob`, `added_amendment`, `changed_amendment`. The key is `(mcc, mnc, country)`, because a code can be listed under two countries. |
| `mcc` (SQLite view) | Country/area per MCC, with network counts |
| `amendments` | Every amendment line, with `outcome` (`applied`/`replaced`/`no_match`/`ambiguous`) and `before` |
| `extraterritorial` | MCC+MNCs used outside their home country, with MSIN ranges (Country A/B) |
| `shared_codes` | Live MCC 901 database: assignee, status, and reservation, assignment and reclamation dates |

## us-hni: US HNIs from IMSI Admin

[IMSI Admin](https://imsiadmin.com) is the US IMSI Administrator, run by
ATIS for the IMSI Oversight Council. It assigns HNIs (MCC+MNC) under US MCCs
310–316, plus IMSI Block Numbers (IBNs) under the shared CBRS HNI 315-010.
The tool reads:

- **Both directories:** `/assignments/hni/` (557 codes) and
  `/assignments/cbrs/` (151 IBNs).
- **Every code's detail page,** extracting all of its fields: HNI code,
  IBN, protocol, wireless license number, the license, application,
  effective, estimated service, deployment, modification and return dates,
  entity name, and entity type.
- **The site's REST post index,** used only to tell which detail pages
  changed. Unchanged pages come from the cache, so a rerun takes about 5
  seconds and 10 requests. A run from scratch takes about 6 minutes and 720
  requests, because the site is slow.

A field label it doesn't know yet is reported as a warning and still kept.

| Table | Contents |
|---|---|
| `hni` | One row per HNI. Directory columns: `entity` as published, and `status` (`assigned`/`returned`/`reclaimed`/`reserved`/`test`/`available`/`unavailable`) with its `status_date` and `related` code. Detail columns: `entity_name`, `entity_type`, `entity_category`, `protocol`, `wireless_license`, the dates (YYYY-MM-DD), `title`, `url`. |
| `cbrs` | The same, per IBN, plus `imsi_prefix` (MCC+MNC+IBN) |
| `details` | Every detail-page field verbatim: `section`, `label`, `value`, and `date` when the value is a date |
| `identifiers` | Identifiers extracted from the free-text fields (Wireless License Number, Entity Type): `call_sign`, `frn`, `uls_file`, `experimental_file`, `cellular_file`, `lease`, `filer_id`, `satellite_filing`, `fcc_document`, `gsma_member`, `market` (CMA/BTA/MTA), `hni` (other HNIs referenced), `number` |
| `protocols` | Technologies per code, normalized (`GPRS`, `EDGE`, `W-CDMA`, `HSPA`, `1xRTT`, `HRPD/EVDO`, `LTE/LTE Advanced`, `5G`) |

## national-mnc: Canada, Mexico, Japan

These national registries each publish one machine-readable file, so one
command pulls them all, each into its own `data/carriers/<cc>/` (or
`<cc>.sqlite`). A failing source doesn't stop the others.

```
bin/national-mnc [-only ca,mx,jp] [-format sqlite|csv] [-out-dir data/carriers]
                 [-cache-dir data/cache] [-offline] [-verify]
```

| cc | Source | Fetch | Tables (besides `assignments`, `sources`, `meta`) |
|---|---|---|---|
| `ca` | CNAC `MNC_Codes.csv` (with an as-of row) | 1 request | `mnc`: status (In Service / Assigned / Recovered/Aging), company, date assigned, remarks, plus `recovered_from`, `formerly` and `experimental` parsed from them |
| `mx` | `mncasignados_DD_MM_YY.xlsx`, found from IFT's "Marcación y Zonas" page; the date in the name is the as-of date | 2 requests; the dated file is cached for good | `mnc`; `assignment_letters` (every oficio with its date and issuer: CFT/IFT/CRT) |
| `jp` | The IMSI `.xlsx` linked from MIC's 電気通信番号指定状況 page (designations; Shift_JIS); its URL changes each edition | 2 requests; cached by edition | `imsi`: all 1,000 codes with operator (NFKC-normalized, raw kept), 使用中 (in use) → `assigned`, 未使用 (unused) → `available`; `recent_designations` (with dates) |

Every source also writes **`assignments`**, the shared core table from
[`research/schema.md`](research/schema.md). It has the same columns in every
country: the first step toward one merged dataset. Germany (a PDF, identical
to ITU) and India (per-allocation letters) aren't included; see
[`research/`](research/README.md).

## Layout

- `pkg/carriers`: shared HTTP client (retries, metrics, cache) and console progress
  - `store`: tables to SQLite or CSV, reading CSV back, and `Verify`
  - `cli`: shared flags and run loop
  - `itu`: E.212 — `.docx` tokenizer, base-list and amendment parsers, replay, 901 database
  - `imsiadmin`: IMSI Admin — directory and detail parsers, identifier and protocol extraction
  - `cnac`, `ift`, `mic`: Canada, Mexico, Japan
  - `xlsx`: a stdlib-only `.xlsx` reader (drops furigana `<rPh>`), with `xlsxtest` to build fixtures
- `cmd/itu-e212`, `cmd/us-hni`, `cmd/national-mnc`: the CLIs
- `data/carriers/<source>/`: CSV output (committed). `data/carriers/*.sqlite` and `data/cache/` are gitignored.
