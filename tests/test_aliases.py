"""Commodity alias normalization: canonicalize, ingest, migrate."""

import pytest

from presyo_bantay import db
from presyo_bantay.aliases import (
    CANONICAL_ALIASES,
    canonicalize,
    group_for,
    normalize_commodities,
)
from presyo_bantay.cli import main


@pytest.mark.parametrize(
    "raw,expected",
    [
        ("Premium Rice (Loca)", "Premium Rice (Local)"),
        ("PREMIUM RICE (Imported, 5% broken)", "Premium Rice (Imported)"),
        ("PREMIUM RICE (Local, 5% broken)", "Premium Rice (Local)"),
        ("premium rice (local)", "Premium Rice (Local)"),
        ("  Bangus  ", "Bangus"),
        ("BANGUS", "Bangus"),
        ("Egg (Medium)*", "Egg (Medium)"),
        ("  tomato ", "Tomato"),
    ],
)
def test_canonicalize_merges(raw, expected):
    canonical, _spec, _unit, _category = canonicalize(raw)
    assert canonical == expected


@pytest.mark.parametrize(
    "raw",
    [
        "Fresh Pork Kasim/Pigue",
        "Fresh Pork (Kasim) Local",
        "Fresh Pork Liempo",
        "Fresh Pork Liempo (Local)",
        "Sugar (Washed)",
        "Sugar (Refined)",
    ],
)
def test_canonicalize_keeps_distinct_specs(raw):
    canonical, _spec, _unit, _category = canonicalize(raw)
    assert canonical == raw


def test_different_specs_not_merged_but_grouped():
    assert canonicalize("Fresh Pork Kasim/Pigue")[0] != canonicalize(
        "Fresh Pork (Kasim) Local"
    )[0]
    assert canonicalize("Fresh Pork Liempo")[0] != canonicalize(
        "Fresh Pork Liempo (Local)"
    )[0]
    assert canonicalize("Sugar (Washed)")[0] != canonicalize(
        "Sugar (Refined)"
    )[0]
    assert group_for("Fresh Pork Kasim/Pigue") == "pork_kasim"
    assert group_for("Fresh Pork (Kasim) Local") == "pork_kasim"
    assert group_for("Fresh Pork Liempo") == "pork_liempo"
    assert group_for("Fresh Pork Liempo (Local)") == "pork_liempo"
    assert group_for("Sugar (Washed)") == "sugar"
    assert group_for("Sugar (Refined)") == "sugar"
    assert group_for("Bangus") is None


def test_canonicalize_returns_spec_unit_category():
    assert canonicalize("Premium Rice (Loca)") == (
        "Premium Rice (Local)", "Local", "per kg", "rice",
    )
    assert canonicalize("Egg (Medium)") == (
        "Egg (Medium)", "Medium", "per piece", "egg",
    )
    assert canonicalize("Bangus")[3] == "fish"


def test_ingest_stores_canonical_names(tmp_path):
    conn = db.connect(str(tmp_path / "t.db"))
    try:
        db.ingest_payload(
            conn,
            {
                "date": "2026-10-23",
                "source_url": None,
                "markets": {
                    "M1": {
                        "Premium Rice (Loca)": {
                            "spec": "Loca", "unit": "per kg", "price": 50.0,
                        },
                        "BANGUS": {"spec": None, "unit": "per kg", "price": 200.0},
                    }
                },
            },
        )
        names = [row[0] for row in conn.execute("SELECT name FROM commodities")]
        assert "Premium Rice (Local)" in names
        assert "Bangus" in names
        assert "Premium Rice (Loca)" not in names
        assert "BANGUS" not in names
        # Canonical row carries the canonical spec, not the raw typo spec.
        spec = conn.execute(
            "SELECT spec FROM commodities WHERE name = 'Premium Rice (Local)'"
        ).fetchone()[0]
        assert spec == "Local"
    finally:
        conn.close()


def _alias_db(path):
    """DB with a canonical row, an alias row, and overlapping prices."""
    conn = db.connect(path)
    conn.execute("PRAGMA busy_timeout = 30000")
    market = conn.execute(
        "INSERT INTO markets (name, city) VALUES (?, ?)", ("M1", None)
    ).lastrowid
    canon = conn.execute(
        "INSERT INTO commodities (name, category, spec, unit)"
        " VALUES (?, ?, ?, ?)",
        ("Premium Rice (Local)", "rice", "Local", "per kg"),
    ).lastrowid
    alias = conn.execute(
        "INSERT INTO commodities (name, category, spec, unit)"
        " VALUES (?, ?, ?, ?)",
        ("Premium Rice (Loca)", "rice", "Loca", "per kg"),
    ).lastrowid
    # Same date+market: canonical NULL, alias has a price -> alias wins.
    conn.execute(
        "INSERT INTO prices (date, market_id, commodity_id, price)"
        " VALUES (?, ?, ?, ?)",
        ("2026-10-01", market, canon, None),
    )
    conn.execute(
        "INSERT INTO prices (date, market_id, commodity_id, price)"
        " VALUES (?, ?, ?, ?)",
        ("2026-10-01", market, alias, 50.0),
    )
    # Same date+market: canonical non-NULL wins over alias.
    conn.execute(
        "INSERT INTO prices (date, market_id, commodity_id, price)"
        " VALUES (?, ?, ?, ?)",
        ("2026-10-02", market, canon, 52.0),
    )
    conn.execute(
        "INSERT INTO prices (date, market_id, commodity_id, price)"
        " VALUES (?, ?, ?, ?)",
        ("2026-10-02", market, alias, 60.0),
    )
    # Alias-only date+market: row moves to the canonical id.
    conn.execute(
        "INSERT INTO prices (date, market_id, commodity_id, price)"
        " VALUES (?, ?, ?, ?)",
        ("2026-10-03", market, alias, 51.0),
    )
    conn.commit()
    return conn


def test_normalize_merge_correctness(tmp_path):
    conn = _alias_db(str(tmp_path / "t.db"))
    try:
        merges = normalize_commodities(conn)
        assert merges == [("Premium Rice (Loca)", "Premium Rice (Local)", 3)]
        names = [row[0] for row in conn.execute("SELECT name FROM commodities")]
        assert names == ["Premium Rice (Local)"]
        rows = conn.execute(
            "SELECT p.date, p.price FROM prices p"
            " JOIN commodities c ON c.id = p.commodity_id"
            " WHERE c.name = 'Premium Rice (Local)' ORDER BY p.date"
        ).fetchall()
        assert [(day, price) for day, price in rows] == [
            ("2026-10-01", 50.0),  # alias filled the NULL
            ("2026-10-02", 52.0),  # existing non-null kept
            ("2026-10-03", 51.0),  # alias-only row moved
        ]
    finally:
        conn.close()


def test_normalize_idempotent(tmp_path):
    conn = _alias_db(str(tmp_path / "t.db"))
    try:
        assert normalize_commodities(conn) != []
        assert normalize_commodities(conn) == []
        assert conn.execute("SELECT COUNT(*) FROM prices").fetchone()[0] == 3
    finally:
        conn.close()


def test_normalize_dry_run_writes_nothing(tmp_path, capsys):
    db_path = str(tmp_path / "t.db")
    conn = _alias_db(db_path)
    conn.close()
    rc = main(["normalize-commodities", "--db", db_path, "--dry-run"])
    assert rc == 0
    assert "dry run" in capsys.readouterr().out.lower()
    conn = db.connect(db_path)
    try:
        names = [row[0] for row in conn.execute("SELECT name FROM commodities")]
        assert "Premium Rice (Loca)" in names
        assert conn.execute("SELECT COUNT(*) FROM prices").fetchone()[0] == 5
    finally:
        conn.close()


def test_normalize_cli_nothing_to_merge(tmp_path, capsys):
    db_path = str(tmp_path / "t.db")
    conn = db.connect(db_path)
    conn.close()
    assert main(["normalize-commodities", "--db", db_path]) == 0
    assert "nothing to merge" in capsys.readouterr().out


def test_alias_map_covers_known_db_typos():
    canonicals = set(CANONICAL_ALIASES)
    assert "Premium Rice (Local)" in canonicals
    assert "Premium Rice (Imported)" in canonicals
