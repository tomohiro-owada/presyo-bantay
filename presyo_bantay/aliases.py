"""Canonical commodity names: alias -> canonical mapping.

The DA's per-market PDFs are not perfectly consistent in how they spell a
commodity from one report to the next (typos, ``5% broken`` qualifiers,
case/whitespace drift, stray ``*`` footnote markers).  This module maps every
raw name seen in the wild to one canonical name so the SQLite store and the
static site treat the "same item and spec" as one series.

Only names that clearly mean the same item *and spec* are merged.  In
particular these stay separate (different specs) but share a ``group`` so the
site can show them as related series:

- ``Fresh Pork Kasim/Pigue`` vs ``Fresh Pork (Kasim) Local`` (group
  ``pork_kasim``)
- ``Fresh Pork Liempo`` vs ``Fresh Pork Liempo (Local)`` (group
  ``pork_liempo``)
- ``Sugar (Washed)`` vs ``Sugar (Refined)`` (group ``sugar``)
"""

from __future__ import annotations

#: Canonical name -> list of known aliases (raw spellings seen in PDFs/JSON).
#: Matching is case/whitespace-insensitive (see :func:`_norm_key`), so entries
#: here only need to cover spellings that differ beyond case/whitespace (plus
#: a few representative case variants for readability).
CANONICAL_ALIASES: dict[str, list[str]] = {
    "Premium Rice (Local)": [
        "Premium Rice (Loca)",  # typo seen 2026-08-18/19
        "PREMIUM RICE (Local, 5% broken)",  # DA premium rice = 5% broken
        "Premium Rice (Local, 5% broken)",
    ],
    "Premium Rice (Imported)": [
        "PREMIUM RICE (Imported, 5% broken)",
        "Premium Rice (Imported, 5% broken)",
    ],
}

#: Every canonical spelling currently in use (keys of
#: :data:`CANONICAL_ALIASES` plus all other distinct names in the store).
#: Lets ``canonicalize`` collapse pure case/whitespace/asterisk variants of
#: *any* known commodity, not just the typo/qualifier aliases above.
KNOWN_CANONICALS: list[str] = [
    "Ampalaya",
    "Bangus",
    "Cabbage",
    "Calamansi",
    "Carrots (Local)",
    "Chayote",
    "Chili (Green)",
    "Egg (Medium)",
    "Eggplant",
    "Fresh Pork (Kasim) Imported",
    "Fresh Pork (Kasim) Local",
    "Fresh Pork Kasim/Pigue",
    "Fresh Pork Liempo",
    "Fresh Pork Liempo (Imported)",
    "Fresh Pork Liempo (Local)",
    "Fresh Whole Chicken",
    "Galunggong (Local)",
    "Pechay Baguio",
    "Premium Rice (Imported)",
    "Premium Rice (Local)",
    "Red Chili (Local)",
    "Red Onion (Local)",
    "Regular Milled (Local)",
    "Sugar (Refined)",
    "Sugar (Washed)",
    "Tilapia",
    "Tomato",
    "Well-milled Rice (Imported)",
    "Well-milled Rice (Local)",
    "White Onion (Local)",
    "White Potato (Local)",
]

#: Canonical commodity name -> related-series group id.  Members of a group
#: are *not* merged; the group only lets the site show them together.
COMMODITY_GROUPS: dict[str, str] = {
    "Fresh Pork (Kasim) Local": "pork_kasim",
    "Fresh Pork (Kasim) Imported": "pork_kasim",
    "Fresh Pork Kasim/Pigue": "pork_kasim",
    "Fresh Pork Liempo": "pork_liempo",
    "Fresh Pork Liempo (Local)": "pork_liempo",
    "Fresh Pork Liempo (Imported)": "pork_liempo",
    "Sugar (Washed)": "sugar",
    "Sugar (Refined)": "sugar",
}


def _norm_key(name: str) -> str:
    """Case/whitespace/asterisk-insensitive match key for *name*."""
    cleaned = (name or "").replace("*", " ")
    return " ".join(cleaned.lower().split())


#: Reverse index: normalized alias -> canonical name.
_ALIAS_TO_CANONICAL: dict[str, str] = {}
for _canonical, _aliases in CANONICAL_ALIASES.items():
    _ALIAS_TO_CANONICAL.setdefault(_norm_key(_canonical), _canonical)
    for _alias in _aliases:
        _ALIAS_TO_CANONICAL.setdefault(_norm_key(_alias), _canonical)
for _known in KNOWN_CANONICALS:
    _ALIAS_TO_CANONICAL.setdefault(_norm_key(_known), _known)


def _tidy(name: str) -> str:
    """Strip ``*`` footnote markers and collapse whitespace (keep case)."""
    return " ".join((name or "").replace("*", " ").split())


def _spec_from_name(canonical: str) -> str | None:
    """Derive ``spec`` from a canonical name (same rule as pdfparse)."""
    from .pdfparse import infer_spec

    return infer_spec(canonical)


def _unit_from_name(canonical: str) -> str:
    """Unit fallback matching pdfparse when the raw header is unavailable."""
    from .pdfparse import infer_unit

    return infer_unit(canonical)


def _category_from_name(canonical: str) -> str:
    """Local import to avoid a cycle (db imports aliases at call time)."""
    from .db import categorize_commodity

    return categorize_commodity(canonical)


def canonicalize(name: str) -> tuple[str, str | None, str, str]:
    """Map a raw commodity *name* to ``(canonical, spec, unit, category)``.

    Matching is case/whitespace/asterisk-insensitive; known aliases collapse
    to their canonical spelling, everything else keeps its tidied spelling.
    """
    key = _norm_key(name)
    canonical = _ALIAS_TO_CANONICAL.get(key)
    if canonical is None:
        canonical = _tidy(name)
    spec = _spec_from_name(canonical)
    unit = _unit_from_name(canonical)
    return (canonical, spec, unit, _category_from_name(canonical))


def group_for(canonical_name: str) -> str | None:
    """Related-series group id for a canonical name, or ``None``."""
    direct = COMMODITY_GROUPS.get(canonical_name)
    if direct is not None:
        return direct
    # Be tolerant of un-migrated raw spellings: resolve via canonicalize.
    canonical, _, _, _ = canonicalize(canonical_name)
    return COMMODITY_GROUPS.get(canonical)


def find_alias_rows(conn) -> list[tuple[int, str, str]]:
    """``(id, raw_name, canonical_name)`` for every non-canonical commodity."""
    rows = conn.execute("SELECT id, name FROM commodities ORDER BY name").fetchall()
    return [
        (int(row[0]), str(row[1]), canonicalize(str(row[1]))[0])
        for row in rows
        if canonicalize(str(row[1]))[0] != str(row[1])
    ]


def normalize_commodities(conn, dry_run: bool = False) -> list[tuple[str, str, int]]:
    """Merge alias commodity rows into their canonical rows.

    For each alias row, its prices move to the canonical ``commodity_id``;
    when a canonical row already exists for the same date+market, the
    existing non-null price wins, otherwise the alias price is taken.  The
    alias commodity row is then deleted.  One short transaction per alias
    (busy timeout 30 s).  Returns ``[(alias, canonical, moved)]`` merges.
    """
    conn.execute("PRAGMA busy_timeout = 30000")
    merges: list[tuple[str, str, int]] = []
    for alias_id, alias_name, canonical_name in find_alias_rows(conn):
        canonical, spec, unit, category = canonicalize(canonical_name)
        assert canonical == canonical_name
        row = conn.execute(
            "SELECT id FROM commodities WHERE name = ?", (canonical_name,)
        ).fetchone()
        if row is None:
            if dry_run:
                count = conn.execute(
                    "SELECT COUNT(*) FROM prices WHERE commodity_id = ?",
                    (alias_id,),
                ).fetchone()[0]
                merges.append((alias_name, canonical_name, int(count)))
                continue
            cur = conn.execute(
                "INSERT INTO commodities (name, category, spec, unit)"
                " VALUES (?, ?, ?, ?)",
                (canonical_name, category, spec, unit),
            )
            canonical_id = int(cur.lastrowid)
        else:
            canonical_id = int(row[0])
        prices = conn.execute(
            "SELECT date, market_id, price, price_low, price_high, source_url"
            " FROM prices WHERE commodity_id = ?",
            (alias_id,),
        ).fetchall()
        moved = 0
        if not dry_run:
            for day, market_id, price, low, high, url in prices:
                existing = conn.execute(
                    "SELECT price, source_url FROM prices"
                    " WHERE date = ? AND market_id = ? AND commodity_id = ?",
                    (day, market_id, canonical_id),
                ).fetchone()
                if existing is None:
                    conn.execute(
                        "UPDATE prices SET commodity_id = ?"
                        " WHERE date = ? AND market_id = ? AND commodity_id = ?",
                        (canonical_id, day, market_id, alias_id),
                    )
                    moved += 1
                else:
                    if existing[0] is None and price is not None:
                        conn.execute(
                            "UPDATE prices SET price = ?, price_low = ?,"
                            " price_high = ? WHERE date = ? AND market_id = ?"
                            " AND commodity_id = ?",
                            (price, low, high, day, market_id, canonical_id),
                        )
                    if existing[1] is None and url is not None:
                        conn.execute(
                            "UPDATE prices SET source_url = ?"
                            " WHERE date = ? AND market_id = ? AND commodity_id = ?",
                            (url, day, market_id, canonical_id),
                        )
                    conn.execute(
                        "DELETE FROM prices WHERE date = ? AND market_id = ?"
                        " AND commodity_id = ?",
                        (day, market_id, alias_id),
                    )
                    moved += 1
            conn.execute("DELETE FROM commodities WHERE id = ?", (alias_id,))
            conn.commit()
        else:
            moved = len(prices)
        merges.append((alias_name, canonical_name, moved))
    return merges


def cmd_normalize_commodities(args) -> int:
    from . import db as _db

    conn = _db.connect(args.db)
    try:
        merges = normalize_commodities(conn, dry_run=args.dry_run)
    finally:
        conn.close()
    if args.dry_run:
        print("normalize-commodities: dry run, no changes written")
    for alias_name, canonical_name, moved in merges:
        print(f"normalize-commodities: {alias_name!r} -> {canonical_name!r} ({moved} prices)")
    if not merges:
        print("normalize-commodities: nothing to merge")
    return 0


def build_normalize_parser(sub) -> None:
    import os as _os

    cmd = sub.add_parser(
        "normalize-commodities",
        help="Merge alias commodity rows into canonical rows",
    )
    cmd.add_argument("--db", default=_os.path.join("data", "presyo.db"))
    cmd.add_argument(
        "--dry-run", action="store_true", help="Show merges without writing"
    )
    cmd.set_defaults(func=cmd_normalize_commodities)
