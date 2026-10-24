# Mexico: IFT → CRT (MCC 334)

**Status: surveyed, not built. Easy, but watch the regulator change.** One
XLSX file with assignment letter numbers and dates, which none of the
other sources have.

## Authority

MNCs under 334 are assigned by the telecom regulator. Under the 2018
numbering plan (*Plan Técnico Fundamental de Numeración*, in force
2019-08-03) that was the **Instituto Federal de Telecomunicaciones (IFT)**.
The plan says the register of assigned MNCs is public and published on the
regulator's website. Mexico's 2024 notice to ITU (July 2024) repeats the
structure: MCC 334, **3-digit MNCs** administered by the regulator, and a
9-digit MSIN run by the operator.

**Regulator transition.** The most recent row's letter is
`CRT/DG-CAR/8514/2026` (2026). That points to the **Comisión Reguladora de
Telecomunicaciones (CRT)**, IFT's successor under the 2024–25 telecom
reform. The file is still hosted on ift.org.mx, but expect the host and
URLs to move. `crt.gob.mx` didn't resolve when checked.

## What's published

- **Index page:** <https://www.ift.org.mx/usuarios-telefonia-fija/marcacion-y-zonas>,
  with a link labelled "Consulta de Código de Red Móvil (MNC)."
- **File:** `https://www.ift.org.mx/sites/default/files/mncasignados_06_05_26.xlsx`.
  **The date is in the filename** (DD_MM_YY, here 2026-05-06), so the URL
  changes with every update.

One sheet, `MNC`, 26 data rows:

| MCC | MNC | PST (provider) | Oficio de asignación (assignment letter) | Fecha de asignación (date assigned) |
|---|---|---|---|---|
| 334 | 020 | RADIOMÓVIL DIPSA, S.A. DE C.V. (Telcel) | CFT/DO4/AGIT/DGPFT/3089/01 | 36934 → 2001-02-12 |
| 334 | 090 | AT&T COMERCIALIZACIÓN MÓVIL, S. DE R.L. DE C.V. | 6 letters, newline-separated: CFT/D03/USI/DGA/1782/10 … IFT/223/UCS/DG-AUSE/6966/17 | 6 text dates: 11/10/2010 … 15/12/2017 |
| 334 | 250 | WAL-MART INNOVACIÓN, S. DE R.L. DE C.V. | CRT/DG-CAR/8514/2026 | 46148 → 2026-05-06 |

Parsing details:
- **Dates** are usually **Excel serial numbers**, but text (DD/MM/YYYY) in
  multi-letter cells.
- **Letter prefixes date the regulator:** CFT (COFETEL, before 2013), IFT
  (2013–2025), CRT (2025–).
- **Holder names** are full legal names with trailing spaces and uppercase
  accented text.
- **Some rows carry a stray 6th empty cell.**
- **Only assigned codes are listed:** no status column, and no returned
  codes.

## Compared with ITU

On 2026-10-24: 26 national, 25 in ITU's list. **334-250 (Walmart, 2026)**
isn't in ITU yet. Nothing is ITU-only.

## How to build the downloader

Package `pkg/carriers/ift` (name it after the source; rename if CRT takes
over hosting), command `cmd/mx-mnc`, output `data/carriers/mx/`:

1. **Discover:**
   - fetch the index page with `Client.Fresh`;
   - find the link whose text contains "Código de Red Móvil" or whose href
     matches `mncasignados_*.xlsx`;
   - parse the filename date as the as-of date;
   - fall back to searching for "mncasignados" on the regulator's site, or
     a configured URL, and warn.
2. **Fetch** the XLSX (`Client.Cached`, keyed by filename, since a dated
   file never changes).
3. **Read the XLSX** with the standard library: zip plus
   `xl/sharedStrings.xml` and `xl/worksheets/sheet1.xml`, much like the ITU
   package's `.docx` reader. Skip `<rPh>` phonetic runs, which Japan needs
   too. Put a shared `xlsx` reader in `pkg/carriers/` for both.
4. **Normalize:**
   - split multi-letter cells on newlines into
     `assignment_letters(mnc, seq, letter, date)`, pairing letters with
     dates;
   - convert Excel serial dates (1900 system) to ISO;
   - trim names;
   - derive `regulator` from each letter's prefix.
5. **Tables:**
   - `mnc`: mcc, mnc, plmn, holder, first_assigned, last_letter, last_date;
   - `assignment_letters`;
   - `sources`, `meta`.
6. **Checks:**
   - ≥ 20 rows;
   - 334-020 is Radiomóvil Dipsa;
   - MNCs are 3 digits;
   - the filename date is within a year.

Cost: 2 requests. Updates are rare: a few new MNCs a year. **Refresh weekly.**
Change detection compares filenames; a new filename means a new edition.
When codes drop out of the file, record them as withdrawn by comparing
with the previous edition, since the source has no status.

## Notes for the shared schema

- **`reference`:** the assignment letter, the best legal provenance of any
  source.
- **`assigned_date`:** the first letter's date.
- **Status:** every listed row is `assigned`. Withdrawals can only be
  inferred from diffs between editions.
