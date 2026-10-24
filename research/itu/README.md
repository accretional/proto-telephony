# ITU-T E.212: the worldwide MNC list

**Status: built.** Code is in `pkg/carriers/itu` and `cmd/itu-e212`. Output
goes to `data/carriers/itu/` and is refreshed by `./LET_IT_RIP.sh`.

## Who publishes what

ITU-T (the TSB) collects the MNC assignments countries report under E.212
and publishes them in three ways. There is no single download.

| Part | Where | Format | Content |
|---|---|---|---|
| **Base list** | `https://www.itu.int/pub/T-SP-E.212B` → `T-SP-E.212B-<year>`; current edition **2023**, the annex to **Operational Bulletin (OB) 1280**, "position on 15 November 2023" | `.docx` (also PDF; 6 languages) | Four tables: geographic MNCs (country → network → `MCC MNC`), shared MCC 901, ROIO MCC 902, extraterritorial MSIN ranges |
| **Amendments** | Every later Operational Bulletin, `https://www.itu.int/pub/T-SP-OB.<n>-<year>`, about 24 a year | `.docx`, sometimes only inside a `.zip` with annexes | A section "Mobile Network Codes (MNC)… (Amendment No. N)" listing ADD / SUP (withdraw) / LIR (change) by country, sometimes with an "Extra-territorial use" table |
| **Live 901 database** | `https://www.itu.int/net/ITU-T/inrdb/e212_901.aspx` | ASP.NET HTML, paged by postback | Shared-code (901) assignments with status ASSIGNED / RECLAIMED / RETURNED and reservation, assignment and reclamation dates |

A per-country MCC database (`/net/itu-t/inrdb/secured/e212mcc.aspx`) also
exists, but it needs an ITU TIES login. It isn't used.

## Data as of 2026-10-24

- Base list: 2,343 networks in 215 countries/groups, plus 16
  extraterritorial rows.
- Amendments: bulletins 1281–1350 carry amendments 1–66, with 498 changes
  (281 ADD, 124 SUP, 93 LIR). All of them replay cleanly.
- Current list: **2,496 networks**:
  - 2,415 geographic, 80 shared 901, 1 ROIO 902;
  - 224 MCCs;
  - 1,758 two-digit MNCs and 738 three-digit ones.
- Live 901 database: 99 codes (82 assigned, 16 reclaimed, 1 returned). Two
  were assigned in October 2026 and aren't in any bulletin yet.

Output tables (`data/carriers/itu/`):
- `networks`: the current list;
- `amendments`: every change, with its outcome and the prior value;
- `extraterritorial`;
- `shared_codes`;
- `sources`, `meta`.

## Layout quirks we handle

These all appeared in real bulletins.

- **Headings vary.** A heading can be a paragraph or a table cell. Two
  bulletins drop the word "Mobile" from "Mobile Network Codes (MNC)". The
  "Amendment No." line can come before or after the "Annex to OB 1280"
  line. So the parser reads a flat stream of document text, not table
  shapes.
- **Rows are split.** Country/action lines ("Estonia SUP") and code/name
  rows are separate rows or separate paragraphs.
- **Extraterritorial tables** appear inside amendments and have their own
  4-column layout.
- **URLs depend on the year.** A bulletin's URL contains its publication
  year, which you can't derive from the number. It's estimated (about 24
  bulletins a year) and the neighboring years are tried. Unknown
  publications return a 200 page with no files rather than a 404.
- **Some bulletins are zip-only.** The zip also holds annexes, so the
  bulletin is picked as the largest non-annex `.docx`.
- **User-Agent matters.** The 901 page leaves out its row total unless the
  User-Agent looks like a browser, so the tool sends `Mozilla/5.0
  (compatible; proto-telephony/1; …)`.

## Download / refresh / sync

How the built tool works:

1. **Discover:**
   - newest base edition from `/pub/T-SP-E.212B`;
   - newest bulletin number from `/pub/T-SP-OB`;
   - the base list's own OB number from its header.
2. **Fetch** the base `.docx` and every bulletin after it. Published
   bulletins never change, so they're **cached for good** under
   `data/cache/itu/`.
3. **Parse and replay** the amendments in order. Every outcome goes to the
   `amendments` table. Warnings cover any missing amendment number or a
   change that matches no entry.
4. **Fetch the live 901 database**, every page, and report codes assigned
   there but not yet in a bulletin.
5. **Write** atomically, then `-verify`.

Cost: a run from scratch takes about 1.5 minutes and about 210 requests
(26 MB). A rerun takes about 6 seconds and 4 requests.

When ITU publishes a **new base edition**, the tool picks it up and starts
replaying from that edition's OB. Nothing needs changing, but check the
first run's warnings.

## Role in a shared dataset

ITU's list is the **fallback for every country** and the only source for
most. It's also the only machine-readable history of changes, through the
amendments. It lags national registries by weeks to years, and sometimes
keeps codes the country has reclaimed. Compared on 2026-10-24:

| | ITU only | National only |
|---|---|---|
| US | 35 | 25 |
| Canada | 3 | 5 |
| Japan | 2 | 11 |
| Mexico | 0 | 1 |
| Germany | 0 | 0 |

So for countries with a usable registry, prefer the national row and use
ITU to find conflicts. See [`../schema.md`](../schema.md).

## Possible improvements

- **Seed the `mccs` table** for the shared schema from the MCC list annex
  (`T-SP-E.212A`, the "List of mobile country or geographical area codes")
  and its amendments, which the same bulletins carry.
- **Read the French or Spanish editions** as a cross-check on hard-to-parse
  English layouts. They have the same tables.
