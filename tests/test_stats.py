"""Pearson correlation and lagged price/FX correlation (hand-computed)."""

import pytest

from presyo_bantay import db
from presyo_bantay import fx as fx_module
from presyo_bantay.cli import main
from presyo_bantay.stats import lagged_correlation, pearson


def test_pearson_perfect_positive():
    r, n = pearson([1.0, 2.0, 3.0], [2.0, 4.0, 6.0])
    assert n == 3
    assert r == pytest.approx(1.0)


def test_pearson_perfect_negative():
    r, n = pearson([1.0, 2.0, 3.0], [6.0, 4.0, 2.0])
    assert n == 3
    assert r == pytest.approx(-1.0)


def test_pearson_known_value():
    # mean_x=3, mean_y=4; sxy=6, sxx=10, syy=6 -> 6/sqrt(60).
    r, n = pearson([1, 2, 3, 4, 5], [2, 4, 5, 4, 5])
    assert n == 5
    assert r == pytest.approx(0.7745966692)


def test_pearson_too_few_points():
    r, n = pearson([1.0, 2.0], [3.0, 4.0])
    assert (r, n) == (None, 2)


def test_pearson_zero_variance():
    r, n = pearson([5.0, 5.0, 5.0], [1.0, 2.0, 3.0])
    assert (r, n) == (None, 3)
    r, n = pearson([1.0, 2.0, 3.0], [7.0, 7.0, 7.0])
    assert (r, n) == (None, 3)


def test_lagged_weekend_asof_pairs_with_friday():
    # ECB skips weekends: Sat/Sun prices pair with Friday's rate.
    fx = [("2026-10-02", 60.0), ("2026-10-05", 62.0)]  # Fri, Mon
    prices = [
        ("2026-10-03", 120.0),  # Sat -> Fri 60
        ("2026-10-04", 120.0),  # Sun -> Fri 60
        ("2026-10-05", 124.0),  # Mon -> Mon 62
        ("2026-10-06", 124.0),  # Tue -> Mon 62
    ]
    result = lagged_correlation(prices, fx, 0)
    assert result == {"lag": 0, "r": pytest.approx(1.0), "n": 4}


def test_lagged_correlation_applies_lag():
    fx = [("2026-10-01", 60.0), ("2026-10-02", 70.0)]
    prices = [
        ("2026-10-08", 600.0),  # lag 7 -> 10-01 -> 60
        ("2026-10-09", 700.0),  # lag 7 -> 10-02 -> 70
        ("2026-10-10", 800.0),  # lag 7 -> 10-03 -> as-of 10-02 -> 70
    ]
    result = lagged_correlation(prices, fx, 7)
    assert result["lag"] == 7
    assert result["n"] == 3
    assert result["r"] == pytest.approx(0.8660254)


def test_lagged_correlation_skips_stale_fx():
    fx = [("2026-10-01", 60.0)]
    prices = [("2026-10-10", 600.0), ("2026-10-11", 610.0)]  # 9-10 days stale
    result = lagged_correlation(prices, fx, 0)
    assert result == {"lag": 0, "r": None, "n": 0}


def _price_db(path):
    conn = db.connect(path)
    market = conn.execute(
        "INSERT INTO markets (name, city) VALUES (?, ?)",
        ("New Las Piñas City Public Market", "Las Piñas"),
    ).lastrowid
    item = conn.execute(
        "INSERT INTO commodities (name, category, spec, unit)"
        " VALUES (?, ?, ?, ?)",
        ("Bangus", "fish", None, "per kg"),
    ).lastrowid
    prices = [100.0, 110.0, 120.0, 130.0, 140.0]
    for day, price in zip(
        ["2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"],
        prices,
    ):
        conn.execute(
            "INSERT INTO prices (date, market_id, commodity_id, price)"
            " VALUES (?, ?, ?, ?)",
            (day, market, item, price),
        )
    fx_module.upsert_rates(
        conn,
        {
            "2026-10-12": 62.0,
            "2026-10-13": 63.0,
            "2026-10-14": 64.0,
            "2026-10-15": 65.0,
            "2026-10-16": 66.0,
        },
    )
    conn.commit()
    return conn


def test_correlate_command_prints_r_and_n(tmp_path, capsys):
    db_path = str(tmp_path / "t.db")
    conn = _price_db(db_path)
    conn.close()
    rc = main(
        ["correlate", "--market", "las piñas", "--item", "bang",
         "--db", db_path, "--days", "all", "--lags", "0,7"]
    )
    assert rc == 0
    out = capsys.readouterr().out
    assert "lag 0d: r=+1.000 (n=5)" in out
    assert "lag 7d:" in out and "(n=" in out
    assert "correlation is not causation" in out


def test_correlate_fuzzy_match_errors(tmp_path, capsys):
    db_path = str(tmp_path / "t.db")
    conn = _price_db(db_path)
    conn.close()
    assert main(["correlate", "--market", "Nope", "--item", "Bangus",
                 "--db", db_path]) == 1
    assert "no market matches" in capsys.readouterr().err.lower()
    assert main(["correlate", "--market", "Las Piñas", "--item", "Nope",
                 "--db", db_path]) == 1
    assert "no commodity matches" in capsys.readouterr().err.lower()
