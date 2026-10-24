"""Parse the DA price-monitoring listing page and pick the latest PDF.

Rules (from the project spec):
- Never construct URLs: scrape ``<a href>`` links from the listing page.
- Keep only main-target filenames of the shape
  ``Price-Monitoring-{Month}-{D}-{YYYY}.pdf`` with an optional ``Revised-``
  or ``Updated-`` prefix, an optional ``-N`` numeric suffix, or an optional
  ``-Revised`` suffix.
- Exclude ``Daily-Cigarette-Price-Monitoring-...`` (cigarette), as well as
  ``Daily-Price-Index-...``, ``Weekly-Average-Prices-...``,
  ``Price-Watch-...`` and ``RETAIL-*`` links.
- Parse the date from the filename, pick the latest date; for the same date
  prefer ``Revised`` over non-revised and a higher numeric suffix over lower.
"""

from __future__ import annotations

import datetime as _datetime
import re
from urllib.parse import urljoin

LISTING_URL = "https://www.da.gov.ph/price-monitoring/"

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

# e.g. Price-Monitoring-October-23-2026.pdf
#      Revised-Price-Monitoring-October-13-2026.pdf
#      Updated-Price-Monitoring-May-22-2023.pdf
#      Price-Monitoring-October-23-2026-1.pdf
#      Price-Monitoring-December-6-2023-Revised.pdf
_FILENAME_RE = re.compile(
    r"^(?:(?P<prefix>Revised|Updated)-)?"
    r"Price-Monitoring-"
    r"(?P<month>[A-Za-z]+)-(?P<day>\d{1,2})-(?P<year>\d{4})"
    r"(?:-(?P<suffix>\d+))?"
    r"(?:-(?P<revised_suffix>[Rr]evised))?"
    r"\.pdf$",
    re.IGNORECASE,
)


def parse_filename_date(filename: str) -> _datetime.date | None:
    """Return the report date encoded in *filename*, or ``None``."""
    # Strip query strings / whitespace that sometimes leak into hrefs.
    clean = filename.strip().split("?")[0].split("#")[0]
    match = _FILENAME_RE.match(clean)
    if not match:
        return None
    month = _MONTHS.get(match.group("month").lower())
    if month is None:
        return None
    try:
        return _datetime.date(
            int(match.group("year")), month, int(match.group("day"))
        )
    except ValueError:
        return None


def _ordering_key(filename: str, date: _datetime.date) -> tuple:
    """Sort key: latest date wins; ties prefer Revised/Updated, then higher suffix."""
    clean = filename.strip().split("?")[0].split("#")[0]
    match = _FILENAME_RE.match(clean)
    revised = bool(
        match and (match.group("prefix") or match.group("revised_suffix"))
    )
    suffix = int(match.group("suffix")) if match and match.group("suffix") else 0
    return (date, revised, suffix)


def _is_candidate_filename(filename: str) -> bool:
    """True if *filename* is a main-target Price-Monitoring PDF name."""
    clean = filename.strip().split("?")[0].split("#")[0]
    lowered = clean.lower()
    # Explicitly excluded kinds (defensive: the regex below already rejects
    # most of them, but cigarette links contain "price-monitoring" too).
    if "cigarette" in lowered:
        return False
    if "daily-price-index" in lowered or "weekly-average" in lowered:
        return False
    if "price-watch" in lowered:
        return False
    if "retail" in lowered:
        return False
    if parse_filename_date(clean) is None:
        return False
    return True


def find_latest_from_urls(urls: list[str]) -> tuple[str, _datetime.date] | None:
    """Pick the latest Price-Monitoring PDF URL from *urls*.

    Returns ``(url, date)`` or ``None`` when nothing matches.
    """
    best: tuple[str, _datetime.date] | None = None
    best_key: tuple | None = None
    for url in urls:
        filename = url.rsplit("/", 1)[-1]
        if not _is_candidate_filename(filename):
            continue
        date = parse_filename_date(filename)
        if date is None:  # pragma: no cover - guarded above
            continue
        key = _ordering_key(filename, date)
        if best_key is None or key > best_key:
            best_key = key
            best = (url, date)
    return best


def find_latest_from_html(html: str, base_url: str = LISTING_URL) -> tuple[str, _datetime.date] | None:
    """Scrape listing *html* and return the latest PDF ``(url, date)``."""
    from bs4 import BeautifulSoup

    soup = BeautifulSoup(html, "html.parser")
    urls: list[str] = []
    for anchor in soup.find_all("a", href=True):
        href = anchor.get("href", "").strip()
        if not href:
            continue
        if ".pdf" not in href.lower():
            continue
        urls.append(urljoin(base_url, href))
    return find_latest_from_urls(urls)


def find_all_from_html(
    html: str, base_url: str = LISTING_URL
) -> list[tuple[str, _datetime.date]]:
    """Scrape listing *html* and return every per-market PDF ``(url, date)``.

    One entry per report date (best variant wins: ``Revised``/``Updated`` >
    plain, higher ``-N`` suffix wins), sorted newest first.
    """
    from bs4 import BeautifulSoup

    soup = BeautifulSoup(html, "html.parser")
    urls: list[str] = []
    for anchor in soup.find_all("a", href=True):
        href = anchor.get("href", "").strip()
        if not href:
            continue
        if ".pdf" not in href.lower():
            continue
        urls.append(urljoin(base_url, href))
    return find_all_from_urls(urls)


def find_all_from_urls(urls: list[str]) -> list[tuple[str, _datetime.date]]:
    """Collect the best PDF URL per report date, newest first."""
    best: dict[str, tuple[str, _datetime.date, tuple]] = {}  # iso-date -> entry
    for url in urls:
        filename = url.rsplit("/", 1)[-1].split("?")[0].split("#")[0]
        if not _is_candidate_filename(filename):
            continue
        date = parse_filename_date(filename)
        if date is None:  # pragma: no cover - guarded above
            continue
        key = _ordering_key(filename, date)
        iso = date.isoformat()
        if iso not in best or key > best[iso][2]:
            best[iso] = (url, date, key)
    ranked = sorted(best.values(), key=lambda entry: entry[2], reverse=True)
    return [(url, date) for url, date, _key in ranked]
