"""Command-line interface: ``python -m presyo_bantay fetch|parse``."""

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
