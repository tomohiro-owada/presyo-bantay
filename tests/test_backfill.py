"""Backfill tests. All network access and sleeping are mocked (offline)."""

import json
import os
from unittest.mock import patch

import requests

from presyo_bantay import backfill as bf
from presyo_bantay.cli import main

from .conftest import LISTING_HTML, SAMPLE_PDF


class FakeResponse:
    def __init__(self, *, status=200, content=b"", headers=None, text=""):
        self.status_code = status
        self.content = content
        self.headers = headers or {}
        self.text = text

    def raise_for_status(self):
        if self.status_code >= 400:
            raise requests.HTTPError(f"HTTP {self.status_code}")


def _pdf_bytes() -> bytes:
    with open(SAMPLE_PDF, "rb") as handle:
        return handle.read()


TINY_LISTING = """<html><body>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-October-23-2026.pdf">a</a>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-October-22-2026.pdf">b</a>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Daily-Cigarette-Price-Monitoring-October-23-2026.pdf">c</a>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Watch-October-23-2026.pdf">d</a>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/RETAIL-Price-Monitoring-Report_04OCT19.pdf">e</a>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Daily-Price-Index-October-23-2026.pdf">f</a>
<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Weekly-Average-Prices-October-12-18-2026.pdf">g</a>
</body></html>
"""


def _write_listing(tmp_path, text=TINY_LISTING):
    path = tmp_path / "listing.html"
    path.write_text(text, encoding="utf-8")
    return str(path)


def _run_backfill(tmp_path, listing_path, fake_get, sleeps, extra=None):
    raw = str(tmp_path / "raw")
    out = str(tmp_path / "json")
    bdir = str(tmp_path / "backfill")
    db = str(tmp_path / "t.db")
    argv = [
        "backfill", "--listing-file", listing_path,
        "--raw-dir", raw, "--json-dir", out,
        "--db", db, "--backfill-dir", bdir,
        "--delay", "5",
    ] + (extra or [])
    with patch("presyo_bantay.backfill.requests.get", fake_get), patch(
        "presyo_bantay.backfill.time.sleep", sleeps.append
    ):
        rc = main(argv)
    return rc, {"raw": raw, "json": out, "bdir": bdir, "db": db}


def _fixture_html() -> str:
    with open(LISTING_HTML, encoding="utf-8", errors="replace") as handle:
        return handle.read()


# -- selection ----------------------------------------------------------

def test_selection_from_fixture_html_counts_and_order():
    targets, n_listed = bf.select_targets(_fixture_html())
    assert n_listed == 1892
    assert len(targets) == 1892  # one PDF per date
    dates = [t[2] for t in targets]
    assert dates == sorted(dates, reverse=True)  # newest first
    assert len(set(dates)) == len(dates)
    assert targets[0][2] == "2026-10-23"
    assert targets[0][0] == "Price-Monitoring-October-23-2026.pdf"


def test_selection_prefers_updated_and_revised():
    targets, _ = bf.select_targets(_fixture_html())
    by_date = {t[2]: t[0] for t in targets}
    assert by_date["2023-05-22"] == "Updated-Price-Monitoring-May-22-2023.pdf"
    assert by_date["2026-10-13"] == "Revised-Price-Monitoring-October-13-2026.pdf"


def test_selection_excludes_non_market_pdfs():
    targets, _ = bf.select_targets(TINY_LISTING)
    assert [t[0] for t in targets] == [
        "Price-Monitoring-October-23-2026.pdf",
        "Price-Monitoring-October-22-2026.pdf",
    ]


def test_selection_since_until_limit():
    targets, _ = bf.select_targets(
        TINY_LISTING, since="2026-10-23", until="2026-10-23"
    )
    assert [t[2] for t in targets] == ["2026-10-23"]
    targets, _ = bf.select_targets(TINY_LISTING, limit=1)
    assert [t[2] for t in targets] == ["2026-10-23"]


# -- dry-run: zero network requests -------------------------------------

def test_dry_run_makes_zero_requests(tmp_path, capsys):
    listing = _write_listing(tmp_path)
    calls = []

    def _boom(url, **kwargs):
        calls.append(url)
        raise AssertionError("dry-run must not issue PDF requests")

    sleeps = []
    rc, _ = _run_backfill(tmp_path, listing, _boom, sleeps, ["--dry-run", "--limit", "5"])
    assert rc == 0
    assert calls == []
    out = capsys.readouterr().out
    assert "would download" in out
    assert "Price-Monitoring-October-23-2026.pdf" in out


# -- politeness: delay enforced ------------------------------------------

def test_delay_enforced_between_requests(tmp_path):
    listing = _write_listing(tmp_path)

    def _ok(url, **kwargs):
        return FakeResponse(status=200, content=_pdf_bytes())

    sleeps = []
    rc, _ = _run_backfill(tmp_path, listing, _ok, sleeps)
    assert rc == 0
    assert sleeps, "expected polite sleeps between requests"
    assert all(s >= 5.0 for s in sleeps)


def test_delay_minimum_enforced():
    worker = bf.Backfiller(
        raw_dir="x", json_dir="y", db_path="z", backfill_dir="w", delay=1.0
    )
    assert worker.delay == 5.0


# -- success path: JSON + DB ingest ---------------------------------------

def test_success_writes_json_and_ingests_db(tmp_path):
    from presyo_bantay import db as _db

    listing = _write_listing(tmp_path)

    def _ok(url, **kwargs):
        return FakeResponse(status=200, content=_pdf_bytes())

    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _ok, sleeps)
    assert rc == 0
    out_json = os.path.join(paths["json"], "2026-10-23.json")
    assert os.path.exists(out_json)
    with open(out_json, encoding="utf-8") as handle:
        payload = json.load(handle)
    assert payload["date"] == "2026-10-23"
    assert payload["source_url"].endswith("Price-Monitoring-October-23-2026.pdf")
    assert "New Las Piñas City Public Market" in payload["markets"]
    # meta.json written next to the raw PDF.
    assert os.path.exists(
        os.path.join(paths["raw"], "Price-Monitoring-October-23-2026.pdf.meta.json")
    )
    # Ingested into the DB immediately.
    conn = _db.connect(paths["db"])
    try:
        assert "2026-10-23" in _db.list_dates(conn)
        series = _db.price_series(
            conn, "New Las Piñas City Public Market", "Bangus"
        )
        assert ("2026-10-23", 270.0) in series
    finally:
        conn.close()
    # State saved with ok status + timestamp.
    with open(os.path.join(paths["bdir"], "state.json"), encoding="utf-8") as handle:
        state = json.load(handle)
    entry = state["Price-Monitoring-October-23-2026.pdf"]
    assert entry["status"] == "ok"
    assert entry["timestamp"]
    assert entry["error"] is None
    # Progress + summary lines, also in the log file.
    with open(os.path.join(paths["bdir"], "backfill.log"), encoding="utf-8") as handle:
        logged = handle.read()
    assert "listed=" in logged and "selected=" in logged
    assert "skipped-cached=" in logged and "downloaded=" in logged


# -- skip cached: no request ----------------------------------------------

def test_skip_cached_makes_no_request(tmp_path):
    listing = _write_listing(tmp_path)
    requested = []

    def _ok(url, **kwargs):
        requested.append(url)
        return FakeResponse(status=200, content=_pdf_bytes())

    raw = tmp_path / "raw"
    raw.mkdir()
    (raw / "Price-Monitoring-October-23-2026.pdf").write_bytes(b"cached")
    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _ok, sleeps)
    assert rc == 0
    assert all("October-23-2026.pdf" not in u for u in requested)
    assert any("October-22-2026.pdf" in u for u in requested)
    with open(os.path.join(paths["bdir"], "state.json"), encoding="utf-8") as handle:
        state = json.load(handle)
    assert state["Price-Monitoring-October-23-2026.pdf"]["status"] == "skipped"
    with open(raw / "Price-Monitoring-October-23-2026.pdf", "rb") as handle:
        assert handle.read() == b"cached"


# -- resume from state -----------------------------------------------------

def test_resume_skips_ok_and_failed_unless_retry(tmp_path):
    listing = _write_listing(tmp_path)
    bdir = tmp_path / "backfill"
    bdir.mkdir()
    (bdir / "state.json").write_text(
        json.dumps(
            {
                "Price-Monitoring-October-23-2026.pdf": {
                    "status": "ok", "date": "2026-10-23",
                    "url": "u1", "timestamp": "t", "error": None,
                },
                "Price-Monitoring-October-22-2026.pdf": {
                    "status": "failed", "date": "2026-10-22",
                    "url": "u2", "timestamp": "t", "error": "HTTP 500 (gave up)",
                },
            }
        ),
        encoding="utf-8",
    )
    requested = []

    def _ok(url, **kwargs):
        requested.append(url)
        return FakeResponse(status=200, content=_pdf_bytes())

    sleeps = []
    rc, _ = _run_backfill(tmp_path, listing, _ok, sleeps)
    assert rc == 0
    assert requested == []  # both skipped from state
    # --retry-failed re-processes the failed file only.
    requested.clear()
    sleeps = []
    rc, _ = _run_backfill(tmp_path, listing, _ok, sleeps, ["--retry-failed"])
    assert rc == 0
    assert len(requested) == 1 and "October-22-2026.pdf" in requested[0]


# -- 429 honors Retry-After --------------------------------------------------

def test_429_retry_after_honored(tmp_path):
    listing = _write_listing(tmp_path)
    calls = []

    def _flaky(url, **kwargs):
        calls.append(url)
        if len(calls) == 1:
            return FakeResponse(status=429, headers={"Retry-After": "7"})
        return FakeResponse(status=200, content=_pdf_bytes())

    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _flaky, sleeps, ["--limit", "1"])
    assert rc == 0
    assert 7.0 in sleeps  # Retry-After honored exactly
    with open(os.path.join(paths["bdir"], "state.json"), encoding="utf-8") as handle:
        state = json.load(handle)
    assert state["Price-Monitoring-October-23-2026.pdf"]["status"] == "ok"


def test_429_without_header_uses_backoff(tmp_path):
    listing = _write_listing(tmp_path)
    calls = []

    def _flaky(url, **kwargs):
        calls.append(url)
        if len(calls) == 1:
            return FakeResponse(status=503)
        return FakeResponse(status=200, content=_pdf_bytes())

    sleeps = []
    rc, _ = _run_backfill(tmp_path, listing, _flaky, sleeps, ["--limit", "1"])
    assert rc == 0
    assert 60.0 in sleeps  # exponential backoff starts at 60 s


# -- 404: failed, not a network error -----------------------------------------

def test_404_marked_failed_and_continues(tmp_path, capsys):
    listing = _write_listing(tmp_path)

    def _mixed(url, **kwargs):
        if "October-23-2026.pdf" in url:
            return FakeResponse(status=404)
        return FakeResponse(status=200, content=_pdf_bytes())

    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _mixed, sleeps)
    assert rc == 0  # 404 alone never stops the run
    with open(os.path.join(paths["bdir"], "state.json"), encoding="utf-8") as handle:
        state = json.load(handle)
    assert state["Price-Monitoring-October-23-2026.pdf"]["status"] == "failed"
    assert "404" in state["Price-Monitoring-October-23-2026.pdf"]["error"]
    assert state["Price-Monitoring-October-22-2026.pdf"]["status"] == "ok"


# -- 5 consecutive failures stop the run ---------------------------------------

def test_five_consecutive_failures_stop(tmp_path, capsys):
    urls = "".join(
        f'<a href="https://example.test/Price-Monitoring-October-{d}-2026.pdf">x</a>'
        for d in (23, 22, 21, 20, 19, 18)
    )
    listing = _write_listing(tmp_path, f"<html><body>{urls}</body></html>")
    requested = []

    def _down(url, **kwargs):
        requested.append(url)
        return FakeResponse(status=500)

    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _down, sleeps)
    assert rc != 0
    # 5 files x 3 tries each, then stop: the 6th file is never requested.
    assert len(requested) == 15
    assert not any("October-18-2026" in u for u in requested)
    assert "stopping" in capsys.readouterr().out


# -- sanity failure: logged, never ingested --------------------------------------

def test_sanity_failure_logged_not_ingested(tmp_path, monkeypatch):
    from presyo_bantay import db as _db

    listing = _write_listing(tmp_path)

    def _ok(url, **kwargs):
        return FakeResponse(status=200, content=_pdf_bytes())

    def _broken(_path):
        return {}, None  # no markets -> sanity failure

    monkeypatch.setattr(bf, "parse_pdf", _broken)
    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _ok, sleeps, ["--limit", "1"])
    assert rc == 0
    assert not os.path.exists(os.path.join(paths["json"], "2026-10-23.json"))
    assert not os.path.exists(paths["json"]) or os.listdir(paths["json"]) == []
    conn = _db.connect(paths["db"])
    try:
        assert _db.list_dates(conn) == []
    finally:
        conn.close()
    with open(os.path.join(paths["bdir"], "failures.jsonl"), encoding="utf-8") as handle:
        lines = handle.readlines()
    assert len(lines) == 1
    record = json.loads(lines[0])
    assert record["filename"] == "Price-Monitoring-October-23-2026.pdf"
    assert record["date"] == "2026-10-23"
    assert "url" in record and "error" in record
    assert "n_pages" in record and "first_header_row" in record
    with open(os.path.join(paths["bdir"], "state.json"), encoding="utf-8") as handle:
        state = json.load(handle)
    assert state["Price-Monitoring-October-23-2026.pdf"]["status"] == "failed"


def test_backfill_warns_on_missing_markets_of_interest(tmp_path, monkeypatch):
    listing = _write_listing(tmp_path)

    def _ok(url, **kwargs):
        return FakeResponse(status=200, content=_pdf_bytes())

    real_parse = bf.parse_pdf

    def _no_laspinas(path):
        markets, hint = real_parse(path)
        markets = {k: v for k, v in markets.items() if "Piñas" not in k}
        return markets, hint

    monkeypatch.setattr(bf, "parse_pdf", _no_laspinas)
    sleeps = []
    rc, paths = _run_backfill(tmp_path, listing, _ok, sleeps, ["--limit", "1"])
    assert rc == 0  # warning, not a failure
    with open(os.path.join(paths["bdir"], "state.json"), encoding="utf-8") as handle:
        state = json.load(handle)
    entry = state["Price-Monitoring-October-23-2026.pdf"]
    assert entry["status"] == "ok"
    assert any("Las Pi" in w for w in entry.get("warnings", []))
