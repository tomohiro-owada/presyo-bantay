"""SQLite store for Presyo Bantay snapshots (D1-compatible plain SQLite).

Schema lives in :file:`presyo_bantay/schema.sql` and uses only simple
``INTEGER``/``TEXT``/``REAL`` types with no extensions.
"""

from __future__ import annotations

import json
import os
import sqlite3

SCHEMA_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "schema.sql")

# Well-known market -> city mapping (used when the name itself is obvious).
_KNOWN_CITIES = {
    "new las piñas city public market": "Las Piñas",
    "alabang central market": "Muntinlupa",
    "bicutan market": "Taguig",
}

_FISH = {"bangus", "tilapia", "galunggong (local)"}
_VEGETABLES = {
    "ampalaya",
    "eggplant",
    "tomato",
    "cabbage",
    "carrots (local)",
    "chayote",
    "pechay baguio",
    "white potato (local)",
}
_SPICES = {
    "red onion (local)",
    "white onion (local)",
    "chili (green)",
    "red chili (local)",
    "calamansi",
}


def derive_city(market_name: str) -> str | None:
    """Derive a city from a market name, or ``None`` when unsure.

    A ``/`` suffix is treated as a city hint (``La Huerta Market/Parañaque``
    -> ``Parañaque``); a few well-known markets map to their cities; anything
    else yields ``None``.
    """
    name = (market_name or "").strip()
    if "/" in name:
        hint = name.rsplit("/", 1)[-1].strip().strip("() ")
        if hint:
            return hint
    known = _KNOWN_CITIES.get(" ".join(name.lower().split()))
    if known:
        return known
    return None


def categorize_commodity(commodity_name: str) -> str:
    """Map a commodity name to a broad category."""
    lowered = " ".join((commodity_name or "").lower().split())
    if "rice" in lowered or "milled" in lowered:
        return "rice"
    if "egg" in lowered and "eggplant" not in lowered:
        return "egg"
    if lowered in _FISH or any(f in lowered for f in ("bangus", "tilapia", "galunggong")):
        return "fish"
    if lowered in _VEGETABLES:
        return "vegetables"
    if lowered in _SPICES or "onion" in lowered or "chili" in lowered or "calamansi" in lowered:
        return "spices"
    if "sugar" in lowered:
        return "sugar"
    if "pork" in lowered:
        return "pork"
    if "chicken" in lowered:
        return "poultry"
    return "other"


def _ensure_price_range_columns(conn: sqlite3.Connection) -> None:
    """Add ``price_low``/``price_high`` to an old-schema ``prices`` table.

    Uses plain ``ALTER TABLE ... ADD COLUMN`` (D1-compatible); the
    duplicate-column error is ignored so this is safe to run on every
    open, including databases that already have the columns.
    """
    for column in ("price_low", "price_high"):
        try:
            conn.execute(f"ALTER TABLE prices ADD COLUMN {column} REAL")
        except sqlite3.OperationalError as exc:
            if "duplicate column name" not in str(exc).lower():
                raise
    conn.commit()


def connect(db_path: str) -> sqlite3.Connection:
    """Open *db_path* (creating parent dirs) and ensure the schema exists."""
    parent = os.path.dirname(os.path.abspath(db_path))
    if parent:
        os.makedirs(parent, exist_ok=True)
    conn = sqlite3.connect(db_path)
    with open(SCHEMA_PATH, encoding="utf-8") as handle:
        conn.executescript(handle.read())
    _ensure_price_range_columns(conn)
    return conn


def _get_or_create_market(conn: sqlite3.Connection, name: str) -> int:
    row = conn.execute("SELECT id FROM markets WHERE name = ?", (name,)).fetchone()
    if row is not None:
        return int(row[0])
    cur = conn.execute(
        "INSERT INTO markets (name, city) VALUES (?, ?)", (name, derive_city(name))
    )
    return int(cur.lastrowid)


def _get_or_create_commodity(
    conn: sqlite3.Connection, name: str, spec: str | None, unit: str | None
) -> int:
    row = conn.execute(
        "SELECT id FROM commodities WHERE name = ?", (name,)
    ).fetchone()
    if row is not None:
        return int(row[0])
    cur = conn.execute(
        "INSERT INTO commodities (name, category, spec, unit) VALUES (?, ?, ?, ?)",
        (name, categorize_commodity(name), spec, unit),
    )
    return int(cur.lastrowid)


def ingest_payload(
    conn: sqlite3.Connection,
    payload: dict,
    *,
    allow_synthetic: bool = False,
) -> tuple[int, int, int]:
    """Upsert one snapshot payload; return ``(markets, commodities, prices)``.

    Idempotent: uses ``INSERT ... ON CONFLICT DO UPDATE`` so re-ingesting the
    same file overwrites prices instead of duplicating rows. Payloads flagged
    ``"synthetic": true`` are refused unless *allow_synthetic* is set.
    """
    if payload.get("synthetic") and not allow_synthetic:
        return (0, 0, 0)
    date = payload.get("date")
    source_url = payload.get("source_url")
    markets = payload.get("markets", {})
    if payload.get("synthetic"):
        # Track synthetic dates (only reachable with allow_synthetic=True)
        # so summaries can still warn when the baseline week is synthetic.
        conn.execute(
            "INSERT OR IGNORE INTO synthetic_dates (date) VALUES (?)", (date,)
        )
    n_markets = 0
    n_commodities = 0
    n_prices = 0
    for market_name, items in markets.items():
        market_id = _get_or_create_market(conn, market_name)
        n_markets += 1
        for commodity_name, entry in (items or {}).items():
            spec = entry.get("spec") if isinstance(entry, dict) else None
            unit = entry.get("unit") if isinstance(entry, dict) else None
            # Store canonical commodity names (typo/case/qualifier variants
            # collapse to one series); raw JSON payloads are left untouched.
            from .aliases import canonicalize as _canonicalize

            canonical, cspec, cunit, _ccat = _canonicalize(commodity_name)
            if canonical != commodity_name:
                spec, unit = cspec, cunit
            else:
                spec = spec if spec is not None else cspec
                unit = unit if unit is not None else cunit
            price = entry.get("price") if isinstance(entry, dict) else None
            # Older JSON payloads lack price_low/price_high: fall back to
            # the (midpoint) price so they still load.
            if isinstance(entry, dict) and "price_low" in entry:
                price_low = entry.get("price_low")
            else:
                price_low = price
            if isinstance(entry, dict) and "price_high" in entry:
                price_high = entry.get("price_high")
            else:
                price_high = price
            commodity_id = _get_or_create_commodity(conn, canonical, spec, unit)
            n_commodities += 1
            conn.execute(
                "INSERT INTO prices (date, market_id, commodity_id, price,"
                " price_low, price_high, source_url)"
                " VALUES (?, ?, ?, ?, ?, ?, ?)"
                " ON CONFLICT (date, market_id, commodity_id) DO UPDATE SET"
                " price = excluded.price, price_low = excluded.price_low,"
                " price_high = excluded.price_high,"
                " source_url = excluded.source_url",
                (date, market_id, commodity_id, price, price_low, price_high,
                 source_url),
            )
            n_prices += 1
    conn.commit()
    return (n_markets, n_commodities, n_prices)


def ingest_file(
    conn: sqlite3.Connection, path: str, *, allow_synthetic: bool = False
) -> tuple[int, int, int, bool]:
    """Ingest one JSON file; return ``(markets, commodities, prices, skipped)``."""
    with open(path, encoding="utf-8") as handle:
        payload = json.load(handle)
    if payload.get("synthetic") and not allow_synthetic:
        return (0, 0, 0, True)
    counts = ingest_payload(conn, payload, allow_synthetic=allow_synthetic)
    return (*counts, False)


def list_dates(conn: sqlite3.Connection) -> list[str]:
    """All snapshot dates in the DB, ascending (``YYYY-MM-DD`` text)."""
    rows = conn.execute("SELECT DISTINCT date FROM prices ORDER BY date").fetchall()
    return [str(row[0]) for row in rows]


def latest_date(conn: sqlite3.Connection) -> str | None:
    rows = conn.execute("SELECT MAX(date) FROM prices").fetchone()
    return str(rows[0]) if rows and rows[0] is not None else None


def snapshot_for_date(conn: sqlite3.Connection, date: str) -> dict:
    """Rebuild a ``{market: {commodity: entry}}`` snapshot for *date*."""
    try:
        rows = conn.execute(
            "SELECT m.name, c.name, c.spec, c.unit, p.price, p.price_low,"
            " p.price_high, p.source_url"
            " FROM prices p"
            " JOIN markets m ON m.id = p.market_id"
            " JOIN commodities c ON c.id = p.commodity_id"
            " WHERE p.date = ?",
            (date,),
        ).fetchall()
    except sqlite3.OperationalError:
        # Pre-migration database without the range columns.
        rows = [
            (market, commodity, spec, unit, price, price, price, url)
            for market, commodity, spec, unit, price, url in conn.execute(
                "SELECT m.name, c.name, c.spec, c.unit, p.price, p.source_url"
                " FROM prices p"
                " JOIN markets m ON m.id = p.market_id"
                " JOIN commodities c ON c.id = p.commodity_id"
                " WHERE p.date = ?",
                (date,),
            ).fetchall()
        ]
    markets: dict[str, dict[str, dict]] = {}
    source_url: str | None = None
    for market, commodity, spec, unit, price, low, high, url in rows:
        if source_url is None and url is not None:
            source_url = url
        markets.setdefault(market, {})[commodity] = {
            "spec": spec,
            "unit": unit,
            "price": price,
            "price_low": low if low is not None else price,
            "price_high": high if high is not None else price,
        }
    synthetic = (
        conn.execute(
            "SELECT 1 FROM synthetic_dates WHERE date = ?", (date,)
        ).fetchone()
        is not None
    )
    return {
        "date": date,
        "source_url": source_url,
        "markets": markets,
        "synthetic": synthetic,
    }


def match_names(
    conn: sqlite3.Connection, table: str, needle: str
) -> list[str]:
    """Case-insensitive substring match of *needle* against table names."""
    norm = " ".join(needle.lower().split())
    if table == "markets":
        rows = conn.execute("SELECT name FROM markets ORDER BY name").fetchall()
    else:
        rows = conn.execute("SELECT name FROM commodities ORDER BY name").fetchall()
    return [
        str(row[0])
        for row in rows
        if norm in " ".join(str(row[0]).lower().split())
    ]


def price_series(
    conn: sqlite3.Connection, market: str, commodity: str
) -> list[tuple[str, float | None]]:
    """``(date, price)`` series for one market/commodity, ascending by date.

    ``price`` is the midpoint for range-layout rows; see
    :func:`price_range_series` for the printed bounds.
    """
    return [
        (str(date), price)
        for date, price in conn.execute(
            "SELECT p.date, p.price FROM prices p"
            " JOIN markets m ON m.id = p.market_id"
            " JOIN commodities c ON c.id = p.commodity_id"
            " WHERE m.name = ? AND c.name = ? ORDER BY p.date",
            (market, commodity),
        ).fetchall()
    ]


def price_range_series(
    conn: sqlite3.Connection, market: str, commodity: str
) -> list[tuple[str, float | None, float | None, float | None]]:
    """``(date, price, low, high)`` series, ascending by date.

    Falls back to ``(date, price, price, price)`` on pre-migration
    databases lacking the range columns.
    """
    try:
        rows = conn.execute(
            "SELECT p.date, p.price, p.price_low, p.price_high FROM prices p"
            " JOIN markets m ON m.id = p.market_id"
            " JOIN commodities c ON c.id = p.commodity_id"
            " WHERE m.name = ? AND c.name = ? ORDER BY p.date",
            (market, commodity),
        ).fetchall()
    except sqlite3.OperationalError:
        rows = [
            (date, price, price, price)
            for date, price in conn.execute(
                "SELECT p.date, p.price FROM prices p"
                " JOIN markets m ON m.id = p.market_id"
                " JOIN commodities c ON c.id = p.commodity_id"
                " WHERE m.name = ? AND c.name = ? ORDER BY p.date",
                (market, commodity),
            ).fetchall()
        ]
    return [
        (str(date), price, low if low is not None else price,
         high if high is not None else price)
        for date, price, low, high in rows
    ]
