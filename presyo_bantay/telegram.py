"""Send a message via the Telegram Bot API (no other network use)."""

from __future__ import annotations

import os
import sys
import time

import requests

API_BASE = "https://api.telegram.org"
REQUEST_TIMEOUT = 30
MAX_RETRY_AFTER = 60


def build_payload(chat_id: str, text: str, parse_mode: str) -> dict:
    return {
        "chat_id": chat_id,
        "text": text,
        "parse_mode": parse_mode,
        "disable_web_page_preview": True,
    }


def _post(url: str, payload: dict) -> requests.Response:
    return requests.post(url, json=payload, timeout=REQUEST_TIMEOUT)


def post_telegram(
    text: str, *, parse_mode: str = "HTML", dry_run: bool = False
) -> int:
    """POST *text* to the Telegram chat from env config.

    ``TELEGRAM_BOT_TOKEN`` and ``TELEGRAM_CHAT_ID`` are required unless
    *dry_run* is set (dry-run prints the target and message without network
    and without requiring the token). Returns 0 on success, 1 on error; the
    token is never printed.
    """
    chat_id = os.environ.get("TELEGRAM_CHAT_ID", "").strip()
    if dry_run:
        target = chat_id or "(TELEGRAM_CHAT_ID not set)"
        print(f"dry-run: would send to {target} with parse_mode={parse_mode}")
        print(text)
        return 0

    token = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
    if not token:
        print("error: TELEGRAM_BOT_TOKEN is not set", file=sys.stderr)
        return 1
    if not chat_id:
        print("error: TELEGRAM_CHAT_ID is not set", file=sys.stderr)
        return 1

    url = f"{API_BASE}/bot{token}/sendMessage"
    payload = build_payload(chat_id, text, parse_mode)
    attempts = 0
    while True:
        attempts += 1
        try:
            response = _post(url, payload)
        except requests.RequestException as exc:
            print(f"error: Telegram request failed: {exc}", file=sys.stderr)
            return 1
        if response.status_code == 429 and attempts == 1:
            try:
                retry_after = response.json().get("parameters", {}).get(
                    "retry_after", 0
                )
            except ValueError:
                retry_after = 0
            try:
                wait = min(float(retry_after or 0), MAX_RETRY_AFTER)
            except (TypeError, ValueError):
                wait = 0
            if wait > 0:
                time.sleep(wait)
            else:
                time.sleep(1)
            continue  # single retry for 429
        break

    try:
        body = response.json()
    except ValueError:
        print(
            f"error: Telegram returned HTTP {response.status_code} "
            "with a non-JSON body",
            file=sys.stderr,
        )
        return 1
    if not body.get("ok"):
        description = body.get("description", "unknown error")
        print(f"error: Telegram sendMessage failed: {description}", file=sys.stderr)
        return 1
    return 0
