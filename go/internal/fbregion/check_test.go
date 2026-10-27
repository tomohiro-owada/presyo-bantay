package fbregion

import (
	"strings"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

func testSignals() *signals.RegionSignals {
	pct := func(v float64) *float64 { return &v }
	_ = pct
	return &signals.RegionSignals{
		AsOf:   "2026-10-23",
		Region: signals.RegionRef{ID: "south", TitleTL: "South Metro", TitleEN: "South Metro", TitleJA: "南部（ラス・ピニャス周辺）"},
		Thresholds: signals.RegionThresholds{
			DealPct: 8, MinDays: 10, MaxTrend7dPct: 5,
		},
		Deals: []signals.RegionDeal{
			{Name: "Chayote", Market: "New Las Piñas City Public Market", MarketShort: "Las Piñas", Unit: "per kg", Price: 60, Median30d: 90, PctVsMedian30d: -33.3, PctRounded: -33, DiffVsMedian30d: -30},
			{Name: "Egg (Medium)", Market: "Alabang Central Market", MarketShort: "Alabang", Unit: "per piece", Price: 8.5, Median30d: 10, PctVsMedian30d: -15, PctRounded: -15, DiffVsMedian30d: -1.5},
		},
		Pricier: []signals.RegionPricier{
			{Name: "Red Chili (Local)", Unit: "per kg", PriceMedian: 450, PctMedian: 70, PctRounded: 70, NMarketsPricier: 4, NMarkets: 6},
		},
		GeneratedBy: "test",
	}
}

func TestRenderPostShape(t *testing.T) {
	post := RenderPost(testSignals())
	for _, want := range []string{
		"🛒 Sulit sa South Metro — presyo noong Okt 23",
		"✅ Mas mura kaysa usual:",
		"• Sayote ₱60/kg (−33%) · Las Piñas",
		"• Itlog ₱8.50/piraso (−15%) · Alabang",
		"⚠️ Mas mahal ngayon:",
		"• Siling pula ₱450/kg (+70%)",
		"━━━━━━━━━━",
		"🇬🇧 Deals in South Metro — prices of Oct 23",
		"✅ Chayote ₱60 (−33%), Egg ₱8.50/pc (−15%)",
		"⚠️ Pricier: Red chili ₱450 (+70%)",
		"🇯🇵 南部（ラス・ピニャス周辺）のお買い得 — 10/23の価格",
		"✅ サヨテ ₱60（−33%）、卵 1個 ₱8.50（−15%）",
		"⚠️ 高め：赤唐辛子 ₱450（+70%）",
		"% = kumpara sa usual na presyo (30 araw) / vs. 30-day usual price / 30日間のいつもの価格比",
		"Source: DA Price Monitoring · Hindi opisyal na DA page",
		"📊 https://abalol.xyz/presyo-bantay/",
	} {
		if !strings.Contains(post, want) {
			t.Errorf("post missing %q:\n%s", want, post)
		}
	}
	if strings.Contains(post, "$") {
		t.Error("post must not contain $")
	}
}

func TestRenderPostEmpty(t *testing.T) {
	sig := testSignals()
	sig.Deals = nil
	sig.Pricier = nil
	post := RenderPost(sig)
	for _, want := range []string{
		"✅ Walang kapansin-pansing mura ngayon.",
		"✅ No notable deals today.",
		"✅ 今日は目立つお買い得なし。",
	} {
		if !strings.Contains(post, want) {
			t.Errorf("empty post missing %q", want)
		}
	}
	if strings.Contains(post, "Mas mahal") || strings.Contains(post, "Pricier:") || strings.Contains(post, "高め") {
		t.Error("empty post must omit pricier blocks")
	}
}

func TestCheckPostPass(t *testing.T) {
	sig := testSignals()
	rep := CheckPost(sig, RenderPost(sig))
	if rep.Status != StatusPass {
		t.Errorf("status = %q, flagged=%+v lint=%+v", rep.Status, rep.Flagged, rep.Lint)
	}
	if rep.Total == 0 || rep.OK != rep.Total {
		t.Errorf("total=%d ok=%d", rep.Total, rep.OK)
	}
}

func TestCheckPostEmptyPass(t *testing.T) {
	sig := testSignals()
	sig.Deals = nil
	sig.Pricier = nil
	rep := CheckPost(sig, RenderPost(sig))
	if rep.Status != StatusPass {
		t.Errorf("status = %q, flagged=%+v lint=%+v", rep.Status, rep.Flagged, rep.Lint)
	}
}

func tamper(post, old, new string) string {
	if !strings.Contains(post, old) {
		panic("tamper anchor missing: " + old)
	}
	return strings.Replace(post, old, new, 1)
}

func TestCheckPostTampering(t *testing.T) {
	sig := testSignals()
	good := RenderPost(sig)
	cases := []struct {
		name string
		bad  string
	}{
		{"wrong market", tamper(good, "· Las Piñas", "· Alabang")},
		{"wrong pct", tamper(good, "(−33%) · Las Piñas", "(−30%) · Las Piñas")},
		{"wrong price", tamper(good, "₱60/kg (−33%)", "₱61/kg (−33%)")},
		{"dollar", tamper(good, "₱60/kg", "$60/kg")},
		{"wrong EN pct", tamper(good, "Red chili ₱450 (+70%)", "Red chili ₱450 (+71%)")},
		{"wrong JA price", tamper(good, "サヨテ ₱60（−33%）", "サヨテ ₱61（−33%）")},
		{"jargon", good + "\nNCR average ₱60\n"},
		{"latin in JA", tamper(good, "赤唐辛子 ₱450", "Red chili ₱450")},
		{"nested parens", tamper(good, "· Las Piñas", "· Grace (Pateros)")},
		{"double parens heading", tamper(good, "🇬🇧 Deals in South Metro — prices of Oct 23", "🇬🇧 Deals in South Metro (prices of Oct 23)")},
	}
	for _, c := range cases {
		rep := CheckPost(sig, c.bad)
		if rep.Status != StatusPass {
			continue
		}
		t.Errorf("%s: tampered post passed (total=%d ok=%d)", c.name, rep.Total, rep.OK)
	}
}

func overlapSignals() *signals.RegionSignals {
	return &signals.RegionSignals{
		AsOf:   "2026-10-26",
		Region: signals.RegionRef{ID: "qc", TitleTL: "Quezon City", TitleEN: "Quezon City", TitleJA: "ケソン市"},
		Thresholds: signals.RegionThresholds{
			DealPct: 8, MinDays: 10, MaxTrend7dPct: 5,
		},
		Deals: []signals.RegionDeal{
			{Name: "Red Chili (Local)", Market: "Murphy Public Market", MarketShort: "Murphy", Unit: "per kg", Price: 300, Median30d: 350, PctVsMedian30d: -14.3, PctRounded: -14, DiffVsMedian30d: -50, NMarketsWithDeal: 1},
		},
		Pricier: []signals.RegionPricier{
			{Name: "Red Chili (Local)", Unit: "per kg", PriceMedian: 300, PctMedian: 21, PctRounded: 21, NMarketsPricier: 6, NMarkets: 7},
		},
		GeneratedBy: "test",
	}
}

// Same commodity name and same price in both lists: each line binds to
// the entry of its own kind. Correct pcts pass; a pricier line carrying
// the wrong pct still fails with "wrong pct".
func TestCheckPostOverlapNamePrice(t *testing.T) {
	sig := overlapSignals()
	good := RenderPost(sig)
	if rep := CheckPost(sig, good); rep.Status != StatusPass {
		t.Fatalf("overlap post should pass: flagged=%+v lint=%+v\n%s", rep.Flagged, rep.Lint, good)
	}
	cases := []struct {
		name string
		bad  string
	}{
		{"TL pricier +21->+14", tamper(good, "₱300/kg (+21%)", "₱300/kg (+14%)")},
		{"TL pricier +21->-14", tamper(good, "• Siling pula ₱300/kg (+21%)", "• Siling pula ₱300/kg (−14%)")},
		{"EN pricier +21->+14", tamper(good, "Red chili ₱300 (+21%)", "Red chili ₱300 (+14%)")},
		{"EN pricier +21->-14", tamper(good, "Red chili ₱300 (+21%)", "Red chili ₱300 (−14%)")},
		{"JA pricier +21->+14", tamper(good, "赤唐辛子 ₱300（+21%）", "赤唐辛子 ₱300（+14%）")},
		{"JA pricier +21->-14", tamper(good, "赤唐辛子 ₱300（+21%）", "赤唐辛子 ₱300（−14%）")},
		{"TL deal -14->-15", tamper(good, "• Siling pula ₱300/kg (−14%) · Murphy", "• Siling pula ₱300/kg (−15%) · Murphy")},
		{"EN deal -14->-15", tamper(good, "Red chili ₱300 (−14%)", "Red chili ₱300 (−15%)")},
		{"JA deal -14->-15", tamper(good, "赤唐辛子 ₱300（−14%）", "赤唐辛子 ₱300（−15%）")},
	}
	for _, c := range cases {
		rep := CheckPost(sig, c.bad)
		if rep.Status != StatusFail {
			t.Errorf("%s: tampered post passed (total=%d ok=%d)", c.name, rep.Total, rep.OK)
			continue
		}
		found := false
		for _, f := range rep.Flagged {
			if strings.Contains(f.Reason, "wrong pct") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: no wrong pct flag: %+v", c.name, rep.Flagged)
		}
	}
}

// TestNoNestedParensAllRegions renders a post for every region (using that
// region's own market shorts, including the ex-parenthesised "Grace,
// Pateros" and "Farmers, Cubao") and asserts no text line contains nested
// parentheses and no heading contains a ") (" / "）（" double group. It also
// asserts the PNG market labels "(<short>)" built by fbchart have no
// nested parentheses.
func TestNoNestedParensAllRegions(t *testing.T) {
	if len(regions.Regions) != 5 {
		t.Fatalf("got %d regions, want 5", len(regions.Regions))
	}
	for _, r := range regions.Regions {
		for _, m := range r.Markets {
			if strings.ContainsAny(m.Short, "()（）") {
				t.Errorf("region %s short %q contains parentheses", r.ID, m.Short)
			}
			if label := "(" + m.Short + ")"; HasNestedParens(label) {
				t.Errorf("region %s PNG label %q is nested", r.ID, label)
			}
		}
		shorts := []string{"Las Piñas", "Alabang"}
		if len(r.Markets) >= 2 {
			shorts[0] = r.Markets[0].Short
			shorts[1] = r.Markets[1].Short
		}
		sig := &signals.RegionSignals{
			AsOf:   "2026-10-23",
			Region: signals.RegionRef{ID: r.ID, TitleTL: r.TitleTL, TitleEN: r.TitleEN, TitleJA: r.TitleJA},
			Deals: []signals.RegionDeal{
				{Name: "Chayote", Market: r.Markets[0].Name, MarketShort: shorts[0], Unit: "per kg", Price: 60, Median30d: 90, PctVsMedian30d: -33.3, PctRounded: -33, DiffVsMedian30d: -30},
				{Name: "Egg (Medium)", Market: r.Markets[1%len(r.Markets)].Name, MarketShort: shorts[1], Unit: "per piece", Price: 8.5, Median30d: 10, PctVsMedian30d: -15, PctRounded: -15, DiffVsMedian30d: -1.5},
			},
			Pricier: []signals.RegionPricier{
				{Name: "Red Chili (Local)", Unit: "per kg", PriceMedian: 450, PctMedian: 70, PctRounded: 70, NMarketsPricier: 4, NMarkets: 6},
			},
		}
		post := RenderPost(sig)
		for _, ln := range strings.Split(post, "\n") {
			if HasNestedParens(ln) {
				t.Errorf("region %s line has nested parens: %q", r.ID, ln)
			}
			if isHeadingLine(ln) && HasHeadingDoubleParens(ln) {
				t.Errorf("region %s heading has double parens: %q", r.ID, ln)
			}
		}
		// Headings must use the em dash for the date part.
		for _, want := range []string{
			"🇬🇧 Deals in " + r.TitleEN + " — prices of Oct 23",
			"🇯🇵 " + r.TitleJA + "のお買い得 — 10/23の価格",
		} {
			if !strings.Contains(post, want) {
				t.Errorf("region %s post missing heading %q:\n%s", r.ID, want, post)
			}
		}
		if rep := CheckPost(sig, post); rep.Status != StatusPass {
			t.Errorf("region %s checker failed: flagged=%+v lint=%+v", r.ID, rep.Flagged, rep.Lint)
		}
	}
}
