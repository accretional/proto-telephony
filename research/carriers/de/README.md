# Germany: Bundesnetzagentur (MCC 262)

**Status: surveyed, not built. Low value.** The list is a PDF and matched
ITU's exactly on 2026-10-24. The interesting German data, campus-network
sub-allocations, isn't published.

## Authority

The **Bundesnetzagentur (BNetzA)** assigns IMSI blocks (MCC 262 + MNC)
under the **Nummernplan IMSI**. The consolidated version has applied since
2024-05-09. The IMSI page
<https://www.bundesnetzagentur.de/DE/Fachthemen/Telekommunikation/Nummerierung/IMSI/start.html>
links:
- the numbering plan (`imsi_np.pdf`);
- the application procedure;
- the list of assigned blocks;
- the campus-network and MCC 999 material below.

## What's published

**"Liste der zugeteilten IMSI-Blöcke"** (list of assigned IMSI blocks):
<https://www.bundesnetzagentur.de/DE/Fachthemen/Telekommunikation/Nummerierung/IMSI/DL/imsi_zugbloecke.html>.
Despite the `.html` extension it serves a **PDF**, 2 pages, about 84 KB.

```
Internationale Kennungen für Mobile Teilnehmer (IMSI)
Zugeteilte Blöcke, Stand: 03.08.2026          <- assigned blocks, as of 2026-08-03
MCC  MNC  Zuteilungsinhaber                   <- assignment holder
262  01   Telekom Deutschland GmbH
262  13   Bundesamt für Ausrüstung, Informationstechnik und Nutzung der Bundeswehr
          (BAAINBw)                           <- wrapped onto a second line
262  70   BDBOS                               <- federal agency for public-safety radio (BOS)
262  868  BDBOS                               <- 3-digit MNC
262  869  TKÜV-Netzanbindungen                <- lawful-interception network connections
```

- **34 rows**, columns MCC, MNC and holder only. No status, dates or
  remarks.
- MNCs are mostly **2 digits**, plus two **3-digit** codes, 868 and 869.
- The list is overwritten in place, so holder changes show only between
  editions. A search-indexed copy of the February 2026 edition reportedly
  had 262 19 = Inquam Deutschland GmbH; the August edition, which I read,
  has 450connect GmbH. This is unverified, since I didn't fetch the
  February file.

**Campus networks: 262 98, not published.** Since 2019 BNetzA has assigned
3.7–3.8 GHz spectrum for private local 5G ("Campusnetze"). Under a separate
numbering plan (Vfg. 15/2022), the block **262 98** is shared. Each
campus-network licensee can get sub-blocks of **10,000 IMSIs**, plus
related network identifiers (CSG-ID, TAI, ECGI, GUMMEI, NID). It's the
German counterpart of US CBRS 315-010. **No list of sub-block holders is
published.**

**MCC 999 directory, not published.** ITU allows MCC 999 for private
networks without assignment. BNetzA keeps a **voluntary** directory of
German 999 users (Mitteilung 100/2020) to avoid clashes. Only the
registration form is public.

## Compared with ITU

On 2026-10-24: 34 national = 34 ITU, a perfect match. The PDF adds only an
as-of date and possibly a quicker view of holder changes.

## How to build the downloader

Low priority; ITU covers it. If wanted (package `pkg/carriers/bnetza`,
command `cmd/de-imsi`, output `data/carriers/de/`):

1. **Fetch** the fixed URL above (`Client.Fresh`), since BNetzA overwrites
   the file in place. Its **`Stand:` date is the change marker**: compare it
   with the cached one, and skip parsing when it's unchanged. Also follow
   the link from the IMSI start page in case the URL moves.
2. **Extract the PDF text.** This is the hard part in pure Go. Options:
   - a small PDF text extractor: decompress FlateDecode streams and read
     Tj/TJ text operators. Workable for this simple, single-font table, but
     brittle;
   - a pure-Go library such as `ledongthuc/pdf` or `pdfcpu` content
     extraction (a new dependency);
   - shelling out to `pdftotext -layout` (poppler), with `setup.sh`
     installing it. Simplest and most robust, but adds a system dependency.
3. **Parse lines** matching `^262\s+(\d{2,3})\s+(.+)$`. A line without a
   code continues the previous holder's name (the BAAINBw case). Parse
   `Stand: DD.MM.YYYY`.
4. **Tables:**
   - `imsi_blocks`: mcc, mnc, plmn, holder;
   - `sources`, `meta` (including `stand`).
5. **Checks:**
   - ≥ 30 rows;
   - 262 01 is Telekom Deutschland;
   - the Stand date is within a year.

Cost: 1 request. **Refresh weekly or monthly.** History comes from diffing
editions, which shows holder changes (e.g. the reported 262 19 change).

## Notes for the shared schema

- Rows map directly with `status = assigned`, and `as_of` from `Stand`.
- **Sub-allocations:** if BNetzA ever publishes the 262 98 campus blocks,
  they belong in `sub_allocations` (`kind = campus_block`), like US CBRS.
  Ask BNetzA whether the list can be shared. A freedom-of-information
  request (IFG) is a possible route.
