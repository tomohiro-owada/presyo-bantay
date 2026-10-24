"""Parse a DA Price-Monitoring PDF into per-market commodity prices.

Pages 1-3 hold market x commodity tables readable via
``pdfplumber`` ``page.extract_tables()``. Row 0 is a title row, row 1 holds
the commodity headers (with embedded newlines), row 2 is blank, and the
remaining rows are per-market prices. Markets repeat on each page with a
different commodity block, so results are merged by market name.

Units are PHP/KG except egg, which the PDF footnotes as "* per piece".
"""

from __future__ import annotations

import datetime as _datetime
import os
import re

import pdfplumber

NOT_AVAILABLE_TOKENS = {"NOT AVAILABLE", "NOT AVAILALBLE", "N/A", "NA", "-", "--"}

_MONTHS = {
    "january": 1, "jan": 1,
    "february": 2, "feb": 2,
    "march": 3, "mar": 3,
    "april": 4, "apr": 4,
    "may": 5,
    "june": 6, "jun": 6,
    "july": 7, "jul": 7,
    "august": 8, "aug": 8,
    "september": 9, "sept": 9, "sep": 9,
    "october": 10, "oct": 10,
    "november": 11, "nov": 11,
    "december": 12, "dec": 12,
}

# "Friday, 23 October 2026" or "October 23, 2026"
_DATE_TEXT_RES = [
    re.compile(r"(\d{1,2})\s+([A-Za-z]+)\s+(\d{4})"),
    re.compile(r"([A-Za-z]+)\s+(\d{1,2}),?\s+(\d{4})"),
]

_FILENAME_DATE_RE = re.compile(
    r"Price-Monitoring-([A-Za-z]+)-(\d{1,2})-(\d{4})", re.IGNORECASE
)


def normalize_commodity(raw: str) -> str:
    """Collapse whitespace/newlines and strip footnote markers (``*``)."""
    text = (raw or "").replace("\n", " ")
    text = re.sub(r"\s+", " ", text).strip()
    text = text.lstrip("*").strip()
    text = re.sub(r"\s+", " ", text)
    return text


def infer_spec(commodity: str) -> str | None:
    """Derive a spec qualifier from the commodity name, if any.

    Examples: ``Premium Rice (Imported)`` -> ``Imported``,
    ``Egg (Medium)`` -> ``Medium``, ``Bangus`` -> ``None``.
    """
    parens = re.findall(r"\(([^)]+)\)", commodity)
    after = re.sub(r".*\)\s*", "", commodity) if ")" in commodity else ""
    parts = [p.strip() for p in parens if p.strip()]
    if after.strip():
        parts.append(after.strip())
    if parts:
        return ", ".join(parts)
    return None


def infer_unit(raw_header: str) -> str:
    """Infer the unit from the raw commodity header.

    Per-piece commodities are marked in the PDF with a leading ``*``
    (e.g. ``'*Egg (Medium)'``) plus a "* per piece" footnote. The
    asterisk must be checked on the raw header *before* it is stripped
    by :func:`normalize_commodity` -- matching on the name alone (e.g.
    ``"egg" in name``) wrongly flags "Eggplant" as per piece.

    Fallback: when the header carries no asterisk (e.g. an already
    normalized name), only the exact commodity ``Egg (Medium)`` is
    treated as per piece; everything else defaults to per kg.
    """
    text = (raw_header or "").replace("\n", " ").strip()
    if text.startswith("*"):
        return "per piece"
    if normalize_commodity(raw_header).lower() == "egg (medium)":
        return "per piece"
    return "per kg"


def parse_price(raw: str | None) -> float | None:
    """Parse a table cell into a float price, or ``None`` when unavailable."""
    if raw is None:
        return None
    text = raw.strip().replace(",", "")
    if not text:
        return None
    if text.upper() in NOT_AVAILABLE_TOKENS:
        return None
    try:
        return float(text)
    except ValueError:
        return None


def parse_date_from_filename(pdf_path: str) -> _datetime.date | None:
    """Extract the report date from the PDF filename."""
    base = os.path.basename(pdf_path)
    match = _FILENAME_DATE_RE.search(base)
    if not match:
        return None
    month = _MONTHS.get(match.group(1).lower())
    if month is None:
        return None
    try:
        return _datetime.date(int(match.group(3)), month, int(match.group(2)))
    except ValueError:
        return None


def parse_date_from_text(text: str) -> _datetime.date | None:
    """Extract the report date from PDF text (e.g. 'Friday, 23 October 2026')."""
    for pattern in _DATE_TEXT_RES:
        for match in pattern.finditer(text or ""):
            groups = match.groups()
            try:
                if pattern is _DATE_TEXT_RES[0]:
                    day, mon, year = groups
                else:
                    mon, day, year = groups
                month = _MONTHS.get(mon.lower())
                if month is None:
                    continue
                candidate = _datetime.date(int(year), month, int(day))
            except ValueError:
                continue
            if 2020 <= candidate.year <= 2035:
                return candidate
    return None


def _parse_market_table(table: list[list]) -> tuple[list[str], list[str], list[list]]:
    """Split one pdfplumber table into (commodities, units, data_rows)."""
    if not table or len(table) < 4:
        return [], [], []
    header = table[1]
    raw_headers = [(c or "") for c in header[1:]]
    commodities = [normalize_commodity(c) for c in raw_headers]
    units = [infer_unit(c) for c in raw_headers]
    rows = []
    for row in table[3:]:
        if not row or not (row[0] or "").strip():
            continue
        market = " ".join((row[0] or "").split())
        rows.append([market] + list(row[1:]))
    return commodities, units, rows


def parse_pdf(pdf_path: str) -> tuple[dict, _datetime.date | None]:
    """Parse *pdf_path* into ``{market: {commodity: entry}}`` plus date hint.

    Entry shape: ``{"spec": str | None, "unit": str, "price": float | None}``.
    Only pages 1-3 (the per-market tables) are parsed; pages 4-6 (the NCR
    low/high/average summary) are intentionally skipped.
    """
    markets: dict[str, dict[str, dict]] = {}
    date_hint: _datetime.date | None = None
    with pdfplumber.open(pdf_path) as pdf:
        for page in pdf.pages[:3]:
            try:
                tables = page.extract_tables() or []
            except Exception:
                continue
            if date_hint is None:
                try:
                    date_hint = parse_date_from_text(page.extract_text() or "")
                except Exception:
                    date_hint = None
            for table in tables:
                commodities, units, rows = _parse_market_table(table)
                if not commodities:
                    continue
                # Skip degenerate non-market tables.
                if not any(c for c in commodities):
                    continue
                for row in rows:
                    market = row[0]
                    cells = row[1:]
                    entry = markets.setdefault(market, {})
                    for commodity, unit, cell in zip(commodities, units, cells):
                        if not commodity or commodity.upper() == "MARKET":
                            continue
                        entry[commodity] = {
                            "spec": infer_spec(commodity),
                            "unit": unit,
                            "price": parse_price(cell if isinstance(cell, str) else (cell or "")),
                        }
    return markets, date_hint


def report_date(pdf_path: str, markets_hint_text: str | None = None) -> _datetime.date | None:
    """Best-effort report date: filename first, then PDF text."""
    date = parse_date_from_filename(pdf_path)
    if date is not None:
        return date
    if markets_hint_text:
        return parse_date_from_text(markets_hint_text)
    return None
