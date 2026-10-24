"""Shared helpers for the third-party MCC/MNC index fetchers.

Each fetcher (wireshark.py, aosp.py, wikipedia.py) downloads one
non-authoritative index, parses it into tables, and writes them with
write_tables() as CSV and/or SQLite under data/vendored/third_party_indices/.
Downloads are cached under data/cache/third_party_indices/<source>/ so that
--offline can rebuild from the last fetch.

Standard library only (Python 3.9+).
"""

import argparse
import csv
import datetime
import hashlib
import json
import os
import sqlite3
import sys
import time
import urllib.error
import urllib.request

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
DEFAULT_OUT = os.path.join(REPO, "data", "vendored", "third_party_indices")
DEFAULT_CACHE = os.path.join(REPO, "data", "cache", "third_party_indices")
USER_AGENT = "proto-telephony/third_party_indices (+https://github.com/accretional/proto-telephony)"


def parse_args(source, description, extra=None):
    """Common flags: --format, --out, --cache, --offline, plus per-source ones."""
    p = argparse.ArgumentParser(description=description)
    p.add_argument("--format", default="csv",
                   help="csv, sqlite, or csv,sqlite (default: csv)")
    p.add_argument("--out", default=DEFAULT_OUT,
                   help="output root (default: data/vendored/third_party_indices)")
    p.add_argument("--cache", default=DEFAULT_CACHE,
                   help="download cache root (default: data/cache/third_party_indices)")
    p.add_argument("--offline", action="store_true",
                   help="use only cached downloads; fail if one is missing")
    if extra:
        extra(p)
    args = p.parse_args()
    args.formats = [f.strip() for f in args.format.split(",") if f.strip()]
    bad = [f for f in args.formats if f not in ("csv", "sqlite")]
    if bad or not args.formats:
        p.error("--format must be csv, sqlite, or csv,sqlite")
    args.source = source
    args.cache_dir = os.path.join(args.cache, source)
    return args


class Fetcher:
    """HTTP GET with an on-disk cache; records every document in `sources`."""

    def __init__(self, args):
        self.args = args
        self.sources = []
        os.makedirs(args.cache_dir, exist_ok=True)

    def get(self, name, url, kind="document", record=True):
        """Return the body (bytes) of url, cached as <cache_dir>/<name>."""
        path = os.path.join(self.args.cache_dir, name)
        from_cache = False
        if self.args.offline:
            if not os.path.exists(path):
                sys.exit(f"{self.args.source}: --offline but {path} is not cached")
            with open(path, "rb") as f:
                body = f.read()
            from_cache = True
        else:
            body = http_get(url)
            tmp = path + ".tmp"
            with open(tmp, "wb") as f:
                f.write(body)
            os.replace(tmp, path)
        if record:
            self.sources.append({
                "kind": kind, "name": name, "url": url, "bytes": len(body),
                "sha256": hashlib.sha256(body).hexdigest(),
                "from_cache": int(from_cache),
            })
        return body

    def get_json(self, name, url, kind="api", record=False):
        body = self.get(name, url, kind=kind, record=record)
        text = body.decode("utf-8")
        if text.startswith(")]}'"):  # gitiles XSSI prefix
            text = text.split("\n", 1)[1]
        return json.loads(text)


def http_get(url, tries=4):
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    for attempt in range(tries):
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                return r.read()
        except urllib.error.HTTPError as e:
            if e.code not in (429, 500, 502, 503, 504) or attempt == tries - 1:
                raise
        except urllib.error.URLError:
            if attempt == tries - 1:
                raise
        time.sleep(2 ** attempt)
    raise RuntimeError("unreachable")


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def write_tables(args, tables, meta, sources):
    """Write {table: (columns, rows)} plus meta and sources.

    csv:    <out>/<source>/<table>.csv
    sqlite: <out>/<source>.sqlite (one table per name)
    Every file is written to a temporary name and renamed into place.
    """
    tables = dict(tables)
    meta = dict(meta)
    meta.setdefault("written_at", utc_now())
    meta["tool"] = f"scripts/third_party_indices/{os.path.basename(sys.argv[0])}"
    tables["meta"] = (["key", "value"], sorted(meta.items()))
    tables["sources"] = (["kind", "name", "url", "bytes", "sha256", "from_cache"],
                         [[s[c] for c in ("kind", "name", "url", "bytes", "sha256", "from_cache")]
                          for s in sources])

    if "csv" in args.formats:
        d = os.path.join(args.out, args.source)
        os.makedirs(d, exist_ok=True)
        for name, (cols, rows) in tables.items():
            path = os.path.join(d, name + ".csv")
            tmp = path + ".tmp"
            with open(tmp, "w", newline="", encoding="utf-8") as f:
                w = csv.writer(f, lineterminator="\n")
                w.writerow(cols)
                for row in rows:
                    w.writerow(["" if v is None else v for v in row])
            os.replace(tmp, path)
        print(f"{args.source}: wrote {d}/ ({', '.join(f'{n} {len(r)}' for n, (_, r) in tables.items())})")

    if "sqlite" in args.formats:
        os.makedirs(args.out, exist_ok=True)
        path = os.path.join(args.out, args.source + ".sqlite")
        tmp = path + ".tmp"
        if os.path.exists(tmp):
            os.remove(tmp)
        db = sqlite3.connect(tmp)
        for name, (cols, rows) in tables.items():
            db.execute(f'CREATE TABLE "{name}" ({", ".join(f"{c!r}" for c in cols)})')
            db.executemany(f'INSERT INTO "{name}" VALUES ({", ".join("?" * len(cols))})', rows)
        db.commit()
        db.close()
        os.replace(tmp, path)
        print(f"{args.source}: wrote {path}")


def read_table(out, source, table):
    """Read a table written by write_tables, preferring CSV, else SQLite.

    Returns a list of dicts with string values ('' for NULL).
    """
    path = os.path.join(out, source, table + ".csv")
    if os.path.exists(path):
        with open(path, newline="", encoding="utf-8") as f:
            return list(csv.DictReader(f))
    db_path = os.path.join(out, source + ".sqlite")
    if os.path.exists(db_path):
        db = sqlite3.connect(db_path)
        db.row_factory = sqlite3.Row
        rows = [{k: "" if r[k] is None else str(r[k]) for k in r.keys()}
                for r in db.execute(f'SELECT * FROM "{table}"')]
        db.close()
        return rows
    sys.exit(f"no {source}/{table}.csv or {source}.sqlite under {out}; run {source}.py first")
