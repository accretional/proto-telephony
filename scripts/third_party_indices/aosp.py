#!/usr/bin/env python3
"""AOSP carrier ID table (TelephonyProvider carrier_list.textpb) -> CSV/SQLite.

Android identifies carriers (including MVNOs) with a "carrier ID" table
shipped in packages/providers/TelephonyProvider/assets/latest_carrier_id/.
Each carrier has one or more attribute sets; a SIM matches a set when every
field it sets matches (MCC+MNC tuple, plus optionally SPN, GID1/2, IMSI
prefix pattern, ICCID prefix, ...). Only the MCC+MNC part is a network code;
the rest is how MVNOs sharing a host network's code are told apart.

Output tables (data/vendored/third_party_indices/aosp/):
  carriers    canonical_id, carrier_name, parent_canonical_id, attribute_sets, mccmnc_tuples
  attributes  canonical_id, attribute_index, key, value   (every attribute value, long form)
  networks    mcc, mnc, plmn, canonical_id, carrier_name, parent_canonical_id,
              attribute_index, match_keys   (one row per MCC+MNC tuple per attribute set;
              match_keys lists the other fields that narrow the match, e.g. "spn")
  meta, sources

Usage: aosp.py [--ref android17-release|main|<tag>] [--format csv,sqlite] [--offline]
The default ref is the newest androidNN-release branch: since 2025 AOSP's
main branch is no longer updated publicly, so release branches are newer.
"""

import base64
import re
import sys

import common

REPO_URL = "https://android.googlesource.com/platform/packages/providers/TelephonyProvider"
ASSET = "assets/latest_carrier_id/carrier_list.textpb"
VERSION_BITMASK = 0x00FFFFFF  # CarrierIdProvider: top 8 bits are reserved for the SDK


def extra(p):
    p.add_argument("--ref", default="",
                   help="branch or tag (default: newest androidNN-release branch)")


def newest_release_branch(f):
    refs = f.get_json("refs.json", f"{REPO_URL}/+refs?format=JSON")
    best = None
    for ref in refs:
        m = re.fullmatch(r"refs/heads/android(\d+)-release", ref)
        if m and (best is None or int(m.group(1)) > best[0]):
            best = (int(m.group(1)), ref[len("refs/heads/"):])
    if not best:
        sys.exit("aosp: no androidNN-release branch found; pass --ref")
    return best[1]


# --- minimal text-format protobuf reader -------------------------------------

TOKEN = re.compile(r'\s*(?:(#[^\n]*)|([A-Za-z_][A-Za-z0-9_]*)|("(?:[^"\\]|\\.)*")|(-?[0-9][0-9.]*)|([{}:]))')


def tokens(text):
    pos = 0
    while pos < len(text):
        m = TOKEN.match(text, pos)
        if not m or m.end() == pos:
            if text[pos:].strip() == "":
                return
            sys.exit(f"aosp: cannot parse textpb near: {text[pos:pos + 60]!r}")
        pos = m.end()
        comment, ident, string, number, punct = m.groups()
        if comment:
            continue
        if string is not None:
            yield ("str", string[1:-1].encode("latin-1", "backslashreplace").decode("unicode_escape"))
        elif ident is not None:
            yield ("id", ident)
        elif number is not None:
            yield ("num", number)
        else:
            yield ("p", punct)


def parse_message(toks):
    """Parse fields until '}' or EOF into {name: [values...]}."""
    msg = {}
    for kind, val in toks:
        if kind == "p" and val == "}":
            return msg
        if kind != "id":
            sys.exit(f"aosp: expected field name, got {val!r}")
        k2, v2 = next(toks)
        if k2 == "p" and v2 == ":":
            k2, v2 = next(toks)
            if k2 == "p" and v2 == "{":
                msg.setdefault(val, []).append(parse_message(toks))
            else:
                msg.setdefault(val, []).append(v2)
        elif k2 == "p" and v2 == "{":
            msg.setdefault(val, []).append(parse_message(toks))
        else:
            sys.exit(f"aosp: expected ':' or '{{' after {val}")
    return msg


# -----------------------------------------------------------------------------

def main():
    args = common.parse_args("aosp", __doc__.splitlines()[0], extra)
    f = common.Fetcher(args)
    ref = args.ref or newest_release_branch(f)
    ref_path = ref if ref.startswith("refs/") or re.fullmatch(r"[0-9a-f]{40}", ref) else (
        f"refs/tags/{ref}" if ref.startswith("android-") else f"refs/heads/{ref}")

    head = f.get_json("commit.json", f"{REPO_URL}/+/{ref_path}?format=JSON")
    commit = head["commit"]
    body = f.get("carrier_list.textpb.b64", f"{REPO_URL}/+/{commit}/{ASSET}?format=TEXT", kind="source")
    text = base64.b64decode(body).decode("utf-8")
    root = parse_message(tokens(text))

    version = int(root.get("version", ["0"])[0])
    carriers, attributes, networks = [], [], []
    plmns, nonstandard = set(), set()
    for c in root.get("carrier_id", []):
        cid = int(c.get("canonical_id", ["-1"])[0])
        name = c.get("carrier_name", [""])[0]
        parent = c.get("parent_canonical_id", [""])[0]
        sets = c.get("carrier_attribute", [])
        tuples = 0
        for i, attr in enumerate(sets):
            match_keys = sorted(k for k in attr if k != "mccmnc_tuple")
            for key in sorted(attr):
                for v in attr[key]:
                    attributes.append([cid, i, key, v])
            for t in attr.get("mccmnc_tuple", []):
                tuples += 1
                if not re.fullmatch(r"\d{5,6}", t):
                    nonstandard.add(t)  # e.g. GSMA VoLTE test profiles use "111".."114"
                plmns.add(t)
                networks.append([t[:3], t[3:], t, cid, name, parent, i, " ".join(match_keys)])
        carriers.append([cid, name, parent, len(sets), tuples])

    networks.sort(key=lambda r: (r[2], r[3], r[6]))
    meta = {
        "ref": ref,
        "commit": commit,
        "commit_time": head["committer"]["time"],
        "version": version,
        "version_sdk_byte": version >> 24,
        "version_relative": version & VERSION_BITMASK,
        "carriers": len(carriers),
        "carriers_with_parent": sum(1 for c in carriers if c[2] != ""),
        "attribute_sets": sum(c[3] for c in carriers),
        "network_rows": len(networks),
        "unique_plmns": len(plmns),
        "unique_mccs": len({p[:3] for p in plmns}),
        "nonstandard_tuples": " ".join(sorted(nonstandard)),
    }
    if len(carriers) < 1000:
        sys.exit(f"aosp: suspiciously few carriers ({len(carriers)})")

    common.write_tables(args, {
        "carriers": (["canonical_id", "carrier_name", "parent_canonical_id", "attribute_sets", "mccmnc_tuples"], carriers),
        "attributes": (["canonical_id", "attribute_index", "key", "value"], attributes),
        "networks": (["mcc", "mnc", "plmn", "canonical_id", "carrier_name", "parent_canonical_id",
                      "attribute_index", "match_keys"], networks),
    }, meta, f.sources)


if __name__ == "__main__":
    main()
