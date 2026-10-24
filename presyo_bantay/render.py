"""Render a :class:`WeeklySummary` as a short Taglish Telegram message.

Two formats are supported: ``html`` (Telegram HTML parse mode, the default)
and ``markdownv2`` (Telegram MarkdownV2 parse mode). Messages are capped at
``MAX_CHARS`` characters (``len(str)``); lower-priority sections are trimmed
first while the Source line and disclaimer are always kept.
"""

from __future__ import annotations

import datetime as _datetime
import html
import re

from .summary import WeeklySummary

SOURCE_LINE = "Source: DA Bantay Presyo (da.gov.ph)"
DISCLAIMER = "Paalala: iba-iba ang presyo kada pwesto/stall; gabay lang ito."
SYNTHETIC_LINE = "⚠️ SYNTHETIC TEST DATA - hindi totoong presyo"

MAX_CHARS = 1500

_MD_V2_SPECIAL = re.compile(r"([_*\[\]()~`>#+\-=|{}.!])")

#: Tagalog month abbreviations for the friendly header.
_TAGALOG_MONTHS = {
    1: "Ene", 2: "Peb", 3: "Mar", 4: "Abr", 5: "May", 6: "Hun",
    7: "Hul", 8: "Ago", 9: "Set", 10: "Okt", 11: "Nob", 12: "Dis",
}


def escape_markdown_v2(text: str) -> str:
    """Escape every Telegram MarkdownV2 special character in *text*."""
    return _MD_V2_SPECIAL.sub(r"\\\1", text)


def format_price(price: float | None) -> str:
    if price is None:
        return "wala"
    return f"₱{price:,.2f}"


def unit_suffix(item: str) -> str:
    if " ".join(item.lower().split()) == "egg (medium)":
        return "/pc"
    return "/kg"


def price_text(price: float | None, item: str) -> str:
    """Compact price rendering: ``₱59.00/kg`` or ``wala`` (no unit)."""
    if price is None:
        return "wala"
    return f"{format_price(price)}{unit_suffix(item)}"


def friendly_week_ending(week_ending: str) -> str:
    """``2026-10-24`` -> ``Okt 24, 2026`` (ISO string on parse failure)."""
    try:
        day = _datetime.date.fromisoformat(week_ending)
    except ValueError:
        return week_ending
    return f"{_TAGALOG_MONTHS[day.month]} {day.day}, {day.year}"


def short_market(market: str) -> str:
    """Short display name for a resolved market of interest."""
    lowered = market.lower()
    if "las pi" in lowered:
        return "Las Piñas"
    if "alabang" in lowered:
        return "Alabang"
    if "huerta" in lowered:
        return "La Huerta"
    if "bicutan" in lowered:
        return "Bicutan"
    return market


def _change_suffix(pct: float | None) -> str:
    # Unchanged items show no arrow: this keeps the Las Piñas section short
    # so all 14 cheapest-market lines fit under the character cap.
    if pct is None or pct == 0.0:
        return ""
    arrow = "▲" if pct > 0 else "▼"
    return f" {arrow}{abs(pct):.1f}%"


def render_summary(summary: WeeklySummary, fmt: str = "html") -> str:
    """Render *summary* in the ``html`` or ``markdownv2`` Telegram format."""
    if fmt not in ("html", "markdownv2"):
        raise ValueError(f"unknown format: {fmt!r}")
    esc = html.escape if fmt == "html" else escape_markdown_v2

    def bold(text: str) -> str:
        if fmt == "html":
            return f"<b>{esc(text)}</b>"
        return f"*{esc(text)}*"

    lines: list[str] = []
    lines.append(bold(f"🛒 Presyo Bantay — linggo hanggang {friendly_week_ending(summary.week_ending)}"))
    las_pinas = summary.markets[0] if summary.markets else None
    if las_pinas:
        lines.append(
            esc(f"Presyo sa {las_pinas} (DA data, {summary.latest_date})")
        )
    else:
        lines.append(esc(f"Presyo sa Las Piñas (DA data, {summary.latest_date})"))
    if summary.synthetic:
        lines.append(esc(SYNTHETIC_LINE) if fmt == "html" else esc(SYNTHETIC_LINE))

    las_lines: list[str] = []
    if las_pinas:
        for item in summary.items:
            price = (summary.latest.get(item) or {}).get(las_pinas)
            suffix = ""
            if summary.has_changes:
                pct = _pct_for(summary, las_pinas, item)
                suffix = _change_suffix(pct)
            las_lines.append(
                f"{esc('•')} {esc(item)}: {esc(price_text(price, item))}{esc(suffix)}"
            )

    cheap_lines: list[str] = [bold("Pinakamurang palengke (cheapest per item)")]
    for entry in summary.cheapest:
        if entry.price is None or not entry.markets:
            cheap_lines.append(f"{esc('•')} {esc(entry.item)}: {esc('wala')}")
        else:
            names = ", ".join(short_market(m) for m in entry.markets)
            cheap_lines.append(
                f"{esc('•')} {esc(entry.item)}: {esc(names)} {esc(price_text(entry.price, entry.item))}"
            )

    move_lines: list[str] = []
    if summary.has_changes:
        if summary.moves:
            move_lines.append(bold("Kapansin-pansing galaw vs nakaraang linggo"))
            for move in summary.moves:
                arrow = "▲" if (move.pct_change or 0) > 0 else "▼"
                move_lines.append(
                    f"{esc('•')} {esc(move.item)} ({esc(short_market(move.market))}): "
                    f"{esc(price_text(move.latest, move.item))} "
                    f"{esc(arrow + f'{abs(move.pct_change or 0.0):.1f}%')}"
                )
        else:
            move_lines.append(esc("Walang malaking galaw ngayong linggo."))
    else:
        move_lines.append(
            esc("Unang ulat pa lang ito; walang paghahambing sa nakaraang linggo.")
        )

    tail = [esc(SOURCE_LINE), esc(DISCLAIMER)]

    # Priority: header > Las Piñas > moves > cheapest is trimmed last, so all
    # 14 cheapest-market lines survive whenever possible. Length is counted
    # in characters (len(str)) and kept under MAX_CHARS.
    header = lines
    body_sections: list[list[str]] = [las_lines, move_lines, cheap_lines]
    while True:
        full = header + [ln for sec in body_sections for ln in ([""] + sec if sec else [])] + [""] + tail
        text = "\n".join(full)
        if len(text) < MAX_CHARS:
            # A moves header with no items left is confusing; drop it so the
            # section disappears cleanly instead of dangling.
            if summary.has_changes and summary.moves and len(move_lines) == 1:
                move_lines.clear()
                continue
            return text
        # Drop one line from the lowest-priority non-minimal section:
        # moves first, then Las Piñas lines, cheapest last.
        trimmed = False
        for section in (move_lines, las_lines, cheap_lines):
            keep = 1 if section is move_lines else 2
            if len(section) > keep:
                section.pop()
                trimmed = True
                break
        if not trimmed:  # pragma: no cover - defensive
            return text[:MAX_CHARS]


def _pct_for(summary: WeeklySummary, market: str, item: str) -> float | None:
    new = (summary.latest.get(item) or {}).get(market)
    old = (summary.baseline.get(item) or {}).get(market)
    if new is None or old is None or old == 0:
        return None
    return round((new - old) / old * 100.0, 1)
