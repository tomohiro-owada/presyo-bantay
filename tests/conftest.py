"""Shared fixtures/paths for the Presyo Bantay test suite (offline only)."""

import os

TESTS_DIR = os.path.dirname(os.path.abspath(__file__))
FIXTURES_DIR = os.path.join(TESTS_DIR, "fixtures")
LISTING_HTML = os.path.join(FIXTURES_DIR, "price-monitoring-listing.html")
SAMPLE_PDF = os.path.join(FIXTURES_DIR, "Price-Monitoring-October-23-2026.pdf")
SAMPLE_SOURCE_URL = (
    "https://www.da.gov.ph/wp-content/uploads/2026/10/"
    "Price-Monitoring-October-23-2026.pdf"
)
