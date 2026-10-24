# Research: mobile network code (MCC/MNC) registries

Notes on where the world's mobile network codes are officially published,
what each source contains, and how to download and keep each one in sync.
**The goal is one shared, regularly refreshed dataset** of every MCC+MNC
assignment: who holds it, its status, and its provenance. It combines ITU's
worldwide list with national registries wherever they are better.
[`schema.md`](schema.md) covers whether and how that shared schema works.

## Background

An IMSI (the subscriber identity on a SIM) is `MCC + MNC + MSIN`. ITU-T
Recommendation **E.212** governs these codes:

- **MCC:** 3 digits, assigned to countries by ITU. Some countries hold
  several: the US has 310–316, Japan 440–441, India 404–406.
- **MNC:** 2 or 3 digits. Each country assigns its own under its MCCs.
  ITU assigns them directly only under the shared MCCs 901 and 902.
- **MSIN:** the rest of the IMSI, assigned by the network operator.

The same pair, MCC+MNC, also identifies the network as a whole. It's called
the PLMN ID, or in the US the HNI. It's what phone lookups return as
`mobile_country_code`/`mobile_network_code`.

Countries are supposed to report their assignments to ITU, which publishes
them in a combined list. They report late or never, so **ITU's list is
broad but lagging, and national registries are authoritative but
scattered.**

## Sources surveyed

| Source | Scope | Format | Extras beyond code + holder | Status in this repo | Notes |
|---|---|---|---|---|---|
| [ITU-T E.212](itu/) | Worldwide, all MCCs | `.docx` base list + `.docx` bulletin amendments + live HTML (901) | Amendment history, extraterritorial MSIN ranges, 901 status/dates | **Built**: `cmd/itu-e212` → `data/carriers/itu/` | [itu/](itu/README.md) |
| [US](carriers/us/): IMSI Admin (ATIS) | 310–316 + CBRS 315-010 | HTML directory + 708 detail pages | Status, dates, FCC licenses, protocol, entity type | **Built**: `cmd/us-hni` → `data/carriers/us/` | [us/](carriers/us/README.md) |
| [Canada](carriers/ca/): CNAC | 302 | **CSV** | Status (incl. recovered), date assigned, remarks | Proposed (easy) | [ca/](carriers/ca/README.md) |
| [Mexico](carriers/mx/): IFT → CRT | 334 | **XLSX** | Assignment letter numbers + dates | Proposed (easy) | [mx/](carriers/mx/README.md) |
| [Japan](carriers/jp/): MIC (総務省) | 440, 441 | **XLSX** (+PDF) | Full code space with in-use / unused | Proposed (medium) | [jp/](carriers/jp/README.md) |
| [Germany](carriers/de/): BNetzA | 262 | **PDF** | Only an as-of date; identical to ITU | Low value | [de/](carriers/de/README.md) |
| [India](carriers/in/): DoT | 404, 405, 406 | One PDF letter per allocation, behind a JS app | Circle (licence-area) structure | Hard; use ITU for now | [in/](carriers/in/README.md) |

All figures in these notes come from runs and downloads made on 2026-10-24,
unless stated otherwise.

## Findings

1. **Every registry has the same core:** MCC, MNC and holder. E.212 defines
   what an assignment is, so this part lines up everywhere. Most also have
   some kind of status. Fewer have dates.
2. **Everything else varies:**
   - who runs the registry: a regulator, or an industry body in the US
   - format: CSV, XLSX, PDF, HTML, or letters
   - detail, from 3 columns (Germany) to 20+ fields (US)
   - language and character set
   - how codes are organized: per licence area in India, the full code
     space in Japan, sub-blocks of a shared code (US CBRS, German campus
     networks)
3. **ITU lags the national registries.** Comparing national lists with
   ITU's for the same MCCs:

   | | National | ITU | National only | ITU only |
   |---|---|---|---|---|
   | US (assigned HNIs) | 439 | 449 | 25 | 35 (mostly codes returned or reclaimed nationally) |
   | Canada (active) | 71 | 69 | 5 | 3 (recovered nationally) |
   | Japan (in use) | 68 | 59 | 11 | 2 (incl. a stale 2-digit `441 00`) |
   | Mexico | 26 | 25 | 1 (assigned 2026) | 0 |
   | Germany | 34 | 34 | 0 | 0 |

4. **Sources move and change format.**
   - Two of the four downloadable national files (Japan, Mexico) get a new
     URL on every update.
   - Mexico's regulator is being replaced (IFT → CRT).
   - India's site was rebuilt as a client-rendered app.
   - So a sync tool has to find each file from a stable index page, not a
     hard-coded URL, and it needs a cache plus change detection.
5. **Some codes are subdivided below the MNC level.** In the US, the shared
   CBRS code 315-010 has 151 IMSI block numbers (IBNs). In Germany, 262 98
   is shared by 5G campus networks in sub-blocks of 10,000 IMSIs, but who
   holds them isn't published. ITU's list has extraterritorial MSIN ranges.
   A shared schema needs a level below the MNC.

## Layout

```
research/
  README.md          this overview
  schema.md          a shared schema and dataset: whether and how
  itu/README.md      ITU-T E.212 (built)
  carriers/
    us/README.md     IMSI Admin (built)
    ca/README.md     CNAC
    mx/README.md     IFT / CRT
    jp/README.md     MIC (総務省)
    de/README.md     BNetzA
    in/README.md     DoT
```

Code lives in `pkg/carriers/` (one package per source plus shared
`store`/`cli`). Committed output lives in `data/carriers/<source>/`. See the
repo [README](../README.md).

## Suggested next steps

1. Add downloaders for **Canada** and **Mexico**. Both are single-file sources
   with good extras.
2. Then **Japan**. It needs an XLSX reader and Japanese text normalization.
3. Build the **merged dataset** from [`schema.md`](schema.md): the `assignments`
   view across ITU plus national sources, with conflicts listed.
4. Survey more countries the same way, starting with large markets and
   regulators known to publish machine-readable data. Ofcom (UK) and ARCEP
   (France) are likely candidates, though not yet verified.
5. Run `LET_IT_RIP.sh` on a schedule, e.g. weekly. Commit only when the data
   tables change, not when only `meta.csv` or `sources.csv` does.
