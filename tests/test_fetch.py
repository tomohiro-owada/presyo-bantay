"""Fetch-command tests. All network access is mocked (offline)."""

import json
import os
from unittest.mock import patch

from presyo_bantay.cli import main

from .conftest import LISTING_HTML, SAMPLE_PDF


class _FakeResponse:
    def __init__(self, text=None, content=None):
        self.text = text or ""
        self.content = content or b""

    def raise_for_status(self):
        return None


def _listing_html() -> str:
    with open(LISTING_HTML, encoding="utf-8", errors="replace") as handle:
        return handle.read()


def _pdf_bytes() -> bytes:
    with open(SAMPLE_PDF, "rb") as handle:
        return handle.read()


def test_fetch_from_listing_file_downloads_latest(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    with patch("presyo_bantay.cli.requests.get") as mock_get:
        mock_get.return_value = _FakeResponse(content=_pdf_bytes())
        rc = main(["fetch", "--listing-file", LISTING_HTML])
    assert rc == 0
    # Exactly one request: the PDF download (listing came from a file).
    assert mock_get.call_count == 1
    dest = os.path.join("data", "raw", "Price-Monitoring-October-23-2026.pdf")
    assert os.path.exists(dest)
    meta = dest + ".meta.json"
    assert os.path.exists(meta)
    with open(meta, encoding="utf-8") as handle:
        assert "Price-Monitoring-October-23-2026.pdf" in json.load(handle)["source_url"]


def test_fetch_uses_cache_and_skips_download(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    os.makedirs(os.path.join("data", "raw"), exist_ok=True)
    dest = os.path.join("data", "raw", "Price-Monitoring-October-23-2026.pdf")
    with open(dest, "wb") as handle:
        handle.write(b"cached")
    with patch("presyo_bantay.cli.requests.get") as mock_get:
        rc = main(["fetch", "--listing-file", LISTING_HTML])
    assert rc == 0
    assert mock_get.call_count == 0
    with open(dest, "rb") as handle:
        assert handle.read() == b"cached"


def test_fetch_hits_network_only_twice(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    calls = []

    def _fake_get(url, **kwargs):
        calls.append(url)
        if url.endswith(".pdf"):
            return _FakeResponse(content=_pdf_bytes())
        return _FakeResponse(text=_listing_html())

    with patch("presyo_bantay.cli.requests.get", side_effect=_fake_get):
        rc = main(["fetch"])
    assert rc == 0
    assert len(calls) == 2  # one listing request + one PDF request
    assert calls[0] == "https://www.da.gov.ph/price-monitoring/"
    assert calls[1].endswith("Price-Monitoring-October-23-2026.pdf")


def test_fetch_then_parse_roundtrip(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    with patch("presyo_bantay.cli.requests.get") as mock_get:
        mock_get.return_value = _FakeResponse(content=_pdf_bytes())
        rc = main(["fetch", "--listing-file", LISTING_HTML, "--parse"])
    assert rc == 0
    out_path = os.path.join("data", "json", "2026-10-23.json")
    assert os.path.exists(out_path)
    with open(out_path, encoding="utf-8") as handle:
        payload = json.load(handle)
    assert payload["date"] == "2026-10-23"
    assert payload["source_url"].endswith("Price-Monitoring-October-23-2026.pdf")
    assert payload["markets"]["New Las Piñas City Public Market"]["Bangus"]["price"] == 270.0
