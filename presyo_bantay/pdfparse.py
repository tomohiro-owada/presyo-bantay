"""Parse a DA Price-Monitoring PDF into per-market commodity prices.

Two layouts are supported (auto-detected):

- ``"single"`` (current): pages 1-3 hold market x commodity tables readable
  via ``pdfplumber`` ``page.extract_tables()``. Row 0 is a title row, row 1
  holds the commodity headers (with embedded newlines), row 2 is blank, and
  the remaining rows are per-market prices. Markets repeat on each page with
  a different commodity block, so results are merged by market name. Cells
  hold single values (``"45.00"``) or ``NOT AVAILABLE``.

- ``"range"`` (before 2026-08-18, titled
  ``Retail Price Range of Selected Agri-fishery Commodities per Market``):
  3 table pages with ~13 commodities total (page 1: Well-milled Rice
  (Local), Tilapia, Galunggong (Local), *Egg (Medium); page 2: Ampalaya,
  Eggplant, Tomato, Cabbage, Red Onion (Local); page 3: Sugar (Washed),
  Fresh Pork Kasim/Pigue, Fresh Pork Liempo, Fresh Whole Chicken). Cells are
  single values (``"45.00"``), ranges (``"150.00-160.00"`` /
  ``"300.00 - 350.00"``), or NOT AVAILABLE. On page 2 ``extract_tables``
  yields a spurious ``None`` header column next to Tomato and range cells
  like ``"100.00 - 120.00"`` or ``"100.00 -100.00"`` may be split across
  adjacent cells; the range parser merges any empty-header column into the
  previous commodity (verified against ``page.extract_text()`` lines).

Entry shape: ``{"spec": str | None, "unit": str, "price": float | None,
"price_low": float | None, "price_high": float | None}``. For ranges
``price_low``/``price_high`` are the printed bounds and ``price`` is the
midpoint rounded to 2 decimals (derived); for single values
``price_low == price_high == price``.

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


# "150.00-160.00", "300.00 - 350.00", "45.00 -50.00", "100.00 -100.00"
_RANGE_RE = re.compile(
    r"^\s*([0-9]+(?:,[0-9]{3})*(?:\.[0-9]+)?)\s*-\s*"
    r"([0-9]+(?:,[0-9]{3})*(?:\.[0-9]+)?)\s*$"
)


def parse_range_price(raw: str | None) -> tuple[float | None, float | None, float | None]:
    """Parse a range-layout cell into ``(price, low, high)``.

    Single values yield ``(value, value, value)``; ranges yield the
    midpoint (rounded to 2 decimals, documented as derived) plus the
    printed bounds; unavailable/blank/unparseable cells yield
    ``(None, None, None)``.
    """
    if raw is None:
        return (None, None, None)
    text = raw.strip()
    if not text:
        return (None, None, None)
    squished = text.replace(",", "")
    if squished.upper() in NOT_AVAILABLE_TOKENS:
        return (None, None, None)
    match = _RANGE_RE.match(squished)
    if match:
        try:
            low = float(match.group(1))
            high = float(match.group(2))
        except ValueError:
            return (None, None, None)
        return (round((low + high) / 2.0, 2), low, high)
    single = parse_price(text)
    if single is None:
        return (None, None, None)
    return (single, single, single)


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


def _parse_range_market_table(
    table: list[list],
) -> tuple[list[str], list[str], list[list[str | None]]]:
    """Split one range-layout table into (commodities, units, data_rows).

    Unlike the single layout there is no blank row 2, so data starts at
    ``table[2:]`` (blank rows are skipped). Columns whose header is empty
    (the spurious ``None`` column next to Tomato on page 2) are merged
    into the previous commodity by concatenating their cell text, so
    ranges split across cells (``"75.00"`` + ``"- 85.00"``) reassemble
    before parsing.
    """
    if not table or len(table) < 3:
        return [], [], []
    header = table[1]
    raw_headers = [(c or "") for c in header[1:]]
    # Group data-column indexes by commodity: an empty header continues
    # the previous commodity instead of starting a new one.
    groups: list[list[int]] = []
    commodities: list[str] = []
    units: list[str] = []
    for idx, raw in enumerate(raw_headers):
        name = normalize_commodity(raw)
        if not name and groups:
            groups[-1].append(idx)
            continue
        if not name:
            continue  # leading empty header: no commodity to attach to
        groups.append([idx])
        commodities.append(name)
        units.append(infer_unit(raw))
    rows: list[list[str | None]] = []
    for row in table[2:]:
        if not row or not (row[0] or "").strip():
            continue
        market = " ".join((row[0] or "").split())
        cells = list(row[1:])
        merged: list[str | None] = []
        for group in groups:
            parts = [
                cells[i] if i < len(cells) and isinstance(cells[i], str) else (cells[i] or "")
                for i in group
            ]
            text = "".join(p if isinstance(p, str) else "" for p in parts)
            merged.append(text if text.strip() else None)
        if not any((c or "").strip() for c in merged):
            continue
        rows.append([market] + merged)
    return commodities, units, rows


#: Commodity names that only appear in the single layout.
_SINGLE_MARKERS = {
    "premium rice (imported)",
    "well-milled rice (imported)",
    "premium rice (local)",
    "regular milled (local)",
    "bangus",
}

#: Core commodities of the range layout.
RANGE_CORE_COMMODITIES = ["Well-milled Rice (Local)", "Egg (Medium)"]


def _layout_from_commodity_names(names: set[str]) -> str | None:
    """Map a set of lowercase commodity names to ``"single"``/``"range"``."""
    lowered = {n for n in names if n}
    if lowered & _SINGLE_MARKERS:
        return "single"
    if (
        "well-milled rice (local)" in lowered
        and "egg (medium)" in lowered
        and len(lowered) <= 16
    ):
        return "range"
    if len(lowered) >= 20:
        return "single"
    return None


def detect_layout(pdf_path: str) -> str:
    """Detect the table layout of *pdf_path* (``"single"`` or ``"range"``).

    Raises :class:`ValueError` when the headers match neither layout.
    """
    from .sanity import LayoutChangedError

    names: set[str] = set()
    with pdfplumber.open(pdf_path) as pdf:
        for page in pdf.pages[:3]:
            try:
                tables = page.extract_tables() or []
            except Exception:
                continue
            for table in tables:
                if not table or len(table) < 2:
                    continue
                header = table[1]
                for cell in header[1:]:
                    name = normalize_commodity(cell or "")
                    if name and name.upper() != "MARKET":
                        names.add(name.lower())
    layout = _layout_from_commodity_names(names)
    if layout is None:
        raise LayoutChangedError(
            f"Layout may have changed: unrecognized commodity headers "
            f"({len(names)} commodities)"
        )
    return layout


def infer_layout(markets: dict) -> str:
    """Infer the layout from already-parsed *markets*.

    Raises :class:`LayoutChangedError` when the commodities match neither
    layout (e.g. an empty parse result).
    """
    from .sanity import LayoutChangedError

    names: set[str] = set()
    for items in (markets or {}).values():
        for key in (items or {}):
            names.add(" ".join(str(key).lower().split()))
    layout = _layout_from_commodity_names(names)
    if layout is None:
        raise LayoutChangedError(
            "Layout may have changed: unrecognized commodity set "
            f"({len(names)} commodities)"
        )
    return layout


def _parse_single_pdf(pdf_path: str) -> tuple[dict, _datetime.date | None]:
    """Parse a single-layout PDF (original behaviour, plus low/high)."""
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
                        price = parse_price(cell if isinstance(cell, str) else (cell or ""))
                        entry[commodity] = {
                            "spec": infer_spec(commodity),
                            "unit": unit,
                            "price": price,
                            "price_low": price,
                            "price_high": price,
                        }
    return markets, date_hint


def _parse_range_pdf(pdf_path: str) -> tuple[dict, _datetime.date | None]:
    """Parse a range-layout PDF (pre-2026-08-18)."""
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
                commodities, units, rows = _parse_range_market_table(table)
                if not commodities:
                    continue
                if not any(c for c in commodities):
                    continue
                for row in rows:
                    market = row[0]
                    cells = row[1:]
                    entry = markets.setdefault(market, {})
                    for commodity, unit, cell in zip(commodities, units, cells):
                        if not commodity or commodity.upper() == "MARKET":
                            continue
                        price, low, high = parse_range_price(
                            cell if isinstance(cell, str) else (cell or "")
                        )
                        entry[commodity] = {
                            "spec": infer_spec(commodity),
                            "unit": unit,
                            "price": price,
                            "price_low": low,
                            "price_high": high,
                        }
    return markets, date_hint


def parse_pdf(pdf_path: str) -> tuple[dict, _datetime.date | None]:
    """Parse *pdf_path* into ``{market: {commodity: entry}}`` plus date hint.

    The layout is auto-detected (``"single"`` vs ``"range"``); unknown
    layouts raise :class:`LayoutChangedError`.

    Entry shape: ``{"spec": str | None, "unit": str, "price": float | None,
    "price_low": float | None, "price_high": float | None}``. For ranges
    ``price`` is the midpoint of the printed bounds (derived); for single
    values ``price_low == price_high == price``. Only pages 1-3 (the
    per-market tables) are parsed; later pages (the NCR low/high/average
    summary) are intentionally skipped.
    """
    layout = detect_layout(pdf_path)
    if layout == "range":
        return _parse_range_pdf(pdf_path)
    return _parse_single_pdf(pdf_path)


def parse_pdf_with_layout(pdf_path: str) -> tuple[dict, _datetime.date | None, str]:
    """Parse *pdf_path*, also returning the detected layout name."""
    layout = detect_layout(pdf_path)
    if layout == "range":
        markets, hint = _parse_range_pdf(pdf_path)
    else:
        markets, hint = _parse_single_pdf(pdf_path)
    return markets, hint, layout


def report_date(pdf_path: str, markets_hint_text: str | None = None) -> _datetime.date | None:
    """Best-effort report date: filename first, then PDF text."""
    date = parse_date_from_filename(pdf_path)
    if date is not None:
        return date
    if markets_hint_text:
        return parse_date_from_text(markets_hint_text)
    return None
