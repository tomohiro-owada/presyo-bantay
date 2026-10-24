"""SQLite store tests: idempotent ingest, NULLs, synthetic refusal, trends."""

import json
import os

import pytest

from presyo_bantay import db
from presyo_bantay.cli import main

from .conftest import SYNTHETIC_DIR, SYNTHETIC_EARLY, SYNTHETIC_LATE

REAL_JSON = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    "data",
    "json",
    "2026-10-23.json",
)

LAS_PINAS = "New Las Piñas City Public Market"


def _row_count(conn):
    return conn.execute("SELECT COUNT(*) FROM prices").fetchone()[0]


def test_schema_file_exists():
    assert os.path.exists(db.SCHEMA_PATH)
    with open(db.SCHEMA_PATH, encoding="utf-8") as handle:
        assert "CREATE TABLE IF NOT EXISTS prices" in handle.read()


def test_city_derivation():
    assert db.derive_city("New Las Piñas City Public Market") == "Las Piñas"
    assert db.derive_city("Alabang Central Market") == "Muntinlupa"
    assert db.derive_city("La Huerta Market/Parañaque") == "Parañaque"
    assert db.derive_city("Bicutan Market") == "Taguig"
    assert db.derive_city("Agora Public Market/San Juan") == "San Juan"
    assert db.derive_city("Balintawak (Cloverleaf) Market") is None


def test_commodity_categories():
    assert db.categorize_commodity("Premium Rice (Imported)") == "rice"
    assert db.categorize_commodity("Well-milled Rice (Local)") == "rice"
    assert db.categorize_commodity("Regular Milled (Local)") == "rice"
    assert db.categorize_commodity("Bangus") == "fish"
    assert db.categorize_commodity("Egg (Medium)") == "egg"
    assert db.categorize_commodity("Eggplant") == "vegetables"
    assert db.categorize_commodity("Tomato") == "vegetables"
    assert db.categorize_commodity("Red Onion (Local)") == "spices"
    assert db.categorize_commodity("Sugar (Refined)") == "sugar"
    assert db.categorize_commodity("Fresh Pork (Kasim) Local") == "pork"
    assert db.categorize_commodity("Fresh Whole Chicken") == "poultry"


def test_idempotent_ingest_overwrites(tmp_path):
    db_path = str(tmp_path / "test.db")
    conn = db.connect(db_path)
    try:
        n1 = db.ingest_file(conn, REAL_JSON)
        assert n1[3] is False
        count1 = _row_count(conn)
        assert count1 > 0
        # Ingest twice -> same row count.
        db.ingest_file(conn, REAL_JSON)
        assert _row_count(conn) == count1
        # Updated price overwrites instead of duplicating.
        with open(REAL_JSON, encoding="utf-8") as handle:
            payload = json.load(handle)
        payload["markets"][LAS_PINAS]["Bangus"]["price"] = 999.0
        db.ingest_payload(conn, payload)
        assert _row_count(conn) == count1
        series = db.price_series(conn, LAS_PINAS, "Bangus")
        assert series == [("2026-10-23", 999.0)]
    finally:
        conn.close()


def test_null_price_stored_for_not_available(tmp_path):
    db_path = str(tmp_path / "test.db")
    conn = db.connect(db_path)
    try:
        db.ingest_file(conn, REAL_JSON)
        row = conn.execute(
            "SELECT p.price FROM prices p"
            " JOIN markets m ON m.id = p.market_id"
            " JOIN commodities c ON c.id = p.commodity_id"
            " WHERE m.name = ? AND c.name = ? AND p.date = ?",
            ("La Huerta Market/Parañaque", "Well-milled Rice (Imported)", "2026-10-23"),
        ).fetchone()
        assert row is not None and row[0] is None
        # Cities and categories are stored.
        city = conn.execute(
            "SELECT city FROM markets WHERE name = ?", (LAS_PINAS,)
        ).fetchone()[0]
        assert city == "Las Piñas"
        cat = conn.execute(
            "SELECT category FROM commodities WHERE name = ?", ("Bangus",)
        ).fetchone()[0]
        assert cat == "fish"
    finally:
        conn.close()


def test_synthetic_refused_by_default(tmp_path, capsys):
    db_path = str(tmp_path / "test.db")
    rc = main(["ingest", SYNTHETIC_EARLY, "--db", db_path])
    assert rc == 0
    err = capsys.readouterr().err
    assert "synthetic" in err.lower()
    conn = db.connect(db_path)
    try:
        assert _row_count(conn) == 0
    finally:
        conn.close()
    # ... unless --allow-synthetic is given.
    rc = main(["ingest", SYNTHETIC_EARLY, "--db", db_path, "--allow-synthetic"])
    assert rc == 0
    conn = db.connect(db_path)
    try:
        assert _row_count(conn) > 0
    finally:
        conn.close()


def test_trend_series_from_two_fixtures(tmp_path, capsys):
    db_path = str(tmp_path / "trend.db")
    assert main(["ingest", SYNTHETIC_DIR, "--db", db_path, "--allow-synthetic"]) == 0
    capsys.readouterr()
    rc = main(
        ["trend", "--market", "Las Piñas", "--item", "Bangus",
         "--db", db_path, "--json"]
    )
    assert rc == 0
    out = json.loads(capsys.readouterr().out)
    assert out["series"] == [["2026-10-16", 240.0], ["2026-10-23", 270.0]]
    assert out["stats"]["latest"] == pytest.approx(270.0)
    assert out["stats"]["min"] == pytest.approx(240.0)
    assert out["stats"]["max"] == pytest.approx(270.0)
    assert out["stats"]["change"] == pytest.approx(30.0)
    assert out["stats"]["change_pct"] == pytest.approx(12.5)


def test_trend_text_output(tmp_path, capsys):
    db_path = str(tmp_path / "trend.db")
    assert main(["ingest", SYNTHETIC_DIR, "--db", db_path, "--allow-synthetic"]) == 0
    capsys.readouterr()
    rc = main(["trend", "--market", "las piñas", "--item", "bangus", "--db", db_path])
    assert rc == 0
    out = capsys.readouterr().out
    assert "2026-10-16" in out and "2026-10-23" in out
    assert "₱270.00" in out


def test_trend_fuzzy_ambiguity_and_none(tmp_path, capsys):
    db_path = str(tmp_path / "trend.db")
    assert main(["ingest", SYNTHETIC_DIR, "--db", db_path, "--allow-synthetic"]) == 0
    capsys.readouterr()
    # 'Market' matches many markets -> ambiguity error listing candidates.
    rc = main(["trend", "--market", "Market", "--item", "Bangus", "--db", db_path])
    assert rc == 1
    assert "Las Piñas" in capsys.readouterr().err
    # 'Rice' matches many commodities -> ambiguity error.
    rc = main(["trend", "--market", "Bicutan", "--item", "Rice", "--db", db_path])
    assert rc == 1
    assert "Premium Rice" in capsys.readouterr().err
    # No match at all -> error.
    rc = main(
        ["trend", "--market", "NoSuchMarketXYZ", "--item", "Bangus", "--db", db_path]
    )
    assert rc == 1
    assert "no market matches" in capsys.readouterr().err.lower()


def test_trend_days_window(tmp_path, capsys):
    db_path = str(tmp_path / "trend.db")
    assert main(["ingest", SYNTHETIC_DIR, "--db", db_path, "--allow-synthetic"]) == 0
    capsys.readouterr()
    rc = main(
        ["trend", "--market", "Bicutan", "--item", "Galunggong (Local)",
         "--db", db_path, "--days", "3", "--json"]
    )
    assert rc == 0
    out = json.loads(capsys.readouterr().out)
    # Only the latest date falls inside the 3-day window.
    assert out["series"] == [["2026-10-23", 200.0]]


def test_ingest_directory_counts(tmp_path, capsys):
    db_path = str(tmp_path / "counts.db")
    rc = main(["ingest", SYNTHETIC_DIR, "--db", db_path, "--allow-synthetic"])
    assert rc == 0
    assert "ingested 2 file(s)" in capsys.readouterr().out
