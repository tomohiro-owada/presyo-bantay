"""Renderer tests: formats, escaping, length cap, required lines."""

import html as _html
import json

import pytest

from presyo_bantay.render import (
    DISCLAIMER,
    MAX_CHARS,
    SOURCE_LINE,
    SYNTHETIC_LINE,
    escape_markdown_v2,
    format_price,
    render_summary,
    unit_suffix,
)
from presyo_bantay.summary import build_summary, load_snapshots, select_snapshots

from .conftest import SYNTHETIC_DIR

LAS_PINAS = "New Las Piñas City Public Market"


def _fixture_summary(week_ending="2026-10-24"):
    snapshots = load_snapshots(SYNTHETIC_DIR)
    latest, baseline = select_snapshots(snapshots, week_ending)
    return build_summary(latest, baseline, week_ending)


def test_html_is_default_and_under_limit():
    text = render_summary(_fixture_summary(), fmt="html")
    assert len(text) < MAX_CHARS
    assert len(text) < 1500


def test_markdownv2_under_limit():
    text = render_summary(_fixture_summary(), fmt="markdownv2")
    assert len(text) < 1500


def test_source_disclaimer_exact_html():
    text = render_summary(_fixture_summary(), fmt="html")
    assert f"\n{SOURCE_LINE}\n" in text or text.endswith(SOURCE_LINE) or SOURCE_LINE in text.splitlines()
    assert DISCLAIMER in text


def test_source_disclaimer_present_markdownv2():
    text = render_summary(_fixture_summary(), fmt="markdownv2")
    assert escape_markdown_v2(SOURCE_LINE) in text
    assert escape_markdown_v2(DISCLAIMER) in text


def test_header_week_ending_and_las_pinas_section():
    text = render_summary(_fixture_summary(), fmt="html")
    assert "Okt 24, 2026" in text  # friendly Taglish header
    assert "2026-10-23" in text  # latest snapshot date
    assert "Presyo sa New Las Piñas City Public Market (DA data, 2026-10-23)" in text
    assert "Las Piñas (Las Piñas prices)" not in text
    assert "Las Piñas" in text
    assert "₱59.00" in text  # Premium Rice (Imported), Las Piñas
    assert "₱8.50/pc" in text  # Egg per piece


def test_units_egg_vs_kg():
    assert unit_suffix("Egg (Medium)") == "/pc"
    assert unit_suffix("Bangus") == "/kg"
    assert format_price(59.0) == "₱59.00"
    assert format_price(8.5) == "₱8.50"
    assert format_price(None) == "wala"


def test_notable_moves_arrows_and_pct():
    text = render_summary(_fixture_summary(), fmt="html")
    assert "▲" in text and "▼" in text
    assert "12.5%" in text  # Bangus Las Piñas +12.5%


def test_notable_moves_present_in_sample():
    """Moves section survives truncation: other-market top moves, compact."""
    text = render_summary(_fixture_summary(), fmt="html")
    assert len(text) < 1500
    assert "Kapansin-pansing galaw" in text
    # Exact compact example from the spec.
    assert "• Tilapia (La Huerta) ▲14.3% → ₱160.00" in text
    moves_start = text.index("Kapansin-pansing galaw")
    cheap_start = text.index("Pinakamurang palengke")
    moves_section = text[moves_start:cheap_start]
    # Only the OTHER 3 markets (Las Piñas moves are inline above).
    assert "Las Piñas" not in moves_section
    assert moves_section.count("•") <= 4
    assert moves_section.count("•") >= 1
    assert "→ ₱" in moves_section
    # Short labels in moves/cheapest, rice names stay full.
    assert "Pork Kasim" in text
    assert "Premium Rice (Imported)" in text
    # Markdown variant also fits with moves present.
    md_text = render_summary(_fixture_summary(), fmt="markdownv2")
    assert len(md_text) < 1500
    assert "Kapansin-pansing galaw" in md_text.replace("\\-", "-").replace("\\ ", " ")


def test_cheapest_lines_present():
    text = render_summary(_fixture_summary(), fmt="html")
    assert "Alabang" in text
    assert "₱240.00" in text  # cheapest Bangus


def test_all_14_cheapest_lines_fit():
    """All KEY_ITEMS appear in the cheapest section (message stays short)."""
    from presyo_bantay.summary import KEY_ITEMS

    from presyo_bantay.render import short_item

    text = render_summary(_fixture_summary(), fmt="html")
    assert len(text) < 1500  # characters, not bytes
    # The message contains multibyte chars (₱/•/—/🛒): byte length exceeds the
    # cap while character length stays under it, proving len(str) counting.
    assert len(text.encode("utf-8")) > len(text)
    cheap_start = text.index("Pinakamurang palengke")
    cheap_section = text[cheap_start:]
    for item in KEY_ITEMS:
        assert short_item(item) in cheap_section, f"cheapest line missing: {item}"
    # All 14 cheapest lines survive (header + 14 bullets).
    assert cheap_section.count("•") >= 14


def test_unavailable_shown_as_wala():
    text = render_summary(_fixture_summary(), fmt="html")
    assert "wala" in text.lower()
    assert "Wala ngayon: Well-milled Rice (Local), Regular Milled (Local)" in text
    assert "n/a" not in text


def test_short_market_labels():
    text = render_summary(_fixture_summary(), fmt="html")
    for label in ("Las Piñas", "Alabang", "La Huerta", "Bicutan"):
        assert label in text


def test_unchanged_items_show_no_arrow():
    """Shortened Las Piñas section: no ``→0.0%`` for unchanged items."""
    text = render_summary(_fixture_summary(), fmt="html")
    assert "→0.0%" not in text
    # Las Piñas inline lines never use → (moves section does).
    las_part = text.split("Kapansin-pansing galaw")[0]
    assert "→" not in las_part
    # ... but real moves still show arrows computed in code.
    assert "▲14.9%" in text  # Well-milled Rice (Imported), Las Piñas
    assert "▼9.1%" in text  # Premium Rice (Local), Las Piñas
    # Moves section uses compact → format.
    assert "→ ₱" in text


def test_no_baseline_shows_no_changes():
    text = render_summary(_fixture_summary("2026-10-16"), fmt="html")
    assert "▲" not in text and "▼" not in text and "→" not in text


def test_synthetic_warning_only_when_synthetic():
    # Baseline week is synthetic here (real latest + synthetic baseline).
    real_latest_week = render_summary(_fixture_summary("2026-10-24"), fmt="html")
    assert SYNTHETIC_LINE in real_latest_week
    synth_week = render_summary(_fixture_summary("2026-10-16"), fmt="html")
    assert SYNTHETIC_LINE in synth_week
    # All-real snapshots -> no warning.
    markets = {
        LAS_PINAS: {
            "Bangus": {"spec": None, "unit": "per kg", "price": 270.0},
        }
    }
    summary = build_summary(
        {"date": "2026-10-23", "markets": markets},
        {"date": "2026-10-16", "markets": markets},
        "2026-10-24",
        items=["Bangus"],
    )
    assert SYNTHETIC_LINE not in render_summary(summary, fmt="html")


def test_no_baseline_message():
    text = render_summary(_fixture_summary("2026-10-16"), fmt="html")
    assert "Unang ulat" in text


def test_html_escaping():
    markets = {
        LAS_PINAS: {
            "Fish & Chips <mix>": {"spec": None, "unit": "per kg", "price": 10.0},
            "Egg (Medium)": {"spec": "Medium", "unit": "per piece", "price": 8.5},
        }
    }
    latest = {"date": "2026-10-23", "markets": markets}
    summary = build_summary(
        latest, None, "2026-10-23", items=["Fish & Chips <mix>", "Egg (Medium)"]
    )
    text = render_summary(summary, fmt="html")
    assert "Fish &amp; Chips &lt;mix&gt;" in text
    assert "Fish & Chips <mix>" not in text


def test_markdownv2_escaping_all_specials():
    specials = "_*[]()~`>#+-=|{}.!"
    for char in specials:
        assert escape_markdown_v2(f"a{char}b") == f"a\\{char}b"
    markets = {
        LAS_PINAS: {
            "A_B": {"spec": None, "unit": "per kg", "price": 10.0},
        }
    }
    latest = {"date": "2026-10-23", "markets": markets}
    summary = build_summary(latest, None, "2026-10-23", items=["A_B"])
    text = render_summary(summary, fmt="markdownv2")
    assert "A\\_B" in text
    assert "\nA_B" not in text


def test_unknown_format_rejected():
    with pytest.raises(ValueError):
        render_summary(_fixture_summary(), fmt="bogus")


def test_html_escape_covers_dynamic_text():
    # html.escape must be applied: no raw '<' from dynamic content survives.
    text = render_summary(_fixture_summary(), fmt="html")
    stripped = text.replace("<b>", "").replace("</b>", "")
    assert "<" not in stripped and ">" not in stripped
    assert _html.escape("a&b") == "a&amp;b"
