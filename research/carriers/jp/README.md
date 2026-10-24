# Japan: MIC / 総務省 (MCC 440, 441)

**Status: surveyed, not built. Medium difficulty:** XLSX plus Japanese text
normalization. The only source that lists the **whole code space**, used
and unused.

## Authority

The **Ministry of Internal Affairs and Communications (MIC, 総務省)**
designates telecom numbers (電気通信番号) to carriers, IMSI among them.
It's done under the Telecommunications Business Act and the numbering
rules (電気通信番号計画).

**Since 2019:**
- MCC **440** keeps **2-digit** MNCs (`440-YY-…`);
- MCC **441** uses **3-digit** MNCs (`441-YYY-…`), to make room for
  full-MVNOs and private-LTE/5G operators.

## What's published

- **Index:** <https://www.soumu.go.jp/main_sosiki/joho_tsusin/top/tel_number/number_shitei.html>,
  "電気通信番号指定状況" (designations by number type). The page is
  Shift_JIS. Item **"9. IMSI"** links a PDF and an XLSX:
  - as of 2026-09-01: `https://www.soumu.go.jp/main_content/001090876.xlsx`
    (PDF `001090875.pdf`);
  - **the `main_content` ID changes with every edition.**
- The same page lists **recent designations**, e.g. "IMSI 441214
  ヴルーヴ株式会社 令和8年8月10日" (designated 2026-08-10).
- **e-Gov open data** (`data.e-gov.go.jp`, dataset `soumu_20160325_0034`)
  lists an IMSI resource. Its URL (`000749616.xlsx`) is **dead (404)**, so
  don't rely on the e-Gov catalog.

XLSX (sheet `LST`, about 1,005 rows):

```
電気通信番号指定状況                      <- title
・IMSI
（令和８年９月１日現在）                   <- as-of date, Japanese era: Reiwa 8 = 2026
番号    | 事業者                       | 使用状況     <- number | operator | in-use status
44000   | ソフトバンク株式会社         | 使用中      <- SoftBank, in use
44010   | 株式会社ＮＴＴドコモ         | 使用中      <- NTT Docomo
441005  | Ｗｉｒｅｌｅｓｓ　Ｃｉｔｙ　Ｐｌａｎｎｉｎｇ株式会社 | 使用中
44xxx   |                              | 未使用      <- unused
```

- **`番号` (number)** is MCC+MNC as one string: 5 digits under 440, 6 under
  441. All 100 + 900 codes are listed: **68 in use (使用中), 932 unused
  (未使用).**
- **Operator names** are in Japanese, often with **full-width** Latin
  letters and digits (ＫＤＤＩ, ＮＴＴ). Some cells carry **furigana
  (`<rPh>`)**, Excel's phonetic readings. A naive XML reader appends them
  ("株式会社ｃｌｏｓｉｐカブシキカイシャ"), and they leak into the status
  column too ("使用中シヨウチュウ").
- **No dates per code.** Only the as-of date, in Japanese era format.

## Compared with ITU

On 2026-10-24: 68 in use nationally, 59 in ITU's list. **11** are
national-only, mostly 441-000…009, Wireless City Planning's 3-digit codes.
ITU instead has a stale 2-digit `441 00`. ITU also lists **441 214**,
designated 2026-08-10, which the national XLSX shows as unused. It's
probably designated but not yet in service, so check the national
`recent designations` list before calling it a conflict.

## How to build the downloader

Package `pkg/carriers/mic` (or `soumu`), command `cmd/jp-imsi`, output
`data/carriers/jp/`:

1. **Discover:**
   - fetch the index (`Client.Fresh`) and decode it as **cp932 (Shift_JIS)**,
     which needs `golang.org/x/text/encoding/japanese` or a small table;
   - find the "IMSI" entry and take the `.xlsx` link next to it (in the
     same `<tr>`/list item as the PDF link);
   - also parse the recent-designations rows that mention IMSI.
2. **Fetch** the XLSX (`Client.Cached` by `main_content` ID; editions don't
   change).
3. **Read the XLSX** with the shared reader (also for Mexico). **Drop
   `<rPh>` elements** before collecting `<t>` text.
4. **Normalize:**
   - **NFKC** for full-width → ASCII (`golang.org/x/text/unicode/norm`),
     then trim;
   - split `番号` into MCC (first 3) and MNC (the rest, keeping its length);
   - status: 使用中 (in use) → `assigned`, 未使用 (unused) → `available`;
   - **as-of date:** parse 令和N年M月D日 (Reiwa N = 2018 + N), with
     full-width digits.
5. **Tables:**
   - `imsi`: mcc, mnc, plmn, operator, operator_raw, status_raw, status;
   - `recent_designations`: code, operator, date, from the index page;
   - `sources`, `meta`.
6. **Checks:**
   - exactly 1,000 codes (100 under 440, 900 under 441);
   - ≥ 50 in use;
   - 440-10 is NTT Docomo;
   - no `<rPh>` leaks, meaning no katakana-only suffixes on status values.

Cost: 2 requests. MIC updates about monthly (the page says 令和8年9月1日現在,
"as of 2026-09-01"). **Refresh weekly.** Change detection compares
`main_content` IDs and diffs the table between editions to see new
designations and releases.

## Notes for the shared schema

- **Only source listing unused codes.** Store them as `status = available`.
  It's useful to show free MNCs. In merged views, filter to assigned.
- **MNC length:** Japan proves that 2-digit and 3-digit MNCs coexist in one
  country. The merge must treat `441 00` (ITU, stale) and `441 000`
  (national) as different codes, and flag the ITU one.
- **`holder`:** NFKC-normalized Japanese name. An English name isn't
  published; ITU's English name for the same code can go in attributes
  (`name_en`).
