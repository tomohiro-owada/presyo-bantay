"""Sanity checks that catch upstream layout changes."""

from __future__ import annotations

MIN_MARKETS = 20
MIN_COMMODITIES_PER_MARKET = 20

MARKETS_OF_INTEREST = [
    "New Las Piñas City Public Market",
    "Alabang Central Market",
    "La Huerta",  # matched by substring: "La Huerta Market/Parañaque"
    "Bicutan",  # matched by substring: "Bicutan Market"
]

# Commodity keys (normalized) that must exist for the markets of interest.
CORE_COMMODITIES = [
    "Premium Rice (Imported)",
    "Well-milled Rice (Imported)",
    "Premium Rice (Local)",
    "Well-milled Rice (Local)",
    "Regular Milled (Local)",
    "Bangus",
    "Egg (Medium)",
]


class LayoutChangedError(Exception):
    """Raised when the parsed PDF no longer looks like the expected layout."""


def _norm(name: str) -> str:
    return " ".join(name.lower().split())


def match_market(markets: dict, wanted: str) -> str | None:
    """Find the actual market key matching *wanted* (substring, normalized)."""
    needle = _norm(wanted)
    for actual in markets:
        hay = _norm(actual)
        if needle in hay or hay in needle:
            return actual
    return None


def run_sanity_checks(markets: dict) -> None:
    """Validate parsed output; raise :class:`LayoutChangedError` on failure."""
    problems: list[str] = []

    if len(markets) < MIN_MARKETS:
        problems.append(
            f"only {len(markets)} markets parsed (minimum {MIN_MARKETS})"
        )

    thin = {
        name: len(items)
        for name, items in markets.items()
        if len(items) < MIN_COMMODITIES_PER_MARKET
    }
    if thin:
        worst = sorted(thin.items(), key=lambda kv: kv[1])[:5]
        detail = ", ".join(f"{name} ({count})" for name, count in worst)
        problems.append(
            f"{len(thin)} market(s) have fewer than "
            f"{MIN_COMMODITIES_PER_MARKET} commodities: {detail}"
        )

    for wanted in MARKETS_OF_INTEREST:
        actual = match_market(markets, wanted)
        if actual is None:
            problems.append(f"market of interest not found: {wanted!r}")
            continue
        missing = [c for c in CORE_COMMODITIES if c not in markets[actual]]
        if missing:
            problems.append(
                f"market {actual!r} is missing core commodities: "
                + ", ".join(missing)
            )

    if problems:
        raise LayoutChangedError(
            "Layout may have changed: " + "; ".join(problems)
        )
