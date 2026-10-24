"""Pearson correlation between commodity prices and the USD/PHP rate.

Correlation is not causation: a high ``r`` only says two series moved
together over the window, not that FX moves caused price moves.
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import math
import os
import sys


def pearson(xs: list[float], ys: list[float]) -> tuple[float | None, int]:
    """Pearson ``r`` over paired samples; ``(None, n)`` when undefined.

    Undefined (``r is None``) when fewer than 3 pairs or when either series
    has zero variance.  ``n`` is the number of paired samples.
    """
    pairs = [
        (float(x), float(y))
        for x, y in zip(xs, ys)
        if x is not None and y is not None
    ]
    n = len(pairs)
    if n < 3:
        return (None, n)
    mean_x = sum(x for x, _ in pairs) / n
    mean_y = sum(y for _, y in pairs) / n
    sxx = sum((x - mean_x) ** 2 for x, _ in pairs)
    syy = sum((y - mean_y) ** 2 for _, y in pairs)
    if sxx == 0 or syy == 0:
        return (None, n)
    sxy = sum((x - mean_x) * (y - mean_y) for x, y in pairs)
    return (sxy / math.sqrt(sxx * syy), n)


def lagged_correlation(
    price_series: list[tuple[str, float | None]],
    fx_series: list[tuple[str, float | None]],
    lag_days: int,
    max_stale_days: int = 5,
) -> dict:
    """Correlate prices with FX as-of ``lag_days`` earlier.

    The price on date ``d`` pairs with the FX rate as-of ``d - lag``: the
    last available FX rate on or before that target date, skipped when the
    nearest rate is more than *max_stale_days* old (ECB publishes business
    days only, so plain weekends resolve to Friday).  Returns
    ``{"lag": lag_days, "r": r, "n": n}``.
    """
    fx_by_date: dict[str, float] = {}
    for day, rate in fx_series:
        if rate is not None:
            fx_by_date[day] = float(rate)
    fx_dates = sorted(fx_by_date)

    def asof(target: str) -> float | None:
        best: str | None = None
        for day in fx_dates:
            if day <= target:
                best = day
            else:
                break
        if best is None:
            return None
        stale = (
            _datetime.date.fromisoformat(target)
            - _datetime.date.fromisoformat(best)
        ).days
        if stale > max_stale_days:
            return None
        return fx_by_date[best]

    xs: list[float] = []
    ys: list[float] = []
    for day, price in price_series:
        if price is None:
            continue
        target = (
            _datetime.date.fromisoformat(day)
            - _datetime.timedelta(days=lag_days)
        ).isoformat()
        rate = asof(target)
        if rate is None:
            continue
        xs.append(float(price))
        ys.append(rate)
    r, n = pearson(xs, ys)
    return {"lag": lag_days, "r": r, "n": n}


def cmd_correlate(args: argparse.Namespace) -> int:
    import json as _json

    from . import db as _db
    from . import fx as _fx

    if not os.path.exists(args.db):
        print(f"error: database not found: {args.db}", file=sys.stderr)
        return 1
    conn = _db.connect(args.db)
    try:
        market_hits = _db.match_names(conn, "markets", args.market)
        if len(market_hits) != 1:
            if not market_hits:
                all_markets = _db.match_names(conn, "markets", "")
                print(
                    f"error: no market matches {args.market!r}; available: "
                    + ", ".join(all_markets),
                    file=sys.stderr,
                )
            else:
                print(
                    f"error: ambiguous market {args.market!r}; candidates: "
                    + ", ".join(market_hits),
                    file=sys.stderr,
                )
            return 1
        item_hits = _db.match_names(conn, "commodities", args.item)
        if len(item_hits) != 1:
            if not item_hits:
                all_items = _db.match_names(conn, "commodities", "")
                print(
                    f"error: no commodity matches {args.item!r}; available: "
                    + ", ".join(all_items),
                    file=sys.stderr,
                )
            else:
                print(
                    f"error: ambiguous commodity {args.item!r}; candidates: "
                    + ", ".join(all_items),
                    file=sys.stderr,
                )
            return 1
        market, item = market_hits[0], item_hits[0]
        series = _db.price_series(conn, market, item)
        if args.days != "all":
            latest = _db.latest_date(conn)
            if latest is None:
                print(f"error: no prices in {args.db}", file=sys.stderr)
                return 1
            cutoff = (
                _datetime.date.fromisoformat(latest)
                - _datetime.timedelta(days=int(args.days))
            ).isoformat()
            series = [(day, price) for day, price in series if day >= cutoff]
        fx_rates = _fx.fx_series(conn)
        lags = [int(part) for part in args.lags.split(",") if part.strip() != ""]
        results = [
            lagged_correlation(series, fx_rates, lag) for lag in lags
        ]
    finally:
        conn.close()
    if args.json:
        print(
            _json.dumps(
                {"market": market, "item": item, "results": results},
                ensure_ascii=False,
            )
        )
    else:
        print(f"{market} — {item} vs USD/PHP")
        for entry in results:
            r = entry["r"]
            text = f"{r:+.3f}" if r is not None else "n/a"
            print(f"lag {entry['lag']}d: r={text} (n={entry['n']})")
        print("note: correlation is not causation")
    return 0


def build_stats_parser(sub) -> None:
    cmd = sub.add_parser(
        "correlate", help="Correlate a price series with USD/PHP by lag"
    )
    cmd.add_argument("--market", required=True)
    cmd.add_argument("--item", required=True)
    cmd.add_argument("--days", default="90")
    cmd.add_argument("--lags", default="0,7,14,30")
    cmd.add_argument("--db", default=os.path.join("data", "presyo.db"))
    cmd.add_argument("--json", action="store_true")
    cmd.set_defaults(func=cmd_correlate)
