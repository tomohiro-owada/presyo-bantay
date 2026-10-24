"""Telegram client tests with mocked HTTP (no network)."""

import json

import pytest

from presyo_bantay import telegram
from presyo_bantay.telegram import build_payload, post_telegram


class FakeResponse:
    def __init__(self, status_code=200, payload=None):
        self.status_code = status_code
        self._payload = payload if payload is not None else {"ok": True}

    def json(self):
        if isinstance(self._payload, Exception):
            raise self._payload
        return self._payload


@pytest.fixture()
def env(monkeypatch):
    monkeypatch.setenv("TELEGRAM_BOT_TOKEN", "test-token-123")
    monkeypatch.setenv("TELEGRAM_CHAT_ID", "@testchannel")


def test_payload_shape():
    payload = build_payload("@chan", "hi", "HTML")
    assert payload == {
        "chat_id": "@chan",
        "text": "hi",
        "parse_mode": "HTML",
        "disable_web_page_preview": True,
    }


def test_success_posts_expected_request(env, monkeypatch):
    calls = []

    def _post(url, **kwargs):
        calls.append((url, kwargs))
        return FakeResponse(200, {"ok": True, "result": {"message_id": 1}})

    monkeypatch.setattr(telegram.requests, "post", _post)
    assert post_telegram("hello", parse_mode="HTML") == 0
    assert len(calls) == 1
    url, kwargs = calls[0]
    assert url == "https://api.telegram.org/bottest-token-123/sendMessage"
    assert kwargs["json"]["chat_id"] == "@testchannel"
    assert kwargs["json"]["text"] == "hello"
    assert kwargs["json"]["parse_mode"] == "HTML"
    assert kwargs["json"]["disable_web_page_preview"] is True
    assert "timeout" in kwargs


def test_missing_token_errors(env, monkeypatch, capsys):
    monkeypatch.delenv("TELEGRAM_BOT_TOKEN")
    assert post_telegram("hello") == 1
    err = capsys.readouterr().err
    assert "TELEGRAM_BOT_TOKEN" in err
    assert "test-token-123" not in err + capsys.readouterr().out


def test_missing_chat_id_errors(monkeypatch, capsys):
    monkeypatch.setenv("TELEGRAM_BOT_TOKEN", "test-token-123")
    monkeypatch.delenv("TELEGRAM_CHAT_ID", raising=False)
    assert post_telegram("hello") == 1
    assert "TELEGRAM_CHAT_ID" in capsys.readouterr().err


def test_ok_false_prints_description(env, monkeypatch, capsys):
    def _post(url, **kwargs):
        return FakeResponse(200, {"ok": False, "description": "chat not found"})

    monkeypatch.setattr(telegram.requests, "post", _post)
    assert post_telegram("hello") == 1
    out = capsys.readouterr()
    assert "chat not found" in out.err
    assert "test-token-123" not in out.err + out.out


def test_429_retries_once_with_retry_after(env, monkeypatch):
    sleeps = []
    monkeypatch.setattr(telegram.time, "sleep", lambda s: sleeps.append(s))
    responses = [
        FakeResponse(
            429,
            {"ok": False, "parameters": {"retry_after": 2}},
        ),
        FakeResponse(200, {"ok": True}),
    ]

    def _post(url, **kwargs):
        return responses.pop(0)

    monkeypatch.setattr(telegram.requests, "post", _post)
    assert post_telegram("hello") == 0
    assert sleeps == [2]


def test_429_retry_after_capped(env, monkeypatch):
    sleeps = []
    monkeypatch.setattr(telegram.time, "sleep", lambda s: sleeps.append(s))
    responses = [
        FakeResponse(429, {"ok": False, "parameters": {"retry_after": 300}}),
        FakeResponse(200, {"ok": True}),
    ]

    def _post(url, **kwargs):
        return responses.pop(0)

    monkeypatch.setattr(telegram.requests, "post", _post)
    assert post_telegram("hello") == 0
    assert sleeps == [telegram.MAX_RETRY_AFTER]


def test_network_error_nonzero(env, monkeypatch, capsys):
    def _post(url, **kwargs):
        raise telegram.requests.ConnectionError("offline")

    monkeypatch.setattr(telegram.requests, "post", _post)
    assert post_telegram("hello") == 1
    assert "test-token-123" not in capsys.readouterr().err


def test_dry_run_makes_no_request(monkeypatch, capsys):
    monkeypatch.delenv("TELEGRAM_BOT_TOKEN", raising=False)
    monkeypatch.delenv("TELEGRAM_CHAT_ID", raising=False)

    def _boom(url, **kwargs):
        raise AssertionError("dry-run must not touch the network")

    monkeypatch.setattr(telegram.requests, "post", _boom)
    assert post_telegram("hello dry", dry_run=True) == 0
    out = capsys.readouterr().out
    assert "hello dry" in out
    assert "dry-run" in out.lower()


def test_cli_post_telegram_dry_run(tmp_path, monkeypatch, capsys):
    from presyo_bantay.cli import main

    monkeypatch.delenv("TELEGRAM_BOT_TOKEN", raising=False)
    msg = tmp_path / "msg.html"
    msg.write_text("dry message", encoding="utf-8")

    def _boom(url, **kwargs):
        raise AssertionError("dry-run must not touch the network")

    monkeypatch.setattr(telegram.requests, "post", _boom)
    rc = main(["post-telegram", "--file", str(msg), "--dry-run"])
    assert rc == 0
    assert "dry message" in capsys.readouterr().out
