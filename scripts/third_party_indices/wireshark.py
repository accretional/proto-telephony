#!/usr/bin/env python3
"""Wireshark's E.212 tables (epan/dissectors/packet-e212.c) -> CSV/SQLite.

Wireshark keeps three C value_string tables used to label IMSIs/PLMNs in
packet dissection:
  E212_codes             MCC -> country ("Unassigned" placeholders included)
  mcc_mnc_2digits_codes  MCC*100  + MNC -> operator name (2-digit MNCs)
  mcc_mnc_3digits_codes  MCC*1000 + MNC -> operator name (3-digit MNCs)

Output tables (data/vendored/third_party_indices/wireshark/):
  mcc       mcc, country, unassigned, comment
  networks  mcc, mnc, plmn, mnc_digits, name, comment
  meta, sources

Usage: wireshark.py [--ref master|<commit>] [--format csv,sqlite] [--offline]
"""

import re
import sys
import urllib.parse

import common

PROJECT = "wireshark/wireshark"
FILE = "epan/dissectors/packet-e212.c"

ENTRY = re.compile(r'^\s*\{\s*(\d+)\s*,\s*"((?:[^"\\]|\\.)*)"\s*\}\s*,?\s*(?:/\*(.*?)\*/)?\s*$')


def extra(p):
    p.add_argument("--ref", default="master",
                   help="branch or commit to read packet-e212.c from (default: master)")


def table_entries(src, name):
    """Yield (key, value, comment) for every entry of `static const value_string name[]`."""
    m = re.search(r"value_string\s+" + re.escape(name) + r"\[\]\s*=\s*\{", src)
    if not m:
        sys.exit(f"wireshark: table {name} not found in {FILE}; has the file changed?")
    end = src.index("\n};", m.end())
    for line in src[m.end():end].splitlines():
        e = ENTRY.match(line)
        if e:
            key, val, comment = e.groups()
            yield int(key), val.encode().decode("unicode_escape").encode("latin-1").decode("utf-8"), (comment or "").strip()
        elif line.strip() and not line.strip().startswith(("{ 0, NULL }", "{0, NULL}", "/*", "*", "//")):
            print(f"wireshark: unparsed line in {name}: {line.strip()}", file=sys.stderr)


def main():
    args = common.parse_args("wireshark", __doc__.splitlines()[0], extra)
    f = common.Fetcher(args)
    proj = urllib.parse.quote(PROJECT, safe="")
    path = urllib.parse.quote(FILE, safe="")

    # Resolve the ref to the last commit that touched the file, so the
    # download (and sources.csv) is pinned to an exact revision.
    commits = f.get_json(
        "commits.json",
        f"https://gitlab.com/api/v4/projects/{proj}/repository/commits?path={path}&ref_name={urllib.parse.quote(args.ref)}&per_page=1")
    if not commits:
        sys.exit(f"wireshark: no commits for {FILE} at {args.ref}")
    commit = commits[0]
    src = f.get("packet-e212.c",
                f"https://gitlab.com/{PROJECT}/-/raw/{commit['id']}/{FILE}",
                kind="source").decode("utf-8")

    mcc_rows = []
    for key, country, comment in table_entries(src, "E212_codes"):
        if comment == f"{key:03d}":
            comment = ""
        mcc_rows.append([f"{key:03d}", country, int(country == "Unassigned"), comment])

    networks, seen, dupes = [], {}, 0
    for table, digits in (("mcc_mnc_2digits_codes", 2), ("mcc_mnc_3digits_codes", 3)):
        div = 10 ** digits
        for key, name, comment in table_entries(src, table):
            mcc, mnc = f"{key // div:03d}", f"{key % div:0{digits}d}"
            plmn = mcc + mnc
            if plmn in seen:
                dupes += 1
                print(f"wireshark: duplicate {plmn}: {seen[plmn]!r} / {name!r}", file=sys.stderr)
            seen[plmn] = name
            if comment == plmn:  # e.g. /* 00101 */ just restates the code
                comment = ""
            networks.append([mcc, mnc, plmn, digits, name, comment])
    networks.sort(key=lambda r: (r[0], r[1]))

    meta = {
        "ref": args.ref,
        "commit": commit["id"],
        "commit_date": commit["committed_date"],
        "commit_title": commit["title"],
        "mccs": sum(1 for r in mcc_rows if not r[2]),
        "mccs_unassigned": sum(1 for r in mcc_rows if r[2]),
        "networks": len(networks),
        "networks_2digit": sum(1 for r in networks if r[3] == 2),
        "networks_3digit": sum(1 for r in networks if r[3] == 3),
        "duplicate_plmns": dupes,
    }
    if len(networks) < 2500 or meta["mccs"] < 200:
        sys.exit(f"wireshark: suspiciously few rows ({len(networks)} networks, {meta['mccs']} MCCs)")

    common.write_tables(args, {
        "mcc": (["mcc", "country", "unassigned", "comment"], mcc_rows),
        "networks": (["mcc", "mnc", "plmn", "mnc_digits", "name", "comment"], networks),
    }, meta, f.sources)


if __name__ == "__main__":
    main()
