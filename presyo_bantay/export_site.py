"""Static-site data export: compact JSON snapshots of prices + FX + week.

Applies :func:`aliases.canonicalize` at export time so the site stays
correct even when the DB still contains un-migrated alias rows.  All JSON is
compact (no indentation, short keys) with prices rounded to 2 decimals.

Layout under ``--out``::

    meta.json            markets, commodities, date range, attributions
    fx.json              {d: [...], r: [...]} USD/PHP series
    week.json            latest/baseline/change per commodity + cheapest
    series/c<ID>.json    {d: [...], m: {market_id: [...]}} per commodity
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import json
import os

FX_SOURCE = (
    "USD/PHP: European Central Bank reference rates "
    "via Frankfurter (frankfurter.dev)"
)
PRICE_SOURCE = (
    "Philippine Department of Agriculture "
    "Price Monitoring (da.gov.ph)"
)


def _round2(value):
    return round(float(value), 2) if value is not None else None


def _dump(path: str, payload) -> None:
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))


def _canonical_groups(conn):
    """Group DB commodity rows by canonical name.

    Returns ``(info, order, member_canonical)`` where ``info[canonical]``
    is the export record, ``order`` lists canonicals in export-id order,
    and ``member_canonical[commodity_id]`` is ``(canonical, is_canonical)``.
    """
    from .aliases import canonicalize, group_for

    rows = conn.execute(
        "SELECT id, name, category, spec, unit FROM commodities"
    ).fetchall()
    groups: dict[str, list[dict]] = {}
    for row in rows:
        cid, name = int(row[0]), str(row[1])
        canonical = canonicalize(name)[0]
        groups.setdefault(canonical, []).append(
            {
                "id": cid,
                "name": name,
                "category": row[2],
                "spec": row[3],
                "unit": row[4],
                "canonical": canonical == name,
            }
        )
    for members in groups.values():
        members.sort(key=lambda m: (not m["canonical"], m["id"]))
    info: dict[str, dict] = {}
    member_canonical: dict[int, tuple[str, bool]] = {}
    for canonical, members in groups.items():
        first = members[0]
        _, spec, unit, category = canonicalize(canonical)
        info[canonical] = {
            "id": first["id"],
            "name": canonical,
            "unit": first["unit"] or unit,
            "category": first["category"] or category,
            "group": group_for(canonical),
        }
        for member in members:
            member_canonical[member["id"]] = (canonical, member["canonical"])
    order = sorted(info, key=lambda name: info[name]["id"])
    return info, order, member_canonical


def export_site(conn, out_dir: str) -> list[str]:
    """Write the site data files under *out_dir*; return written paths."""
    from . import db as _db
    from . import fx as _fx
    from .render import short_market
    from .summary import compute_cheapest, pct_change, select_snapshots

    os.makedirs(out_dir, exist_ok=True)
    series_dir = os.path.join(out_dir, "series")
    os.makedirs(series_dir, exist_ok=True)

    dates = _db.list_dates(conn)
    latest_date = max(dates) if dates else None

    markets = [
        {"id": int(row[0]), "name": str(row[1]), "city": row[2]}
        for row in conn.execute("SELECT id, name, city FROM markets ORDER BY id").fetchall()
    ]
    for entry in markets:
        entry["short"] = short_market(entry["name"])

    info, order, member_canonical = _canonical_groups(conn)

    # Default markets: the 4 markets of interest, Las Piñas first.
    resolved = _db.match_names(conn, "markets", "")
    by_name = {m["name"]: m["id"] for m in markets}
    from .summary import resolve_markets as _resolve

    market_names = {name: {} for name in resolved}
    default_names = _resolve(market_names)
    default_ids = [by_name[name] for name in default_names if name in by_name]

    # Merged price lookup: canonical-named member wins on conflicts (same
    # rule as normalize-commodities), alias prices fill gaps.
    price_rows = conn.execute(
        "SELECT date, market_id, commodity_id, price FROM prices"
    ).fetchall()
    merged: dict[tuple[str, int, str], float | None] = {}
    for day, market_id, commodity_id, price in price_rows:
        mapping = member_canonical.get(int(commodity_id))
        if mapping is None:
            continue
        canonical, is_canonical = mapping
        if is_canonical:
            merged[(str(day), int(market_id), canonical)] = price
    for day, market_id, commodity_id, price in price_rows:
        mapping = member_canonical.get(int(commodity_id))
        if mapping is None:
            continue
        canonical, is_canonical = mapping
        if is_canonical:
            continue
        key = (str(day), int(market_id), canonical)
        if key not in merged or (merged[key] is None and price is not None):
            merged[key] = price

    market_ids = [m["id"] for m in markets]
    for canonical in order:
        entry = info[canonical]
        columns: dict[str, list] = {}
        for mid in market_ids:
            columns[str(mid)] = [
                _round2(merged.get((day, mid, canonical))) for day in dates
            ]
        _dump(
            os.path.join(series_dir, f"c{entry['id']}.json"),
            {"d": dates, "m": columns},
        )

    fx_rows = _fx.fx_series(conn)
    _dump(
        os.path.join(out_dir, "fx.json"),
        {
            "d": [day for day, _ in fx_rows],
            "r": [round(rate, 4) for _, rate in fx_rows],
        },
    )

    # Week snapshot: reuse summary.py selection/cheapest/change logic on the
    # canonical-merged view of the DB.
    week_items = []
    if latest_date is not None:
        snapshots_meta = [{"date": day, "markets": {}} for day in dates]
        _, baseline = select_snapshots(snapshots_meta, latest_date)
        baseline_date = baseline["date"] if baseline else None
        latest_by_market: dict[str, dict[str, float | None]] = {}
        baseline_by_market: dict[str, dict[str, float | None]] = {}
        for canonical in order:
            per_market_latest = {}
            per_market_baseline = {}
            for m in markets:
                mid = m["id"]
                per_market_latest[str(mid)] = _round2(
                    merged.get((latest_date, mid, canonical))
                )
                per_market_baseline[str(mid)] = (
                    _round2(merged.get((baseline_date, mid, canonical)))
                    if baseline_date
                    else None
                )
            latest_by_market[canonical] = per_market_latest
            baseline_by_market[canonical] = per_market_baseline
        for canonical in order:
            entry = info[canonical]
            per_market = {}
            for m in markets:
                mid = str(m["id"])
                new = latest_by_market[canonical][mid]
                old = baseline_by_market[canonical][mid]
                per_market[mid] = {"p": new, "b": old, "c": pct_change(new, old)}
            cheap_all = compute_cheapest(
                [canonical],
                [str(m["id"]) for m in markets],
                {canonical: latest_by_market[canonical]},
            )[0]
            default_keys = [str(mid) for mid in default_ids]
            cheap_default = compute_cheapest(
                [canonical], default_keys, {canonical: latest_by_market[canonical]}
            )[0]
            week_items.append(
                {
                    "id": entry["id"],
                    "name": canonical,
                    "unit": entry["unit"],
                    "group": entry["group"],
                    "m": per_market,
                    "cheap4": {"p": cheap_default.price, "m": cheap_default.markets},
                    "cheap_all": {"p": cheap_all.price, "m": cheap_all.markets},
                }
            )
    else:
        baseline_date = None
    _dump(
        os.path.join(out_dir, "week.json"),
        {"latest": latest_date, "baseline": baseline_date, "items": week_items},
    )

    generated_at = (
        _datetime.datetime.now(_datetime.timezone.utc)
        .isoformat(timespec="seconds")
        .replace("+00:00", "Z")
    )
    _dump(
        os.path.join(out_dir, "meta.json"),
        {
            "updated": latest_date,
            "generated_at": generated_at,
            "date_min": min(dates) if dates else None,
            "date_max": latest_date,
            "markets": markets,
            "commodities": [
                {
                    "id": info[name]["id"],
                    "name": name,
                    "unit": info[name]["unit"],
                    "category": info[name]["category"],
                    "group": info[name]["group"],
                }
                for name in order
            ],
            "default_markets": default_ids,
            "fx_source": FX_SOURCE,
            "price_source": PRICE_SOURCE,
        },
    )
    written = (
        ["meta.json", "fx.json", "week.json"]
        + [f"series/c{info[name]['id']}.json" for name in order]
    )
    return [os.path.join(out_dir, path) for path in written]


def cmd_export_site(args: argparse.Namespace) -> int:
    from . import db as _db

    conn = _db.connect(args.db)
    try:
        written = export_site(conn, args.out)
    finally:
        conn.close()
    total = sum(os.path.getsize(path) for path in written)
    print(f"export-site: wrote {len(written)} file(s) to {args.out} ({total} bytes)")
    return 0


def build_export_parser(sub) -> None:
    cmd = sub.add_parser("export-site", help="Export compact JSON for the site")
    cmd.add_argument("--db", default=os.path.join("data", "presyo.db"))
    cmd.add_argument("--out", default=os.path.join("site", "public", "data"))
    cmd.set_defaults(func=cmd_export_site)
