package parse

import (
	"strings"
	"testing"
)

func mustParseFixture(t *testing.T, name string) map[string]map[string]Entry {
	t.Helper()
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return markets
}

func TestFixturePassesSanityChecks(t *testing.T) {
	markets := mustParseFixture(t, "Price-Monitoring-October-23-2026.pdf")
	if err := RunSanityChecks(markets, "single"); err != nil {
		t.Fatalf("sanity failed: %v", err)
	}
}

func TestSanityFailsOnEmptyResult(t *testing.T) {
	err := RunSanityChecks(map[string]map[string]Entry{}, "single")
	if err == nil || !strings.Contains(err.Error(), "Layout may have changed") {
		t.Fatalf("got %v", err)
	}
	if _, ok := err.(*LayoutChangedError); !ok {
		t.Fatalf("wrong type %T", err)
	}
}

func TestSanityFailsWhenMarketOfInterestMissing(t *testing.T) {
	markets := mustParseFixture(t, "Price-Monitoring-October-23-2026.pdf")
	trimmed := map[string]map[string]Entry{}
	for name, items := range markets {
		if strings.Contains(name, "Piñas") || strings.Contains(name, "Alabang") {
			continue
		}
		trimmed[name] = items
	}
	// Removing two markets still leaves >= 20, so the failure must come
	// from the missing markets of interest.
	if err := RunSanityChecks(trimmed, "single"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
}

func TestSanityFailsWhenCoreCommodityMissing(t *testing.T) {
	tiny := map[string]map[string]Entry{
		"New Las Piñas City Public Market": {"Bangus": {Price: fptr(1.0)}},
	}
	if err := RunSanityChecks(tiny, "single"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
}

func TestSanityFailsOnThinMarket(t *testing.T) {
	markets := mustParseFixture(t, "Price-Monitoring-October-23-2026.pdf")
	first := ""
	for name := range markets {
		first = name
		break
	}
	i := 0
	thin := map[string]Entry{}
	for c, e := range markets[first] {
		if i < 3 {
			thin[c] = e
		}
		i++
	}
	markets[first] = thin
	if err := RunSanityChecks(markets, "single"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
}

func TestSanityUnknownLayout(t *testing.T) {
	if err := RunSanityChecks(map[string]map[string]Entry{}, "bogus"); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestMatchMarket(t *testing.T) {
	markets := mustParseFixture(t, "Price-Monitoring-October-23-2026.pdf")
	if got := MatchMarket(markets, "La Huerta"); got == "" {
		t.Fatal("La Huerta not matched")
	}
	if got := MatchMarket(markets, "Bicutan"); got != "Bicutan Market" {
		t.Fatalf("Bicutan matched %q", got)
	}
	if got := MatchMarket(markets, "NoSuchMarket"); got != "" {
		t.Fatalf("bogus matched %q", got)
	}
}

func TestRunBackfillChecksSingleOK(t *testing.T) {
	markets := mustParseFixture(t, "Price-Monitoring-October-23-2026.pdf")
	warnings, err := RunBackfillChecks(markets, "single")
	if err != nil {
		t.Fatalf("backfill checks failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestRunBackfillChecksRangeOK(t *testing.T) {
	markets := mustParseFixture(t, "Price-Monitoring-August-14-2026.pdf")
	warnings, err := RunBackfillChecks(markets, "range")
	if err != nil {
		t.Fatalf("range backfill checks failed: %v", err)
	}
	// Old range PDFs lack La Huerta entirely: warnings, not failure.
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "La Huerta") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want La Huerta warning, got %v", warnings)
	}
}

func TestRunBackfillChecksHardFailure(t *testing.T) {
	if _, err := RunBackfillChecks(map[string]map[string]Entry{}, "single"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
	if _, err := RunBackfillChecks(map[string]map[string]Entry{}, "range"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
	if _, err := RunBackfillChecks(map[string]map[string]Entry{}, "bogus"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
}

func TestRunBackfillChecksMissingCore(t *testing.T) {
	// No rice, no Bangus, no egg: hard failure for single layout.
	markets := map[string]map[string]Entry{}
	for i := 0; i < 20; i++ {
		name := "Market " + itoa(i)
		items := map[string]Entry{}
		for j := 0; j < 20; j++ {
			items["Tomato"] = Entry{Price: fptr(1.0)}
		}
		markets[name] = items
	}
	// Each market has only 1 commodity (< 20) -> thin failure too.
	if _, err := RunBackfillChecks(markets, "single"); err == nil {
		t.Fatal("want LayoutChangedError, got nil")
	}
}

func TestRoundNMatchesPython(t *testing.T) {
	// round(53.456, 2) banker's on binary value.
	checkPrice(t, "round2 53.456", fptr(RoundN(53.456, 2)), 53.46)
	checkPrice(t, "round1 10.05ish", fptr(RoundN(10.0, 1)), 10.0)
	checkPrice(t, "round4 fx", fptr(RoundN(62.968, 4)), 62.968)
	// Half-even: round(2.675, 2) == 2.67 in Python.
	checkPrice(t, "round2 2.675", fptr(RoundN(2.675, 2)), 2.67)
	if got := RoundN(2.675, 2); got != Round2(2.675) {
		t.Fatalf("RoundN(x,2) != Round2(x): %v", got)
	}
}
