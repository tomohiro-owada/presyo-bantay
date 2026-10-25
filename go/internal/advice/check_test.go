package advice

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExtractTable(t *testing.T) {
	cases := []struct {
		in    string
		value float64
		dec   int
		kind  string
	}{
		{"halagang ₱1,234.50 kada kilo", 1234.5, 2, KindPeso},
		{"P180 lang", 180, 0, KindPeso},
		{"PHP 99.50", 99.5, 2, KindPeso},
		{"180 pesos bawat kilo", 180, 0, KindPeso},
		{"tumaas ng 12.5%", 12.5, 1, KindPercent},
		{"bumaba ng -3.2%", -3.2, 1, KindPercent},
		{"１８０ペソです", 180, 0, KindPeso},
		{"５％上がった", 5, 0, KindPercent},
		{"10 percent higher", 10, 0, KindPercent},
		{"5 porsyento", 5, 0, KindPercent},
		{"20 piso", 20, 0, KindPeso},
	}
	for _, c := range cases {
		got := Extract(c.in)
		if len(got) != 1 {
			t.Errorf("%q: got %d mentions, want 1 (%+v)", c.in, len(got), got)
			continue
		}
		m := got[0]
		if m.Value != c.value || m.Decimals != c.dec || m.Kind != c.kind {
			t.Errorf("%q: got %+v, want value=%v dec=%d kind=%s",
				c.in, m, c.value, c.dec, c.kind)
		}
	}
}

func TestExtractSectionsAndLines(t *testing.T) {
	text := "## Taglish\nmura ang ₱100\n## English\ncheap at ₱100\n## 日本語\n１００ペソ\n"
	got := Extract(text)
	if len(got) != 3 {
		t.Fatalf("got %d mentions: %+v", len(got), got)
	}
	if got[0].Section != "Taglish" || got[0].Line != 2 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Section != "English" || got[1].Line != 4 {
		t.Errorf("second = %+v", got[1])
	}
	if got[2].Section != "日本語" || got[2].Line != 6 || got[2].Value != 100 {
		t.Errorf("third = %+v", got[2])
	}
}

func TestExtractGluedHeaders(t *testing.T) {
	// Real-model failure: preamble glued to the first header put the whole
	// Taglish section in section "".
	text := "Gagawa ako ng guide mula sa JSON.## Taglish\nmura ang ₱100\n## English\ncheap at ₱100\n## 日本語\n１００ペソ\n"
	got := Extract(text)
	if len(got) != 3 {
		t.Fatalf("got %d mentions: %+v", len(got), got)
	}
	if got[0].Section != "Taglish" {
		t.Errorf("glued header: first section = %q, want Taglish (%+v)", got[0].Section, got[0])
	}
	if got[1].Section != "English" || got[2].Section != "日本語" {
		t.Errorf("sections = %q %q", got[1].Section, got[2].Section)
	}
}

func TestCheckEmptySectionWarning(t *testing.T) {
	// Number before any header keeps section "" and raises a warning.
	rep, err := Check([]byte(testSignals), "Presyong ₱110 bago ang header\n## Taglish\nTilapia ₱110\n## English\nTilapia ₱110\n## 日本語\nTilapia ₱110\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Warning == "" {
		t.Error("expected warning for empty-section mention")
	}
	if _, ok := rep.PerSection[""]; !ok {
		t.Errorf("expected per_section[\"\"] entry, got %+v", rep.PerSection)
	}
}

func TestCheckUnknownSectionWarning(t *testing.T) {
	rep, err := Check([]byte(testSignals), "## Taglish\nTilapia ₱110\n## Summary\nTilapia ₱110\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Warning == "" {
		t.Error("expected warning for unknown section")
	}
}

// testSignals is the v2 local-basis shape.
const testSignals = `{
  "as_of": "2026-10-23",
  "commodities": [
    {"name": "Tilapia", "basis": "las_pinas", "price": 108.0,
     "median_30d": 100.0, "pct_vs_median_30d": 10.0, "diff_vs_median_30d": 8.0,
     "trend_7d_pct": 10.0, "trend_7d_base_value": 98.2,
     "market_prices": {"Las Piñas": 108.0, "Alabang": 104.0},
     "cheapest_nearby": {"market": "Alabang", "price": 104.0, "diff_vs_basis": -4.0}},
    {"name": "Bangus", "basis": "las_pinas", "price": 96.0,
     "median_30d": 100.0, "pct_vs_median_30d": -4.0, "diff_vs_median_30d": -4.0,
     "market_prices": {"Las Piñas": 96.0, "Alabang": 90.0}}
  ],
  "fx": {"latest_rate": 63.64, "rate_30d_ago": 62.55, "fx_change_30d_pct": 1.7}
}`

func TestCheckDirect(t *testing.T) {
	rep, err := Check([]byte(testSignals), "## Taglish\nTilapia ₱108, tumaas ng 10%\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.Total != 2 || rep.OK != 2 || len(rep.Flagged) != 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestCheckDerivedMoney(t *testing.T) {
	// ₱6 = Bangus price 96 - Alabang market 90: a derived within-commodity
	// gap, not a leaf (leaves: 96, 100, 4, 96, 90).
	rep, err := Check([]byte(testSignals), "## Taglish\nBangus ₱96, mas mura ng ₱6 kaysa dati\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 2 || rep.OK != 1 || rep.Derived != 1 || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep)
		t.Fatalf("report = %s", b)
	}
	if rep.Status != StatusPass {
		t.Errorf("status = %q, want pass", rep.Status)
	}
}

func TestCheckDerivedPercent(t *testing.T) {
	// 3.7% = (108-104)/108*100 from the Tilapia price/Alabang pair.
	rep, err := Check([]byte(testSignals), "## Taglish\nTilapia 3.7% difference\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.Derived != 1 {
		b, _ := json.Marshal(rep)
		t.Errorf("report = %s", b)
	}
}

func TestCheckFlagInvented(t *testing.T) {
	// ₱999 matches nothing; the isolated 42% matches no percent leaf.
	rep, err := Check([]byte(testSignals), "## Taglish\nPresyong ₱999 at 42%\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 2 {
		t.Errorf("report = %+v", rep)
	}
	ps := rep.PerSection["Taglish"]
	if ps.Total != 2 || ps.Flagged != 2 {
		t.Errorf("per_section = %+v", ps)
	}
}

func TestMatchesTolerance(t *testing.T) {
	// 0-decimal peso: |180 - 180.4| <= 0.5 matches.
	if !Matches(180, 0, []float64{180.4}) {
		t.Error("peso 0-dec tolerance failed")
	}
	if Matches(180, 0, []float64{181}) {
		t.Error("peso 0-dec should not match 181")
	}
	// 1-decimal percent: rounded equality.
	if !Matches(9.1, 1, []float64{9.09}) {
		t.Error("percent rounding failed")
	}
}

const kindSignals = `{
  "commodities": [
    {"name": "Tilapia", "basis": "las_pinas", "price": 180.0,
     "median_30d": 160.0, "pct_vs_median_30d": 12.5, "diff_vs_median_30d": 20.0,
     "trend_7d_pct": 5.0, "trend_7d_base_value": 150.0, "n_days_30d": 30,
     "market_prices": {"Las Piñas": 180.0, "Alabang": 170.0},
     "cheapest_nearby": {"market": "Alabang", "price": 165.0, "diff_vs_basis": -15.0}}
  ],
  "fx": {"latest_rate": 63.64, "rate_30d_ago": 62.55, "fx_change_30d_pct": 1.7}
}`

func TestCheckKindAware(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantFlags int
	}{
		{"peso matches price", "## Taglish\nTilapia ₱180\n", 0},
		{"peso matches median", "## Taglish\nTilapia ₱160\n", 0},
		{"peso matches diff", "## Taglish\nTilapia ₱20\n", 0},
		{"peso matches base value", "## Taglish\nTilapia ₱150\n", 0},
		{"peso matches market price", "## Taglish\nTilapia ₱170\n", 0},
		{"peso matches cheapest price", "## Taglish\nTilapia ₱165\n", 0},
		{"peso matches cheapest diff magnitude", "## Taglish\nTilapia ₱15\n", 0},
		{"peso must not match pct", "## Taglish\nTilapia ₱12.5\n", 1},
		{"peso must not match count", "## Taglish\nTilapia ₱30\n", 1},
		{"percent matches pct", "## Taglish\nTilapia 12.5%\n", 0},
		{"percent matches trend", "## Taglish\nTilapia 5%\n", 0},
		{"percent must not match money", "## Taglish\nTilapia 160%\n", 1},
		{"percent must not match count", "## Taglish\nTilapia 30%\n", 1},
		{"percent must not match fx rate", "## Taglish\nTilapia 63.64%\n", 1},
	}
	for _, c := range cases {
		rep, err := Check([]byte(kindSignals), c.text)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Flagged) != c.wantFlags {
			b, _ := json.Marshal(rep)
			t.Errorf("%s: got %d flags, want %d (%s)", c.name, len(rep.Flagged), c.wantFlags, b)
		}
		if c.wantFlags == 1 && rep.Flagged[0].Reason != ReasonNotBound {
			t.Errorf("%s: reason = %q, want %q", c.name, rep.Flagged[0].Reason, ReasonNotBound)
		}
	}
}

func TestCheckCommodityBinding(t *testing.T) {
	// ₱96 is Bangus's Las Piñas price, not Tilapia's.
	rep, err := Check([]byte(testSignals), "## Taglish\nTilapia ₱96\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 1 {
		b, _ := json.Marshal(rep)
		t.Fatalf("wrong-commodity money should flag: %s", b)
	}
	f := rep.Flagged[0]
	if f.Reason != ReasonNotBound {
		t.Errorf("reason = %q, want %q", f.Reason, ReasonNotBound)
	}
	if len(f.Commodities) != 1 || f.Commodities[0] != "Tilapia" {
		t.Errorf("commodities_in_context = %v, want [Tilapia]", f.Commodities)
	}

	rep, err = Check([]byte(testSignals), "## Taglish\nBangus ₱96\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep)
		t.Errorf("right-commodity money should pass: %s", b)
	}
}

const aliasSignals = `{
  "commodities": [
    {"name": "Tilapia", "basis": "las_pinas", "price": 180.0,
     "median_30d": 160.0, "pct_vs_median_30d": 12.5, "trend_7d_pct": 0.0,
     "market_prices": {"Las Piñas": 180.0}},
    {"name": "Chayote", "basis": "nearby_median", "price": 60.0,
     "median_30d": 92.5, "pct_vs_median_30d": -43.2, "trend_7d_pct": -25.0,
     "market_prices": {"Alabang": 60.0, "Bicutan": 62.0}}
  ]
}`

func TestCheckBindingAliases(t *testing.T) {
	// Filipino / Japanese aliases bind to the same commodity, and a bound
	// match counts as OK (not unbound).
	for _, text := range []string{
		"## Taglish\nginisang sayote (Chayote, ₱60 sa Las Piñas)\n",
		"## 日本語\nサヨテは₱60です\n",
		"## 日本語\nティラピアは₱180です\n",
		"## Taglish\nsinigang na tilapia (Tilapia, ₱180 sa Las Piñas)\n",
	} {
		rep, err := Check([]byte(aliasSignals), text)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != StatusPass || rep.OK != 1 || rep.Unbound != 0 {
			b, _ := json.Marshal(rep)
			t.Errorf("%q: report = %s", text, b)
		}
	}
	// "egg" must not bind inside "eggplant" (word boundaries).
	rep, err := Check([]byte(`{"commodities": [
		{"name": "Eggplant", "price": 80.0, "market_prices": {"Las Piñas": 80.0}},
		{"name": "Egg (Medium)", "price": 8.5, "market_prices": {"Las Piñas": 8.5}}]}`),
		"## Taglish\ntortang talong (Eggplant, ₱80 sa Las Piñas)\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.OK != 1 || rep.Unbound != 0 {
		b, _ := json.Marshal(rep)
		t.Errorf("egg/eggplant boundary: report = %s", b)
	}
}

func TestCheckSentenceContext(t *testing.T) {
	// One line, two sentences: each number is bound to its own sentence.
	got := Extract("## Taglish\nChayote -25% drop. Tilapia ₱160 today.\n")
	if len(got) != 2 {
		t.Fatalf("got %d mentions: %+v", len(got), got)
	}
	if got[0].Kind != KindPercent || !strings.Contains(got[0].Context, "Chayote") || strings.Contains(got[0].Context, "Tilapia") {
		t.Errorf("percent context = %q", got[0].Context)
	}
	if got[1].Kind != KindPeso || !strings.Contains(got[1].Context, "Tilapia") || strings.Contains(got[1].Context, "Chayote") {
		t.Errorf("peso context = %q", got[1].Context)
	}
	rep, err := Check([]byte(aliasSignals), "## Taglish\nChayote -25% drop. Tilapia ₱160 today.\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.OK != 2 {
		b, _ := json.Marshal(rep)
		t.Errorf("report = %s", b)
	}
}

const signSignals = `{
  "commodities": [
    {"name": "Chayote", "price": 52.5, "median_30d": 92.5,
     "pct_vs_median_30d": -43.2, "trend_7d_pct": -25.0,
     "market_prices": {"Las Piñas": 52.5}},
    {"name": "White Potato (Local)", "price": 180.0, "median_30d": 150.0,
     "pct_vs_median_30d": 20.0, "trend_7d_pct": 18.0,
     "market_prices": {"Las Piñas": 180.0}},
    {"name": "Tilapia", "price": 160.0, "median_30d": 160.0,
     "pct_vs_median_30d": 0.0, "trend_7d_pct": 10.0,
     "market_prices": {"Las Piñas": 160.0}}
  ]
}`

func TestCheckPercentSign(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantFlags int
	}{
		{"negative matches negative", "## Taglish\nChayote -25%\n", 0},
		{"unsigned matches negative", "## Taglish\nChayote 25%\n", 0},
		{"unsigned matches positive", "## English\nWhite Potato (Local) 18%\n", 0},
		{"negative must not match positive", "## Taglish\nWhite Potato (Local) -18%\n", 1},
		{"negative must not match other pct", "## Taglish\nTilapia -10%\n", 1},
	}
	for _, c := range cases {
		rep, err := Check([]byte(signSignals), c.text)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Flagged) != c.wantFlags {
			b, _ := json.Marshal(rep)
			t.Errorf("%s: got %d flags, want %d (%s)", c.name, len(rep.Flagged), c.wantFlags, b)
		}
	}
}

func TestCheckUnbound(t *testing.T) {
	// No commodity in context: a globally derivable number counts as
	// unbound, not a failure.
	rep, err := Check([]byte(testSignals), "## English\nMarket overview is steady, save ₱6 overall\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.Unbound != 1 || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep)
		t.Errorf("unbound match: report = %s", b)
	}
	// An unbound invented number still fails.
	rep, err = Check([]byte(testSignals), "## English\nMarket overview is steady, save ₱999 overall\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 1 {
		b, _ := json.Marshal(rep)
		t.Fatalf("unbound invented should flag: %s", b)
	}
	if rep.Flagged[0].Reason != ReasonNoMatch {
		t.Errorf("reason = %q, want %q", rep.Flagged[0].Reason, ReasonNoMatch)
	}
	if len(rep.Flagged[0].Commodities) != 0 {
		t.Errorf("commodities = %v, want []", rep.Flagged[0].Commodities)
	}
}

func TestCheckMarketBinding(t *testing.T) {
	// Correct market price next to the market name passes.
	rep, err := Check([]byte(testSignals), "## Taglish\nTilapia sa Alabang ₱104\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.OK != 1 || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep)
		t.Errorf("right market price should pass: %s", b)
	}
	// Wrong market price next to the market name flags market mismatch,
	// even though ₱96 is a real signals number (Bangus's price).
	rep, err = Check([]byte(testSignals), "## Taglish\nTilapia sa Alabang ₱96\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 1 {
		b, _ := json.Marshal(rep)
		t.Fatalf("wrong market price should flag once: %s", b)
	}
	if rep.Flagged[0].Reason != ReasonMarketMismatch {
		t.Errorf("reason = %q, want %q", rep.Flagged[0].Reason, ReasonMarketMismatch)
	}
	// Japanese market name works too.
	rep, err = Check([]byte(testSignals), "## 日本語\nティラピアはアラバンで₱96です\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 1 || rep.Flagged[0].Reason != ReasonMarketMismatch {
		b, _ := json.Marshal(rep)
		t.Errorf("JP market mismatch: %s", b)
	}
}

func TestCheckBasisMismatch(t *testing.T) {
	// Any NCR-naming sentence carrying a number flags, even when the
	// number itself exists in signals.
	for _, text := range []string{
		"## Taglish\nNCR median ng Tilapia ay ₱110\n",
		"## English\nMetro Manila price for Tilapia is ₱110\n",
		"## Taglish\nAyon sa Kamaynilaan, tumaas ng 10% ang Tilapia\n",
		"## 日本語\n首都圏ではティラピアは₱110です\n",
	} {
		rep, err := Check([]byte(testSignals), text)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != StatusFail || len(rep.Flagged) != 1 {
			b, _ := json.Marshal(rep)
			t.Errorf("%q: should flag once: %s", text, b)
			continue
		}
		if rep.Flagged[0].Reason != ReasonBasisMismatch {
			t.Errorf("%q: reason = %q, want %q", text, rep.Flagged[0].Reason, ReasonBasisMismatch)
		}
	}
	// Without a number there is nothing to flag.
	rep, err := Check([]byte(testSignals), "## Taglish\nWalang NCR dito\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || rep.Total != 0 {
		b, _ := json.Marshal(rep)
		t.Errorf("numberless NCR sentence should pass: %s", b)
	}
}

const metricSignals = `{
  "commodities": [
    {"name": "Chayote", "price": 52.5, "median_30d": 92.5,
     "pct_vs_median_30d": -43.2, "trend_7d_pct": -25.0,
     "market_prices": {"Las Piñas": 52.5}},
    {"name": "Tilapia", "price": 160.0, "median_30d": 160.0,
     "pct_vs_median_30d": 8.0, "trend_7d_pct": 10.0,
     "market_prices": {"Las Piñas": 160.0}}
  ]
}`

func TestCheckMetricMismatch(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantFlags int
		reason    string
	}{
		{"7-day matches trend", "## Taglish\nSayote 7-day change -25%\n", 0, ""},
		{"7-day carrying 30-day pct flags", "## Taglish\nSayote 7-day change -43.2%\n", 1, ReasonMetricMismatch},
		{"usual matches 30-day pct", "## Taglish\nSayote karaniwan -43.2%\n", 0, ""},
		{"usual carrying 7-day pct flags", "## Taglish\nSayote karaniwan -25%\n", 1, ReasonMetricMismatch},
		{"7-day window matching trend passes", "## Taglish\nTilapia 10% this week, still 10% vs usual\n", 0, ""},
		{"JP 7-day phrase", "## 日本語\n先週ハヤトウリは-25%でした\n", 0, ""},
	}
	for _, c := range cases {
		rep, err := Check([]byte(metricSignals), c.text)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Flagged) != c.wantFlags {
			b, _ := json.Marshal(rep)
			t.Errorf("%s: got %d flags, want %d (%s)", c.name, len(rep.Flagged), c.wantFlags, b)
			continue
		}
		if c.wantFlags == 1 && rep.Flagged[0].Reason != c.reason {
			t.Errorf("%s: reason = %q, want %q", c.name, rep.Flagged[0].Reason, c.reason)
		}
	}
}

func TestLintJargon(t *testing.T) {
	clean := "## Taglish\nSulit ngayon sa palengke (2026-10-23)\nTilapia ₱108\n## English\nGood deals today (2026-10-23)\nTilapia ₱108\n## 日本語\n今日のお買い得（2026-10-23）\nティラピアは1kg₱108です\n"
	if got := LintAdvice(clean); len(got.Flags) != 0 {
		b, _ := json.Marshal(got.Flags)
		t.Errorf("clean text flagged: %s", b)
	}
	for tok, want := range map[string]int{
		"NCR": 1, "Metro Manila": 1, "basis": 1, "focus_market": 1,
		"nearby_median": 1, "median_30d": 1, "pct_vs": 1, "las_pinas": 1,
		"NCR-wide": 2, // also contains NCR
	} {
		text := "## Taglish\nSulit ngayon (" + tok + " ₱108)\n## English\nx\n## 日本語\nあ\n"
		got := LintAdvice(text)
		if len(got.Flags) != want {
			b, _ := json.Marshal(got.Flags)
			t.Errorf("token %q: got %d flags, want %d (%s)", tok, len(got.Flags), want, b)
		}
	}
}

func TestLintJapaneseLatin(t *testing.T) {
	// Latin garbage in the Japanese section flags; kg is allowed.
	bad := "## Taglish\nx\n## English\ny\n## 日本語\nトマトはulamです₱100\n"
	got := LintAdvice(bad)
	if len(got.Flags) != 1 {
		b, _ := json.Marshal(got.Flags)
		t.Fatalf("latin garbage: got %d flags, want 1 (%s)", len(got.Flags), b)
	}
	if !strings.Contains(got.Flags[0].Reason, "latin") {
		t.Errorf("reason = %q, want latin mention", got.Flags[0].Reason)
	}
	ok := "## Taglish\nx\n## English\ny\n## 日本語\nトマト1kgは₱100です\n"
	if got := LintAdvice(ok); len(got.Flags) != 0 {
		b, _ := json.Marshal(got.Flags)
		t.Errorf("kg should be allowed: %s", b)
	}
	// Latin in Taglish/English sections is not linted.
	ok2 := "## Taglish\nBuy Tilapia now\n## English\nBuy Tilapia now\n## 日本語\nティラピア\n"
	if got := LintAdvice(ok2); len(got.Flags) != 0 {
		b, _ := json.Marshal(got.Flags)
		t.Errorf("latin outside Japanese section should pass: %s", b)
	}
}

func TestCheckRealFilesV2OK(t *testing.T) {
	sig, err := os.ReadFile("testdata/signals_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	adv, err := os.ReadFile("testdata/advice_ok_v2.md")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Check(sig, string(adv))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep.Flagged)
		t.Fatalf("advice_ok_v2.md: status=%s flagged=%s", rep.Status, b)
	}
	if rep.Total == 0 {
		t.Error("advice_ok_v2.md: expected mentions")
	}
	if got := LintAdvice(string(adv)); len(got.Flags) != 0 {
		b, _ := json.Marshal(got.Flags)
		t.Errorf("advice_ok_v2.md lint flags: %s", b)
	}
}

func TestCheckRealFilesV2Bad(t *testing.T) {
	sig, err := os.ReadFile("testdata/signals_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	adv, err := os.ReadFile("testdata/advice_bad_v2.md")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Check(sig, string(adv))
	if err != nil {
		t.Fatal(err)
	}
	// Exactly four checker flags: market, NCR-basis, metric and
	// wrong-commodity, one of each reason.
	if rep.Status != StatusFail {
		t.Fatalf("bad: status=%s, want fail", rep.Status)
	}
	if len(rep.Flagged) != 4 {
		b, _ := json.Marshal(rep.Flagged)
		t.Fatalf("bad: got %d flags, want exactly 4: %s", len(rep.Flagged), b)
	}
	seen := map[string]int{}
	for _, fl := range rep.Flagged {
		seen[fl.Reason]++
	}
	for _, want := range []string{
		ReasonMarketMismatch, ReasonBasisMismatch, ReasonMetricMismatch, ReasonNotBound,
	} {
		if seen[want] != 1 {
			t.Errorf("reason %q flagged %d times, want exactly once (all=%v)", want, seen[want], rep.Flagged)
		}
	}
	// The Latin-garbage token in the Japanese section is a lint flag.
	lint := LintAdvice(string(adv))
	latin := 0
	for _, fl := range lint.Flags {
		if strings.Contains(fl.Reason, "latin") {
			latin++
		}
	}
	if latin != 1 {
		b, _ := json.Marshal(lint.Flags)
		t.Errorf("latin garbage: got %d latin flags, want 1 (%s)", latin, b)
	}
}

// TestCheckRealFilesV3OK is the regression test for the bullet-line
// inheritance fix: "; cheapest at <Market>: ₱X" clauses (TL/EN, after
// ";") and "。<マーケット>が最安 ₱X" clauses (JA, after "。") name no
// commodity in their own segment, so they used to fall back to the
// global pool (9 unbound) and tampering passed. They must now inherit
// the commodities from earlier in the same bullet line, bind to the
// market, and count as bound (0 unbound for bullet lines).
func TestCheckRealFilesV3OK(t *testing.T) {
	sig, err := os.ReadFile("testdata/signals_v3.json")
	if err != nil {
		t.Fatal(err)
	}
	adv, err := os.ReadFile("testdata/advice_ok_v3.md")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Check(sig, string(adv))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep.Flagged)
		t.Fatalf("advice_ok_v3.md: status=%s flagged=%s", rep.Status, b)
	}
	if rep.Total == 0 {
		t.Error("advice_ok_v3.md: expected mentions")
	}
	if rep.Unbound != 0 {
		b, _ := json.Marshal(rep.Flagged)
		t.Errorf("advice_ok_v3.md: got %d unbound, want 0 (bullet cheapest clauses must inherit): %s", rep.Unbound, b)
	}
	if got := LintAdvice(string(adv)); len(got.Flags) != 0 {
		b, _ := json.Marshal(got.Flags)
		t.Errorf("advice_ok_v3.md lint flags: %s", b)
	}
}

// TestCheckRealFilesV3Tampered flags the two prototype tamperings as
// market mismatches (not unbound passes): EN Pechay
// "cheapest at Pasay: ₱80"->"₱30" and JA Tomato
// "アラバンが最安 ₱80"->"₱70".
func TestCheckRealFilesV3Tampered(t *testing.T) {
	sig, err := os.ReadFile("testdata/signals_v3.json")
	if err != nil {
		t.Fatal(err)
	}
	adv, err := os.ReadFile("testdata/advice_ok_v3.md")
	if err != nil {
		t.Fatal(err)
	}
	base := string(adv)
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{"en cheapest", "cheapest at Pasay: ₱80", "cheapest at Pasay: ₱30"},
		{"ja cheapest", "アラバンが最安 ₱80", "アラバンが最安 ₱70"},
	}
	for _, c := range cases {
		if !strings.Contains(base, c.old) {
			t.Fatalf("%s: fixture missing %q", c.name, c.old)
		}
		tampered := strings.Replace(base, c.old, c.new, 1)
		rep, err := Check(sig, tampered)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != StatusFail || len(rep.Flagged) != 1 {
			b, _ := json.Marshal(rep.Flagged)
			t.Errorf("%s: got status=%s flags=%d, want fail with 1 flag: %s", c.name, rep.Status, len(rep.Flagged), b)
			continue
		}
		if rep.Flagged[0].Reason != ReasonMarketMismatch {
			t.Errorf("%s: reason = %q, want %q", c.name, rep.Flagged[0].Reason, ReasonMarketMismatch)
		}
		if len(rep.Flagged[0].Commodities) == 0 {
			t.Errorf("%s: expected inherited commodities in context, got none", c.name)
		}
	}
}

// TestCheckFBPostV3Inherited covers the same bullet inheritance in the
// combined FB post: the template-derived post passes with 0 unbound,
// and a tampered cheapest price in the post flags a market mismatch.
func TestCheckFBPostV3Inherited(t *testing.T) {
	sig, err := os.ReadFile("testdata/signals_v3.json")
	if err != nil {
		t.Fatal(err)
	}
	texts, err := RenderTemplateJSON(sig)
	if err != nil {
		t.Fatal(err)
	}
	post := RenderFBPost(texts)
	rep, err := CheckFBPost(sig, post)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusPass || len(rep.Flagged) != 0 {
		b, _ := json.Marshal(rep.Flagged)
		t.Fatalf("v3 fb post: status=%s flagged=%s\n%s", rep.Status, b, post)
	}
	if rep.Unbound != 0 {
		t.Errorf("v3 fb post: got %d unbound, want 0", rep.Unbound)
	}
	if !strings.Contains(post, "cheapest at Pasay: ₱80") {
		t.Fatalf("v3 fb post missing EN cheapest clause:\n%s", post)
	}
	tampered := strings.Replace(post, "cheapest at Pasay: ₱80", "cheapest at Pasay: ₱30", 1)
	trep, err := CheckFBPost(sig, tampered)
	if err != nil {
		t.Fatal(err)
	}
	if trep.Status != StatusFail || len(trep.Flagged) == 0 {
		b, _ := json.Marshal(trep.Flagged)
		t.Errorf("tampered fb post should fail: status=%s flagged=%s", trep.Status, b)
	} else {
		found := false
		for _, fl := range trep.Flagged {
			if fl.Reason == ReasonMarketMismatch {
				found = true
				break
			}
		}
		if !found {
			b, _ := json.Marshal(trep.Flagged)
			t.Errorf("tampered fb post: want a market mismatch, got %s", b)
		}
	}
}
