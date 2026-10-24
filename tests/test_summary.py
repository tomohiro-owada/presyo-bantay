"""Weekly summary computation tests using the self-contained synthetic dir."""

import json
import os

import pytest

from presyo_bantay.summary import (
    KEY_ITEMS,
    abs_change,
    build_summary,
    compute_cheapest,
    load_snapshots,
    pct_change,
    select_snapshots,
)

from .conftest import SYNTHETIC_DIR, SYNTHETIC_EARLY, SYNTHETIC_LATE

LAS_PINAS = "New Las Piñas City Public Market"
ALABANG = "Alabang Central Market"
LA_HUERTA = "La Huerta Market/Parañaque"
BICUTAN = "Bicutan Market"


def _load(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


@pytest.fixture()
def early():
    return _load(SYNTHETIC_EARLY)


@pytest.fixture()
def late():
    return _load(SYNTHETIC_LATE)


def test_synthetic_dir_has_two_snapshots():
    snapshots = load_snapshots(SYNTHETIC_DIR)
    assert [p["date"] for p in snapshots] == ["2026-10-16", "2026-10-23"]
    assert snapshots[0].get("synthetic") is True
    assert "SYNTHETIC TEST DATA" in snapshots[0].get("note", "")
    assert snapshots[0].get("source_url") is None


def test_baseline_selection_week_ending():
    snapshots = load_snapshots(SYNTHETIC_DIR)
    latest, baseline = select_snapshots(snapshots, "2026-10-24")
    assert latest["date"] == "2026-10-23"
    assert baseline["date"] == "2026-10-16"  # exactly 7 days earlier


def test_baseline_selection_none_when_single_snapshot():
    snapshots = load_snapshots(SYNTHETIC_DIR)
    latest, baseline = select_snapshots(snapshots, "2026-10-16")
    assert latest["date"] == "2026-10-16"
    assert baseline is None


def test_baseline_selection_window():
    def snap(date):
        return {"date": date, "markets": {}}

    snapshots = [
        snap("2026-10-12"),  # 11 days earlier: outside the 5..10 window
        snap("2026-10-13"),  # 10 days: inside
        snap("2026-10-18"),  # 5 days: inside
        snap("2026-10-23"),
    ]
    latest, baseline = select_snapshots(snapshots, "2026-10-23")
    assert latest["date"] == "2026-10-23"
    # Closest to latest-7d (2026-10-16) wins: 10-13 is 3 days off, 10-18 is 2.
    assert baseline["date"] == "2026-10-18"

    only_old = [snap("2026-10-12"), snap("2026-10-23")]
    _, baseline = select_snapshots(only_old, "2026-10-23")
    assert baseline is None

    assert select_snapshots([], "2026-10-23") == (None, None)
    # Nothing on or before the week-ending date.
    assert select_snapshots(snapshots, "2026-10-01") == (None, None)


def test_change_calc_exact_values(early, late):
    summary = build_summary(late, early, "2026-10-24")
    # Las Piñas Bangus: synthetic baseline 240.0, real latest 270.0.
    assert summary.latest["Bangus"][LAS_PINAS] == pytest.approx(270.0)
    assert summary.baseline["Bangus"][LAS_PINAS] == pytest.approx(240.0)
    assert abs_change(270.0, 240.0) == pytest.approx(30.0)
    assert pct_change(270.0, 240.0) == pytest.approx(12.5)
    # Las Piñas Tomato: 90.0 vs 100.0 -> -10.0%.
    assert pct_change(
        summary.latest["Tomato"][LAS_PINAS],
        summary.baseline["Tomato"][LAS_PINAS],
    ) == pytest.approx(-10.0)
    # Unchanged items -> 0.0%.
    assert pct_change(
        summary.latest["Premium Rice (Imported)"][LAS_PINAS],
        summary.baseline["Premium Rice (Imported)"][LAS_PINAS],
    ) == pytest.approx(0.0)
    assert pct_change(8.5, 8.5) == pytest.approx(0.0)
    # NULL handling.
    assert pct_change(None, 240.0) is None
    assert pct_change(80.0, None) is None
    assert pct_change(10.0, 0) is None
    assert abs_change(None, 1.0) is None
    # Alabang Tomato is NULL in the baseline week but 80.0 latest.
    assert summary.baseline["Tomato"][ALABANG] is None
    assert summary.latest["Tomato"][ALABANG] == pytest.approx(80.0)


def test_markets_order_las_pinas_first(early, late):
    summary = build_summary(late, early, "2026-10-24")
    assert summary.markets[0] == LAS_PINAS
    assert summary.markets == [LAS_PINAS, ALABANG, LA_HUERTA, BICUTAN]
    assert summary.items == KEY_ITEMS


def test_cheapest_market_from_fixtures(early, late):
    summary = build_summary(late, early, "2026-10-24")
    cheapest = {c.item: c for c in summary.cheapest}
    # Bangus latest: Las Piñas 270, Alabang 240, La Huerta 260, Bicutan 250.
    assert cheapest["Bangus"].price == pytest.approx(240.0)
    assert cheapest["Bangus"].markets == [ALABANG]


def test_cheapest_ties_and_nulls():
    markets = [LAS_PINAS, ALABANG, LA_HUERTA]
    latest = {
        "Bangus": {LAS_PINAS: 240.0, ALABANG: 240.0, LA_HUERTA: 260.0},
        "Tomato": {LAS_PINAS: None, ALABANG: None, LA_HUERTA: None},
    }
    results = compute_cheapest(["Bangus", "Tomato"], markets, latest)
    by_item = {r.item: r for r in results}
    assert by_item["Bangus"].price == pytest.approx(240.0)
    assert by_item["Bangus"].markets == [LAS_PINAS, ALABANG]  # tie listed
    assert by_item["Tomato"].price is None  # NULLs skipped
    assert by_item["Tomato"].markets == []


def test_notable_moves_sorted_capped(early, late):
    summary = build_summary(late, early, "2026-10-24")
    assert summary.has_changes
    assert len(summary.moves) <= 6
    mags = [abs(m.pct_change) for m in summary.moves]
    assert mags == sorted(mags, reverse=True)
    assert all(m >= 5.0 for m in mags)
    assert mags, "expected notable moves between the fixtures"
    # Spot-check a known move is present.
    bangus_moves = [
        m for m in summary.moves
        if m.item == "Bangus" and m.market == LAS_PINAS
    ]
    assert bangus_moves and bangus_moves[0].pct_change == pytest.approx(12.5)


def test_no_baseline_has_no_moves(early):
    summary = build_summary(early, None, "2026-10-16")
    assert not summary.has_changes
    assert summary.baseline_date is None
    assert summary.moves == []
    assert summary.synthetic is True


def test_synthetic_flag_when_baseline_is_synthetic(early, late):
    """Warning shows when EITHER week is synthetic (real latest here)."""
    assert late.get("synthetic") is not True
    assert early.get("synthetic") is True
    summary = build_summary(late, early, "2026-10-24")
    assert summary.synthetic is True


def test_summarize_data_dir_warns_on_synthetic_baseline(capsys):
    """Exact reported case: real latest + synthetic baseline still warns."""
    from presyo_bantay.cli import main
    from presyo_bantay.render import SYNTHETIC_LINE

    rc = main(
        ["summarize", "--week-ending", "2026-10-24", "--data-dir", SYNTHETIC_DIR]
    )
    assert rc == 0
    assert SYNTHETIC_LINE in capsys.readouterr().out


def test_summarize_db_path_warns_on_synthetic_baseline(tmp_path, capsys):
    """The DB path also knows: synthetic dates are tracked on ingest."""
    from presyo_bantay import db as _db
    from presyo_bantay.cli import main
    from presyo_bantay.render import SYNTHETIC_LINE

    db_path = str(tmp_path / "synth.db")
    conn = _db.connect(db_path)
    try:
        for path in (SYNTHETIC_EARLY, SYNTHETIC_LATE):
            _db.ingest_file(conn, path, allow_synthetic=True)
        assert _db.snapshot_for_date(conn, "2026-10-16")["synthetic"] is True
        assert _db.snapshot_for_date(conn, "2026-10-23")["synthetic"] is False
    finally:
        conn.close()
    capsys.readouterr()
    rc = main(["summarize", "--week-ending", "2026-10-24", "--db", db_path])
    assert rc == 0
    assert SYNTHETIC_LINE in capsys.readouterr().out
