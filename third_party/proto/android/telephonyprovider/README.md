# Android carrier ID proto (vendored)

`carrierId.proto` defines Android's carrier identification table
(`CarrierList` → `CarrierId` → `CarrierAttribute`). The binary
`carrier_list.pb` and its text form `carrier_list.textpb`, both shipped in
TelephonyProvider's `assets/latest_carrier_id/`, are serialized
`carrierIdentification.CarrierList` messages.
[`scripts/third_party_indices/aosp.py`](../../../../scripts/third_party_indices/aosp.py)
parses the text form.

It has no imports, so there are no dependencies to vendor.

| | |
|---|---|
| Upstream | <https://android.googlesource.com/platform/packages/providers/TelephonyProvider/+/013b803626164c026025897b7dec45521c2cccab/proto/src/carrierId.proto> |
| Upstream path | `proto/src/carrierId.proto` |
| Ref | `android17-release` (newest public release branch on 2026-10-24) |
| Commit | `013b803626164c026025897b7dec45521c2cccab` (2026-04-16) |
| Git blob | `9d2f29e8ed59f0f565dd614b61a6aa4b1098a743` |
| sha256 | `5f08a2fdae39cffdb1a854e4cf961fcd2bf1a2f764320cba2b81988f450a267b` |
| License | Apache License 2.0, per the file's own header (the upstream repository has no separate LICENSE/NOTICE file) |

The file is unmodified. It is `proto2`, `package carrierIdentification`, with
only Java options, so generating Go code needs a `go_package` mapping, for
example:

```bash
protoc --proto_path=third_party/proto \
  --go_out=. --go_opt=Mandroid/telephonyprovider/carrierId.proto=<module>/<pkg> \
  android/telephonyprovider/carrierId.proto
```

## Notes

- `CarrierList.version`: CarrierIdProvider treats the top 8 bits as
  reserved for the SDK and the low 24 bits (`& 0x00FFFFFF`) as the table's
  own version. Android 17's table is `134217778` = SDK byte 8, version 50.
- A SIM matches a `CarrierAttribute` when every field that's set matches
  (AND across fields, OR within a repeated field). `mccmnc_tuple` holds
  5 or 6 digits, so whether the MNC has 2 or 3 digits is implied by the
  string length.

## Updating

```bash
B=https://android.googlesource.com/platform/packages/providers/TelephonyProvider/+
curl -s "$B/refs/heads/<androidNN-release>/proto/src/carrierId.proto?format=TEXT" | base64 -d \
  > third_party/proto/android/telephonyprovider/carrierId.proto
git hash-object third_party/proto/android/telephonyprovider/carrierId.proto   # compare with the upstream blob id
```

Then update the table above.
