# AOSP: Android carrier ID table

**Status: fetched** (`scripts/third_party_indices/aosp.py`, output
`data/vendored/third_party_indices/aosp/`). This isn't an MCC/MNC list. It's
Android's **carrier identification** table, which maps SIM attributes to a
carrier, including MVNOs. MCC+MNC is only one of the attributes. It's the
best open source for "which brand is this SIM", and a poor one for code
coverage.

## What it is

Every Android device ships the table in TelephonyProvider
([`assets/latest_carrier_id/`](https://android.googlesource.com/platform/packages/providers/TelephonyProvider/+/refs/heads/android17-release/assets/latest_carrier_id/)):
- `carrier_list.pb` is the binary used on devices;
- `carrier_list.textpb` is a readable copy of the same data.

Both are a `carrierIdentification.CarrierList` message. The schema is
vendored at
[`third_party/proto/android/telephonyprovider/carrierId.proto`](../../../third_party/proto/android/telephonyprovider/carrierId.proto).

```textproto
carrier_id {
  canonical_id: 1892
  carrier_name: "LG U+"
  carrier_attribute {
    mccmnc_tuple: "45006"
    mccmnc_tuple: "450006"
  }
  parent_canonical_id: 1690
}
```

- Each carrier has a stable `canonical_id` (what
  `TelephonyManager.getSimCarrierId()` returns), a name, and one or more
  `carrier_attribute` sets.
- A SIM matches a set when every field the set specifies matches:
  `mccmnc_tuple`, `spn`, `gid1`, `gid2`, `imsi_prefix_xpattern`,
  `iccid_prefix`, `plmn`, `preferred_apn` or `privilege_access_rule`.
- `parent_canonical_id` links sub-brands to their parent carrier.
- **License:** Apache-2.0.

## Contents (2026-10-24)

From `android17-release` at `013b803` (2026-04-16):
- **Version:** `134217778`. CarrierIdProvider treats the top 8 bits as
  reserved for the SDK, so this is SDK byte 8 and table version **50**.
- **Carriers:** 1,567, of which 41 have a parent. They have 1,721
  attribute sets between them.
- **MCC+MNC:** 2,638 tuple rows, **1,826 unique codes** across 225 MCCs.
  The tuples are 1,680 5-digit and 954 6-digit, plus 4 non-codes (`111`
  to `114`, GSMA VoLTE test profiles).
- **How the rows match:**
  - 1,698 match on MCC+MNC alone;
  - the rest also need another field: `gid1` 558, `spn` 266,
    `imsi_prefix_xpattern` 46, `preferred_apn` 14, and combinations of
    these.
- **Shared codes:** 248 codes are shared by several carriers. The most
  shared are 310-260 (41 carriers), 310-240 (30) and 310-410 (30), which
  are host networks for US MVNOs. A code can also appear under a carrier
  from another country. For example, Vodafone NL's 204-04 is used by
  C Spire, U.S. Cellular, Hot Mobile, Jump and others for roaming SIMs.
- **What's missing:** status, dates, legal holder and country. Names are
  brands ("T-Mobile - US", "MetroPCS").

### Which copy is newest

| Ref | Last change | Table version | Carriers | Unique codes |
|---|---|---|---|---|
| `main` | 2025-03-14 ("Rollout_20250314") | 43 | 1,514 | 1,786 |
| `android16-release` | same file as `main` | 43 | 1,514 | 1,786 |
| `android17-release` / `android-security-17.0.0_r2` | 2026-04-16 snapshot | **50** | 1,567 | 1,826 |

Since 2025 Google develops Android internally and publishes source as
release snapshots, so AOSP `main` is frozen. The fetcher defaults to the
newest `androidNN-release` branch. Before the freeze, the git history showed
roughly monthly "Carrier ID table rollout" commits. Since then, only release
snapshots show changes. googlesource now requires a sign-in to view
history pages, so the date of each version can't be listed.

### Is there a newer copy anywhere?

Android can replace the table without a system update: the broadcast
`android.os.action.UPDATE_CARRIER_ID_DB` installs a new file into
`/data/misc/carrierid/`. Investigated on 2026-10-24:
- **The sender** is Google's `com.google.android.configupdater`
  (CarrierIdUpdateFetcher). It downloads the content and metadata from two
  server-pushed flags (`CarrierIdentification__content_url` and
  `__metadata_url`) with a plain `DownloadManager` GET: no auth headers, no
  token.
- **The built-in default** URLs are empty.
- **Google's own telephony tests** (ACTS `tel_defines.py`) override the
  flags with
  `https://www.gstatic.com/android/config_update/110618-carrier-id.pb` and
  `…/110618-carrier-id-metadata.txt`. Both are public, but that's a 2018
  test table (metadata `VERSION:3`, 37 KB).
- **Real phones:** ConfigUpdater's saved flags in three public forensic
  device images on GitHub show both URLs empty and version 0. A fresh
  Android 37 Google Play emulator showed the same.
- **Play services** also contains a second updater
  (`DynamicCarrierIdOtaIntentOperation`), which is gated off by the flag
  `enable_dynamic_carrier_id`.

Conclusion: phones appear to get the table only with system updates, so
**the newest AOSP release branch is as current as public data gets**.
Pixel factory images may be slightly newer, as each build bundles its own
copy. No newer table was found on gstatic. Guessing names for the 138 dates
around known rollouts found nothing.

## Compared with the official data

From `comparison/summary.csv`:

| | |
|---|---|
| Officially active codes (2,514) listed | **1,320 (52.5%)** |
| Officially active codes missing | 1,194. ITU 835, US 242, Japan 58, Canada 46, Mexico 13. Mostly codes with no consumer SIMs: US 312–314, the shared 901, France 208 |
| Listed codes the registry marks returned/reclaimed | 38. Mostly US 310/311, e.g. 310-300 (T-Mobile/MetroPCS) and 310-560 (AT&T) |
| Listed codes not in the official data at all | 464. India 405 (119), Brazil 724 (21), Canada 302 (13), US 311 (13). Of these, 23 exist officially with the other MNC length |

Coverage is low by design: the table only needs codes that appear on
consumer SIMs Android must recognize. Codes on SIMs that are still around
keep their entries after the registry takes the code back, and legacy
2-digit codes stay for old SIMs, so the extra codes are expected too.

## How the fetcher reads it

1. Pick the ref: `--ref`, or the highest `refs/heads/androidNN-release`
   from `+refs?format=JSON`.
2. Resolve it to a commit and download
   `assets/latest_carrier_id/carrier_list.textpb` at that commit. Gitiles
   `?format=TEXT` returns it base64-encoded.
3. Parse it with a small text-format reader: nested messages, repeated
   fields, string and number scalars.
4. Write it out as `carriers`, the long-form `attributes`, and `networks`
   (one row per tuple per attribute set, with `match_keys`).
5. Fail if there are fewer than 1,000 carriers.

## Usefulness

- **Good:**
  - telling MVNOs and brands apart on a shared host code, using
    gid1/spn/IMSI-prefix rules nobody else publishes;
  - a stable carrier ID per brand, matching what Android reports;
  - parent/child brand links.
- **Not good for:** registry coverage, status, legal holders, or private,
  IoT and non-consumer codes.
