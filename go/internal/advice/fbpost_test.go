package advice

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func mustSignals(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRenderFBPostOrderAndFooter(t *testing.T) {
	sig := mustSignals(t, "testdata/signals_v2.json")
	texts, err := RenderTemplateJSON(sig)
	if err != nil {
		t.Fatal(err)
	}
	post := RenderFBPost(texts)
	parts := splitFBPost(post)
	if len(parts) != 4 {
		t.Fatalf("post should have 4 blocks (tl/en/ja/footer), got %d:\n%s", len(parts), post)
	}
	tlIdx := strings.Index(post, parts[0])
	enIdx := strings.Index(post, "Pricier than usual:")
	jaIdx := strings.Index(post, "いつもより高め")
	if !(tlIdx < enIdx && enIdx < jaIdx) {
		t.Error("language order must be Taglish, English, Japanese")
	}
	if strings.Count(post, FBSeparator) < 3 {
		t.Error("post must have at least 3 separator lines")
	}
	if !strings.Contains(post, FBLink) {
		t.Errorf("post must contain site link %q", FBLink)
	}
	if !strings.Contains(post, "Source/Pinagmulan/出典:") {
		t.Error("post must contain the shared DA source note")
	}
	// No markdown tables/bold anywhere.
	if strings.Contains(post, "**") || strings.Contains(post, "|") {
		t.Error("fb post must be plain text, no markdown")
	}
	// No per-language closing line (shared footer replaces it).
	for _, closing := range []string{
		"Batay sa DA price monitoring",
		"Based on DA price monitoring",
		"農業省（DA）の価格調査",
	} {
		if strings.Contains(post, closing) {
			t.Errorf("fb post must drop per-language closing: %q", closing)
		}
	}
}

func TestRenderFBPostLimits(t *testing.T) {
	// 7 deals + 4 pricier: fb post keeps 5 + 2 per language.
	var deals, pricier []string
	for i := 0; i < 7; i++ {
		deals = append(deals, "• Item"+string(rune('A'+i))+": ₱100/kg — 10% below usual (-₱10)")
	}
	for i := 0; i < 4; i++ {
		pricier = append(pricier, "• P"+string(rune('A'+i))+": ₱200/kg — 10% above usual")
	}
	mk := func(title, pHead, empty string) string {
		ls := []string{title}
		ls = append(ls, deals...)
		if pHead != "" {
			ls = append(ls, pHead)
			ls = append(ls, pricier...)
		} else if empty != "" {
			ls = append(ls, empty)
		}
		ls = append(ls, "closing line")
		return strings.Join(ls, "\n")
	}
	texts := Texts{
		TL: mk("🛒 Sulit (d)", "Mas mahal ngayon:", ""),
		EN: mk("🛒 Deals (d)", "Pricier than usual:", ""),
		JA: mk("🛒 お買い得（d）", "いつもより高め：", ""),
	}
	post := RenderFBPost(texts)
	parts := splitFBPost(post)
	if len(parts) != 4 {
		t.Fatalf("want 4 blocks, got %d", len(parts))
	}
	for i, lang := range []string{"tl", "en", "ja"} {
		bullets := 0
		for _, ln := range strings.Split(parts[i], "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "• ") {
				bullets++
			}
		}
		if bullets != FBMaxDeals+FBMaxPricier {
			t.Errorf("%s block: got %d bullets, want %d", lang, bullets, FBMaxDeals+FBMaxPricier)
		}
		if strings.Contains(parts[i], "closing line") {
			t.Errorf("%s block must drop the closing line", lang)
		}
	}
}

func TestRenderFBPostLength(t *testing.T) {
	sig := mustSignals(t, "testdata/signals_v2.json")
	texts, err := RenderTemplateJSON(sig)
	if err != nil {
		t.Fatal(err)
	}
	post := RenderFBPost(texts)
	if len([]rune(post)) > 1800 {
		t.Errorf("real-fixture fb post is %d runes, want <= ~1800", len([]rune(post)))
	}
}

func TestFBPostRealFixturePassesCheck(t *testing.T) {
	sig := mustSignals(t, "testdata/signals_v2.json")
	texts, err := RenderTemplateJSON(sig)
	if err != nil {
		t.Fatal(err)
	}
	post, rep := FBPostAndCheck(sig, texts)
	if rep == nil {
		t.Fatal("nil fb check report")
	}
	if rep.Status != StatusPass {
		b, _ := json.Marshal(rep.Flagged)
		lb, _ := json.Marshal(rep.Lint.Flags)
		t.Errorf("real-fixture fb post must pass: flagged=%s lint=%s\n%s", b, lb, post)
	}
	if len(splitFBPost(post)) != 4 {
		t.Error("fb post must have tl/en/ja/footer blocks")
	}
}

func TestFBPostEmptyDeals(t *testing.T) {
	sig := []byte(`{
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
	}`)
	texts, err := RenderTemplateJSON(sig)
	if err != nil {
		t.Fatal(err)
	}
	post, rep := FBPostAndCheck(sig, texts)
	if rep == nil || rep.Status != StatusPass {
		t.Fatalf("empty-deals fb post must pass: %+v\n%s", rep, post)
	}
	for _, note := range []string{
		"Walang kapansin-pansing mura ngayon.",
		"No notable deals today.",
		"今日は特に目立つお買い得はありません。",
	} {
		if !strings.Contains(post, note) {
			t.Errorf("empty-deals note missing: %q", note)
		}
	}
}
