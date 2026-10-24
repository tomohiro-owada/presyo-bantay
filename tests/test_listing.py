"""Listing-page tests (offline; fixture HTML only, no network)."""

from presyo_bantay.listing import (
    find_latest_from_html,
    find_latest_from_urls,
    parse_filename_date,
)

from .conftest import LISTING_HTML


def _read_fixture_html() -> str:
    with open(LISTING_HTML, encoding="utf-8", errors="replace") as handle:
        return handle.read()


def test_listing_picks_latest_october_23_2026():
    found = find_latest_from_html(_read_fixture_html())
    assert found is not None
    url, date = found
    assert url.endswith("Price-Monitoring-October-23-2026.pdf")
    assert date.isoformat() == "2026-10-23"


def test_listing_excludes_cigarette_links():
    found = find_latest_from_html(_read_fixture_html())
    assert found is not None
    url, _date = found
    assert "Cigarette" not in url


def test_listing_excludes_daily_index_and_weekly_average():
    urls = [
        "https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-October-23-2026.pdf",
        "https://www.da.gov.ph/wp-content/uploads/2026/10/Daily-Price-Index-October-23-2026.pdf",
        "https://www.da.gov.ph/wp-content/uploads/2026/10/Weekly-Average-Prices-October-12-18-2026.pdf",
        "https://www.da.gov.ph/wp-content/uploads/2026/10/Daily-Cigarette-Price-Monitoring-October-23-2026.pdf",
    ]
    found = find_latest_from_urls(urls)
    assert found is not None
    url, _date = found
    assert url.endswith("Price-Monitoring-October-23-2026.pdf")


def test_revised_beats_non_revised_same_date():
    base = "https://www.da.gov.ph/wp-content/uploads/2026/10/{}"
    urls = [
        base.format("Price-Monitoring-October-13-2026.pdf"),
        base.format("Revised-Price-Monitoring-October-13-2026.pdf"),
    ]
    found = find_latest_from_urls(urls)
    assert found is not None
    url, _date = found
    assert url.endswith("Revised-Price-Monitoring-October-13-2026.pdf")
    # Order should not matter.
    found = find_latest_from_urls(list(reversed(urls)))
    assert found is not None
    assert found[0].endswith("Revised-Price-Monitoring-October-13-2026.pdf")


def test_suffix_ordering_higher_suffix_wins():
    base = "https://www.da.gov.ph/wp-content/uploads/2026/10/{}"
    urls = [
        base.format("Price-Monitoring-October-23-2026.pdf"),
        base.format("Price-Monitoring-October-23-2026-1.pdf"),
        base.format("Price-Monitoring-October-23-2026-2.pdf"),
    ]
    found = find_latest_from_urls(urls)
    assert found is not None
    assert found[0].endswith("Price-Monitoring-October-23-2026-2.pdf")
    assert parse_filename_date("Price-Monitoring-October-23-2026-2.pdf").isoformat() == "2026-10-23"


def test_parse_filename_date_variants():
    assert parse_filename_date("Price-Monitoring-October-23-2026.pdf").isoformat() == "2026-10-23"
    assert parse_filename_date("Revised-Price-Monitoring-October-13-2026.pdf").isoformat() == "2026-10-13"
    assert parse_filename_date("Price-Monitoring-December-6-2023-Revised.pdf").isoformat() == "2023-12-06"
    assert parse_filename_date("Daily-Cigarette-Price-Monitoring-October-23-2026.pdf") is None
    assert parse_filename_date("Daily-Price-Index-October-23-2026.pdf") is None
    assert parse_filename_date("not-a-report.pdf") is None
