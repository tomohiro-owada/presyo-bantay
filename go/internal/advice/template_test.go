package advice

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func checkTemplatePasses(t *testing.T, signalsData []byte, label string) Texts {
	t.Helper()
	txts, err := RenderTemplateJSON(signalsData)
	if err != nil {
		t.Fatalf("%s: render: %v", label, err)
	}
	for _, body := range []string{txts.TL, txts.EN, txts.JA} {
		if strings.Contains(body, "**") || strings.Contains(body, "|") {
			t.Errorf("%s: template body must not contain markdown: %q", label, body)
		}
	}
	combined := txts.CombinedAdvice()
	rep, err := Check(signalsData, combined)
	if err != nil {
		t.Fatalf("%s: check: %v", label, err)
	}
	var probe struct {
		UsesNearbyBasis *bool `json:"uses_nearby_basis"`
	}
	usesNearby := true
	if jerr := json.Unmarshal(signalsData, &probe); jerr == nil && probe.UsesNearbyBasis != nil {
		usesNearby = *probe.UsesNearbyBasis
	}
	ApplyLint(rep, LintAdviceWithBasis(combined, usesNearby))
	if rep.Status != StatusPass {
		b, _ := json.Marshal(rep.Flagged)
		lb, _ := json.Marshal(rep.Lint.Flags)
		t.Errorf("%s: template must pass check: flagged=%s lint=%s", label, b, lb)
	}
	return txts
}

func TestTemplateRealFixture(t *testing.T) {
	sig, err := os.ReadFile("testdata/signals_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	txts := checkTemplatePasses(t, sig, "real fixture")
	// Spot-check structure: one emoji on title, bullets, closing.
	if c := strings.Count(txts.TL, "🛒"); c != 1 {
		t.Errorf("TL title emoji count = %d, want 1", c)
	}
	if !strings.Contains(txts.TL, "• ") || !strings.Contains(txts.EN, "• ") || !strings.Contains(txts.JA, "• ") {
		t.Error("template should use • bullets")
	}
	// Old fixture has no uses_nearby_basis: no footnote markers.
	for _, body := range []string{txts.TL, txts.EN, txts.JA} {
		if strings.Contains(body, "kalapit na palengke") || strings.Contains(body, "nearby markets") || strings.Contains(body, "近隣市場") {
			t.Errorf("old fixture without nearby basis must omit footnote: %q", body)
		}
	}
}

func TestTemplateSynthetic(t *testing.T) {
	// Deal with sharp drop + cheapest + nearby basis; pricier with sharp rise.
	sig := `{
		"as_of": "2026-10-23",
		"deals": [{
			"rank": 1, "name": "Pechay Baguio", "basis": "nearby_median",
			"basis_label": "nearby markets (Alabang, La Huerta, Bicutan, Pasay)",
			"unit": "per kg", "price": 130, "median_30d": 220,
			"pct_vs_median_30d": -40.9, "diff_vs_median_30d": -90,
			"trend_7d_pct": -40.9, "sharp_drop_this_week": true,
			"cheapest_nearby": {"market": "Pasay", "price": 80, "diff_vs_basis": -50, "days_cheaper": 3, "days_checked": 3}
		}],
		"pricier": [{
			"rank": 1, "name": "Red Chili (Local)", "basis": "las_pinas",
			"basis_label": "Las Piñas", "unit": "per kg", "price": 450,
			"median_30d": 265, "pct_vs_median_30d": 69.8, "diff_vs_median_30d": 185,
			"trend_7d_pct": 60.7, "sharp_rise_this_week": true, "cheapest_nearby": null
		}],
		"commodities": [
			{"name": "Pechay Baguio", "price": 130, "median_30d": 220,
			 "pct_vs_median_30d": -40.9, "diff_vs_median_30d": -90,
			 "trend_7d_pct": -40.9, "market_prices": {"Alabang": 100, "Pasay": 80}},
			{"name": "Red Chili (Local)", "price": 450, "median_30d": 265,
			 "pct_vs_median_30d": 69.8, "diff_vs_median_30d": 185,
			 "trend_7d_pct": 60.7, "market_prices": {"Las Piñas": 450}}
		],
		"uses_nearby_basis": true,
		"thresholds": {"deal_pct": 8, "min_days": 10, "max_trend_7d_pct": 5}
	}`
	txts := checkTemplatePasses(t, []byte(sig), "synthetic")
	if !strings.Contains(txts.TL, "bumaba ng 40.9% mula noong nakaraang linggo") {
		t.Error("TL deal should note sharp drop with 7-day number")
	}
	if !strings.Contains(txts.EN, "down 40.9% vs last week") {
		t.Error("EN deal should note sharp drop with 7-day number")
	}
	if !strings.Contains(txts.JA, "先週比 -40.9%") || !strings.Contains(txts.JA, "今週大きく下がった") {
		t.Error("JA deal should note sharp drop with 先週比 number")
	}
	if !strings.Contains(txts.TL, "pinakamura sa Pasay") {
		t.Error("TL deal should name cheapest market")
	}
	if !strings.Contains(txts.TL, "(kalapit na palengke)") || !strings.Contains(txts.JA, "（近隣市場）") {
		t.Error("nearby-basis item must carry the marker when uses_nearby_basis is true")
	}
	if !strings.Contains(txts.TL, "tumaas ng 60.7% mula noong nakaraang linggo") ||
		!strings.Contains(txts.EN, "up 60.7% vs last week") ||
		!strings.Contains(txts.JA, "先週比 +60.7%") || !strings.Contains(txts.JA, "今週大きく上がった") {
		t.Error("pricier sharp rise wording missing")
	}
	// Peso diff format: minus before ₱, never ₱-.
	for _, body := range []string{txts.TL, txts.EN, txts.JA} {
		if strings.Contains(body, "₱-") {
			t.Errorf("peso diff must be -₱90, not ₱-90: %q", body)
		}
	}
	if !strings.Contains(txts.TL, "(-₱90)") || !strings.Contains(txts.EN, "(-₱90)") || !strings.Contains(txts.JA, "（-₱90）") {
		t.Errorf("deals must show -₱ diff:\nTL:%s\nEN:%s\nJA:%s", txts.TL, txts.EN, txts.JA)
	}
	if !strings.Contains(txts.TL, "mas mura kaysa karaniwan") || !strings.Contains(txts.EN, "below usual") || !strings.Contains(txts.JA, "いつもより") {
		t.Error("deals must use the below-usual phrasing")
	}
	if !strings.Contains(txts.TL, "mas mahal kaysa karaniwan") || !strings.Contains(txts.EN, "above usual") || !strings.Contains(txts.JA, "高い") {
		t.Error("pricier must use the above-usual phrasing")
	}
	// Per-piece unit rendering.
	sigPc := `{
		"as_of": "2026-10-23",
		"deals": [{
			"rank": 1, "name": "Egg (Medium)", "basis": "las_pinas",
			"basis_label": "Las Piñas", "unit": "per piece", "price": 8.5,
			"median_30d": 10, "pct_vs_median_30d": -15, "diff_vs_median_30d": -1.5,
			"trend_7d_pct": 0, "sharp_drop_this_week": false, "cheapest_nearby": null
		}],
		"pricier": [], "commodities": [
			{"name": "Egg (Medium)", "price": 8.5, "median_30d": 10,
			 "pct_vs_median_30d": -15, "diff_vs_median_30d": -1.5,
			 "market_prices": {"Las Piñas": 8.5}}
		],
		"uses_nearby_basis": false,
		"thresholds": {"deal_pct": 8, "min_days": 10, "max_trend_7d_pct": 5}
	}`
	txtsPc := checkTemplatePasses(t, []byte(sigPc), "per-piece")
	if !strings.Contains(txtsPc.TL, "/piraso") || !strings.Contains(txtsPc.EN, "/pc") || !strings.Contains(txtsPc.JA, "1個") {
		t.Errorf("per-piece units wrong:\nTL:%s\nEN:%s\nJA:%s", txtsPc.TL, txtsPc.EN, txtsPc.JA)
	}
}

func TestTemplateEmptyDeals(t *testing.T) {
	sig := `{
		"as_of": "2026-10-23", "deals": [],
		"pricier": [{
			"rank": 1, "name": "Red Chili (Local)", "basis": "las_pinas",
			"basis_label": "Las Piñas", "unit": "per kg", "price": 450,
			"median_30d": 265, "pct_vs_median_30d": 69.8, "diff_vs_median_30d": 185,
			"trend_7d_pct": 60.7, "sharp_rise_this_week": true, "cheapest_nearby": null
		}],
		"commodities": [
			{"name": "Red Chili (Local)", "price": 450, "median_30d": 265,
			 "pct_vs_median_30d": 69.8, "diff_vs_median_30d": 185,
			 "trend_7d_pct": 60.7, "market_prices": {"Las Piñas": 450}}
		],
		"uses_nearby_basis": false,
		"thresholds": {"deal_pct": 8, "min_days": 10, "max_trend_7d_pct": 5}
	}`
	txts := checkTemplatePasses(t, []byte(sig), "empty deals")
	if !strings.Contains(txts.TL, "Walang kapansin-pansing mura ngayon.") ||
		!strings.Contains(txts.EN, "No notable deals today.") ||
		!strings.Contains(txts.JA, "今日は特に目立つお買い得はありません。") {
		t.Errorf("empty-deals lines missing:\n%+v", txts)
	}
	if strings.Contains(txts.TL, "kalapit na palengke") || strings.Contains(txts.EN, "nearby markets") || strings.Contains(txts.JA, "近隣市場") {
		t.Error("uses_nearby_basis=false must omit the nearby footnote")
	}
}

func TestTemplateFootnoteDisplayedOnly(t *testing.T) {
	// Pricier #4/#5 on nearby basis (not shown) must not trigger the
	// footnote: only the first 3 pricier lines are displayed.
	mk := func(basis string) string {
		return `{"rank": 1, "name": "Tilapia", "basis": "` + basis + `",
			"basis_label": "x", "unit": "per kg", "price": 200,
			"median_30d": 150, "pct_vs_median_30d": 33.3, "diff_vs_median_30d": 50,
			"trend_7d_pct": 0, "sharp_drop_this_week": false, "sharp_rise_this_week": false, "cheapest_nearby": null}`
	}
	sig := `{
		"as_of": "2026-10-23", "deals": [],
		"pricier": [` + mk("las_pinas") + `,` + mk("las_pinas") + `,` + mk("las_pinas") + `,
			{"rank": 4, "name": "Chayote", "basis": "nearby_median",
			"basis_label": "x", "unit": "per kg", "price": 60,
			"median_30d": 100, "pct_vs_median_30d": 40, "diff_vs_median_30d": 40,
			"trend_7d_pct": 0, "sharp_drop_this_week": false, "sharp_rise_this_week": false, "cheapest_nearby": null}],
		"commodities": [
			{"name": "Tilapia", "price": 200, "median_30d": 150,
			 "pct_vs_median_30d": 33.3, "diff_vs_median_30d": 50,
			 "market_prices": {"Las Piñas": 200}},
			{"name": "Chayote", "price": 60, "median_30d": 100,
			 "pct_vs_median_30d": 40, "diff_vs_median_30d": 40,
			 "market_prices": {"Alabang": 60}}
		],
		"uses_nearby_basis": true,
		"thresholds": {"deal_pct": 8, "min_days": 10, "max_trend_7d_pct": 5}
	}`
	// Render directly (not via checkTemplatePasses with the stale
	// signals-level flag): the template must omit the footnote because
	// the nearby-basis item is not displayed.
	txts, err := RenderTemplateJSON([]byte(sig))
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{txts.TL, txts.EN, txts.JA} {
		if strings.Contains(body, "kalapit na palengke") || strings.Contains(body, "nearby markets") || strings.Contains(body, "近隣市場") {
			t.Errorf("footnote must be omitted when only undisplayed pricier uses nearby basis: %q", body)
		}
	}
	combined := txts.CombinedAdvice()
	rep, err := Check([]byte(sig), combined)
	if err != nil {
		t.Fatal(err)
	}
	ApplyLint(rep, LintAdviceWithBasis(combined, displayedUsesNearbyFromData([]byte(sig))))
	if rep.Status != StatusPass {
		b, _ := json.Marshal(rep.Flagged)
		t.Errorf("displayed-only footnote template must pass check: %s", b)
	}
}

func TestCheckNewPhrasingMetrics(t *testing.T) {
	sig := `{
		"commodities": [
			{"name": "Tomato", "price": 90, "median_30d": 105,
			 "pct_vs_median_30d": -14.3, "diff_vs_median_30d": -15,
			 "trend_7d_pct": -40.9, "market_prices": {"Las Piñas": 90}},
			{"name": "White Potato (Local)", "price": 200, "median_30d": 160,
			 "pct_vs_median_30d": 25.0, "diff_vs_median_30d": 40,
			 "trend_7d_pct": 60.7, "market_prices": {"Las Piñas": 200}}
		]
	}`
	cases := []string{
		"## Taglish\nKamatis ₱90/kg — 14.3% mas mura kaysa karaniwan (-₱15)\n",
		"## English\nTomato ₱90/kg — 14.3% below usual (-₱15), down 40.9% vs last week\n",
		"## Taglish\nPatatas ₱200/kg — 25% mas mahal kaysa karaniwan, tumaas ng 60.7% mula noong nakaraang linggo\n",
		"## English\nWhite Potato ₱200/kg — 25% above usual, up 60.7% vs last week\n",
		"## 日本語\nトマト: 1kg ₱90 — いつもより14.3%安い（-₱15）、先週比 -40.9%（今週大きく下がった）\n",
		"## 日本語\nじゃがいも: 1kg ₱200 — いつもより25%高い、先週比 +60.7%（今週大きく上がった）\n",
	}
	for _, text := range cases {
		rep, err := Check([]byte(sig), text)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Status != StatusPass || len(rep.Flagged) != 0 {
			b, _ := json.Marshal(rep.Flagged)
			t.Errorf("%q must pass: %s", text, b)
		}
	}
	// Wrong 7-day number in a last-week context flags metric mismatch.
	rep, err := Check([]byte(sig), "## English\nTomato 14.3% vs last week\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 1 || rep.Flagged[0].Reason != ReasonMetricMismatch {
		b, _ := json.Marshal(rep.Flagged)
		t.Errorf("7-day context carrying the 30-day pct must flag metric mismatch: %s", b)
	}
	// Wrong 30-day number in a below-usual context flags metric mismatch.
	rep, err = Check([]byte(sig), "## English\nTomato 40.9% below usual\n")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != StatusFail || len(rep.Flagged) != 1 || rep.Flagged[0].Reason != ReasonMetricMismatch {
		b, _ := json.Marshal(rep.Flagged)
		t.Errorf("usual context carrying the 7-day pct must flag metric mismatch: %s", b)
	}
}

func TestLintAdditions(t *testing.T) {
	// still-falling phrases flagged.
	for _, ph := range []string{"still falling", "still down", "tuloy-baba"} {
		got := LintAdvice("## Taglish\nTilapia is " + ph + " ₱100\n## English\nx\n## 日本語\nあ\n")
		if len(got.Flags) == 0 {
			t.Errorf("phrase %q should be flagged", ph)
		}
	}
	// Markdown flagged; bullets allowed.
	bad := "## Taglish\n# header\n## English\nx\n## 日本語\nあ\n"
	if len(LintAdvice(bad).Flags) == 0 {
		t.Error("# header should be flagged")
	}
	bad = "## Taglish\n| a | b |\n## English\nx\n## 日本語\nあ\n"
	if len(LintAdvice(bad).Flags) == 0 {
		t.Error("| table should be flagged")
	}
	bad = "## Taglish\n**bold**\n## English\nx\n## 日本語\nあ\n"
	if len(LintAdvice(bad).Flags) == 0 {
		t.Error("** bold should be flagged")
	}
	ok := "## Taglish\n• item ₱100\n- item ₱100\n## English\nx\n## 日本語\nあ\n"
	if len(LintAdvice(ok).Flags) != 0 {
		t.Errorf("bullets should be allowed: %+v", LintAdvice(ok).Flags)
	}
	// Nearby footnote flagged only when usesNearby is false.
	withFoot := "## Taglish\nmura (kalapit na palengke)\n## English\ncheap (nearby markets)\n## 日本語\n安い（近隣市場）\n"
	if len(LintAdviceWithBasis(withFoot, true).Flags) != 0 {
		t.Error("footnote allowed when usesNearby=true")
	}
	if len(LintAdviceWithBasis(withFoot, false).Flags) != 3 {
		t.Errorf("footnote should flag 3 times when usesNearby=false, got %d", len(LintAdviceWithBasis(withFoot, false).Flags))
	}
	// DA allowed in Japanese section.
	okJA := "## Taglish\nx\n## English\ny\n## 日本語\n農業省（DA）の価格調査、ラス・ピニャス市場の価格。\n"
	if len(LintAdvice(okJA).Flags) != 0 {
		t.Errorf("DA in JA should be allowed: %+v", LintAdvice(okJA).Flags)
	}
}

func TestSplitSections(t *testing.T) {
	in := "preamble\n## Taglish\ntl body\n## English\nen body\n## 日本語\nja body\n"
	tl, en, ja, ok := SplitSections(in)
	if !ok || tl != "tl body" || en != "en body" || ja != "ja body" {
		t.Errorf("split = %q %q %q %v", tl, en, ja, ok)
	}
	if _, _, _, ok := SplitSections("## Taglish\nx\n## English\ny\n"); ok {
		t.Error("missing section should not split")
	}
}
