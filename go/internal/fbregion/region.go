// Package fbregion renders the approved short regional Facebook post
// (REGIONS_SPEC.md section 3): one region per post, trilingual (TL/EN/JA),
// deterministic template text from region signals only. An optional LLM
// rewrite (llm.go, off by default) reuses the opencode runner; the region
// checker (check.go) must pass or the template is used.
package fbregion

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Separator is the divider line between post blocks.
const Separator = "━━━━━━━━━━"

// Link is the site link closing the post.
const Link = "https://abalol.xyz/presyo-bantay/"

// tlShort overrides the shared Taglish display names with the short forms
// approved for regional posts.
var tlShort = map[string]string{
	"Red Chili (Local)":   "Siling pula",
	"Red Onion (Local)":   "Sibuyas pula",
	"White Onion (Local)": "Sibuyas puti",
}

// DisplayTL returns the regional-post Taglish name for a canonical item.
func DisplayTL(canonical string) string {
	if s, ok := tlShort[canonical]; ok {
		return s
	}
	return advice.DisplayTL(canonical)
}

// enShort overrides the shared English display names with the short forms
// approved for regional posts.
var enShort = map[string]string{
	"Red Chili (Local)":    "Red chili",
	"Chili (Green)":        "Green chili",
	"White Potato (Local)": "Potato",
	"Fresh Whole Chicken":  "Chicken",
	"Carrots (Local)":      "Carrots",
	"Red Onion (Local)":    "Red onion",
	"White Onion (Local)":  "White onion",
	"Egg (Medium)":         "Egg",
	"Galunggong (Local)":   "Galunggong",
	"Galunggong":           "Galunggong",
	"Sugar (Refined)":      "Sugar",
	"Sugar (Washed)":       "Sugar",
}

// DisplayEN returns the regional-post English name for a canonical item.
func DisplayEN(canonical string) string {
	if s, ok := enShort[canonical]; ok {
		return s
	}
	return advice.DisplayEN(canonical)
}

// DisplayJA returns the regional-post Japanese name for a canonical item:
// the shared display name without the long parenthetical gloss.
func DisplayJA(canonical string) string {
	s := advice.DisplayJA(canonical)
	if i := strings.Index(s, "（"); i >= 0 {
		s = s[:i]
	}
	return s
}

// isPiece reports whether a signals unit is per-piece.
func isPiece(unit string) bool {
	return strings.Contains(strings.ToLower(unit), "piece")
}

// fmtPrice prints whole peso amounts without decimals (₱130), else 2 dp.
func fmtPrice(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// fmtPct formats a rounded integer percent with U+2212 minus / plus sign.
func fmtPct(p int) string {
	if p < 0 {
		return "−" + strconv.Itoa(-p)
	}
	return "+" + strconv.Itoa(p)
}

var tlMonths = map[time.Month]string{
	time.January: "Ene", time.February: "Peb", time.March: "Mar",
	time.April: "Abr", time.May: "May", time.June: "Hun",
	time.July: "Hul", time.August: "Ago", time.September: "Set",
	time.October: "Okt", time.November: "Nob", time.December: "Dis",
}

var enMonths = map[time.Month]string{
	time.January: "Jan", time.February: "Feb", time.March: "Mar",
	time.April: "Apr", time.May: "May", time.June: "Jun",
	time.July: "Jul", time.August: "Aug", time.September: "Sep",
	time.October: "Oct", time.November: "Nov", time.December: "Dec",
}

// Dates holds the three locale date strings for an as-of date.
type Dates struct {
	TL string // "Okt 23"
	EN string // "Oct 23"
	JA string // "10/23"
}

// FormatDates formats asOf (YYYY-MM-DD) for the three post languages.
func FormatDates(asOf string) Dates {
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return Dates{TL: asOf, EN: asOf, JA: asOf}
	}
	return Dates{
		TL: fmt.Sprintf("%s %d", tlMonths[t.Month()], t.Day()),
		EN: fmt.Sprintf("%s %d", enMonths[t.Month()], t.Day()),
		JA: fmt.Sprintf("%d/%d", int(t.Month()), t.Day()),
	}
}

// TopDealTL formats one deal without emoji or bullet, e.g.
// "Sayote ₱40/kg (−50%) · Alabang" (same segments as RenderPost TL).
func TopDealTL(dl signals.RegionDeal) string {
	unit := "/kg"
	if isPiece(dl.Unit) {
		unit = "/piraso"
	}
	return fmt.Sprintf("%s ₱%s%s (%s%%) · %s",
		DisplayTL(dl.Name), fmtPrice(dl.Price), unit, fmtPct(dl.PctRounded), dl.MarketShort)
}

// TopDealEN formats one deal without emoji, e.g. "Chayote ₱40 (−50%)".
func TopDealEN(dl signals.RegionDeal) string {
	unit := ""
	if isPiece(dl.Unit) {
		unit = "/pc"
	}
	return fmt.Sprintf("%s ₱%s%s (%s%%)",
		DisplayEN(dl.Name), fmtPrice(dl.Price), unit, fmtPct(dl.PctRounded))
}

// TopDealJA formats one deal without emoji, e.g. "サヨテ ₱40（−50%）".
func TopDealJA(dl signals.RegionDeal) string {
	pre := ""
	if isPiece(dl.Unit) {
		pre = "1個 "
	}
	return fmt.Sprintf("%s %s₱%s（%s%%）",
		DisplayJA(dl.Name), pre, fmtPrice(dl.Price), fmtPct(dl.PctRounded))
}

// TopDeal holds the per-language top-deal summary (no emoji, no title).
type TopDeal struct {
	TL string
	EN string
	JA string
}

// SummarizeTopDeal builds the top-deal summary from structured signals
// (empty when there are no deals).
func SummarizeTopDeal(sig *signals.RegionSignals) TopDeal {
	if sig == nil || len(sig.Deals) == 0 {
		return TopDeal{}
	}
	d := sig.Deals[0]
	return TopDeal{TL: TopDealTL(d), EN: TopDealEN(d), JA: TopDealJA(d)}
}

// RenderPost builds the full regional post text from region signals.
func RenderPost(sig *signals.RegionSignals) string {
	d := FormatDates(sig.AsOf)
	var b strings.Builder

	// Taglish block.
	fmt.Fprintf(&b, "🛒 Sulit sa %s — presyo noong %s\n\n", sig.Region.TitleTL, d.TL)
	if len(sig.Deals) == 0 {
		b.WriteString("✅ Walang kapansin-pansing mura ngayon.\n")
	} else {
		b.WriteString("✅ Mas mura kaysa usual:\n")
		for _, dl := range sig.Deals {
			unit := "/kg"
			if isPiece(dl.Unit) {
				unit = "/piraso"
			}
			fmt.Fprintf(&b, "• %s ₱%s%s (%s%%) · %s\n",
				DisplayTL(dl.Name), fmtPrice(dl.Price), unit, fmtPct(dl.PctRounded), dl.MarketShort)
		}
	}
	if len(sig.Pricier) > 0 {
		b.WriteString("\n⚠️ Mas mahal ngayon:\n")
		for _, p := range sig.Pricier {
			unit := "/kg"
			if isPiece(p.Unit) {
				unit = "/piraso"
			}
			fmt.Fprintf(&b, "• %s ₱%s%s (%s%%)\n",
				DisplayTL(p.Name), fmtPrice(p.PriceMedian), unit, fmtPct(p.PctRounded))
		}
	}
	b.WriteString("\n" + Separator + "\n")

	// English block.
	fmt.Fprintf(&b, "🇬🇧 Deals in %s — prices of %s\n", sig.Region.TitleEN, d.EN)
	if len(sig.Deals) == 0 {
		b.WriteString("✅ No notable deals today.\n")
	} else {
		segs := make([]string, 0, len(sig.Deals))
		for _, dl := range sig.Deals {
			unit := ""
			if isPiece(dl.Unit) {
				unit = "/pc"
			}
			segs = append(segs, fmt.Sprintf("%s ₱%s%s (%s%%)",
				DisplayEN(dl.Name), fmtPrice(dl.Price), unit, fmtPct(dl.PctRounded)))
		}
		b.WriteString("✅ " + strings.Join(segs, ", ") + "\n")
	}
	if len(sig.Pricier) > 0 {
		segs := make([]string, 0, len(sig.Pricier))
		for _, p := range sig.Pricier {
			unit := ""
			if isPiece(p.Unit) {
				unit = "/pc"
			}
			segs = append(segs, fmt.Sprintf("%s ₱%s%s (%s%%)",
				DisplayEN(p.Name), fmtPrice(p.PriceMedian), unit, fmtPct(p.PctRounded)))
		}
		b.WriteString("⚠️ Pricier: " + strings.Join(segs, ", ") + "\n")
	}
	b.WriteString("\n" + Separator + "\n")

	// Japanese block.
	fmt.Fprintf(&b, "🇯🇵 %sのお買い得 — %sの価格\n", sig.Region.TitleJA, d.JA)
	if len(sig.Deals) == 0 {
		b.WriteString("✅ 今日は目立つお買い得なし。\n")
	} else {
		segs := make([]string, 0, len(sig.Deals))
		for _, dl := range sig.Deals {
			pre := ""
			if isPiece(dl.Unit) {
				pre = "1個 "
			}
			segs = append(segs, fmt.Sprintf("%s %s₱%s（%s%%）",
				DisplayJA(dl.Name), pre, fmtPrice(dl.Price), fmtPct(dl.PctRounded)))
		}
		b.WriteString("✅ " + strings.Join(segs, "、") + "\n")
	}
	if len(sig.Pricier) > 0 {
		segs := make([]string, 0, len(sig.Pricier))
		for _, p := range sig.Pricier {
			pre := ""
			if isPiece(p.Unit) {
				pre = "1個 "
			}
			segs = append(segs, fmt.Sprintf("%s %s₱%s（%s%%）",
				DisplayJA(p.Name), pre, fmtPrice(p.PriceMedian), fmtPct(p.PctRounded)))
		}
		b.WriteString("⚠️ 高め：" + strings.Join(segs, "、") + "\n")
	}
	b.WriteString("\n" + Separator + "\n")

	// Shared footer.
	b.WriteString("% = kumpara sa usual na presyo (30 araw) / vs. 30-day usual price / 30日間のいつもの価格比\n")
	b.WriteString("Source: DA Price Monitoring · Hindi opisyal na DA page\n")
	b.WriteString("📊 " + Link + "\n")
	return b.String()
}
