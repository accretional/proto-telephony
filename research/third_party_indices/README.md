# Third-party MCC/MNC indices

Widely used lists that are **not** registries, compared with the official
data in `data/carriers/merged/` (ITU plus national registries). Fetched by
[`scripts/third_party_indices/`](../../scripts/third_party_indices/README.md)
into `data/vendored/third_party_indices/`. All figures are from runs on
2026-10-24.

| Source | What it really is | Codes | Active official codes listed | Extra codes not in official data | Listed but officially returned/reclaimed | Freshness |
|---|---|---|---|---|---|---|
| [Wikipedia](wikipedia/README.md) | Crowd-edited reference list, with status/bands/notes | 3,395 | **2,388 (95.0%)** | 877 (mostly history) | 130 | Edited weekly |
| [Wireshark](wireshark/README.md) | Name lookup for packet dissection | 3,062 | 2,284 (90.9%) | 657 (mostly history) | 121 | Last data change 2026-02-09 |
| [AOSP](aosp/README.md) | Android carrier/MVNO identification rules | 1,822 | 1,320 (52.5%) | 464 (legacy SIM codes) | 38 | Table v50, 2026-04-16 snapshot |

The official set has 2,514 active codes, out of 2,644 codes in total.
Codes counted here are standard ones only (3-digit MCC, 2–3 digit MNC). The
details are in `data/vendored/third_party_indices/comparison/`.

## Findings

1. **None of these is a registry.**
   - None has assignment dates or a legal-holder field you can rely on.
   - Wikipedia's status means on air, not assigned.
   - Wireshark and AOSP have no status at all.
2. **All three keep codes the registries have dropped.**
   - Most of each source's extra codes are former assignments: India's
     2-digit 405 codes, Russia, Finland, Poland, Italy, Taiwan.
   - About 120–130 of the codes Wikipedia and Wireshark list are ones a
     registry has marked returned or reclaimed: mostly US IMSI Admin, plus
     ITU's shared-901 database and CNAC.
3. **Recent assignments are missing.** All three miss new US 314/316,
   Hong Kong 454 and Japan 441 codes. 49 officially active codes appear in
   none of them.
4. **The three agree on less than you'd hope.**
   - 3,654 codes appear in at least one source or in the official data.
   - Only 1,593 appear in all three sources.
   - 323 appear only in Wikipedia, 75 only in Wireshark, 126 only in AOSP.
   - Holder names often conflict, e.g. 244-20 is Telia in Wireshark and
     Elisa in Wikipedia.
5. **MNC length is a constant trap.** The same network can be written
   with a 2- or 3-digit MNC (405-05 vs 405-005; AOSP stores `45006` and
   `450006` side by side). Compare codes as written, and treat a match at
   the other length as a hint, not a match.
6. **Each source has one unique strength:**
   - Wikipedia has status, bands, notes, ISO country per code, and
     unofficial MCCs (Abkhazia 289, Kosovo 221);
   - AOSP has MVNO matching rules (GID1/SPN/IMSI prefix) and stable
     Android carrier IDs;
   - Wireshark is a dependency-free, long-lived name table.
7. **Licences differ.** Wikipedia is CC BY-SA 4.0, Wireshark GPL-2.0+ and
   AOSP Apache-2.0. Keep that in mind before republishing the derived
   CSVs.

## Suggested use

- **Registry questions** (is a code assigned, and to whom): use
  `data/carriers/merged/` only.
- **Labels for codes the registries don't have** (old traces, retired
  codes): Wikipedia first, then Wireshark.
- **SIM to brand/MVNO:** AOSP `networks` + `attributes`.
- **Change detection:** rerun `scripts/third_party_indices/fetch.sh` and
  diff the CSVs. Wikipedia changes most often.

## Layout

```
research/third_party_indices/
  README.md            this overview
  wireshark/README.md  Wireshark packet-e212.c
  aosp/README.md       AOSP carrier ID table (and where newer copies might be)
  wikipedia/README.md  Wikipedia MNC lists
```
