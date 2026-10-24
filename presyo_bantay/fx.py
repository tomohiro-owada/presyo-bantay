"""USD/PHP reference rates via the Frankfurter API (ECB reference rates).

Frankfurter is keyless: ``GET https://api.frankfurter.dev/v1/{start}..{end}
?from=USD&to=PHP`` returns ECB business-day rates only (no weekends or
holidays)::

    {"amount": 1.0, "base": "USD", "start_date": ..., "end_date": ...,
     "rates": {"2026-10-01": {"PHP": 62.799}, ...}}

Rates are cached in the ``fx_rates`` table (``source='ECB via Frankfurter'``)
so weekends can be answered as-of the previous business day.
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import os
import sys
import time

import requests

FX_BASE = "USD"
FX_QUOTE = "PHP"
FX_SOURCE = "ECB via Frankfurter"
FX_API = "https://api.frankfurter.dev/v1"

USER_AGENT = (
    "PresyoBantay/0.1 (non-profit community project; "
    "contact via github)"
)
REQUEST_TIMEOUT = 30
CHUNK_DELAY_SECONDS = 2.0


def manila_today() -> str:
    """Today's date in Asia/Manila (UTC+8, no DST) as ``YYYY-MM-DD``."""
    tz = _datetime.timezone(_datetime.timedelta(hours=8), name="Asia/Manila")
    return _datetime.datetime.now(tz).date().isoformat()


def _add_days(date_str: str, days: int) -> str:
    day = _datetime.date.fromisoformat(date_str) + _datetime.timedelta(days=days)
    return day.isoformat()


def latest_fx_date(conn, base: str = FX_BASE, quote: str = FX_QUOTE) -> str | None:
    row = conn.execute(
        "SELECT MAX(date) FROM fx_rates WHERE base = ? AND quote = ?",
        (base, quote),
    ).fetchone()
    return str(row[0]) if row and row[0] is not None else None


def default_since(conn) -> str | None:
    """Day after the latest stored FX date, else the earliest price date."""
    latest = latest_fx_date(conn)
    if latest is not None:
        return _add_days(latest, 1)
    from . import db as _db

    dates = _db.list_dates(conn)
    return min(dates) if dates else None


def split_yearly(since: str, until: str) -> list[tuple[str, str]]:
    """Split ``[since, until]`` into calendar-year chunks (each <= 1 year)."""
    chunks: list[tuple[str, str]] = []
    start = _datetime.date.fromisoformat(since)
    end = _datetime.date.fromisoformat(until)
    while start <= end:
        year_end = _datetime.date(start.year, 12, 31)
        chunk_end = min(year_end, end)
        chunks.append((start.isoformat(), chunk_end.isoformat()))
        start = chunk_end + _datetime.timedelta(days=1)
    return chunks


def fetch_rates(
    since: str, until: str, base: str = FX_BASE, quote: str = FX_QUOTE
) -> dict[str, float]:
    """Fetch business-day rates for ``[since, until]`` (one HTTP request)."""
    url = f"{FX_API}/{since}..{until}"
    response = requests.get(
        url,
        params={"from": base, "to": quote},
        headers={"User-Agent": USER_AGENT},
        timeout=REQUEST_TIMEOUT,
    )
    response.raise_for_status()
    payload = response.json()
    rates = payload.get("rates", {})
    return {
        day: float(per_currency[quote])
        for day, per_currency in rates.items()
        if isinstance(per_currency, dict) and quote in per_currency
    }


def upsert_rates(
    conn,
    rates: dict[str, float],
    base: str = FX_BASE,
    quote: str = FX_QUOTE,
    source: str = FX_SOURCE,
) -> int:
    """Upsert ``{date: rate}`` rows; return the number of rows written."""
    count = 0
    for day in sorted(rates):
        conn.execute(
            "INSERT INTO fx_rates (date, base, quote, rate, source)"
            " VALUES (?, ?, ?, ?, ?)"
            " ON CONFLICT (date, base, quote, source) DO UPDATE SET"
            " rate = excluded.rate",
            (day, base, quote, float(rates[day]), source),
        )
        count += 1
    conn.commit()
    return count


def fx_asof(
    conn, date: str, base: str = FX_BASE, quote: str = FX_QUOTE
) -> float | None:
    """Latest stored rate on or before *date* (covers weekends), else None."""
    row = conn.execute(
        "SELECT rate FROM fx_rates WHERE base = ? AND quote = ? AND date <= ?"
        " ORDER BY date DESC LIMIT 1",
        (base, quote, date),
    ).fetchone()
    return float(row[0]) if row and row[0] is not None else None


def fx_series(conn, base: str = FX_BASE, quote: str = FX_QUOTE) -> list[tuple[str, float]]:
    """``(date, rate)`` series ascending by date."""
    return [
        (str(day), float(rate))
        for day, rate in conn.execute(
            "SELECT date, rate FROM fx_rates WHERE base = ? AND quote = ?"
            " ORDER BY date",
            (base, quote),
        ).fetchall()
    ]


def cmd_fx_update(args: argparse.Namespace) -> int:
    from . import db as _db

    conn = _db.connect(args.db)
    try:
        since = args.since or default_since(conn)
        until = args.until or manila_today()
        if since is None:
            print("fx-update: no price dates in DB; nothing to do")
            return 0
        if since > until:
            print(f"fx-update: since {since} > until {until}; nothing to do")
            return 0
        total = 0
        chunks = split_yearly(since, until)
        for index, (start, end) in enumerate(chunks):
            if index > 0:
                time.sleep(CHUNK_DELAY_SECONDS)
            rates = fetch_rates(start, end)
            total += upsert_rates(conn, rates)
        print(f"fx-update: {total} rate(s) for {since}..{until}")
        return 0
    finally:
        conn.close()


def build_fx_parser(sub) -> None:
    cmd = sub.add_parser("fx-update", help="Fetch USD/PHP rates via Frankfurter")
    cmd.add_argument("--db", default=os.path.join("data", "presyo.db"))
    cmd.add_argument("--since", default=None, help="Start date YYYY-MM-DD")
    cmd.add_argument("--until", default=None, help="End date YYYY-MM-DD")
    cmd.set_defaults(func=cmd_fx_update)
