"""Week-over-week price summary: pure computation over JSON snapshots.

Snapshots have the shape produced by ``parse``::

    {"date": "2026-10-23", "source_url": ..., "synthetic": ...,
     "markets": {market: {commodity: {"spec", "unit", "price"}}}}
"""

from __future__ import annotations

import datetime as _datetime
import glob
import json
import os
from dataclasses import dataclass, field

from .sanity import match_market

#: Markets of interest, Las Piñas first.
MARKETS_OF_INTEREST_ORDERED = [
    "New Las Piñas City Public Market",
    "Alabang Central Market",
    "La Huerta Market/Parañaque",
    "Bicutan Market",
]

#: Commodities tracked in the weekly summary.
KEY_ITEMS = [
    "Premium Rice (Imported)",
    "Well-milled Rice (Imported)",
    "Premium Rice (Local)",
    "Well-milled Rice (Local)",
    "Regular Milled (Local)",
    "Bangus",
    "Galunggong (Local)",
    "Tilapia",
    "Egg (Medium)",
    "Red Onion (Local)",
    "Tomato",
    "Fresh Pork (Kasim) Local",
    "Fresh Pork Liempo (Local)",
    "Fresh Whole Chicken",
]

NOTABLE_THRESHOLD_PCT = 5.0
MAX_NOTABLE_MOVES = 6


@dataclass
class ItemMove:
    """One (market, item) week-over-week change."""

    market: str
    item: str
    latest: float | None
    baseline: float | None
    abs_change: float | None
    pct_change: float | None  # rounded to 1 decimal


@dataclass
class CheapestResult:
    """Cheapest market(s) for one item (ties listed, NULLs skipped)."""

    item: str
    price: float | None
    markets: list[str] = field(default_factory=list)


@dataclass
class WeeklySummary:
    week_ending: str
    latest_date: str
    baseline_date: str | None
    synthetic: bool
    markets: list[str]  # resolved actual market names, Las Piñas first
    items: list[str]
    latest: dict[str, dict[str, float | None]]  # item -> market -> price
    baseline: dict[str, dict[str, float | None]]
    cheapest: list[CheapestResult] = field(default_factory=list)
    moves: list[ItemMove] = field(default_factory=list)  # notable, by |pct| desc

    @property
    def has_changes(self) -> bool:
        return self.baseline_date is not None


def parse_week_ending(value: str) -> _datetime.date:
    return _datetime.date.fromisoformat(value)


def get_price(payload: dict, market: str | None, item: str) -> float | None:
    if not market:
        return None
    entry = (payload.get("markets", {}) or {}).get(market, {}).get(item)
    if not isinstance(entry, dict):
        return None
    return entry.get("price")


def resolve_markets(markets: dict) -> list[str]:
    """Resolve the 4 markets of interest (Las Piñas first), skipping misses."""
    resolved = []
    for wanted in MARKETS_OF_INTEREST_ORDERED:
        actual = match_market(markets, wanted)
        if actual is not None and actual not in resolved:
            resolved.append(actual)
    return resolved


def load_snapshots(data_dir: str) -> list[dict]:
    """Load every ``*.json`` snapshot in *data_dir*, sorted by date."""
    snapshots = []
    for path in sorted(glob.glob(os.path.join(data_dir, "*.json"))):
        try:
            with open(path, encoding="utf-8") as handle:
                payload = json.load(handle)
        except (OSError, ValueError):
            continue
        date = payload.get("date")
        if not date:
            continue
        try:
            _datetime.date.fromisoformat(date)
        except ValueError:
            continue
        snapshots.append(payload)
    snapshots.sort(key=lambda p: p["date"])
    return snapshots


def select_snapshots(
    snapshots: list[dict], week_ending: str
) -> tuple[dict | None, dict | None]:
    """Pick ``(latest, baseline)`` snapshots for *week_ending*.

    Latest = newest snapshot with ``date <= week_ending``. Baseline = the
    snapshot whose date is closest to ``latest - 7 days`` within a window of
    5..10 days earlier; ``None`` when no snapshot falls in that window.
    """
    end = parse_week_ending(week_ending)
    eligible = [p for p in snapshots if p["date"] <= end.isoformat()]
    if not eligible:
        return (None, None)
    latest = max(eligible, key=lambda p: p["date"])
    latest_date = _datetime.date.fromisoformat(latest["date"])
    candidates: list[tuple[int, str, dict]] = []
    for payload in eligible:
        if payload["date"] == latest["date"]:
            continue
        day = _datetime.date.fromisoformat(payload["date"])
        diff = (latest_date - day).days
        if 5 <= diff <= 10:
            candidates.append((abs(diff - 7), payload["date"], payload))
    if not candidates:
        return (latest, None)
    # Closest to 7 days; ties prefer the more recent snapshot.
    best_dist = min(dist for dist, _, _ in candidates)
    baseline = max(
        (c for c in candidates if c[0] == best_dist), key=lambda t: t[1]
    )[2]
    return (latest, baseline)


def pct_change(latest: float | None, baseline: float | None) -> float | None:
    if latest is None or baseline is None or baseline == 0:
        return None
    return round((latest - baseline) / baseline * 100.0, 1)


def abs_change(latest: float | None, baseline: float | None) -> float | None:
    if latest is None or baseline is None:
        return None
    return round(latest - baseline, 1)


def compute_cheapest(
    items: list[str], markets: list[str], latest: dict[str, dict[str, float | None]]
) -> list[CheapestResult]:
    results = []
    for item in items:
        best: float | None = None
        winners: list[str] = []
        for market in markets:
            price = (latest.get(item) or {}).get(market)
            if price is None:
                continue
            if best is None or price < best:
                best = price
                winners = [market]
            elif price == best and market not in winners:
                winners.append(market)
        results.append(CheapestResult(item=item, price=best, markets=winners))
    return results


def compute_moves(
    items: list[str],
    markets: list[str],
    latest: dict[str, dict[str, float | None]],
    baseline: dict[str, dict[str, float | None]],
    *,
    threshold_pct: float = NOTABLE_THRESHOLD_PCT,
    max_moves: int = MAX_NOTABLE_MOVES,
) -> list[ItemMove]:
    moves = []
    for item in items:
        for market in markets:
            new = (latest.get(item) or {}).get(market)
            old = (baseline.get(item) or {}).get(market)
            pct = pct_change(new, old)
            if pct is None or abs(pct) < threshold_pct:
                continue
            moves.append(
                ItemMove(
                    market=market,
                    item=item,
                    latest=new,
                    baseline=old,
                    abs_change=abs_change(new, old),
                    pct_change=pct,
                )
            )
    moves.sort(key=lambda m: abs(m.pct_change or 0.0), reverse=True)
    return moves[:max_moves]


def build_summary(
    latest_payload: dict,
    baseline_payload: dict | None,
    week_ending: str,
    *,
    items: list[str] | None = None,
    threshold_pct: float = NOTABLE_THRESHOLD_PCT,
    max_moves: int = MAX_NOTABLE_MOVES,
) -> WeeklySummary:
    items = list(items or KEY_ITEMS)
    markets = resolve_markets(latest_payload.get("markets", {}) or {})
    latest: dict[str, dict[str, float | None]] = {}
    baseline: dict[str, dict[str, float | None]] = {}
    for item in items:
        latest[item] = {
            m: get_price(latest_payload, m, item) for m in markets
        }
        baseline[item] = {
            m: get_price(baseline_payload, m, item) if baseline_payload else None
            for m in markets
        }
    cheapest = compute_cheapest(items, markets, latest)
    moves: list[ItemMove] = []
    baseline_date = baseline_payload.get("date") if baseline_payload else None
    if baseline_payload is not None:
        moves = compute_moves(
            items, markets, latest, baseline,
            threshold_pct=threshold_pct, max_moves=max_moves,
        )
    return WeeklySummary(
        week_ending=week_ending,
        latest_date=latest_payload.get("date", week_ending),
        baseline_date=baseline_date,
        synthetic=bool(latest_payload.get("synthetic"))
        or bool(baseline_payload.get("synthetic") if baseline_payload else False),
        markets=markets,
        items=items,
        latest=latest,
        baseline=baseline,
        cheapest=cheapest,
        moves=moves,
    )


def build_summary_from_db(
    conn,
    week_ending: str,
    *,
    items: list[str] | None = None,
    threshold_pct: float = NOTABLE_THRESHOLD_PCT,
    max_moves: int = MAX_NOTABLE_MOVES,
) -> WeeklySummary | None:
    """Build a summary from the SQLite store (synthetic dates tracked in DB)."""
    from . import db as _db

    end = parse_week_ending(week_ending)
    dates = [d for d in _db.list_dates(conn) if d <= end.isoformat()]
    if not dates:
        return None
    latest_date = max(dates)
    latest_payload = _db.snapshot_for_date(conn, latest_date)
    latest_day = _datetime.date.fromisoformat(latest_date)
    candidates: list[tuple[int, str]] = []
    for day in dates:
        if day == latest_date:
            continue
        diff = (latest_day - _datetime.date.fromisoformat(day)).days
        if 5 <= diff <= 10:
            candidates.append((abs(diff - 7), day))
    best: str | None = None
    if candidates:
        best_dist = min(dist for dist, _ in candidates)
        best = max(day for dist, day in candidates if dist == best_dist)
    baseline_payload = _db.snapshot_for_date(conn, best) if best else None
    summary = build_summary(
        latest_payload, baseline_payload, week_ending,
        items=items, threshold_pct=threshold_pct, max_moves=max_moves,
    )
    return summary
