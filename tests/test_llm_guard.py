"""LLM guard tests: number validation, fallback, and default-off (no HTTP)."""

import json

import pytest

from presyo_bantay import llm
from presyo_bantay.llm import (
    allowed_numbers,
    extract_number_tokens,
    maybe_rewrite,
    validate_numbers,
)
from presyo_bantay.render import DISCLAIMER, SOURCE_LINE, render_summary
from presyo_bantay.summary import build_summary, load_snapshots, select_snapshots

from .conftest import SYNTHETIC_DIR


@pytest.fixture()
def template():
    snapshots = load_snapshots(SYNTHETIC_DIR)
    latest, baseline = select_snapshots(snapshots, "2026-10-24")
    summary = build_summary(latest, baseline, "2026-10-24")
    return render_summary(summary, fmt="html")


def test_allowed_numbers_roundtrip(template):
    numbers, dates = allowed_numbers(template)
    assert 59.0 in numbers
    assert 12.5 in numbers
    assert "2026-10-23" in dates  # latest snapshot date in the message
    nums, found_dates = extract_number_tokens(template)
    assert "2026-10-23" in found_dates


def test_validate_accepts_rephrase(template):
    allowed = allowed_numbers(template)
    rephrase = template.replace("Presyo Bantay", "Presyo Bantay mga kapitbahay")
    assert validate_numbers(rephrase, allowed)


def test_validate_normalizes_59_vs_59_00(template):
    allowed = allowed_numbers(template)
    assert "₱59.00" in template
    rephrase = template.replace("₱59.00", "59", 1)
    assert validate_numbers(rephrase, allowed)


def test_validate_rejects_invented_number(template):
    allowed = allowed_numbers(template)
    assert not validate_numbers(template + " Extra: ₱999.00/kg", allowed)
    assert not validate_numbers(template.replace("12.5%", "12.6%", 1), allowed)


def test_validate_requires_source_disclaimer_length(template):
    allowed = allowed_numbers(template)
    assert not validate_numbers(
        template.replace(SOURCE_LINE, "Source: elsewhere"), allowed
    )
    assert not validate_numbers(
        template.replace(DISCLAIMER, "No disclaimer"), allowed
    )
    assert not validate_numbers(template + "x" * 1500, allowed)


def test_disabled_by_default_makes_no_http(template, monkeypatch):
    def _boom(*args, **kwargs):
        raise AssertionError("no HTTP allowed when disabled")

    monkeypatch.setattr(llm.requests, "post", _boom)
    monkeypatch.delenv("PRESYO_LLM_BASE_URL", raising=False)
    monkeypatch.delenv("PRESYO_LLM_API_KEY", raising=False)
    assert maybe_rewrite(template) == template
    # Even with env set, the flag defaults to off.
    monkeypatch.setenv("PRESYO_LLM_BASE_URL", "https://llm.example.test")
    monkeypatch.setenv("PRESYO_LLM_API_KEY", "secret")
    assert maybe_rewrite(template) == template


def test_enabled_without_env_makes_no_http(template, monkeypatch):
    def _boom(*args, **kwargs):
        raise AssertionError("no HTTP without env credentials")

    monkeypatch.setattr(llm.requests, "post", _boom)
    monkeypatch.delenv("PRESYO_LLM_BASE_URL", raising=False)
    monkeypatch.delenv("PRESYO_LLM_API_KEY", raising=False)
    assert maybe_rewrite(template, enabled=True) == template


class _FakeResponse:
    def __init__(self, payload):
        self._payload = payload

    def raise_for_status(self):
        pass

    def json(self):
        return self._payload


def test_enabled_rewrite_accepted(template, monkeypatch):
    monkeypatch.setenv("PRESYO_LLM_BASE_URL", "https://llm.example.test")
    monkeypatch.setenv("PRESYO_LLM_API_KEY", "secret")
    rephrased = template.replace("Presyo Bantay", "Presyo Bantay, mga suki")
    calls = []

    def _post(url, **kwargs):
        calls.append(url)
        assert url == "https://llm.example.test/chat/completions"
        return _FakeResponse({"choices": [{"message": {"content": rephrased}}]})

    monkeypatch.setattr(llm.requests, "post", _post)
    assert maybe_rewrite(template, enabled=True) == rephrased
    assert len(calls) == 1


def test_enabled_rewrite_rejected_number_falls_back(template, monkeypatch, capsys):
    monkeypatch.setenv("PRESYO_LLM_BASE_URL", "https://llm.example.test")
    monkeypatch.setenv("PRESYO_LLM_API_KEY", "secret")

    def _post(url, **kwargs):
        return _FakeResponse(
            {"choices": [{"message": {"content": template + " ₱1,234.00"}}]}
        )

    monkeypatch.setattr(llm.requests, "post", _post)
    assert maybe_rewrite(template, enabled=True) == template
    assert "warning" in capsys.readouterr().err


def test_enabled_http_error_falls_back(template, monkeypatch, capsys):
    monkeypatch.setenv("PRESYO_LLM_BASE_URL", "https://llm.example.test")
    monkeypatch.setenv("PRESYO_LLM_API_KEY", "secret")

    def _post(url, **kwargs):
        raise llm.requests.ConnectionError("offline")

    monkeypatch.setattr(llm.requests, "post", _post)
    assert maybe_rewrite(template, enabled=True) == template
    assert "warning" in capsys.readouterr().err
