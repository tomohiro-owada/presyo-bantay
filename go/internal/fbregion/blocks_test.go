package fbregion

import (
	"strings"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

func blocksTestSignals() *signals.RegionSignals {
	return &signals.RegionSignals{
		AsOf:   "2026-10-23",
		Region: signals.RegionRef{ID: "south", TitleTL: "South Metro", TitleEN: "South Metro", TitleJA: "南部（ラス・ピニャス周辺）"},
		Deals: []signals.RegionDeal{
			{Name: "Chayote", MarketShort: "Las Piñas", Unit: "per kg", Price: 60, PctRounded: -33},
		},
	}
}

// TestSplitPostRoundTrip requires the per-language blocks to reassemble
// the post body (minus separators) and the footer to drop the site link.
func TestSplitPostRoundTrip(t *testing.T) {
	sig := blocksTestSignals()
	post := RenderPost(sig)
	b := SplitPost(post)
	for _, blk := range []string{b.TL, b.EN, b.JA} {
		if blk == "" {
			t.Error("empty language block")
		}
		if strings.Contains(blk, Separator) || strings.Contains(blk, Link) {
			t.Errorf("block leaks separator/link: %q", blk)
		}
	}
	for _, f := range []string{b.FooterTL, b.FooterEN, b.FooterJA} {
		if f == "" {
			t.Error("empty footer")
		}
		if strings.Contains(f, Link) || strings.Contains(f, "📊") {
			t.Errorf("footer keeps site link: %q", f)
		}
	}
	if !strings.Contains(b.FooterTL, "kumpara") || !strings.Contains(b.FooterEN, "usual price") ||
		!strings.Contains(b.FooterJA, "いつもの価格比") {
		t.Errorf("footers not per-language: %+v", b)
	}
	// Reassembly: blocks + separators + footers(+link) == post.
	if got := b.TL + "\n" + Separator + "\n" + b.EN + "\n" + Separator + "\n" + b.JA + "\n" + Separator + "\n"; !strings.HasPrefix(post, got) {
		t.Error("blocks do not reassemble the post prefix")
	}
}

func TestFormatDateLong(t *testing.T) {
	d := FormatDateLong("2026-10-23")
	if d.TL != "Okt 23, 2026" || d.EN != "Oct 23, 2026" || d.JA != "2026年10月23日" {
		t.Errorf("got %+v", d)
	}
}

func TestFormatMonthLong(t *testing.T) {
	m := FormatMonthLong("2026-10-23")
	if m.TL != "Oktubre 2026" || m.EN != "October 2026" || m.JA != "2026年10月" {
		t.Errorf("got %+v", m)
	}
}

func TestAltTextNoParens(t *testing.T) {
	sig := blocksTestSignals()
	for _, lang := range []string{"tl", "en", "ja"} {
		alt := AltText(sig, lang)
		if alt == "" {
			t.Errorf("empty alt for %s", lang)
		}
		if HasNestedParens(alt) {
			t.Errorf("alt has nested parens for %s: %q", lang, alt)
		}
	}
	empty := &signals.RegionSignals{Region: sig.Region}
	if AltText(empty, "tl") == "" || AltText(empty, "en") == "" || AltText(empty, "ja") == "" {
		t.Error("empty-deals alt must still describe the region")
	}
}

// TestSummarizeTopDeal requires the structured top-deal summary: only
// the first deal, no emoji, matching the RenderPost segments.
func TestSummarizeTopDeal(t *testing.T) {
	sig := &signals.RegionSignals{
		AsOf:   "2026-10-23",
		Region: signals.RegionRef{ID: "south", TitleTL: "South Metro", TitleEN: "South Metro", TitleJA: "南部（ラス・ピニャス周辺）"},
		Deals: []signals.RegionDeal{
			{Name: "Chayote", MarketShort: "Alabang", Unit: "per kg", Price: 40, PctRounded: -50},
			{Name: "Cabbage", MarketShort: "Bicutan", Unit: "per kg", Price: 140, PctRounded: -30},
		},
	}
	got := SummarizeTopDeal(sig)
	if got.TL != "Sayote ₱40/kg (−50%) · Alabang" {
		t.Errorf("tl = %q", got.TL)
	}
	if got.EN != "Chayote ₱40 (−50%)" {
		t.Errorf("en = %q", got.EN)
	}
	if got.JA != "サヨテ ₱40（−50%）" {
		t.Errorf("ja = %q", got.JA)
	}
	for _, s := range []string{got.TL, got.EN, got.JA} {
		if strings.Contains(s, "✅") || strings.Contains(s, "Cabbage") ||
			strings.Contains(s, "Repolyo") || strings.Contains(s, "キャベツ") {
			t.Errorf("summary shows more than the top deal: %q", s)
		}
	}
	if empty := SummarizeTopDeal(&signals.RegionSignals{Region: sig.Region}); empty != (TopDeal{}) {
		t.Errorf("empty deals = %+v, want empty", empty)
	}
}
