"""Range-layout (pre-2026-08-18) parser, migration and reparse tests (offline).

The expected values below were read from the ``extract_text()`` lines of
the August 14 2026 fixture::

    New Las Piñas City Public Market 50.00 170.00 300.00-320.00 8.00-8.50
    New Las Piñas City Public Market 155.00-165.00 170.00-180.00 110.00 - 120.00 110.00 -120.00 80.00 - 90.00
    New Las Piñas City Public Market 78.00 -82.00 305.00 - 310.00 350.00 180.00
    Alabang Central Market 48.00-50.00 140.00-160.00 NOT AVAILABLE 7.00-8.50
"""

import json
import os
import shutil
import sqlite3

import pytest

from presyo_bantay import db as _db
from presyo_bantay.backfill import run_backfill_checks
from presyo_bantay.cli import main
from presyo_bantay.pdfparse import (
    detect_layout,
    infer_layout,
    parse_pdf,
    parse_pdf_with_layout,
    parse_range_price,
)
from presyo_bantay.sanity import LayoutChangedError, run_sanity_checks

from .conftest import FIXTURES_DIR, SAMPLE_PDF

RANGE_PDF = os.path.join(FIXTURES_DIR, "Price-Monitoring-August-14-2026.pdf")
LAS_PINAS = "New Las Piñas City Public Market"
ALABANG = "Alabang Central Market"


def test_range_fixture_exists():
    assert os.path.exists(RANGE_PDF)


def test_range_las_pinas_page1_exact():
    # extract_text(): "New Las Piñas City Public Market 50.00 170.00 300.00-320.00 8.00-8.50"
    markets, _ = parse_pdf(RANGE_PDF)
    entry = markets[LAS_PINAS]
    rice = entry["Well-milled Rice (Local)"]
    assert rice["price"] == pytest.approx(50.00)
    assert rice["price_low"] == pytest.approx(50.00)
    assert rice["price_high"] == pytest.approx(50.00)
    tilapia = entry["Tilapia"]
    assert tilapia["price"] == pytest.approx(170.00)
    assert tilapia["price_low"] == pytest.approx(170.00)
    assert tilapia["price_high"] == pytest.approx(170.00)
    galunggong = entry["Galunggong (Local)"]
    assert galunggong["price_low"] == pytest.approx(300.00)
    assert galunggong["price_high"] == pytest.approx(320.00)
    assert galunggong["price"] == pytest.approx(310.00)  # midpoint
    egg = entry["Egg (Medium)"]
    assert egg["price_low"] == pytest.approx(8.00)
    assert egg["price_high"] == pytest.approx(8.50)
    assert egg["price"] == pytest.approx(8.25)  # midpoint
    assert egg["unit"] == "per piece"


def test_range_las_pinas_page2_exact():
    # extract_text(): "New Las Piñas City Public Market 155.00-165.00 170.00-180.00
    #   110.00 - 120.00 110.00 -120.00 80.00 - 90.00"
    markets, _ = parse_pdf(RANGE_PDF)
    entry = markets[LAS_PINAS]
    assert entry["Ampalaya"]["price"] == pytest.approx(160.00)
    assert (entry["Ampalaya"]["price_low"], entry["Ampalaya"]["price_high"]) == (
        pytest.approx(155.00),
        pytest.approx(165.00),
    )
    assert entry["Eggplant"]["price"] == pytest.approx(175.00)
    assert entry["Tomato"]["price"] == pytest.approx(115.00)
    assert (entry["Tomato"]["price_low"], entry["Tomato"]["price_high"]) == (
        pytest.approx(110.00),
        pytest.approx(120.00),
    )
    assert entry["Cabbage"]["price"] == pytest.approx(115.00)
    assert entry["Red Onion (Local)"]["price"] == pytest.approx(85.00)


def test_range_las_pinas_page3_exact():
    # extract_text(): "New Las Piñas City Public Market 78.00 -82.00 305.00 - 310.00 350.00 180.00"
    markets, _ = parse_pdf(RANGE_PDF)
    entry = markets[LAS_PINAS]
    assert entry["Sugar (Washed)"]["price"] == pytest.approx(80.00)
    assert (entry["Sugar (Washed)"]["price_low"], entry["Sugar (Washed)"]["price_high"]) == (
        pytest.approx(78.00),
        pytest.approx(82.00),
    )
    assert entry["Fresh Pork Kasim/Pigue"]["price"] == pytest.approx(307.50)
    assert entry["Fresh Pork Liempo"]["price"] == pytest.approx(350.00)
    assert entry["Fresh Whole Chicken"]["price"] == pytest.approx(180.00)
    assert len(entry) == 13  # 4 + 5 + 4 commodities


def test_range_not_available_becomes_nulls():
    # extract_text(): "Alabang Central Market 48.00-50.00 140.00-160.00 NOT AVAILABLE 7.00-8.50"
    markets, _ = parse_pdf(RANGE_PDF)
    galunggong = markets[ALABANG]["Galunggong (Local)"]
    assert galunggong["price"] is None
    assert galunggong["price_low"] is None
    assert galunggong["price_high"] is None


def test_range_split_tomato_cells_reassembled():
    # extract_tables() splits some Tomato ranges across the spurious None
    # column (e.g. "75.00" + "- 85.00"); they must reassemble.
    # extract_text(): "Balintawak (Cloverleaf) Market 110.00-150.00 140.00-170.00
    #   75.00 - 85.00 40.00 -80.00 75.00 - 80.00"
    markets, _ = parse_pdf(RANGE_PDF)
    tomato = markets["Balintawak (Cloverleaf) Market"]["Tomato"]
    assert tomato["price_low"] == pytest.approx(75.00)
    assert tomato["price_high"] == pytest.approx(85.00)
    assert tomato["price"] == pytest.approx(80.00)


def test_range_only_egg_medium_is_per_piece():
    markets, _ = parse_pdf(RANGE_PDF)
    per_piece = {
        commodity
        for items in markets.values()
        for commodity, details in items.items()
        if details["unit"] == "per piece"
    }
    assert per_piece == {"Egg (Medium)"}


def test_parse_range_price_cases():
    assert parse_range_price("45.00") == (45.00, 45.00, 45.00)
    assert parse_range_price("150.00-160.00") == (155.00, 150.00, 160.00)
    assert parse_range_price("300.00 - 350.00") == (325.00, 300.00, 350.00)
    assert parse_range_price("45.00 -50.00") == (47.50, 45.00, 50.00)
    assert parse_range_price("100.00 -100.00") == (100.00, 100.00, 100.00)
    assert parse_range_price("NOT AVAILABLE") == (None, None, None)
    assert parse_range_price("") == (None, None, None)
    assert parse_range_price(None) == (None, None, None)


def test_layout_detection_both_fixtures():
    assert detect_layout(RANGE_PDF) == "range"
    assert detect_layout(SAMPLE_PDF) == "single"
    range_markets, _, range_layout = parse_pdf_with_layout(RANGE_PDF)
    assert range_layout == "range"
    single_markets, _, single_layout = parse_pdf_with_layout(SAMPLE_PDF)
    assert single_layout == "single"
    assert infer_layout(range_markets) == "range"
    assert infer_layout(single_markets) == "single"
    with pytest.raises(LayoutChangedError):
        infer_layout({})


def test_single_layout_entries_carry_low_high():
    markets, _ = parse_pdf(SAMPLE_PDF)
    bangus = markets[LAS_PINAS]["Bangus"]
    assert bangus["price"] == pytest.approx(270.0)
    assert bangus["price_low"] == pytest.approx(270.0)
    assert bangus["price_high"] == pytest.approx(270.0)


def test_parse_command_writes_layout(tmp_path):
    out_dir = str(tmp_path / "json")
    assert main(["parse", RANGE_PDF, "--out-dir", out_dir]) == 0
    with open(os.path.join(out_dir, "2026-08-14.json"), encoding="utf-8") as handle:
        payload = json.load(handle)
    assert payload["layout"] == "range"
    assert payload["markets"][LAS_PINAS]["Egg (Medium)"]["price"] == pytest.approx(8.25)
    out_single = str(tmp_path / "json-single")
    assert main(["parse", SAMPLE_PDF, "--out-dir", out_single]) == 0
    with open(os.path.join(out_single, "2026-10-23.json"), encoding="utf-8") as handle:
        single = json.load(handle)
    assert single["layout"] == "single"


def test_range_sanity_passes():
    markets, _ = parse_pdf(RANGE_PDF)
    run_sanity_checks(markets, "range")  # must not raise
    assert len(markets) >= 20


def test_range_sanity_failures():
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks({}, "range")
    thin = {"Some Market": {"Egg (Medium)": {"price": 1.0}}}
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks(thin, "range")
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks({"m": {"Egg (Medium)": {}}}, "weird-layout")


def test_single_sanity_rejects_range_markets_without_layout():
    # Default (single) thresholds must still reject the 13-commodity layout.
    markets, _ = parse_pdf(RANGE_PDF)
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks(markets)


def test_backfill_checks_range_layout():
    markets, _ = parse_pdf(RANGE_PDF)
    warnings = run_backfill_checks(markets, "range")  # must not raise
    assert isinstance(warnings, list)
    # Old range PDFs have no La Huerta market: warning, not failure.
    assert any("Huerta" in w for w in warnings)
    # ... but the strict single-layout core check rejects range markets.
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_backfill_checks(markets)


def _make_old_schema_db(path: str) -> None:
    """Create a DB with the pre-range schema (no price_low/price_high)."""
    if os.path.exists(path):
        os.remove(path)
    conn = sqlite3.connect(path)
    conn.execute(
        "CREATE TABLE markets (id INTEGER PRIMARY KEY,"
        " name TEXT NOT NULL UNIQUE, city TEXT)"
    )
    conn.execute(
        "CREATE TABLE commodities (id INTEGER PRIMARY KEY,"
        " name TEXT NOT NULL UNIQUE, category TEXT, spec TEXT, unit TEXT)"
    )
    conn.execute(
        "CREATE TABLE prices (date TEXT NOT NULL,"
        " market_id INTEGER NOT NULL REFERENCES markets(id),"
        " commodity_id INTEGER NOT NULL REFERENCES commodities(id),"
        " price REAL NULL, source_url TEXT,"
        " UNIQUE(date, market_id, commodity_id))"
    )
    conn.execute("INSERT INTO markets (name, city) VALUES (?, ?)", ("M", "C"))
    conn.execute(
        "INSERT INTO commodities (name, category, spec, unit)"
        " VALUES (?, ?, ?, ?)",
        ("Bangus", "fish", None, "per kg"),
    )
    conn.execute(
        "INSERT INTO prices (date, market_id, commodity_id, price, source_url)"
        " VALUES (?, ?, ?, ?, ?)",
        ("2026-01-01", 1, 1, 200.0, None),
    )
    conn.commit()
    conn.close()


def test_db_migrates_old_schema_and_stores_ranges(tmp_path):
    db_path = str(tmp_path / "old.db")
    _make_old_schema_db(db_path)
    conn = _db.connect(db_path)  # auto-migrate on open
    try:
        cols = [row[1] for row in conn.execute("PRAGMA table_info(prices)")]
        assert "price_low" in cols and "price_high" in cols
        # Old row still readable (bounds fall back to price).
        series = _db.price_range_series(conn, "M", "Bangus")
        assert series == [("2026-01-01", 200.0, 200.0, 200.0)]
        # Ingest a range payload plus an old-style payload without bounds.
        _db.ingest_payload(
            conn,
            {
                "date": "2026-08-14",
                "source_url": None,
                "layout": "range",
                "markets": {
                    LAS_PINAS: {
                        "Galunggong (Local)": {
                            "spec": "Local",
                            "unit": "per kg",
                            "price": 310.0,
                            "price_low": 300.0,
                            "price_high": 320.0,
                        }
                    }
                },
            },
        )
        _db.ingest_payload(
            conn,
            {
                "date": "2026-08-15",
                "source_url": None,
                "markets": {"M": {"Bangus": {"spec": None, "unit": "per kg", "price": 210.0}}},
            },
        )
        row = conn.execute(
            "SELECT price, price_low, price_high FROM prices"
            " JOIN markets m ON m.id = market_id"
            " JOIN commodities c ON c.id = commodity_id"
            " WHERE date = ? AND m.name = ? AND c.name = ?",
            ("2026-08-14", LAS_PINAS, "Galunggong (Local)"),
        ).fetchone()
        assert tuple(row) == (310.0, 300.0, 320.0)
        legacy = conn.execute(
            "SELECT price, price_low, price_high FROM prices WHERE date = ?",
            ("2026-08-15",),
        ).fetchone()
        assert tuple(legacy) == (210.0, 210.0, 210.0)
        snap = _db.snapshot_for_date(conn, "2026-08-14")
        assert snap["markets"][LAS_PINAS]["Galunggong (Local)"]["price_low"] == 300.0
    finally:
        conn.close()


def test_trend_text_shows_range(tmp_path, capsys):
    db_path = str(tmp_path / "trend.db")
    conn = _db.connect(db_path)
    try:
        _db.ingest_payload(
            conn,
            {
                "date": "2026-08-14",
                "source_url": None,
                "layout": "range",
                "markets": {
                    LAS_PINAS: {
                        "Galunggong (Local)": {
                            "spec": "Local",
                            "unit": "per kg",
                            "price": 310.0,
                            "price_low": 300.0,
                            "price_high": 320.0,
                        }
                    }
                },
            },
        )
    finally:
        conn.close()
    capsys.readouterr()
    assert main(["trend", "--market", "Las Piñas", "--item", "Galunggong", "--db", db_path]) == 0
    out = capsys.readouterr().out
    assert "300.00" in out and "320.00" in out
    assert "range" in out
    capsys.readouterr()
    assert (
        main(
            ["trend", "--market", "Las Piñas", "--item", "Galunggong",
             "--db", db_path, "--json"]
        )
        == 0
    )
    payload = json.loads(capsys.readouterr().out)
    assert payload["series"] == [["2026-08-14", 310.0]]


def test_reparse_failed_offline_no_network(tmp_path, monkeypatch, capsys):
    import requests

    def _boom(*args, **kwargs):
        raise AssertionError("reparse-failed must not issue HTTP requests")

    monkeypatch.setattr(requests, "get", _boom)
    monkeypatch.setattr("presyo_bantay.backfill.requests.get", _boom)

    raw_dir = tmp_path / "raw"
    json_dir = tmp_path / "json"
    bdir = tmp_path / "backfill"
    raw_dir.mkdir()
    bdir.mkdir()
    cached_name = "Price-Monitoring-August-14-2026.pdf"
    shutil.copy(RANGE_PDF, raw_dir / cached_name)
    missing_name = "Price-Monitoring-July-09-2026.pdf"
    state_path = bdir / "state.json"
    state_path.write_text(
        json.dumps(
            {
                cached_name: {
                    "status": "failed",
                    "date": "2026-08-14",
                    "url": "https://example.test/a.pdf",
                    "timestamp": "t",
                    "error": "Layout may have changed: boom",
                },
                missing_name: {
                    "status": "failed",
                    "date": "2026-07-09",
                    "url": "https://example.test/b.pdf",
                    "timestamp": "t",
                    "error": "HTTP 404",
                },
                "Price-Monitoring-October-23-2026.pdf": {
                    "status": "ok",
                    "date": "2026-10-23",
                    "url": "https://example.test/c.pdf",
                    "timestamp": "t",
                    "error": None,
                },
            }
        ),
        encoding="utf-8",
    )
    (bdir / "failures.jsonl").write_text(
        json.dumps({"filename": cached_name, "date": "2026-08-14",
                    "url": "https://example.test/a.pdf", "error": "x"})
        + "\n"
        + json.dumps({"filename": missing_name, "date": "2026-07-09",
                      "url": "https://example.test/b.pdf", "error": "y"})
        + "\n",
        encoding="utf-8",
    )
    db_path = str(tmp_path / "t.db")

    rc = main(
        ["reparse-failed", "--state", str(state_path),
         "--raw-dir", str(raw_dir), "--db", db_path,
         "--json-dir", str(json_dir)]
    )
    assert rc == 0
    out = capsys.readouterr().out
    assert "failed=2" in out and "cached=1" in out and "ok=1" in out

    # JSON written with layout, ingested into the DB.
    with open(json_dir / "2026-08-14.json", encoding="utf-8") as handle:
        payload = json.load(handle)
    assert payload["layout"] == "range"
    assert payload["markets"][LAS_PINAS]["Egg (Medium)"]["price"] == pytest.approx(8.25)
    conn = _db.connect(db_path)
    try:
        assert "2026-08-14" in _db.list_dates(conn)
    finally:
        conn.close()

    # State flipped to ok (with layout); missing file still failed.
    with open(state_path, encoding="utf-8") as handle:
        state = json.load(handle)
    assert state[cached_name]["status"] == "ok"
    assert state[cached_name]["layout"] == "range"
    assert state[missing_name]["status"] == "failed"

    # failures.jsonl rewritten keeping only still-failing entries.
    with open(bdir / "failures.jsonl", encoding="utf-8") as handle:
        remaining = handle.read()
    assert cached_name not in remaining
    assert missing_name in remaining
