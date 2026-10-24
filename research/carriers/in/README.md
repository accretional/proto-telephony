# India: Department of Telecommunications (MCC 404, 405, 406)

**Status: surveyed, not built. Hard.** There's no consolidated list. DoT
publishes **one PDF letter per allocation**, listed on a site that's now a
client-rendered app. Use ITU's list for India for now.

## Authority

The **Department of Telecommunications (DoT)**, under the Ministry of
Communications, allots MSC codes, MCC-MNC codes and other "basic codes" to
licensed operators. Unlike most countries, India assigns MNCs **per
operator per licensed service area (LSA, or "circle")**. So one operator
holds dozens of MNCs, one per circle. The MCCs are 404, 405 and 406.

## What's published

- **Listing:** <https://dot.gov.in/allotment-msc-mcc-mnc-and-basic-codes>
  (paginated with `?page=N`, 15+ pages). Older entries are under
  <https://dot.gov.in/access-services/allotment-msc-codes>.
- **Each entry is a letter** with a date and a PDF download, e.g.:
  - "Allocation of MCC-MNC code to M/s BSNL for launching of 4G services in
    Delhi LSA" (Feb 2024, 404-039);
  - a Nov 2024 corrigendum changing BSNL's Delhi code from 039 to 39 and
    Mumbai's from 065 to 65:
    <https://dot.gov.in/sites/default/files/Corrigendum%20Allocation%20of%20MCC%20MNC%20code%20to%20BSNL%20for%20Mumbai%20and%20Delhi%20LSA.pdf?download=1>;
  - "MCC MNC Code for LTE based captive network" (2022).
- **The listing mixes MCC-MNC letters with MSC codes, LRNs and other
  numbering.** Only some entries are MNC allocations.
- **Cancellations and reallocations are letters too.** After the 2012
  Supreme Court licence cancellations, codes were reissued.
- **The site is now a Next.js app.** The listing HTML is a 4.4 KB shell, and
  the data loads client-side from a shared government CMS (the JavaScript
  references `cmsmsde.digifootprint.in`). `dot.gov.in/wp-json/` isn't
  exposed. The PDF URLs under `/sites/default/files/` still work directly.

Third-party lists (TelecomTalk's circle-wise Jio list, user-uploaded Scribd
tables) exist, but they aren't authoritative.

## Compared with ITU

ITU's list has **172** Indian MNCs (93 under 404, 79 under 405, none under
406). With no national list, there's nothing to diff. Spot checks against
the DoT letters above (BSNL 404-039 → 39) are the only check available.

## How a download/sync tool could work

It's hard but possible, in layers:

1. **Get the listing.** Pick one:
   - **(a) Headless browser** (chromedp) to render
     `allotment-msc-mcc-mnc-and-basic-codes?page=N`, then read the entry
     table: title, date, PDF link. Most robust, but heaviest.
   - **(b) Reverse-engineer the data call:** watch the browser's network
     requests for the JSON endpoint the app uses (probably the CMS proxy)
     and call it directly. Cheapest per run, but undocumented and may
     change.
   - **(c) Seed from search engines and the Wayback Machine** for older
     pages that were plain HTML, then take only new entries from (a) or (b).
2. **Filter** to MNC letters by title: `/MCC[\s-]*MNC/i`, excluding MSC/LRN
   letters.
3. **Fetch each PDF once** (`Client.Cached`; letters never change).
4. **Extract the allocation from each letter.** Letters are prose with
   small tables, roughly "MCC-MNC 404-39 is allotted to M/s BSNL for Delhi
   LSA". Steps:
   - PDF → text, with the same tooling as Germany (pdftotext or a pure-Go
     extractor);
   - **scanned letters need OCR** (tesseract), which is a real cost: some
     older DoT PDFs are images;
   - regexes for `(404|405|406)[\s-]*(\d{2,3})`, `M/s ([^,]+)`,
     `(\w[\w ]+) (LSA|Service Area|Circle)`, and the letter date;
   - every extraction gets a confidence score and the letter URL. When
     unsure, flag it rather than guess.
5. **Build state by replaying letters in date order**, like ITU
   amendments: allocations, corrigenda (replace) and cancellations
   (withdraw). Keep `letters(url, date, title, text)` and
   `allocations(mcc, mnc, operator, circle, letter_url, action)`.
6. **Reconcile with ITU:** every ITU India code should map to a letter, and
   every unmatched letter or ITU row goes into a conflicts report. The
   letters alone will be incomplete for old allocations, so **ITU stays the
   baseline** and letters add circle, date and provenance.
7. **Checks:**
   - fail if the listing yields 0 entries, which means the app changed;
   - warn on PDFs that need OCR;
   - check that known anchors (BSNL Delhi 404-39) are found.

Cost: on the first run, every page plus a few hundred PDFs. After that,
only new entries. Updates are infrequent. **Refresh monthly.**

**Alternatives to scraping:**
- ask DoT, or TRAI, for the consolidated list; DoT must keep one internally;
- an RTI (Right to Information) request;
- watch for it appearing on data.gov.in.

## Notes for the shared schema

- **One MNC per (operator, circle)** fits `assignments` as is. The `circle`
  goes in `attributes`, and the letter becomes `reference`.
- **Corrigenda that change a code's form** (039 → 39) show the 2-vs-3-digit
  issue again. Keep the MNC exactly as the current letter states.
