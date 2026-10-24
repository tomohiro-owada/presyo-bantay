"""One-time historical backfill of per-market PDFs from da.gov.ph.

Politeness rules (owner-approved one-time run):

- The listing page is fetched fresh ONCE per run (or ``--listing-file`` reads
  local HTML, making zero network requests).
- Only per-market table PDFs are selected (see :mod:`listing`); one PDF per
  report date (best variant wins), newest first.
- Requests are sequential with ``--delay`` seconds (minimum 5) plus a random
  0..5 s jitter between ANY two HTTP requests.
- Files already cached under ``data/raw/`` are skipped without any request.
- Progress is resumable via ``data/backfill/state.json`` (saved after every
  file); failed/parsed-bad files are recorded in
  ``data/backfill/failures.jsonl`` and never ingested.
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import json
import os
import random
import time

import requests

from . import db as _db
from .cli import REQUEST_TIMEOUT, USER_AGENT
from .listing import LISTING_URL, find_all_from_html
from .pdfparse import infer_layout, parse_date_from_filename, parse_pdf
from .sanity import (
    MARKETS_OF_INTEREST,
    MIN_COMMODITIES_PER_MARKET,
    MIN_MARKETS,
    LayoutChangedError,
    match_market,
)

DEFAULT_DELAY = 10.0
MIN_DELAY = 5.0
MAX_RETRY_AFTER = 600.0  # cap honored Retry-After at 10 minutes
BACKOFF_BASE = 60.0  # exponential backoff starts at 60 s
MAX_FILE_ERRORS = 3  # consecutive errors on one file -> mark failed
MAX_CONSECUTIVE_FAILED = 5  # consecutive failed files (network/HTTP) -> stop

#: Rice names that satisfy the backfill "core rice present" check.
_BACKFILL_RICE = {
    "Premium Rice (Imported)",
    "Well-milled Rice (Imported)",
    "Premium Rice (Local)",
    "Well-milled Rice (Local)",
    "Regular Milled (Local)",
}


def _utcnow() -> str:
    return _datetime.datetime.now(_datetime.timezone.utc).isoformat(
        timespec="seconds"
    )


def select_targets(
    html: str,
    base_url: str = LISTING_URL,
    *,
    since: str | None = None,
    until: str | None = None,
    limit: int | None = None,
) -> tuple[list[tuple[str, str, str]], int]:
    """Select backfill targets from listing *html*.

    Returns ``(targets, n_listed)`` where each target is
    ``(filename, url, iso_date)`` sorted newest first, and *n_listed* is the
    number of per-market candidates found before filtering.
    """
    found = find_all_from_html(html, base_url=base_url)
    n_listed = len(found)
    targets = [
        (url.rsplit("/", 1)[-1].split("?")[0].split("#")[0], url, day.isoformat())
        for url, day in found
    ]
    if since is not None:
        targets = [t for t in targets if t[2] >= since]
    if until is not None:
        targets = [t for t in targets if t[2] <= until]
    if limit is not None:
        targets = targets[: max(0, limit)]
    return targets, n_listed


def run_backfill_checks(markets: dict, layout: str = "single") -> list[str]:
    """Structural sanity checks for backfilled (possibly old) PDFs.

    Raises :class:`LayoutChangedError` on hard structural failure; returns a
    list of warnings (missing markets of interest are warnings here, unlike
    the strict daily ``parse``/``fetch`` check). ``layout`` selects the
    thresholds: the ``"range"`` layout holds ~13 commodities per market
    (minimum 10) with ``Well-milled Rice (Local)`` + ``Egg (Medium)`` as
    the core commodities (old PDFs have no Bangus and no La Huerta).
    """
    from .sanity import RANGE_CORE_COMMODITIES, RANGE_MIN_COMMODITIES_PER_MARKET

    if layout not in ("single", "range"):
        raise LayoutChangedError(
            f"Layout may have changed: unknown layout {layout!r}"
        )
    min_commodities = (
        RANGE_MIN_COMMODITIES_PER_MARKET
        if layout == "range"
        else MIN_COMMODITIES_PER_MARKET
    )
    problems: list[str] = []
    if len(markets) < MIN_MARKETS:
        problems.append(
            f"only {len(markets)} markets parsed (minimum {MIN_MARKETS})"
        )
    thin = {
        name: len(items)
        for name, items in markets.items()
        if len(items) < min_commodities
    }
    if thin:
        worst = sorted(thin.items(), key=lambda kv: kv[1])[:5]
        detail = ", ".join(f"{name} ({count})" for name, count in worst)
        problems.append(
            f"{len(thin)} market(s) have fewer than "
            f"{min_commodities} commodities: {detail}"
        )
    present: set[str] = set()
    for items in markets.values():
        present.update(items.keys())
    if layout == "range":
        for core in RANGE_CORE_COMMODITIES:
            if core not in present:
                problems.append(f"core commodity missing everywhere: {core!r}")
    else:
        if not (present & _BACKFILL_RICE):
            problems.append("no core rice commodity found in any market")
        for core in ("Bangus", "Egg (Medium)"):
            if core not in present:
                problems.append(f"core commodity missing everywhere: {core!r}")
    if problems:
        raise LayoutChangedError(
            "Layout may have changed: " + "; ".join(problems)
        )
    warnings = [
        f"market of interest not found: {wanted!r}"
        for wanted in MARKETS_OF_INTEREST
        if match_market(markets, wanted) is None
    ]
    return warnings


def pdf_debug_info(pdf_path: str) -> tuple[int | None, list | None]:
    """Best-effort ``(n_pages, first_header_row)`` for failure records."""
    try:
        import pdfplumber

        with pdfplumber.open(pdf_path) as pdf:
            n_pages = len(pdf.pages)
            first_header: list | None = None
            for page in pdf.pages[:3]:
                try:
                    tables = page.extract_tables() or []
                except Exception:
                    continue
                for table in tables:
                    if table and len(table) >= 2:
                        first_header = [
                            (c or "").replace("\n", " ").strip()
                            for c in table[1][:8]
                        ]
                        break
                if first_header is not None:
                    break
            return n_pages, first_header
    except Exception:
        return None, None


def _retry_after_seconds(response) -> float | None:
    """Parse a ``Retry-After`` header (seconds), capped at 10 minutes."""
    raw = ""
    try:
        raw = (response.headers or {}).get("Retry-After", "")
    except AttributeError:
        return None
    if raw is None or str(raw).strip() == "":
        return None
    try:
        return min(float(str(raw).strip()), MAX_RETRY_AFTER)
    except ValueError:
        return None


class Backfiller:
    """Sequential polite backfill driver (one instance per run)."""

    def __init__(
        self,
        *,
        raw_dir: str,
        json_dir: str,
        db_path: str,
        backfill_dir: str,
        delay: float,
        dry_run: bool = False,
        retry_failed: bool = False,
        prior_requests: int = 0,
    ) -> None:
        self.raw_dir = raw_dir
        self.json_dir = json_dir
        self.db_path = db_path
        self.backfill_dir = backfill_dir
        self.delay = max(float(delay), MIN_DELAY)
        self.dry_run = dry_run
        self.retry_failed = retry_failed
        self.state_path = os.path.join(backfill_dir, "state.json")
        self.failures_path = os.path.join(backfill_dir, "failures.jsonl")
        self.log_path = os.path.join(backfill_dir, "backfill.log")
        self.state: dict = {}
        # Requests already made this run (e.g. the listing-page fetch), so the
        # delay+jitter also applies between the listing request and the first
        # PDF request.
        self._requests_made = prior_requests
        self.log_handle = None

    # -- helpers ------------------------------------------------------
    def log(self, message: str) -> None:
        print(message)
        if self.log_handle is not None:
            self.log_handle.write(message + "\n")
            self.log_handle.flush()

    def _polite_wait(self) -> None:
        """Sleep ``delay + jitter`` before every request but the first."""
        if self._requests_made > 0:
            time.sleep(self.delay + random.random() * 5.0)

    def _get(self, url: str):
        self._polite_wait()
        self._requests_made += 1
        return requests.get(
            url, headers={"User-Agent": USER_AGENT}, timeout=REQUEST_TIMEOUT
        )

    def load_state(self) -> None:
        try:
            with open(self.state_path, encoding="utf-8") as handle:
                data = json.load(handle)
            self.state = data if isinstance(data, dict) else {}
        except (OSError, ValueError):
            self.state = {}

    def save_state(self) -> None:
        os.makedirs(self.backfill_dir, exist_ok=True)
        tmp = self.state_path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(self.state, handle, indent=2, ensure_ascii=False)
        os.replace(tmp, self.state_path)

    def record_failure(self, *, filename: str, date: str, url: str,
                       error: str, pdf_path: str | None = None) -> None:
        n_pages, first_header_row = (
            pdf_debug_info(pdf_path) if pdf_path else (None, None)
        )
        os.makedirs(self.backfill_dir, exist_ok=True)
        with open(self.failures_path, "a", encoding="utf-8") as handle:
            handle.write(
                json.dumps(
                    {
                        "filename": filename,
                        "date": date,
                        "url": url,
                        "error": error,
                        "n_pages": n_pages,
                        "first_header_row": first_header_row,
                    },
                    ensure_ascii=False,
                )
                + "\n"
            )

    # -- per-file download --------------------------------------------
    def download(self, url: str) -> tuple[bytes | None, str | None, bool]:
        """Fetch *url* with retries.

        Returns ``(content, error, is_network_error)``; ``content`` is None
        on failure. HTTP 404 is a non-network failure (no retry).
        """
        errors = 0
        backoff = BACKOFF_BASE
        while True:
            try:
                response = self._get(url)
            except requests.RequestException as exc:
                errors += 1
                if errors >= MAX_FILE_ERRORS:
                    return None, f"network error: {exc}", True
                time.sleep(backoff)
                backoff *= 2
                continue
            status = getattr(response, "status_code", 200)
            if status == 200:
                return response.content, None, False
            if status == 404:
                return None, "HTTP 404", False
            if status == 429 or 500 <= status < 600:
                errors += 1
                if errors >= MAX_FILE_ERRORS:
                    return None, f"HTTP {status} (gave up after {errors} tries)", True
                wait = _retry_after_seconds(response)
                if wait is None:
                    wait = backoff
                    backoff *= 2
                time.sleep(wait)
                continue
            return None, f"HTTP {status}", True

    # -- main driver ----------------------------------------------------
    def run(self, targets: list[tuple[str, str, str]], n_listed: int) -> int:
        os.makedirs(self.raw_dir, exist_ok=True)
        if not self.dry_run:
            # A dry-run is a pure preview: stdout only, no state/log writes.
            os.makedirs(self.backfill_dir, exist_ok=True)
            self.log_handle = open(self.log_path, "a", encoding="utf-8")
        try:
            return self._run(targets, n_listed)
        finally:
            if self.log_handle is not None:
                self.log_handle.close()
                self.log_handle = None

    def _run(self, targets: list[tuple[str, str, str]], n_listed: int) -> int:
        n_selected = len(targets)
        n_cached = 0
        n_downloaded = 0
        n_ok = 0
        n_failed = 0
        consecutive_failed = 0
        total = len(targets)

        conn = None
        if not self.dry_run:
            conn = _db.connect(self.db_path)
        try:
            for index, (filename, url, iso_date) in enumerate(targets, 1):
                tag = f"[{index}/{total}] {filename}"
                previous = self.state.get(filename, {})
                if previous.get("status") == "ok" or (
                    previous.get("status") == "failed" and not self.retry_failed
                ):
                    # Resume: skip without any request.
                    self.log(f"{tag}: skipped (state: {previous.get('status')})")
                    if previous.get("status") == "ok":
                        consecutive_failed = 0
                    continue

                dest = os.path.join(self.raw_dir, filename)
                if os.path.exists(dest):
                    # Skip cached files: no HTTP request at all.
                    n_cached += 1
                    consecutive_failed = 0
                    if not self.dry_run:
                        self.state[filename] = {
                            "status": "skipped",
                            "date": iso_date,
                            "url": url,
                            "timestamp": _utcnow(),
                            "error": None,
                        }
                        self.save_state()
                    self.log(f"{tag}: skipped (already cached)")
                    continue

                if self.dry_run:
                    self.log(f"{tag}: would download ({url})")
                    continue

                content, error, is_network = self.download(url)
                if content is None:
                    n_failed += 1
                    if is_network:
                        consecutive_failed += 1
                    self.state[filename] = {
                        "status": "failed",
                        "date": iso_date,
                        "url": url,
                        "timestamp": _utcnow(),
                        "error": error,
                    }
                    self.save_state()
                    self.log(f"{tag}: failed ({error})")
                    if consecutive_failed >= MAX_CONSECUTIVE_FAILED:
                        self.log(
                            f"stopping: {consecutive_failed} consecutive failed "
                            "files (network/HTTP)"
                        )
                        self._summary(n_listed, n_selected, n_cached,
                                      n_downloaded, n_ok, n_failed)
                        return 1
                    continue

                n_downloaded += 1
                with open(dest, "wb") as handle:
                    handle.write(content)
                try:
                    with open(dest + ".meta.json", "w", encoding="utf-8") as handle:
                        json.dump(
                            {"source_url": url, "filename": filename},
                            handle, indent=2,
                        )
                except OSError:
                    pass

                try:
                    markets, text_date = parse_pdf(dest)
                    layout = infer_layout(markets)
                    warnings = run_backfill_checks(markets, layout)
                    date = parse_date_from_filename(dest) or text_date
                    if date is None:
                        raise LayoutChangedError(
                            "could not determine report date from "
                            "filename or PDF text"
                        )
                except Exception as exc:  # noqa: BLE001 - record and continue
                    err = f"{type(exc).__name__}: {exc}"
                    if isinstance(exc, LayoutChangedError):
                        err = str(exc)
                    n_failed += 1
                    self.state[filename] = {
                        "status": "failed",
                        "date": iso_date,
                        "url": url,
                        "timestamp": _utcnow(),
                        "error": err,
                    }
                    self.save_state()
                    self.record_failure(
                        filename=filename, date=iso_date, url=url,
                        error=err, pdf_path=dest,
                    )
                    self.log(f"{tag}: failed ({err})")
                    continue

                payload = {
                    "date": date.isoformat(),
                    "source_url": url,
                    "layout": layout,
                    "markets": markets,
                }
                os.makedirs(self.json_dir, exist_ok=True)
                with open(
                    os.path.join(self.json_dir, f"{date.isoformat()}.json"),
                    "w", encoding="utf-8",
                ) as handle:
                    json.dump(payload, handle, indent=2, ensure_ascii=False)
                assert conn is not None  # noqa: S101 - guarded by dry_run
                _db.ingest_payload(conn, payload)
                n_ok += 1
                consecutive_failed = 0
                entry: dict = {
                    "status": "ok",
                    "date": date.isoformat(),
                    "url": url,
                    "timestamp": _utcnow(),
                    "error": None,
                    "layout": layout,
                }
                if warnings:
                    entry["warnings"] = warnings
                self.state[filename] = entry
                self.save_state()
                extra = f" ({len(warnings)} warning(s))" if warnings else ""
                self.log(f"{tag}: ok ({date.isoformat()}){extra}")
        finally:
            if conn is not None:
                conn.close()
        self._summary(n_listed, n_selected, n_cached, n_downloaded, n_ok, n_failed)
        return 0

    def _summary(self, n_listed: int, n_selected: int, n_cached: int,
                 n_downloaded: int, n_ok: int, n_failed: int) -> None:
        self.log(
            "backfill summary: "
            f"listed={n_listed} selected={n_selected} "
            f"skipped-cached={n_cached} downloaded={n_downloaded} "
            f"ok={n_ok} failed={n_failed}"
        )


def build_backfill_parser(sub) -> None:
    backfill = sub.add_parser(
        "backfill", help="One-time historical backfill from da.gov.ph"
    )
    backfill.add_argument(
        "--listing-file", default=None,
        help="Use a local listing HTML file instead of the network",
    )
    backfill.add_argument("--since", default=None, help="Only dates >= YYYY-MM-DD")
    backfill.add_argument("--until", default=None, help="Only dates <= YYYY-MM-DD")
    backfill.add_argument("--limit", type=int, default=None,
                          help="Process at most N newest files")
    backfill.add_argument("--delay", type=float, default=DEFAULT_DELAY,
                          help=f"Seconds between requests (default {DEFAULT_DELAY:g}, minimum {MIN_DELAY:g})")
    backfill.add_argument("--dry-run", action="store_true",
                          help="List what would be downloaded; no PDF requests")
    backfill.add_argument("--retry-failed", action="store_true",
                          help="Re-process files previously marked failed")
    backfill.add_argument("--raw-dir", default=os.path.join("data", "raw"))
    backfill.add_argument("--json-dir", default=os.path.join("data", "json"))
    backfill.add_argument("--db", default=os.path.join("data", "presyo.db"))
    backfill.add_argument("--backfill-dir", default=os.path.join("data", "backfill"))
    backfill.set_defaults(func=cmd_backfill)


def cmd_backfill(args: argparse.Namespace) -> int:
    if args.listing_file:
        with open(args.listing_file, encoding="utf-8", errors="replace") as handle:
            html = handle.read()
        prior_requests = 0
    else:
        response = requests.get(
            LISTING_URL,
            headers={"User-Agent": USER_AGENT},
            timeout=REQUEST_TIMEOUT,
        )
        response.raise_for_status()
        html = response.text
        prior_requests = 1

    targets, n_listed = select_targets(
        html, LISTING_URL, since=args.since, until=args.until, limit=args.limit
    )
    worker = Backfiller(
        raw_dir=args.raw_dir,
        json_dir=args.json_dir,
        db_path=args.db,
        backfill_dir=args.backfill_dir,
        delay=args.delay,
        dry_run=args.dry_run,
        retry_failed=args.retry_failed,
        prior_requests=prior_requests,
    )
    worker.load_state()
    return worker.run(targets, n_listed)
