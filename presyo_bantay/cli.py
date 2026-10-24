"""Command-line interface: ``python -m presyo_bantay <command>``."""

from __future__ import annotations

import argparse
import json
import os
import sys

import requests

from .listing import LISTING_URL, find_latest_from_html
from .pdfparse import parse_date_from_filename, parse_pdf
from .sanity import (
    MARKETS_OF_INTEREST,
    LayoutChangedError,
    match_market,
    run_sanity_checks,
)

USER_AGENT = (
    "PresyoBantay/0.1 (non-profit community project; "
    "contact via github)"
)
REQUEST_TIMEOUT = 30

RAW_DIR = os.path.join("data", "raw")
DEFAULT_OUT_DIR = os.path.join("data", "json")
DEFAULT_DB = os.path.join("data", "presyo.db")


def _read_meta_source_url(pdf_path: str) -> str | None:
    """Look for ``data/raw/<filename>.meta.json`` written by ``fetch``."""
    filename = os.path.basename(pdf_path)
    for candidate in (
        os.path.join(RAW_DIR, filename + ".meta.json"),
        pdf_path + ".meta.json",
    ):
        if os.path.exists(candidate):
            try:
                with open(candidate, encoding="utf-8") as handle:
                    meta = json.load(handle)
                url = meta.get("source_url")
                if url:
                    return url
            except (OSError, ValueError):
                continue
    return None


def cmd_fetch(args: argparse.Namespace) -> int:
    if args.listing_file:
        with open(args.listing_file, encoding="utf-8", errors="replace") as handle:
            html = handle.read()
    else:
        response = requests.get(
            LISTING_URL,
            headers={"User-Agent": USER_AGENT},
            timeout=REQUEST_TIMEOUT,
        )
        response.raise_for_status()
        html = response.text

    found = find_latest_from_html(html, base_url=LISTING_URL)
    if found is None:
        print("error: no Price-Monitoring PDF link found", file=sys.stderr)
        return 1
    pdf_url, _date = found
    filename = pdf_url.rsplit("/", 1)[-1].split("?")[0]
    os.makedirs(RAW_DIR, exist_ok=True)
    dest = os.path.join(RAW_DIR, filename)
    meta_path = dest + ".meta.json"

    if not os.path.exists(dest):
        # Exactly one PDF request per run (skipped when already cached).
        pdf_response = requests.get(
            pdf_url, headers={"User-Agent": USER_AGENT}, timeout=REQUEST_TIMEOUT
        )
        pdf_response.raise_for_status()
        with open(dest, "wb") as handle:
            handle.write(pdf_response.content)
    try:
        with open(meta_path, "w", encoding="utf-8") as handle:
            json.dump({"source_url": pdf_url, "filename": filename}, handle, indent=2)
    except OSError:
        pass

    print(dest)
    if args.parse:
        parse_args = argparse.Namespace(
            pdf=dest,
            source_url=None,
            markets_of_interest=False,
            out_dir=args.out_dir,
        )
        return cmd_parse(parse_args)
    return 0


def cmd_parse(args: argparse.Namespace) -> int:
    pdf_path = args.pdf
    if not os.path.exists(pdf_path):
        print(f"error: file not found: {pdf_path}", file=sys.stderr)
        return 1

    markets, text_date = parse_pdf(pdf_path)
    date = parse_date_from_filename(pdf_path) or text_date
    if date is None:
        print(
            "error: Layout may have changed: "
            "could not determine report date from filename or PDF text",
            file=sys.stderr,
        )
        return 2

    try:
        run_sanity_checks(markets)
    except LayoutChangedError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    if args.markets_of_interest:
        selected: dict = {}
        for wanted in MARKETS_OF_INTEREST:
            actual = match_market(markets, wanted)
            if actual is not None:
                selected[actual] = markets[actual]
        markets = selected

    source_url = args.source_url or _read_meta_source_url(pdf_path)
    payload = {
        "date": date.isoformat(),
        "source_url": source_url,
        "markets": markets,
    }
    os.makedirs(args.out_dir, exist_ok=True)
    out_path = os.path.join(args.out_dir, f"{date.isoformat()}.json")
    with open(out_path, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, indent=2, ensure_ascii=False)
    print(out_path)
    return 0


def cmd_summarize(args: argparse.Namespace) -> int:
    from . import db as _db
    from .llm import maybe_rewrite
    from .render import render_summary
    from .summary import build_summary, build_summary_from_db, load_snapshots, select_snapshots

    fmt = "markdownv2" if args.format == "markdownv2" else "html"
    summary = None
    # --data-dir ingests JSON files into an in-memory DB (synthetic allowed),
    # so file-backed and DB-backed summaries share one code path.
    if args.data_dir:
        snapshots = load_snapshots(args.data_dir)
        latest, baseline = select_snapshots(snapshots, args.week_ending)
        if latest is None:
            print(
                f"error: no snapshot on or before {args.week_ending} "
                f"in {args.data_dir}",
                file=sys.stderr,
            )
            return 1
        conn = _db.connect(":memory:")
        try:
            for payload in snapshots:
                _db.ingest_payload(conn, payload, allow_synthetic=True)
            summary = build_summary_from_db(conn, args.week_ending)
        finally:
            conn.close()
        if summary is None:  # pragma: no cover - guarded above
            summary = build_summary(latest, baseline, args.week_ending)
        # NOTE: the in-memory DB tracks synthetic dates (see db.ingest_payload),
        # so build_summary_from_db already warns when EITHER the latest or the
        # baseline snapshot is synthetic. No manual override needed.
    elif os.path.exists(args.db):
        conn = _db.connect(args.db)
        try:
            summary = build_summary_from_db(conn, args.week_ending)
        finally:
            conn.close()
        if summary is None:
            print(
                f"error: no snapshot on or before {args.week_ending} "
                f"in {args.db}",
                file=sys.stderr,
            )
            return 1
    else:
        snapshots = load_snapshots(DEFAULT_OUT_DIR)
        latest, baseline = select_snapshots(snapshots, args.week_ending)
        if latest is None:
            print(
                f"error: no snapshot on or before {args.week_ending} "
                f"in {DEFAULT_OUT_DIR} (and no DB at {args.db})",
                file=sys.stderr,
            )
            return 1
        summary = build_summary(latest, baseline, args.week_ending)

    text = render_summary(summary, fmt=fmt)
    text = maybe_rewrite(text, enabled=args.llm)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as handle:
            handle.write(text + "\n")
        print(args.out)
    else:
        print(text)
    return 0


def cmd_post_telegram(args: argparse.Namespace) -> int:
    from .telegram import post_telegram

    with open(args.file, encoding="utf-8") as handle:
        text = handle.read()
    return post_telegram(text, parse_mode=args.parse_mode, dry_run=args.dry_run)


def cmd_ingest(args: argparse.Namespace) -> int:
    from . import db as _db

    src = args.path
    files: list[str] = []
    if os.path.isdir(src):
        files = sorted(
            os.path.join(src, name)
            for name in os.listdir(src)
            if name.endswith(".json")
        )
    elif os.path.exists(src):
        files = [src]
    else:
        print(f"error: file not found: {src}", file=sys.stderr)
        return 1

    conn = _db.connect(args.db)
    try:
        n_files = 0
        t_markets = t_commodities = t_prices = 0
        skipped: list[str] = []
        for path in files:
            n_m, n_c, n_p, was_skipped = _db.ingest_file(
                conn, path, allow_synthetic=args.allow_synthetic
            )
            if was_skipped:
                skipped.append(os.path.basename(path))
                print(
                    f"warning: skipping synthetic file {path} "
                    "(use --allow-synthetic to ingest)",
                    file=sys.stderr,
                )
                continue
            n_files += 1
            t_markets += n_m
            t_commodities += n_c
            t_prices += n_p
    finally:
        conn.close()
    print(
        f"ingested {n_files} file(s) into {args.db}: "
        f"{t_markets} market rows, {t_commodities} commodity rows, "
        f"{t_prices} price rows"
        + (f" (skipped synthetic: {', '.join(skipped)})" if skipped else "")
    )
    return 0


def cmd_trend(args: argparse.Namespace) -> int:
    import datetime as _dt
    import json as _json

    from . import db as _db

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
                    "error: no market matches "
                    f"{args.market!r}; available: " + ", ".join(all_markets),
                    file=sys.stderr,
                )
            else:
                print(
                    "error: ambiguous market "
                    f"{args.market!r}; candidates: " + ", ".join(market_hits),
                    file=sys.stderr,
                )
            return 1
        item_hits = _db.match_names(conn, "commodities", args.item)
        if len(item_hits) != 1:
            if not item_hits:
                all_items = _db.match_names(conn, "commodities", "")
                print(
                    "error: no commodity matches "
                    f"{args.item!r}; available: " + ", ".join(all_items),
                    file=sys.stderr,
                )
            else:
                print(
                    "error: ambiguous commodity "
                    f"{args.item!r}; candidates: " + ", ".join(item_hits),
                    file=sys.stderr,
                )
            return 1
        market, item = market_hits[0], item_hits[0]
        latest = _db.latest_date(conn)
        if latest is None:
            print(f"error: no prices in {args.db}", file=sys.stderr)
            return 1
        cutoff = (
            _dt.date.fromisoformat(latest) - _dt.timedelta(days=args.days)
        ).isoformat()
        series = [
            (day, price)
            for day, price in _db.price_series(conn, market, item)
            if day >= cutoff
        ]
        known = [(day, price) for day, price in series if price is not None]
        if known:
            prices_only = [p for _, p in known]
            lo, hi = min(prices_only), max(prices_only)
            first_day, first_price = known[0][0], known[0][1]
            last_day, last_price = known[-1][0], known[-1][1]
            change = (
                round(last_price - first_price, 1)
                if first_price is not None and last_price is not None
                else None
            )
            change_pct = (
                round((last_price - first_price) / first_price * 100.0, 1)
                if first_price
                else None
            )
            stats = {
                "market": market,
                "item": item,
                "latest_date": last_day,
                "latest": last_price,
                "min": lo,
                "max": hi,
                "change": change,
                "change_pct": change_pct,
            }
        else:
            stats = {
                "market": market,
                "item": item,
                "latest_date": None,
                "latest": None,
                "min": None,
                "max": None,
                "change": None,
                "change_pct": None,
            }
    finally:
        conn.close()

    if args.json:
        print(_json.dumps({"series": series, "stats": stats}, ensure_ascii=False))
    else:
        print(f"{stats['market']} — {stats['item']}")
        for day, price in series:
            print(f"{day}: {'n/a' if price is None else f'₱{price:,.2f}'}")
        if stats["latest"] is None:
            print("no prices in range")
        else:
            print(
                f"latest {stats['latest_date']}: ₱{stats['latest']:,.2f} "
                f"(min ₱{stats['min']:,.2f}, max ₱{stats['max']:,.2f})"
            )
            if stats["change"] is not None:
                sign = "+" if stats["change"] >= 0 else ""
                print(
                    f"change: {sign}{stats['change']:.1f} "
                    f"({sign}{stats['change_pct']:.1f}%)"
                )
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="presyo-bantay", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    fetch = sub.add_parser("fetch", help="Download the latest Price-Monitoring PDF")
    fetch.add_argument(
        "--listing-file",
        default=None,
        help="Use a local listing HTML file instead of the network",
    )
    fetch.add_argument(
        "--parse",
        action="store_true",
        help="Parse the PDF right after downloading",
    )
    fetch.add_argument(
        "--out-dir",
        default=DEFAULT_OUT_DIR,
        help="JSON output directory when --parse is given",
    )
    fetch.set_defaults(func=cmd_fetch)

    parse = sub.add_parser("parse", help="Parse a Price-Monitoring PDF to JSON")
    parse.add_argument("pdf", help="Path to the PDF file")
    parse.add_argument("--source-url", default=None, help="Source URL of the PDF")
    parse.add_argument(
        "--markets-of-interest",
        action="store_true",
        help="Only include the 4 markets of interest in the output",
    )
    parse.add_argument(
        "--out-dir", default=DEFAULT_OUT_DIR, help="JSON output directory"
    )
    parse.set_defaults(func=cmd_parse)

    summarize = sub.add_parser("summarize", help="Write the weekly price summary")
    summarize.add_argument("--week-ending", required=True, help="Week-ending YYYY-MM-DD")
    summarize.add_argument(
        "--data-dir",
        default=None,
        help="JSON snapshot directory (alternative to --db; "
        "ingested into an in-memory DB, synthetic allowed)",
    )
    summarize.add_argument(
        "--db", default=DEFAULT_DB, help="SQLite database path (default data/presyo.db)"
    )
    summarize.add_argument(
        "--format", default="html", choices=["html", "markdownv2"]
    )
    summarize.add_argument("--out", default=None, help="Write message to FILE")
    summarize.add_argument(
        "--llm",
        action="store_true",
        help="Allow the optional LLM rephrase hook "
        "(needs PRESYO_LLM_BASE_URL + PRESYO_LLM_API_KEY)",
    )
    summarize.set_defaults(func=cmd_summarize)

    post = sub.add_parser("post-telegram", help="Post a message file to Telegram")
    post.add_argument("--file", required=True, help="Message file to send")
    post.add_argument(
        "--parse-mode", default="HTML", choices=["HTML", "MarkdownV2"]
    )
    post.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the message and target without network or token",
    )
    post.set_defaults(func=cmd_post_telegram)

    ingest = sub.add_parser("ingest", help="Ingest JSON snapshot(s) into SQLite")
    ingest.add_argument("path", help="JSON file or directory of JSON files")
    ingest.add_argument("--db", default=DEFAULT_DB, help="SQLite database path")
    ingest.add_argument(
        "--allow-synthetic",
        action="store_true",
        help="Allow ingesting payloads flagged synthetic (tests only)",
    )
    ingest.set_defaults(func=cmd_ingest)

    trend = sub.add_parser("trend", help="Show a price trend from SQLite")
    trend.add_argument("--market", required=True, help="Market name (substring match)")
    trend.add_argument("--item", required=True, help="Commodity name (substring match)")
    trend.add_argument("--days", type=int, default=90)
    trend.add_argument("--db", default=DEFAULT_DB, help="SQLite database path")
    trend.add_argument("--json", action="store_true", help="Emit JSON")
    trend.set_defaults(func=cmd_trend)

    from .backfill import build_backfill_parser as _backfill_parser

    _backfill_parser(sub)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args))
    except LayoutChangedError as exc:  # pragma: no cover - defensive
        print(f"error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":  # pragma: no cover
    sys.exit(main())
