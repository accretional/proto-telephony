# Third-party MCC/MNC indices

Fetchers for widely used **non-authoritative** MCC/MNC lists, plus a
comparison against the official data in `data/carriers/merged/`. They're
here to measure how these lists drift from the registries, and to
cross-check names. They aren't a source of truth. See
[`research/third_party_indices/`](../../research/third_party_indices/README.md)
for what each list is and how it differs.

| Script | Source | Output |
|---|---|---|
| `wireshark.py` | Wireshark `epan/dissectors/packet-e212.c` (GitLab) | `wireshark/` |
| `aosp.py` | AOSP TelephonyProvider `assets/latest_carrier_id/carrier_list.textpb` (googlesource) | `aosp/` |
| `wikipedia.py` | English Wikipedia "Mobile network codes in ITU region 2xx–7xx" + "Mobile country code" (MediaWiki API) | `wikipedia/` |
| `compare.py` | The three outputs above + `data/carriers/merged/current.csv` | `comparison/` |
| `fetch.sh` | Runs all four | all of the above |

Output goes under `data/vendored/third_party_indices/`.

```bash
scripts/third_party_indices/fetch.sh                       # CSV: <source>/<table>.csv
scripts/third_party_indices/fetch.sh --format sqlite       # SQLite: <source>.sqlite (gitignored)
scripts/third_party_indices/fetch.sh --format csv,sqlite   # both
scripts/third_party_indices/fetch.sh --offline             # rebuild from the download cache only

scripts/third_party_indices/aosp.py --ref main             # a specific branch/tag (default: newest androidNN-release)
scripts/third_party_indices/wireshark.py --ref <commit>    # a specific commit (default: master)
```

Requirements: Python 3.9+, standard library only (`csv`, `sqlite3`,
`urllib`). `compare.py` also needs `data/carriers/merged/current.csv`; run
`./LET_IT_RIP.sh` first if it's missing, otherwise `fetch.sh` skips the
comparison.

## Common flags

| Flag | Meaning |
|---|---|
| `--format csv\|sqlite\|csv,sqlite` | Output format(s). Default `csv`. |
| `--out DIR` | Output root. Default `data/vendored/third_party_indices`. |
| `--cache DIR` | Download cache root. Default `data/cache/third_party_indices` (gitignored). |
| `--offline` | Don't touch the network; fail if a cached download is missing. |

Every download is cached as `<cache>/<source>/<name>`. Each output also
gets:
- `meta`: run info, the exact upstream revision (Wireshark commit, AOSP
  commit and table version, Wikipedia revision IDs), and row counts;
- `sources`: every document used, with its URL and sha256.

This matches the layout of `data/carriers/<source>/`. Files are written to a
temporary name and renamed, so a failed run leaves the previous output in
place. Each script refuses to write if the row count looks implausibly low
(for example, an upstream format change).

## Tables

**`wireshark/`**
- `mcc`: mcc, country, unassigned, comment. All 800 MCC slots, including
  "Unassigned" placeholders.
- `networks`: mcc, mnc, plmn, mnc_digits, name, comment.

**`aosp/`**
- `carriers`: canonical_id, carrier_name, parent_canonical_id,
  attribute_sets, mccmnc_tuples.
- `attributes`: canonical_id, attribute_index, key, value. Every
  `CarrierAttribute` value in long form: mccmnc_tuple, spn, gid1, gid2,
  imsi_prefix_xpattern, iccid_prefix, preferred_apn, privilege_access_rule.
- `networks`: mcc, mnc, plmn, canonical_id, carrier_name,
  parent_canonical_id, attribute_index, match_keys. One row per MCC+MNC
  tuple per attribute set. `match_keys` lists the other fields that
  further narrow the match (for example `gid1` for an MVNO). The schema is
  [`third_party/proto/android/telephonyprovider/carrierId.proto`](../../third_party/proto/android/telephonyprovider/carrierId.proto).

**`wikipedia/`**
- `networks`: mcc, mnc, plmn, code_format, page, section, country, iso,
  brand, operator, status, status_norm, bands, notes.
  - `code_format` is `nonstandard` for France's 5-digit blocks under
    208 50 and one range row ("314 100 - 190").
  - `status_norm` is the status lowercased.
- `mccs`: mcc, country, iso, authority, remarks. From the "Mobile country
  code" article, one row per (MCC, territory).
- `pages`: title, revid, timestamp, url (permalink), network_rows.

**`comparison/`**
- `codes`: one row per PLMN seen anywhere. It holds the official status,
  holder and source, plus each source's name for the code, Wikipedia's
  status, and `in_<source>` flags.
- `summary`: per source:
  - how many PLMNs and MCCs it lists;
  - how many officially active codes it lists, as a count and a
    percentage, and how many it's missing;
  - how many codes it lists that the official data marks returned or
    reclaimed;
  - how many codes it lists that aren't in the official data, and how many
    of those exist officially with a different MNC length.

Codes are compared as written (`mcc + mnc`), so `31026` and `310260` are
different codes. In the official data, "active" means `assigned`,
`reserved` or `test`.

## Licensing of the data

The CSVs are derived from third-party content under different licences.
Keep this in mind before publishing them:
- Wireshark: GPL-2.0-or-later, the licence of `packet-e212.c`.
- AOSP: Apache-2.0.
- Wikipedia: CC BY-SA 4.0. Attribution is via the page permalinks in
  `wikipedia/pages.csv`, and derivatives must be shared alike.
