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


#: Short labels for long commodity names (cheapest/moves sections only).
#: Rice names stay full to avoid confusion.
_SHORT_ITEMS = {
    "Fresh Pork (Kasim) Local": "Pork Kasim",
    "Fresh Pork Liempo (Local)": "Pork Liempo",
    "Fresh Whole Chicken": "Whole Chicken",
    "Galunggong (Local)": "Galunggong",
    "Red Onion (Local)": "Red Onion",
}


def short_item(item: str) -> str:
    """Short display name for a commodity (cheapest/moves sections)."""
    return _SHORT_ITEMS.get(item, item)


def _change_suffix(pct: float | None) -> str:
    # Unchanged items show no arrow: this keeps the Las Piñas section short
    # so all 14 cheapest-market lines survive under the character cap.
    if pct is None or pct == 0.0:
        return ""
    arrow = "▲" if pct > 0 else "▼"
    return f" {arrow}{abs(pct):.1f}%"


def compact_price(price: float | None, item: str) -> str:
    """Compact price: ``₱59.00`` (``₱8.50/pc`` for eggs to keep the unit)."""
    if price is None:
        return "wala"
    if " ".join(item.lower().split()) == "egg (medium)":
        return f"{format_price(price)}/pc"
    return format_price(price)


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
        unavailable: list[str] = []
        for item in summary.items:
            price = (summary.latest.get(item) or {}).get(las_pinas)
            if price is None:
                unavailable.append(item)
                continue
            suffix = ""
            if summary.has_changes:
                pct = _pct_for(summary, las_pinas, item)
                suffix = _change_suffix(pct)
            las_lines.append(
                f"{esc('•')} {esc(item)}: {esc(compact_price(price, item))}{esc(suffix)}"
            )
        if unavailable:
            las_lines.append(
                f"{esc('•')} {esc('Wala ngayon: ' + ', '.join(unavailable))}"
            )

    cheap_lines: list[str] = [bold("Pinakamurang palengke")]
    for entry in summary.cheapest:
        label = short_item(entry.item)
        if entry.price is None or not entry.markets:
            cheap_lines.append(f"{esc('•')} {esc(label)}: {esc('wala')}")
        else:
            names = ", ".join(short_market(m) for m in entry.markets)
            cheap_lines.append(
                f"{esc('•')} {esc(label)}: {esc(names)} {esc(compact_price(entry.price, entry.item))}"
            )

    move_lines: list[str] = []
    if summary.has_changes:
        # Las Piñas moves are already inline above; the notable-moves section
        # shows only the OTHER markets, top 4 by |%|, in compact form.
        others = [m for m in summary.markets[1:] if m != las_pinas] if las_pinas else list(summary.markets)
        ranked: list[tuple[float, float, str, str, float | None]] = []
        for item in summary.items:
            for market in others:
                pct = _pct_for(summary, market, item)
                if pct is None or abs(pct) < 5.0:
                    continue
                latest_price = (summary.latest.get(item) or {}).get(market)
                ranked.append((abs(pct), pct, item, market, latest_price))
        ranked.sort(key=lambda t: t[0], reverse=True)
        top = ranked[:4]
        if top:
            move_lines.append(bold("Kapansin-pansing galaw"))
            for _, pct, item, market, latest_price in top:
                arrow = "▲" if pct > 0 else "▼"
                move_lines.append(
                    f"{esc('•')} {esc(short_item(item))} ({esc(short_market(market))}) "
                    f"{esc(arrow + f'{abs(pct):.1f}%')} {esc('→')} {esc(format_price(latest_price))}"
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
            if len(move_lines) == 1 and "Kapansin-pansing galaw" in move_lines[0]:
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
