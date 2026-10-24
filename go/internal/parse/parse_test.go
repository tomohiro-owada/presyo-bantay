package parse

import (
	"path/filepath"
	"runtime"
	"testing"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	// src is go/internal/parse/parse_test.go; fixtures live in tests/fixtures.
	p := filepath.Join(filepath.Dir(src), "..", "..", "..", "tests", "fixtures", name)
	return filepath.Clean(p)
}

func fptr(f float64) *float64 { return &f }

func checkPrice(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: got nil, want %v", what, want)
	}
	diff := *got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Fatalf("%s: got %v, want %v", what, *got, want)
	}
}

func TestNormalizeCommodity(t *testing.T) {
	cases := map[string]string{
		"Premium Rice\n(Imported)": "Premium Rice (Imported)",
		"*Egg (Medium)":            "Egg (Medium)",
		"Fresh Whole\nChicken":     "Fresh Whole Chicken",
	}
	for in, want := range cases {
		if got := NormalizeCommodity(in); got != want {
			t.Errorf("NormalizeCommodity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePrice(t *testing.T) {
	for _, s := range []string{"NOT AVAILABLE", "not available", "", "  "} {
		v := s
		if got := ParsePrice(&v); got != nil {
			t.Errorf("ParsePrice(%q) = %v, want nil", s, *got)
		}
	}
	if got := ParsePrice(nil); got != nil {
		t.Errorf("ParsePrice(nil) = %v, want nil", *got)
	}
	v := "45.00"
	checkPrice(t, "ParsePrice 45.00", ParsePrice(&v), 45.00)
}

func TestInferUnit(t *testing.T) {
	if got := InferUnit("Eggplant"); got != "per kg" {
		t.Errorf("InferUnit(Eggplant) = %q, want per kg", got)
	}
	if got := InferUnit("*Egg (Medium)"); got != "per piece" {
		t.Errorf("InferUnit(*Egg) = %q, want per piece", got)
	}
	if got := InferUnit("Egg (Medium)"); got != "per piece" {
		t.Errorf("InferUnit(Egg (Medium)) = %q, want per piece", got)
	}
}

func TestInferSpec(t *testing.T) {
	cases := []struct {
		in   string
		want *string
	}{
		{"Premium Rice (Imported)", strp("Imported")},
		{"Egg (Medium)", strp("Medium")},
		{"Bangus", nil},
		{"Fresh Pork Kasim/Pigue", nil},
	}
	for _, c := range cases {
		got := InferSpec(c.in)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("InferSpec(%q) = %v, want %v", c.in, strptr(got), strptr(c.want))
		}
	}
}

func strp(s string) *string { return &s }

func strptr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestParseRangePriceCases(t *testing.T) {
	f := func(s string) (p, l, h *float64) { return ParseRangePrice(&s) }
	p, l, h := f("45.00")
	checkPrice(t, "single price", p, 45.00)
	checkPrice(t, "single low", l, 45.00)
	checkPrice(t, "single high", h, 45.00)

	p, l, h = f("150.00-160.00")
	checkPrice(t, "range price", p, 155.00)
	checkPrice(t, "range low", l, 150.00)
	checkPrice(t, "range high", h, 160.00)

	p, l, h = f("300.00 - 350.00")
	checkPrice(t, "spaced price", p, 325.00)

	p, l, h = f("45.00 -50.00")
	checkPrice(t, "asym price", p, 47.50)

	p, l, h = f("100.00 -100.00")
	checkPrice(t, "flat price", p, 100.00)
	checkPrice(t, "flat low", l, 100.00)

	for _, s := range []string{"NOT AVAILABLE", ""} {
		v := s
		p, l, h := ParseRangePrice(&v)
		if p != nil || l != nil || h != nil {
			t.Errorf("ParseRangePrice(%q) = (%v,%v,%v), want nils", s, p, l, h)
		}
	}
	if p, l, h := ParseRangePrice(nil); p != nil || l != nil || h != nil {
		t.Errorf("ParseRangePrice(nil) non-nil")
	}
}

func TestRound2MatchesPython(t *testing.T) {
	// (8.25+8.8)/2 is just above the decimal midpoint in binary;
	// Python round() gives 8.53 (multiply-then-RoundToEven gives 8.52).
	lo, hi := 8.25, 8.8
	checkPrice(t, "bankers edge", fptr(Round2((lo+hi)/2.0)), 8.53)
	// Exact binary halfway cases go to even (Python round() semantics).
	checkPrice(t, "half even up", fptr(Round2(0.375)), 0.38)
	checkPrice(t, "half even down", fptr(Round2(0.365)), 0.36)
	checkPrice(t, "classic 2.675", fptr(Round2(2.675)), 2.67)
}

func TestParseDateFromFilename(t *testing.T) {
	d := ParseDateFromFilename("Price-Monitoring-October-23-2026.pdf")
	if d == nil || d.String() != "2026-10-23" {
		t.Fatalf("filename date = %v, want 2026-10-23", d)
	}
	d = ParseDateFromFilename("Revised-Price-Monitoring-October-13-2026.pdf")
	if d == nil || d.String() != "2026-10-13" {
		t.Fatalf("revised filename date = %v, want 2026-10-13", d)
	}
	if d := ParseDateFromFilename("no-date-here.pdf"); d != nil {
		t.Fatalf("bogus filename date = %v, want nil", d)
	}
}

func TestParseDateFromText(t *testing.T) {
	d := ParseDateFromText("Friday, 23 October 2026")
	if d == nil || d.String() != "2026-10-23" {
		t.Fatalf("text date = %v, want 2026-10-23", d)
	}
	d = ParseDateFromText("Friday, August 14, 2026")
	if d == nil || d.String() != "2026-08-14" {
		t.Fatalf("us text date = %v, want 2026-08-14", d)
	}
	if d := ParseDateFromText("no date here"); d != nil {
		t.Fatalf("bogus text date = %v, want nil", d)
	}
}

const lasPinas = "New Las Piñas City Public Market"

func TestSingleFixtureCorePrices(t *testing.T) {
	markets, _, layout, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if layout != "single" {
		t.Fatalf("layout = %q, want single", layout)
	}
	entry, ok := markets[lasPinas]
	if !ok {
		t.Fatalf("missing market %q", lasPinas)
	}
	checkPrice(t, "premium rice", entry["Premium Rice (Imported)"].Price, 59.00)
	checkPrice(t, "bangus", entry["Bangus"].Price, 270.0)
	checkPrice(t, "egg", entry["Egg (Medium)"].Price, 8.50)
	if entry["Egg (Medium)"].Unit != "per piece" {
		t.Errorf("egg unit = %q, want per piece", entry["Egg (Medium)"].Unit)
	}
}

func TestSingleFixtureNotAvailable(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	got := markets["La Huerta Market/Parañaque"]["Well-milled Rice (Imported)"].Price
	if got != nil {
		t.Errorf("la huerta well-milled = %v, want nil", *got)
	}
}

func TestSingleFixtureMarketsMerged(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(markets[lasPinas]) != 28 {
		t.Errorf("las piñas commodities = %d, want 28", len(markets[lasPinas]))
	}
	for _, c := range []string{"Ampalaya", "Eggplant", "Fresh Whole Chicken"} {
		if _, ok := markets[lasPinas][c]; !ok {
			t.Errorf("missing commodity %q", c)
		}
	}
}

func TestSingleFixtureEggplantPerKg(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	e := markets[lasPinas]["Eggplant"]
	checkPrice(t, "eggplant", e.Price, 80.0)
	if e.Unit != "per kg" {
		t.Errorf("eggplant unit = %q, want per kg", e.Unit)
	}
}

func TestSingleFixtureOnlyEggPerPiece(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, items := range markets {
		for c, e := range items {
			if e.Unit == "per piece" {
				got[c] = true
			}
		}
	}
	if len(got) != 1 || !got["Egg (Medium)"] {
		t.Errorf("per-piece set = %v, want only Egg (Medium)", got)
	}
}

func TestSingleEntriesCarryLowHigh(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	b := markets[lasPinas]["Bangus"]
	checkPrice(t, "bangus", b.Price, 270.0)
	checkPrice(t, "bangus low", b.PriceLow, 270.0)
	checkPrice(t, "bangus high", b.PriceHigh, 270.0)
}

func TestRangeFixturePage1(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-August-14-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	e := markets[lasPinas]
	checkPrice(t, "rice", e["Well-milled Rice (Local)"].Price, 50.00)
	checkPrice(t, "tilapia", e["Tilapia"].Price, 170.00)
	checkPrice(t, "galunggong price", e["Galunggong (Local)"].Price, 310.00)
	checkPrice(t, "galunggong low", e["Galunggong (Local)"].PriceLow, 300.00)
	checkPrice(t, "galunggong high", e["Galunggong (Local)"].PriceHigh, 320.00)
	checkPrice(t, "egg price", e["Egg (Medium)"].Price, 8.25)
	checkPrice(t, "egg low", e["Egg (Medium)"].PriceLow, 8.00)
	checkPrice(t, "egg high", e["Egg (Medium)"].PriceHigh, 8.50)
	if e["Egg (Medium)"].Unit != "per piece" {
		t.Errorf("egg unit = %q, want per piece", e["Egg (Medium)"].Unit)
	}
}

func TestRangeFixturePage2(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-August-14-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	e := markets[lasPinas]
	checkPrice(t, "ampalaya", e["Ampalaya"].Price, 160.00)
	checkPrice(t, "ampalaya low", e["Ampalaya"].PriceLow, 155.00)
	checkPrice(t, "ampalaya high", e["Ampalaya"].PriceHigh, 165.00)
	checkPrice(t, "eggplant", e["Eggplant"].Price, 175.00)
	checkPrice(t, "tomato", e["Tomato"].Price, 115.00)
	checkPrice(t, "tomato low", e["Tomato"].PriceLow, 110.00)
	checkPrice(t, "tomato high", e["Tomato"].PriceHigh, 120.00)
	checkPrice(t, "cabbage", e["Cabbage"].Price, 115.00)
	checkPrice(t, "red onion", e["Red Onion (Local)"].Price, 85.00)
}

func TestRangeFixturePage3(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-August-14-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	e := markets[lasPinas]
	checkPrice(t, "sugar", e["Sugar (Washed)"].Price, 80.00)
	checkPrice(t, "sugar low", e["Sugar (Washed)"].PriceLow, 78.00)
	checkPrice(t, "sugar high", e["Sugar (Washed)"].PriceHigh, 82.00)
	checkPrice(t, "kasim", e["Fresh Pork Kasim/Pigue"].Price, 307.50)
	checkPrice(t, "liempo", e["Fresh Pork Liempo"].Price, 350.00)
	checkPrice(t, "chicken", e["Fresh Whole Chicken"].Price, 180.00)
	if len(e) != 13 {
		t.Errorf("las piñas commodities = %d, want 13", len(e))
	}
}

func TestRangeFixtureNotAvailable(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-August-14-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	g := markets["Alabang Central Market"]["Galunggong (Local)"]
	if g.Price != nil || g.PriceLow != nil || g.PriceHigh != nil {
		t.Errorf("alabang galunggong = (%v,%v,%v), want nils", g.Price, g.PriceLow, g.PriceHigh)
	}
}

func TestRangeFixtureSplitTomato(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-August-14-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	tm := markets["Balintawak (Cloverleaf) Market"]["Tomato"]
	checkPrice(t, "tomato low", tm.PriceLow, 75.00)
	checkPrice(t, "tomato high", tm.PriceHigh, 85.00)
	checkPrice(t, "tomato price", tm.Price, 80.00)
}

func TestRangeFixtureOnlyEggPerPiece(t *testing.T) {
	markets, _, _, err := ParsePDFWithLayout(fixturePath(t, "Price-Monitoring-August-14-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, items := range markets {
		for c, e := range items {
			if e.Unit == "per piece" {
				got[c] = true
			}
		}
	}
	if len(got) != 1 || !got["Egg (Medium)"] {
		t.Errorf("per-piece set = %v, want only Egg (Medium)", got)
	}
}

func TestLayoutDetectionBothFixtures(t *testing.T) {
	rangePDF := fixturePath(t, "Price-Monitoring-August-14-2026.pdf")
	singlePDF := fixturePath(t, "Price-Monitoring-October-23-2026.pdf")
	if l, err := DetectLayout(rangePDF); err != nil || l != "range" {
		t.Errorf("range detect = %q, %v", l, err)
	}
	if l, err := DetectLayout(singlePDF); err != nil || l != "single" {
		t.Errorf("single detect = %q, %v", l, err)
	}
	rm, _, rl, err := ParsePDFWithLayout(rangePDF)
	if err != nil || rl != "range" {
		t.Fatalf("range with_layout = %q, %v", rl, err)
	}
	sm, _, sl, err := ParsePDFWithLayout(singlePDF)
	if err != nil || sl != "single" {
		t.Fatalf("single with_layout = %q, %v", sl, err)
	}
	if l, err := InferLayout(rm); err != nil || l != "range" {
		t.Errorf("infer range = %q, %v", l, err)
	}
	if l, err := InferLayout(sm); err != nil || l != "single" {
		t.Errorf("infer single = %q, %v", l, err)
	}
	if _, err := InferLayout(map[string]map[string]Entry{}); err == nil {
		t.Errorf("infer empty: want LayoutChangedError, got nil")
	} else if _, ok := err.(*LayoutChangedError); !ok {
		t.Errorf("infer empty: wrong error type %T", err)
	}
}
