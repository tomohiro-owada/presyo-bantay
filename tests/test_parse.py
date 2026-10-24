"""PDF parsing tests against the saved Oct 23 2026 fixture (offline)."""

import json
import os

import pytest

from presyo_bantay.cli import build_parser, main
from presyo_bantay.pdfparse import infer_unit, normalize_commodity, parse_pdf, parse_price

from .conftest import SAMPLE_PDF, SAMPLE_SOURCE_URL

LAS_PINAS = "New Las Piñas City Public Market"


def test_fixture_pdf_exists():
    assert os.path.exists(SAMPLE_PDF)


def test_las_pinas_core_prices():
    markets, _date = parse_pdf(SAMPLE_PDF)
    assert LAS_PINAS in markets
    entry = markets[LAS_PINAS]
    assert entry["Premium Rice (Imported)"]["price"] == pytest.approx(59.00)
    assert entry["Bangus"]["price"] == pytest.approx(270.0)
    assert entry["Egg (Medium)"]["price"] == pytest.approx(8.50)
    assert entry["Egg (Medium)"]["unit"] == "per piece"


def test_not_available_becomes_none():
    assert parse_price("NOT AVAILABLE") is None
    assert parse_price("not available") is None
    assert parse_price("") is None
    assert parse_price(None) is None
    assert parse_price("  ") is None
    markets, _date = parse_pdf(SAMPLE_PDF)
    # La Huerta has NOT AVAILABLE for Well-milled Rice (Imported) on page 1.
    la_huerta = markets["La Huerta Market/Parañaque"]
    assert la_huerta["Well-milled Rice (Imported)"]["price"] is None


def test_commodity_headers_normalized():
    assert normalize_commodity("Premium Rice\n(Imported)") == "Premium Rice (Imported)"
    assert normalize_commodity("*Egg (Medium)") == "Egg (Medium)"
    assert normalize_commodity("Fresh Whole\nChicken") == "Fresh Whole Chicken"


def test_markets_merged_across_pages():
    markets, _date = parse_pdf(SAMPLE_PDF)
    # 10 (page 1) + 9 (page 2) + 9 (page 3) commodities per market.
    assert len(markets[LAS_PINAS]) == 28
    assert "Ampalaya" in markets[LAS_PINAS]
    assert "Eggplant" in markets[LAS_PINAS]
    assert "Fresh Whole Chicken" in markets[LAS_PINAS]


def test_parse_command_writes_json(tmp_path):
    out_dir = str(tmp_path / "json")
    rc = main(["parse", SAMPLE_PDF, "--source-url", SAMPLE_SOURCE_URL, "--out-dir", out_dir])
    assert rc == 0
    out_path = os.path.join(out_dir, "2026-10-23.json")
    assert os.path.exists(out_path)
    with open(out_path, encoding="utf-8") as handle:
        payload = json.load(handle)
    assert payload["date"] == "2026-10-23"
    assert payload["source_url"] == SAMPLE_SOURCE_URL
    las = payload["markets"][LAS_PINAS]
    assert las["Premium Rice (Imported)"]["price"] == pytest.approx(59.00)
    assert las["Bangus"]["price"] == pytest.approx(270.0)
    assert las["Egg (Medium)"]["price"] == pytest.approx(8.50)
    assert las["Egg (Medium)"]["unit"] == "per piece"


def test_parse_markets_of_interest_filter(tmp_path):
    out_dir = str(tmp_path / "json")
    rc = main(
        [
            "parse",
            SAMPLE_PDF,
            "--source-url",
            SAMPLE_SOURCE_URL,
            "--out-dir",
            out_dir,
            "--markets-of-interest",
        ]
    )
    assert rc == 0
    with open(os.path.join(out_dir, "2026-10-23.json"), encoding="utf-8") as handle:
        payload = json.load(handle)
    names = list(payload["markets"])
    assert len(names) == 4
    joined = " | ".join(names)
    assert "Las Piñas" in joined
    assert "Alabang" in joined
    assert "La Huerta" in joined
    assert "Bicutan" in joined


def test_parse_source_url_defaults_to_null_without_meta(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)  # no data/raw/*.meta.json visible here
    out_dir = str(tmp_path / "json")
    rc = main(["parse", SAMPLE_PDF, "--out-dir", out_dir])
    assert rc == 0
    with open(os.path.join(out_dir, "2026-10-23.json"), encoding="utf-8") as handle:
        payload = json.load(handle)
    assert payload["source_url"] is None


def test_cli_parser_defaults():
    parser = build_parser()
    args = parser.parse_args(["parse", "x.pdf"])
    assert args.source_url is None
    assert args.markets_of_interest is False


def test_eggplant_is_per_kg_not_per_piece():
    """Regression: 'Eggplant' must not match the egg per-piece rule."""
    assert infer_unit("Eggplant") == "per kg"
    assert infer_unit("*Egg (Medium)") == "per piece"
    # Fallback for an already-normalized egg header keeps working.
    assert infer_unit("Egg (Medium)") == "per piece"
    markets, _date = parse_pdf(SAMPLE_PDF)
    entry = markets[LAS_PINAS]["Eggplant"]
    assert entry["price"] == pytest.approx(80.0)
    assert entry["unit"] == "per kg"


def test_egg_medium_still_per_piece():
    markets, _date = parse_pdf(SAMPLE_PDF)
    entry = markets[LAS_PINAS]["Egg (Medium)"]
    assert entry["unit"] == "per piece"
    assert entry["price"] == pytest.approx(8.50)


def test_only_egg_medium_is_per_piece():
    markets, _date = parse_pdf(SAMPLE_PDF)
    per_piece = {
        (market, commodity)
        for market, items in markets.items()
        for commodity, details in items.items()
        if details["unit"] == "per piece"
    }
    assert per_piece, "expected at least one per-piece entry"
    assert {commodity for _, commodity in per_piece} == {"Egg (Medium)"}
