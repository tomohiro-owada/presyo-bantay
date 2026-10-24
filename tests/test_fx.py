"""USD/PHP FX rates: storage, as-of lookup, Frankfurter update (mocked HTTP)."""

import json
import os

import pytest

from presyo_bantay import db
from presyo_bantay import fx as fx_module
from presyo_bantay.cli import main

from .conftest import FIXTURES_DIR

FIXTURE_PATH = os.path.join(
    FIXTURES_DIR, "fx", "frankfurter-usd-php-2026-10.json"
)

RATES = {
    "2026-10-01": 62.799,
    "2026-10-02": 62.597,
    "2026-10-05": 62.673,  # Monday: weekend has no rows
}


def _seed(conn, rates=None):
    fx_module.upsert_rates(conn, rates or RATES)


def test_fx_table_created_on_connect(tmp_path):
    conn = db.connect(str(tmp_path / "t.db"))
    try:
        tables = [
            row[0]
            for row in conn.execute(
                "SELECT name FROM sqlite_master WHERE type = 'table'"
            )
        ]
        assert "fx_rates" in tables
    finally:
        conn.close()


def test_fx_asof_weekend_uses_friday(tmp_path):
    conn = db.connect(str(tmp_path / "t.db"))
    try:
        _seed(conn)
        assert fx_module.fx_asof(conn, "2026-10-01") == pytest.approx(62.799)
        # Saturday/Sunday resolve to Friday's ECB rate.
        assert fx_module.fx_asof(conn, "2026-10-03") == pytest.approx(62.597)
        assert fx_module.fx_asof(conn, "2026-10-04") == pytest.approx(62.597)
        assert fx_module.fx_asof(conn, "2026-10-05") == pytest.approx(62.673)
        # Nothing on or before -> None.
        assert fx_module.fx_asof(conn, "2026-09-30") is None
    finally:
        conn.close()


def test_fx_upsert_idempotent(tmp_path):
    conn = db.connect(str(tmp_path / "t.db"))
    try:
        assert fx_module.upsert_rates(conn, RATES) == 3
        assert fx_module.upsert_rates(conn, RATES) == 3
        assert conn.execute("SELECT COUNT(*) FROM fx_rates").fetchone()[0] == 3
        row = conn.execute(
            "SELECT rate, source FROM fx_rates WHERE date = '2026-10-01'"
        ).fetchone()
        assert row[0] == pytest.approx(62.799)
        assert row[1] == "ECB via Frankfurter"
    finally:
        conn.close()


class _FakeResponse:
    def __init__(self, payload):
        self._payload = payload

    def raise_for_status(self):
        return None

    def json(self):
        return self._payload


def _fake_get_factory(calls, payload):
    def fake_get(url, params=None, headers=None, timeout=None):
        calls.append((url, params, headers, timeout))
        return _FakeResponse(payload)

    return fake_get


def test_fx_update_fetches_and_stores(tmp_path, monkeypatch):
    with open(FIXTURE_PATH, encoding="utf-8") as handle:
        payload = json.load(handle)
    calls = []
    monkeypatch.setattr(
        fx_module.requests, "get", _fake_get_factory(calls, payload)
    )
    db_path = str(tmp_path / "t.db")
    conn = db.connect(db_path)
    conn.close()
    rc = main(
        ["fx-update", "--db", db_path,
         "--since", "2026-10-01", "--until", "2026-10-23"]
    )
    assert rc == 0
    # One request for a sub-year range, keyless Frankfurter URL + User-Agent.
    assert len(calls) == 1
    url, params, headers, timeout = calls[0]
    assert url == "https://api.frankfurter.dev/v1/2026-10-01..2026-10-23"
    assert params == {"from": "USD", "to": "PHP"}
    assert "PresyoBantay" in headers["User-Agent"]
    assert timeout == 30
    conn = db.connect(db_path)
    try:
        assert conn.execute("SELECT COUNT(*) FROM fx_rates").fetchone()[0] == 17
        assert fx_module.fx_asof(conn, "2026-10-24") == pytest.approx(63.642)
        # Weekend as-of between stored business days.
        assert fx_module.fx_asof(conn, "2026-10-04") == pytest.approx(62.597)
    finally:
        conn.close()
    # Re-running is an idempotent upsert.
    rc = main(
        ["fx-update", "--db", db_path,
         "--since", "2026-10-01", "--until", "2026-10-23"]
    )
    assert rc == 0
    conn = db.connect(db_path)
    try:
        assert conn.execute("SELECT COUNT(*) FROM fx_rates").fetchone()[0] == 17
    finally:
        conn.close()


def test_fx_update_since_after_until_does_nothing(tmp_path, monkeypatch, capsys):
    calls = []
    monkeypatch.setattr(
        fx_module.requests, "get", _fake_get_factory(calls, {"rates": {}})
    )
    db_path = str(tmp_path / "t.db")
    conn = db.connect(db_path)
    try:
        _seed(conn)
    finally:
        conn.close()
    rc = main(
        ["fx-update", "--db", db_path,
         "--since", "2026-10-06", "--until", "2026-10-05"]
    )
    assert rc == 0
    assert calls == []
    assert "nothing to do" in capsys.readouterr().out


def test_fx_update_default_since_follows_stored_rates(
    tmp_path, monkeypatch, capsys
):
    with open(FIXTURE_PATH, encoding="utf-8") as handle:
        payload = json.load(handle)
    calls = []
    monkeypatch.setattr(
        fx_module.requests, "get", _fake_get_factory(calls, payload)
    )
    db_path = str(tmp_path / "t.db")
    conn = db.connect(db_path)
    try:
        _seed(conn)  # latest stored: 2026-10-05
    finally:
        conn.close()
    rc = main(["fx-update", "--db", db_path, "--until", "2026-10-23"])
    assert rc == 0
    # Default since = day after the latest stored FX date.
    assert calls[0][0] == "https://api.frankfurter.dev/v1/2026-10-06..2026-10-23"
    capsys.readouterr()


def test_fx_update_default_since_uses_earliest_price_date(
    tmp_path, monkeypatch, capsys
):
    calls = []
    monkeypatch.setattr(
        fx_module.requests,
        "get",
        _fake_get_factory(calls, {"rates": {"2026-10-16": {"PHP": 62.968}}}),
    )
    db_path = str(tmp_path / "t.db")
    conn = db.connect(db_path)
    try:
        db.ingest_payload(
            conn,
            {
                "date": "2026-10-16",
                "source_url": None,
                "markets": {
                    "M1": {"Bangus": {"spec": None, "unit": "per kg", "price": 1.0}}
                },
            },
        )
    finally:
        conn.close()
    rc = main(["fx-update", "--db", db_path, "--until", "2026-10-16"])
    assert rc == 0
    assert calls[0][0] == "https://api.frankfurter.dev/v1/2026-10-16..2026-10-16"
    capsys.readouterr()


def test_fx_update_splits_long_ranges_yearly(tmp_path, monkeypatch, capsys):
    calls = []
    monkeypatch.setattr(
        fx_module.requests, "get", _fake_get_factory(calls, {"rates": {}})
    )
    sleeps = []
    monkeypatch.setattr(fx_module.time, "sleep", sleeps.append)
    db_path = str(tmp_path / "t.db")
    conn = db.connect(db_path)
    conn.close()
    rc = main(
        ["fx-update", "--db", db_path,
         "--since", "2024-06-01", "--until", "2026-02-01"]
    )
    assert rc == 0
    urls = [url for url, _p, _h, _t in calls]
    assert urls == [
        "https://api.frankfurter.dev/v1/2024-06-01..2024-12-31",
        "https://api.frankfurter.dev/v1/2025-01-01..2025-12-31",
        "https://api.frankfurter.dev/v1/2026-01-01..2026-02-01",
    ]
    assert len(sleeps) == 2  # 2s delay between sequential chunks
    capsys.readouterr()
