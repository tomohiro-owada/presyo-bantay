"""Optional LLM rewrite hook for the weekly summary message.

Disabled by default: no HTTP request is ever made unless the caller passes
``enabled=True`` (the ``--llm`` CLI flag) *and* the environment provides
``PRESYO_LLM_BASE_URL`` and ``PRESYO_LLM_API_KEY`` (plus optional
``PRESYO_LLM_MODEL``). The LLM is asked to only rephrase the template in
Taglish without changing any numbers; the output is validated by
:func:`validate_numbers` and any failure falls back to the template text.
"""

from __future__ import annotations

import os
import re
import sys

import requests

from .render import DISCLAIMER, MAX_CHARS, SOURCE_LINE

LLM_TIMEOUT = 30

_DATE_RE = re.compile(r"\d{4}-\d{2}-\d{2}")
_NUMBER_TOKEN_RE = re.compile(r"₱?\d[\d,]*(?:\.\d+)?%?")


def _normalize_number(raw: str) -> float | None:
    cleaned = raw.replace("₱", "").replace("%", "").replace(",", "").strip()
    if not cleaned:
        return None
    try:
        return float(cleaned)
    except ValueError:
        return None


def allowed_numbers(template: str) -> tuple[set[float], set[str]]:
    """Collect ``(numbers, dates)`` present in the template output."""
    dates = set(_DATE_RE.findall(template))
    scrubbed = _DATE_RE.sub(" ", template)
    numbers: set[float] = set()
    for token in _NUMBER_TOKEN_RE.findall(scrubbed):
        value = _normalize_number(token)
        if value is not None:
            numbers.add(value)
    # Allow date components too (e.g. an LLM writing "October 24, 2026").
    for date in dates:
        for part in date.split("-"):
            try:
                numbers.add(float(int(part)))
            except ValueError:
                pass
    return (numbers, dates)


def extract_number_tokens(text: str) -> tuple[list[float], list[str]]:
    """Extract ``(numbers, dates)`` tokens from *text*."""
    dates = _DATE_RE.findall(text)
    scrubbed = _DATE_RE.sub(" ", text)
    numbers: list[float] = []
    for token in _NUMBER_TOKEN_RE.findall(scrubbed):
        value = _normalize_number(token)
        if value is not None:
            numbers.append(value)
    return (numbers, dates)


def validate_numbers(text: str, allowed: tuple[set[float], set[str]]) -> bool:
    """Check LLM output against the template's numbers and required lines.

    Every number token (handling ``₱``, commas, decimals, ``%``) and every
    date in *text* must already be present in *allowed* (``59 == 59.00`` after
    normalization). The Source line and disclaimer must still be present and
    the text must be shorter than the Telegram cap.
    """
    allowed_nums, allowed_dates = allowed
    numbers, dates = extract_number_tokens(text)
    for value in numbers:
        if value not in allowed_nums:
            return False
    for date in dates:
        if date not in allowed_dates:
            return False
    if SOURCE_LINE not in text:
        return False
    if DISCLAIMER not in text:
        return False
    if len(text) >= MAX_CHARS:
        return False
    return True


def _warn(message: str) -> None:
    print(f"warning: {message}", file=sys.stderr)


def maybe_rewrite(template: str, *, enabled: bool = False) -> str:
    """Rephrase *template* via an OpenAI-compatible API, or return it as-is."""
    if not enabled:
        return template
    base_url = os.environ.get("PRESYO_LLM_BASE_URL", "").strip().rstrip("/")
    api_key = os.environ.get("PRESYO_LLM_API_KEY", "").strip()
    model = os.environ.get("PRESYO_LLM_MODEL", "gpt-4o-mini").strip()
    if not base_url or not api_key:
        return template
    allowed = allowed_numbers(template)
    try:
        response = requests.post(
            f"{base_url}/chat/completions",
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            json={
                "model": model,
                "temperature": 0,
                "messages": [
                    {
                        "role": "system",
                        "content": (
                            "Rephrase the user's price update in friendly "
                            "Taglish for a Telegram channel. Only rephrase: "
                            "do not change, add, or remove any numbers, "
                            "prices, percentages, or dates, and keep the "
                            "Source line and the disclaimer exactly as they "
                            "are."
                        ),
                    },
                    {"role": "user", "content": template},
                ],
            },
            timeout=LLM_TIMEOUT,
        )
        response.raise_for_status()
        payload = response.json()
        content = payload["choices"][0]["message"]["content"]
    except Exception as exc:  # noqa: BLE001 - any failure falls back
        _warn(f"LLM rewrite failed ({exc}); using template text")
        return template
    if not isinstance(content, str) or not content.strip():
        _warn("LLM rewrite returned empty text; using template text")
        return template
    if not validate_numbers(content.strip(), allowed):
        _warn("LLM rewrite failed validation; using template text")
        return template
    return content.strip()
