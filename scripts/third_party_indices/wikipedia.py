#!/usr/bin/env python3
"""English Wikipedia's MCC/MNC lists -> CSV/SQLite.

Reads the wikitext (via the MediaWiki API) of:
  Mobile network codes in ITU region 2xx (Europe) ... 7xx (South America)
  Mobile country code   (international 901/902 operators, test networks,
                         and the MCC -> country / national authority table)

Every MNC table on these pages uses the {{MNC}} template with 7 columns:
MCC | MNC | Brand | Operator | Status | Bands | References and notes.
Country headings look like "==== [[Anguilla]] (United Kingdom) – AI ====".

Output tables (data/vendored/third_party_indices/wikipedia/):
  networks  mcc, mnc, plmn, page, section, country, iso, brand, operator,
            status, bands, notes
  mccs      mcc, country, iso, authority, remarks   (from "Mobile country code")
  pages     title, revid, timestamp, url, network_rows
  meta, sources

Usage: wikipedia.py [--format csv,sqlite] [--offline]
"""

import re
import sys
import urllib.parse

import common

API = "https://en.wikipedia.org/w/api.php"
PAGES = [
    "Mobile network codes in ITU region 2xx (Europe)",
    "Mobile network codes in ITU region 3xx (North America)",
    "Mobile network codes in ITU region 4xx (Asia)",
    "Mobile network codes in ITU region 5xx (Oceania)",
    "Mobile network codes in ITU region 6xx (Africa)",
    "Mobile network codes in ITU region 7xx (South America)",
    "Mobile country code",
]
# Templates whose first argument is display text; all others are dropped.
KEEP_FIRST_ARG = {"toc tab", "ill", "proper name", "usurped", "nowrap", "sort"}
CELL_ATTR = re.compile(r"^\s*((?:rowspan|colspan|style|align|class|data-sort-value|scope|width)\s*=[^|\[\]{}]*)\|(?!\|)(.*)$", re.S | re.I)
HEADING = re.compile(r"^(={2,6})\s*(.*?)\s*\1\s*$")
COUNTRY = re.compile(r"^(.*?)\s+[–—-]\s+([A-Z]{2}(?:-[A-Z0-9]{1,3})?)$")


def strip_markup(text):
    """Wikitext -> plain text (refs/comments must already be removed)."""
    # Innermost templates first, repeatedly.
    def template(m):
        parts = m.group(1).split("|")
        name = parts[0].strip().lower()
        return parts[1] if name in KEEP_FIRST_ARG and len(parts) > 1 else ""
    prev = None
    while prev != text:
        prev = text
        text = re.sub(r"\{\{([^{}]*)\}\}", template, text)
    text = re.sub(r"\[\[(?:File|Image):[^\]]*\]\]", "", text, flags=re.I)
    text = re.sub(r"\[\[([^\]|]*)\|([^\]]*)\]\]", r"\2", text)
    text = re.sub(r"\[\[([^\]]*)\]\]", r"\1", text)
    text = re.sub(r"\[(?:https?:)?//\S+\s+([^\]]*)\]", r"\1", text)
    text = re.sub(r"\[(?:https?:)?//\S+\]", "", text)
    text = re.sub(r"<br\s*/?>", " / ", text, flags=re.I)
    text = re.sub(r"</?[a-zA-Z][^>]*>", "", text)
    text = text.replace("'''", "").replace("''", "").replace("&nbsp;", " ")
    return re.sub(r"\s+", " ", text).strip(" ;,")


def preprocess(text):
    text = re.sub(r"<!--.*?-->", "", text, flags=re.S)
    text = re.sub(r"<ref[^>]*/>", "", text)
    text = re.sub(r"<ref[^>]*>.*?</ref>", "", text, flags=re.S)
    return text


def tables(text):
    """Yield (context, start_line, rows) for every table; rows are lists of cell strings.

    context is (section, country, iso) from the nearest preceding headings.
    Handles '||'/'!!' cell separators, multi-line cells and rowspan.
    """
    section, country, iso = "", "", ""
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        line = lines[i]
        h = HEADING.match(line)
        if h:
            level, title = len(h.group(1)), strip_markup(h.group(2))
            m = COUNTRY.match(title)
            if m:
                country, iso = m.group(1), m.group(2)
            elif level == 2:
                section, country, iso = title, "", ""
            i += 1
            continue
        if not line.startswith("{|"):
            i += 1
            continue
        start, rows, cur, pending = line, [], None, {}
        i += 1
        while i < len(lines) and not lines[i].startswith("|}"):
            ln = lines[i]
            if ln.startswith("|-"):
                if cur is not None:
                    rows.append(cur)
                cur = []
            elif ln.startswith("|+"):
                pass
            elif ln.startswith("|") or ln.startswith("!"):
                if cur is None:
                    cur = []
                sep = "!!" if ln.startswith("!") else "||"
                cur.extend(ln[1:].split(sep))
            elif cur:
                cur[-1] += "\n" + ln
            i += 1
        if cur:
            rows.append(cur)
        i += 1
        # Apply rowspans: pending[col] = (remaining, value)
        out = []
        for raw in rows:
            cells, col, it = [], 0, iter(raw)
            while True:
                if col in pending and pending[col][0] > 0:
                    n, v = pending[col]
                    cells.append(v)
                    pending[col] = (n - 1, v)
                    col += 1
                    continue
                cell = next(it, None)
                if cell is None:
                    break
                span = 1
                a = CELL_ATTR.match(cell)
                if a:
                    rs = re.search(r"rowspan\s*=\s*\"?(\d+)", a.group(1), re.I)
                    span = int(rs.group(1)) if rs else 1
                    cell = a.group(2)
                value = strip_markup(cell)
                cells.append(value)
                if span > 1:
                    pending[col] = (span - 1, value)
                col += 1
            # Trailing spanned columns after the last explicit cell.
            while col in pending and pending[col][0] > 0:
                n, v = pending[col]
                cells.append(v)
                pending[col] = (n - 1, v)
                col += 1
            out.append(cells)
        yield (section, country, iso), start, out


def main():
    args = common.parse_args("wikipedia", __doc__.splitlines()[0])
    f = common.Fetcher(args)
    networks, mccs, pages, problems = [], [], [], 0
    for title in PAGES:
        q = urllib.parse.urlencode({
            "action": "query", "format": "json", "formatversion": "2", "redirects": "1",
            "prop": "revisions", "rvprop": "ids|timestamp|content", "rvslots": "main",
            "titles": title})
        name = re.sub(r"[^A-Za-z0-9]+", "_", title).strip("_") + ".json"
        data = f.get_json(name, f"{API}?{q}", kind="page", record=True)
        page = data["query"]["pages"][0]
        rev = page["revisions"][0]
        text = preprocess(rev["slots"]["main"]["content"])
        before = len(networks)
        for (section, country, iso), start, rows in tables(text):
            if "{{MNC}}" in start.replace(" ", ""):
                for cells in rows:
                    if not cells or all(c == "" for c in cells):
                        continue
                    if len(cells) not in (6, 7):  # 6 = notes cell omitted
                        problems += 1
                        print(f"wikipedia: {title}: {country or section}: {len(cells)} cells: {cells[:3]}", file=sys.stderr)
                    cells = (cells + [""] * 7)[:7]
                    mcc, mnc, brand, operator, status, bands, notes = cells
                    # France lists 5-digit blocks under 208 50; the 314 page has a range row.
                    standard = bool(re.fullmatch(r"\d{3}", mcc) and re.fullmatch(r"\d{2,3}", mnc))
                    networks.append([mcc, mnc, mcc + mnc, "standard" if standard else "nonstandard",
                                     page["title"], section, country or section, iso, brand, operator,
                                     status, status.lower(), bands, notes])
            elif title == "Mobile country code" and rows and rows[0][:2] == ["Mobile country code", "Country"]:
                for cells in rows[1:]:
                    cells = (cells + [""] * 6)[:6]
                    mcc, country, iso, _list, authority, remarks = cells
                    mccs.append([mcc, country, iso, authority, remarks])
        pages.append([page["title"], rev["revid"], rev["timestamp"],
                      f"https://en.wikipedia.org/w/index.php?oldid={rev['revid']}", len(networks) - before])

    meta = {
        "pages": len(pages),
        "latest_revision": max(p[2] for p in pages),
        "networks": len(networks),
        "networks_nonstandard": sum(1 for r in networks if r[3] != "standard"),
        "unique_plmns": len({r[2] for r in networks}),
        "plmns_in_several_territories": sum(
            1 for p in {r[2] for r in networks} if len({r[7] for r in networks if r[2] == p}) > 1),
        "mccs": len(mccs),
        "parse_problems": problems,
    }
    for status in sorted({r[11] for r in networks}):
        meta[f"status:{status or '(blank)'}"] = sum(1 for r in networks if r[11] == status)
    if len(networks) < 2500 or len(mccs) < 200:
        sys.exit(f"wikipedia: suspiciously few rows ({len(networks)} networks, {len(mccs)} MCCs)")

    networks.sort(key=lambda r: (r[0], r[1], r[7]))
    common.write_tables(args, {
        "networks": (["mcc", "mnc", "plmn", "code_format", "page", "section", "country", "iso", "brand",
                      "operator", "status", "status_norm", "bands", "notes"], networks),
        "mccs": (["mcc", "country", "iso", "authority", "remarks"], mccs),
        "pages": (["title", "revid", "timestamp", "url", "network_rows"], pages),
    }, meta, f.sources)


if __name__ == "__main__":
    main()
