#!/usr/bin/env python3
"""Compare the third-party indices with each other and with the official data.

Reads the `networks` tables written by wireshark.py, aosp.py and
wikipedia.py (CSV or SQLite) and the merged official registry
data/carriers/merged/current.csv (ITU + national registries, built by
cmd/carriers-merge), keyed by PLMN (MCC+MNC as written, so 2- vs 3-digit
MNCs stay distinct).

Output tables (data/vendored/third_party_indices/comparison/):
  codes    one row per PLMN seen anywhere: official status/holder and what
           each third-party source says
  summary  per-source counts: coverage of official codes, codes the
           official data doesn't have, codes official data says are no
           longer assigned, MNC-length mismatches
  meta, sources

Usage: compare.py [--format csv,sqlite] [--official data/carriers/merged/current.csv]
"""

import csv
import hashlib
import os
import re

import common

SOURCES = ("wireshark", "aosp", "wikipedia")
ACTIVE = {"assigned", "reserved", "test"}  # official statuses meaning "still allocated"


def extra(p):
    p.add_argument("--official", default=os.path.join(common.REPO, "data", "carriers", "merged", "current.csv"),
                   help="merged official registry CSV (default: data/carriers/merged/current.csv)")


def main():
    args = common.parse_args("comparison", __doc__.splitlines()[0], extra)

    with open(args.official, newline="", encoding="utf-8") as f:
        official = {}
        for r in csv.DictReader(f):
            official.setdefault(r["plmn"], r)
    with open(args.official, "rb") as f:
        body = f.read()
    inputs = [{"kind": "official", "name": os.path.relpath(args.official, common.REPO), "url": "",
               "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(), "from_cache": 0}]

    names = {s: {} for s in SOURCES}  # source -> plmn -> display name(s)
    wiki_status = {}
    for s in SOURCES:
        for r in common.read_table(args.out, s, "networks"):
            if s == "wikipedia" and r["code_format"] != "standard":
                continue
            p = r["plmn"]
            if not re.fullmatch(r"\d{5,6}", p):
                continue
            label = {"wireshark": r.get("name"), "aosp": r.get("carrier_name"),
                     "wikipedia": r.get("operator") or r.get("brand")}[s] or ""
            names[s].setdefault(p, [])
            if label and label not in names[s][p]:
                names[s][p].append(label)
            if s == "wikipedia":
                wiki_status.setdefault(p, [])
                if r["status_norm"] not in wiki_status[p]:
                    wiki_status[p].append(r["status_norm"])
        meta_rows = common.read_table(args.out, s, "meta")
        inputs.append({"kind": "index", "name": s, "url": "", "bytes": 0,
                       "sha256": next((m["value"] for m in meta_rows if m["key"] in ("commit", "latest_revision")), ""),
                       "from_cache": 0})

    def alt_length(p):
        """The same code with the MNC one digit longer/shorter (31026 <-> 310026)."""
        mcc, mnc = p[:3], p[3:]
        return mcc + "0" + mnc if len(mnc) == 2 else (mcc + mnc[1:] if mnc.startswith("0") else None)

    all_plmns = set(official) | {p for s in SOURCES for p in names[s]}
    codes = []
    for p in sorted(all_plmns):
        o = official.get(p, {})
        codes.append([p, p[:3], p[3:], o.get("status", ""), o.get("holder", ""), o.get("source", ""),
                      " | ".join(names["wireshark"].get(p, [])),
                      " | ".join(names["aosp"].get(p, [])),
                      " | ".join(names["wikipedia"].get(p, [])),
                      " | ".join(wiki_status.get(p, [])),
                      *[int(p in names[s]) for s in SOURCES]])

    official_active = {p for p, r in official.items() if r["status"] in ACTIVE}
    summary = []
    for s in SOURCES:
        have = set(names[s])
        not_official = have - set(official)
        summary.append([
            s, len(have), len({p[:3] for p in have}),
            len(have & official_active),
            round(100 * len(have & official_active) / len(official_active), 1),
            len(official_active - have),
            len(have & (set(official) - official_active)),
            len(not_official),
            sum(1 for p in not_official if alt_length(p) in official),
        ])
    in_all = sum(1 for p in all_plmns if all(p in names[s] for s in SOURCES))
    only = {s: sum(1 for p in names[s] if not any(p in names[t] for t in SOURCES if t != s)) for s in SOURCES}

    meta = {
        "official_codes": len(official),
        "official_active": len(official_active),
        "plmns_total": len(all_plmns),
        "plmns_in_all_three": in_all,
        **{f"plmns_only_in_{s}": n for s, n in only.items()},
    }
    common.write_tables(args, {
        "codes": (["plmn", "mcc", "mnc", "official_status", "official_holder", "official_source",
                   "wireshark_name", "aosp_carriers", "wikipedia_operator", "wikipedia_status",
                   "in_wireshark", "in_aosp", "in_wikipedia"], codes),
        "summary": (["source", "plmns", "mccs", "official_active_listed", "official_active_coverage_pct",
                     "official_active_missing", "listed_but_official_inactive", "not_in_official",
                     "not_in_official_mnc_length_mismatch"], summary),
    }, meta, inputs)


if __name__ == "__main__":
    main()
