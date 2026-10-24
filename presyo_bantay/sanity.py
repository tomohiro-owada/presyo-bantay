"""Sanity checks that catch upstream layout changes."""

from __future__ import annotations

MIN_MARKETS = 20
MIN_COMMODITIES_PER_MARKET = 20

#: Range layout (pre-2026-08-18): only ~13 commodities per market.
RANGE_MIN_COMMODITIES_PER_MARKET = 10

#: Core commodities required by the range layout (old PDFs have no La
#: Huerta market and no Bangus, so the single-layout core list cannot
#: apply).
RANGE_CORE_COMMODITIES = [
    "Well-milled Rice (Local)",
    "Egg (Medium)",
]

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


def run_sanity_checks(markets: dict, layout: str = "single") -> None:
    """Validate parsed output; raise :class:`LayoutChangedError` on failure.

    ``layout`` selects the thresholds: ``"single"`` keeps the original
    behaviour (20+ markets, 20+ commodities per market, all 4 markets of
    interest with the full core list); ``"range"`` requires 20+ markets,
    10+ commodities for most markets, and the range core commodities
    (``Well-milled Rice (Local)``, ``Egg (Medium)``) present in most
    markets. Old range PDFs lack ``La Huerta`` entirely, so markets of
    interest are not hard-required for that layout. Any other *layout*
    raises :class:`LayoutChangedError`.
    """
    if layout == "range":
        _run_range_sanity_checks(markets)
        return
    if layout != "single":
        raise LayoutChangedError(
            f"Layout may have changed: unknown layout {layout!r}"
        )
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


def _run_range_sanity_checks(markets: dict) -> None:
    """Sanity checks for the range layout (fewer commodities)."""
    problems: list[str] = []

    if len(markets) < MIN_MARKETS:
        problems.append(
            f"only {len(markets)} markets parsed (minimum {MIN_MARKETS})"
        )

    n_full = sum(
        1
        for items in markets.values()
        if len(items) >= RANGE_MIN_COMMODITIES_PER_MARKET
    )
    # "Most markets" must each hold 10+ commodities.
    if markets and n_full < (len(markets) + 1) // 2:
        thin = {
            name: len(items)
            for name, items in markets.items()
            if len(items) < RANGE_MIN_COMMODITIES_PER_MARKET
        }
        worst = sorted(thin.items(), key=lambda kv: kv[1])[:5]
        detail = ", ".join(f"{name} ({count})" for name, count in worst)
        problems.append(
            f"only {n_full}/{len(markets)} market(s) have at least "
            f"{RANGE_MIN_COMMODITIES_PER_MARKET} commodities: {detail}"
        )

    for core in RANGE_CORE_COMMODITIES:
        n_have = sum(1 for items in markets.values() if core in items)
        if not markets or n_have < (len(markets) + 1) // 2:
            problems.append(
                f"core commodity {core!r} present in only "
                f"{n_have}/{len(markets)} markets"
            )

    if problems:
        raise LayoutChangedError(
            "Layout may have changed: " + "; ".join(problems)
        )
