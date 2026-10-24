"""Offline retry of backfill failures using the auto-detecting parser.

For every ``state.json`` entry with status ``failed`` whose PDF is cached
locally under ``--raw-dir``, re-parse with layout auto-detection plus the
backfill-style relaxed sanity checks. On success the JSON snapshot is
written, ingested into SQLite, the state entry is flipped to ``ok`` (with
``"layout"``), and the entry is dropped from ``failures.jsonl`` (the file
is rewritten keeping only still-failing entries).

This command never makes any network request.
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import json
import os

from .backfill import run_backfill_checks
from .pdfparse import infer_layout, parse_date_from_filename, parse_pdf
from .sanity import LayoutChangedError

DEFAULT_STATE = os.path.join("data", "backfill", "state.json")
DEFAULT_RAW_DIR = os.path.join("data", "raw")
DEFAULT_DB = os.path.join("data", "presyo.db")
DEFAULT_JSON_DIR = os.path.join("data", "json")


def _utcnow() -> str:
    return _datetime.datetime.now(_datetime.timezone.utc).isoformat(
        timespec="seconds"
    )


def build_reparse_parser(sub) -> None:
    cmd = sub.add_parser(
        "reparse-failed",
        help="Re-parse cached PDFs for backfill entries marked failed (offline)",
    )
    cmd.add_argument("--state", default=DEFAULT_STATE)
    cmd.add_argument("--raw-dir", default=DEFAULT_RAW_DIR)
    cmd.add_argument("--db", default=DEFAULT_DB)
    cmd.add_argument("--json-dir", default=DEFAULT_JSON_DIR)
    cmd.set_defaults(func=cmd_reparse_failed)


def cmd_reparse_failed(args: argparse.Namespace) -> int:
    from . import db as _db

    state_path = args.state
    raw_dir = args.raw_dir
    db_path = args.db
    json_dir = args.json_dir
    backfill_dir = os.path.dirname(os.path.abspath(state_path))
    failures_path = os.path.join(backfill_dir, "failures.jsonl")

    try:
        with open(state_path, encoding="utf-8") as handle:
            data = json.load(handle)
        state = data if isinstance(data, dict) else {}
    except (OSError, ValueError):
        state = {}

    failed = {
        filename: entry
        for filename, entry in state.items()
        if isinstance(entry, dict) and entry.get("status") == "failed"
    }

    n_failed = len(failed)
    n_cached = 0
    n_ok = 0
    n_still_failing = 0
    n_missing = 0
    recovered: set[str] = set()
    dirty = False

    conn = None
    if failed and any(
        os.path.exists(os.path.join(raw_dir, fn)) for fn in failed
    ):
        conn = _db.connect(db_path)
    try:
        for filename, entry in sorted(failed.items()):
            pdf_path = os.path.join(raw_dir, filename)
            if not os.path.exists(pdf_path):
                n_missing += 1
                n_still_failing += 1
                continue
            n_cached += 1
            url = entry.get("url") if isinstance(entry, dict) else None
            try:
                markets, text_date = parse_pdf(pdf_path)
                layout = infer_layout(markets)
                run_backfill_checks(markets, layout)
                date = parse_date_from_filename(pdf_path) or text_date
                if date is None:
                    raise LayoutChangedError(
                        "could not determine report date from "
                        "filename or PDF text"
                    )
            except Exception as exc:  # noqa: BLE001 - keep failed, continue
                n_still_failing += 1
                err = str(exc) if isinstance(exc, LayoutChangedError) else (
                    f"{type(exc).__name__}: {exc}"
                )
                state[filename] = {
                    "status": "failed",
                    "date": entry.get("date") if isinstance(entry, dict) else None,
                    "url": url,
                    "timestamp": _utcnow(),
                    "error": err,
                }
                dirty = True
                continue
            payload = {
                "date": date.isoformat(),
                "source_url": url,
                "layout": layout,
                "markets": markets,
            }
            os.makedirs(json_dir, exist_ok=True)
            with open(
                os.path.join(json_dir, f"{date.isoformat()}.json"),
                "w",
                encoding="utf-8",
            ) as handle:
                json.dump(payload, handle, indent=2, ensure_ascii=False)
            assert conn is not None  # noqa: S101 - guarded above
            _db.ingest_payload(conn, payload)
            n_ok += 1
            recovered.add(filename)
            dirty = True
            state[filename] = {
                "status": "ok",
                "date": date.isoformat(),
                "url": url,
                "timestamp": _utcnow(),
                "error": None,
                "layout": layout,
            }
    finally:
        if conn is not None:
            conn.close()

    # Rewrite failures.jsonl keeping only still-failing entries.
    try:
        with open(failures_path, encoding="utf-8") as handle:
            lines = handle.readlines()
    except OSError:
        lines = []
    kept: list[str] = []
    for line in lines:
        try:
            record = json.loads(line)
        except ValueError:
            kept.append(line)
            continue
        if isinstance(record, dict) and record.get("filename") in recovered:
            continue
        kept.append(line)
    if lines or recovered:
        try:
            os.makedirs(backfill_dir, exist_ok=True)
            with open(failures_path, "w", encoding="utf-8") as handle:
                handle.writelines(kept)
        except OSError:
            pass

    # Persist state only when an entry changed (recovered or re-failed).
    if dirty:
        try:
            os.makedirs(backfill_dir, exist_ok=True)
            tmp = state_path + ".tmp"
            with open(tmp, "w", encoding="utf-8") as handle:
                json.dump(state, handle, indent=2, ensure_ascii=False)
            os.replace(tmp, state_path)
        except OSError:
            pass

    print(
        "reparse-failed summary: "
        f"failed={n_failed} cached={n_cached} missing={n_missing} "
        f"ok={n_ok} still-failing={n_still_failing}"
    )
    return 0
