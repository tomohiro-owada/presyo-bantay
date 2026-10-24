"""Dump Python reference rows for the Go parity harness (read-only).

Usage:
    .venv/bin/python go/tools/parity/ref_dump.py data/raw /workspace/presyo-scratch/ref.jsonl

For each ``*.pdf`` in the raw dir (sorted), runs
``presyo_bantay.pdfparse.parse_pdf_with_layout`` plus the ``cmd_parse``
date rule (``parse_date_from_filename(pdf) or text_date``) and writes one
JSON object per line:

- success: ``{file, layout, date, market, commodity, spec, unit, price,
  price_low, price_high}`` — one row per (market, commodity), sorted by
  (market, commodity); ``date`` is ISO or null.
- failure (Python raised): ``{file, error}``.

Floats are written as-is; the Go harness rounds both sides to 2 decimals
before comparing.
"""

from __future__ import annotations

import glob
import json
import os
import sys


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(f"usage: {argv[0]} RAW_DIR OUT_JSONL", file=sys.stderr)
        return 1
    raw_dir, out_path = argv[1], argv[2]
    # Import here so usage errors surface before the heavy import.
    from presyo_bantay.pdfparse import (
        parse_date_from_filename,
        parse_pdf_with_layout,
    )

    pdfs = sorted(glob.glob(os.path.join(raw_dir, "*.pdf")))
    n_rows = n_err = 0
    with open(out_path, "w", encoding="utf-8") as out:
        for pdf in pdfs:
            fname = os.path.basename(pdf)
            try:
                markets, text_date, layout = parse_pdf_with_layout(pdf)
                date = parse_date_from_filename(pdf) or text_date
                date_s = date.isoformat() if date is not None else None
                rows = []
                for market in sorted(markets):
                    items = markets[market] or {}
                    for commodity in sorted(items):
                        e = items[commodity] or {}
                        rows.append(
                            {
                                "file": fname,
                                "layout": layout,
                                "date": date_s,
                                "market": market,
                                "commodity": commodity,
                                "spec": e.get("spec"),
                                "unit": e.get("unit"),
                                "price": e.get("price"),
                                "price_low": e.get("price_low"),
                                "price_high": e.get("price_high"),
                            }
                        )
                for row in rows:
                    out.write(json.dumps(row, ensure_ascii=False) + "\n")
                n_rows += len(rows)
            except Exception as exc:  # noqa: BLE001 - reference must not crash
                out.write(
                    json.dumps({"file": fname, "error": f"{type(exc).__name__}: {exc}"})
                    + "\n"
                )
                n_err += 1
    print(f"wrote {out_path}: {len(pdfs)} pdfs, {n_rows} rows, {n_err} errors")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
