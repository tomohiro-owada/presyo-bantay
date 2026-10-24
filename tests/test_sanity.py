"""Sanity-check tests (offline)."""

import pytest

from presyo_bantay.pdfparse import parse_pdf
from presyo_bantay.sanity import LayoutChangedError, run_sanity_checks

from .conftest import SAMPLE_PDF


def test_fixture_passes_sanity_checks():
    markets, _date = parse_pdf(SAMPLE_PDF)
    run_sanity_checks(markets)  # must not raise


def test_sanity_fails_on_empty_result():
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks({})


def test_sanity_fails_when_market_of_interest_missing():
    markets, _date = parse_pdf(SAMPLE_PDF)
    trimmed = {
        name: items
        for name, items in markets.items()
        if "Piñas" not in name and "Alabang" not in name
    }
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks(trimmed)


def test_sanity_fails_when_core_commodity_missing():
    tiny = {"New Las Piñas City Public Market": {"Bangus": {"price": 1.0}}}
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks(tiny)


def test_sanity_fails_on_thin_market():
    markets, _date = parse_pdf(SAMPLE_PDF)
    first = next(iter(markets))
    markets[first] = dict(list(markets[first].items())[:3])
    with pytest.raises(LayoutChangedError, match="Layout may have changed"):
        run_sanity_checks(markets)
