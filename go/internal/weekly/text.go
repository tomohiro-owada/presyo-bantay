package weekly

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
)

// Separator is the divider line between post blocks (same as daily).
const Separator = "━━━━━━━━━━"

// LinkBase is the site base for weekly pages.
const LinkBase = "https://abalol.xyz/presyo-bantay/w/"

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

// isPiece reports whether a unit is per-piece.
func isPiece(unit string) bool {
	return strings.Contains(strings.ToLower(unit), "piece")
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

// RangeLabel holds the first..last covered date range per language.
type RangeLabel struct {
	TL string // "Okt 19–23, 2026"
	EN string // "Oct 19–23, 2026"
	JA string // "10/19〜10/23"
}

// FormatRange formats first..last covered dates (YYYY-MM-DD).
func FormatRange(first, last string) RangeLabel {
	t1, err1 := time.Parse("2006-01-02", first)
	t2, err2 := time.Parse("2006-01-02", last)
	if err1 != nil || err2 != nil {
		return RangeLabel{TL: first + "–" + last, EN: first + "–" + last, JA: first + "〜" + last}
	}
	var tl, en string
	if t1.Month() == t2.Month() && t1.Year() == t2.Year() {
		tl = fmt.Sprintf("%s %d–%d, %d", tlMonths[t1.Month()], t1.Day(), t2.Day(), t1.Year())
		en = fmt.Sprintf("%s %d–%d, %d", enMonths[t1.Month()], t1.Day(), t2.Day(), t1.Year())
	} else if t1.Year() == t2.Year() {
		tl = fmt.Sprintf("%s %d–%s %d, %d", tlMonths[t1.Month()], t1.Day(), tlMonths[t2.Month()], t2.Day(), t1.Year())
		en = fmt.Sprintf("%s %d–%s %d, %d", enMonths[t1.Month()], t1.Day(), enMonths[t2.Month()], t2.Day(), t1.Year())
	} else {
		tl = fmt.Sprintf("%s %d, %d–%s %d, %d", tlMonths[t1.Month()], t1.Day(), t1.Year(), tlMonths[t2.Month()], t2.Day(), t2.Year())
		en = fmt.Sprintf("%s %d, %d–%s %d, %d", enMonths[t1.Month()], t1.Day(), t1.Year(), enMonths[t2.Month()], t2.Day(), t2.Year())
	}
	ja := fmt.Sprintf("%d/%d〜%d/%d", int(t1.Month()), t1.Day(), int(t2.Month()), t2.Day())
	return RangeLabel{TL: tl, EN: en, JA: ja}
}

// RangeOf returns the covered range label of sig (first..last covered).
func RangeOf(sig *Signals) RangeLabel {
	if len(sig.Covered) == 0 {
		return RangeLabel{}
	}
	return FormatRange(sig.Covered[0], sig.Covered[len(sig.Covered)-1])
}

// DisplayTL/EN/JA reuse the daily regional display names.
func DisplayTL(canonical string) string { return fbregion.DisplayTL(canonical) }
func DisplayEN(canonical string) string { return fbregion.DisplayEN(canonical) }
func DisplayJA(canonical string) string { return fbregion.DisplayJA(canonical) }

// adviceDisplayJA is the long JA name (unused; kept for checker aliases).
var _ = advice.DisplayTL

// metroTL formats one metro entry for the TL block.
func metroTL(m MetroEntry) string {
	unit := "/kg"
	if isPiece(m.Unit) {
		unit = "/piraso"
	}
	return fmt.Sprintf("%s ₱%s%s (%s%%)",
		DisplayTL(m.Name), fmtPrice(m.DisplayPrice), unit, fmtPct(m.Pct))
}

// metroEN formats one metro entry for the EN block (no unit).
func metroEN(m MetroEntry) string {
	unit := ""
	if isPiece(m.Unit) {
		unit = "/pc"
	}
	return fmt.Sprintf("%s ₱%s%s (%s%%)",
		DisplayEN(m.Name), fmtPrice(m.DisplayPrice), unit, fmtPct(m.Pct))
}

// metroJA formats one metro entry for the JA block.
func metroJA(m MetroEntry) string {
	pre := ""
	if isPiece(m.Unit) {
		pre = "1個 "
	}
	return fmt.Sprintf("%s %s₱%s（%s%%）",
		DisplayJA(m.Name), pre, fmtPrice(m.DisplayPrice), fmtPct(m.Pct))
}

// dealTL formats one area deal for the TL block.
func dealTL(d DealEntry) string {
	return fmt.Sprintf("%s · %s (%s%%)", DisplayTL(d.Name), d.MarketShort, fmtPct(d.Pct))
}

// dealEN formats one area deal for the EN block.
func dealEN(d DealEntry) string {
	return fmt.Sprintf("%s at %s (%s%%)", DisplayEN(d.Name), d.MarketShort, fmtPct(d.Pct))
}

// dealJA formats one area deal for the JA block (market stays Latin).
func dealJA(d DealEntry) string {
	return fmt.Sprintf("%s %s%%（%s）", DisplayJA(d.Name), fmtPct(d.Pct), d.MarketShort)
}

// missingLine formats one missing date per language.
func missingTL(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return fmt.Sprintf("%s %d", tlMonths[t.Month()], t.Day())
}

func missingEN(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return fmt.Sprintf("%s %d", enMonths[t.Month()], t.Day())
}

func missingJA(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return fmt.Sprintf("%d/%d", int(t.Month()), t.Day())
}

// RenderWeekly builds the full combined weekly post text from signals.
func RenderWeekly(sig *Signals) string {
	rng := RangeOf(sig)
	var b strings.Builder

	// Taglish block.
	fmt.Fprintf(&b, "🗓️ Lingguhang ulat — %s\n", rng.TL)
	if len(sig.Drops) == 0 {
		b.WriteString("📉 Bumaba ngayong linggo (buong Metro Manila, vs nakaraang linggo):\nWalang malaking pagbabago\n")
	} else {
		b.WriteString("📉 Bumaba ngayong linggo (buong Metro Manila, vs nakaraang linggo):\n")
		for _, m := range sig.Drops {
			b.WriteString("• " + metroTL(m) + "\n")
		}
	}
	if len(sig.Rises) == 0 {
		b.WriteString("📈 Tumaas:\nWalang malaking pagbabago\n")
	} else {
		b.WriteString("📈 Tumaas:\n")
		for _, m := range sig.Rises {
			b.WriteString("• " + metroTL(m) + "\n")
		}
	}
	b.WriteString("🏪 Pinakasulit kada rehiyon (vs usual na presyo):\n")
	for _, rd := range sig.DealsByRegion {
		if len(rd.Deals) == 0 {
			fmt.Fprintf(&b, "• %s: walang kapansin-pansin\n", rd.TitleTL)
			continue
		}
		segs := make([]string, 0, len(rd.Deals))
		for _, d := range rd.Deals {
			segs = append(segs, dealTL(d))
		}
		fmt.Fprintf(&b, "• %s: %s\n", rd.TitleTL, strings.Join(segs, ", "))
	}
	if len(sig.Missing) > 0 {
		ms := make([]string, 0, len(sig.Missing))
		for _, d := range sig.Missing {
			ms = append(ms, missingTL(d))
		}
		fmt.Fprintf(&b, "Walang datos ng DA: %s\n", strings.Join(ms, ", "))
	}
	b.WriteString("\n" + Separator + "\n")

	// English block.
	fmt.Fprintf(&b, "🇬🇧 Weekly report — %s\n", rng.EN)
	if len(sig.Drops) == 0 {
		b.WriteString("📉 Down vs last week (Metro Manila avg): No big changes\n")
	} else {
		segs := make([]string, 0, len(sig.Drops))
		for _, m := range sig.Drops {
			segs = append(segs, metroEN(m))
		}
		b.WriteString("📉 Down vs last week (Metro Manila avg): " + strings.Join(segs, ", ") + "\n")
	}
	if len(sig.Rises) == 0 {
		b.WriteString("📈 Up: No big changes\n")
	} else {
		segs := make([]string, 0, len(sig.Rises))
		for _, m := range sig.Rises {
			segs = append(segs, metroEN(m))
		}
		b.WriteString("📈 Up: " + strings.Join(segs, ", ") + "\n")
	}
	{
		parts := make([]string, 0, len(sig.DealsByRegion))
		for _, rd := range sig.DealsByRegion {
			if len(rd.Deals) == 0 {
				parts = append(parts, fmt.Sprintf("%s — nothing notable", rd.TitleEN))
				continue
			}
			segs := make([]string, 0, len(rd.Deals))
			for _, d := range rd.Deals {
				segs = append(segs, dealEN(d))
			}
			parts = append(parts, fmt.Sprintf("%s — %s", rd.TitleEN, strings.Join(segs, ", ")))
		}
		b.WriteString("🏪 Best deals by area: " + strings.Join(parts, "; ") + "\n")
	}
	if len(sig.Missing) > 0 {
		ms := make([]string, 0, len(sig.Missing))
		for _, d := range sig.Missing {
			ms = append(ms, missingEN(d))
		}
		fmt.Fprintf(&b, "No DA data: %s\n", strings.Join(ms, ", "))
	}
	b.WriteString("\n" + Separator + "\n")

	// Japanese block.
	fmt.Fprintf(&b, "🇯🇵 週間レポート — %s\n", rng.JA)
	if len(sig.Drops) == 0 {
		b.WriteString("📉 先週比の値下がり（マニラ首都圏平均）：大きな変動なし\n")
	} else {
		segs := make([]string, 0, len(sig.Drops))
		for _, m := range sig.Drops {
			segs = append(segs, metroJA(m))
		}
		b.WriteString("📉 先週比の値下がり（マニラ首都圏平均）：" + strings.Join(segs, "、") + "\n")
	}
	if len(sig.Rises) == 0 {
		b.WriteString("📈 値上がり：大きな変動なし\n")
	} else {
		segs := make([]string, 0, len(sig.Rises))
		for _, m := range sig.Rises {
			segs = append(segs, metroJA(m))
		}
		b.WriteString("📈 値上がり：" + strings.Join(segs, "、") + "\n")
	}
	{
		parts := make([]string, 0, len(sig.DealsByRegion))
		for _, rd := range sig.DealsByRegion {
			if len(rd.Deals) == 0 {
				parts = append(parts, fmt.Sprintf("%s 目立つお買い得なし", rd.ShortJA))
				continue
			}
			segs := make([]string, 0, len(rd.Deals))
			for _, d := range rd.Deals {
				segs = append(segs, dealJA(d))
			}
			parts = append(parts, fmt.Sprintf("%s %s", rd.ShortJA, strings.Join(segs, "、")))
		}
		b.WriteString("🏪 地域別お買い得：" + strings.Join(parts, "；") + "\n")
	}
	if len(sig.Missing) > 0 {
		ms := make([]string, 0, len(sig.Missing))
		for _, d := range sig.Missing {
			ms = append(ms, missingJA(d))
		}
		fmt.Fprintf(&b, "DAデータなし：%s\n", strings.Join(ms, "、"))
	}
	b.WriteString("\n" + Separator + "\n")

	// Shared footer.
	b.WriteString("% = average this week vs last week; area deals vs each market's 30-day usual price\n")
	b.WriteString("Source: DA Price Monitoring · Hindi opisyal na DA page\n")
	fmt.Fprintf(&b, "📊 %s%s/\n", LinkBase, sig.Week)
	return b.String()
}

// WeeklyBlocks is the RenderWeekly output split per language.
type WeeklyBlocks struct {
	TL       string
	EN       string
	JA       string
	FooterTL string
	FooterEN string
	FooterJA string
}

// SplitWeekly splits a RenderWeekly result into language blocks.
func SplitWeekly(post string) WeeklyBlocks {
	parts := strings.Split(post, Separator)
	get := func(i int) string {
		if i >= len(parts) {
			return ""
		}
		return strings.TrimSuffix(strings.TrimPrefix(parts[i], "\n"), "\n")
	}
	b := WeeklyBlocks{TL: get(0), EN: get(1), JA: get(2)}
	var footer []string
	if len(parts) >= 4 {
		for _, ln := range strings.Split(get(3), "\n") {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "📊") || strings.Contains(t, LinkBase) {
				continue
			}
			footer = append(footer, t)
		}
	}
	if len(footer) == 0 {
		return b
	}
	rest := ""
	if len(footer) > 1 {
		rest = strings.Join(footer[1:], "\n")
	}
	segs := strings.Split(footer[0], "; ")
	if len(segs) != 2 {
		all := strings.Join(footer, "\n")
		b.FooterTL, b.FooterEN, b.FooterJA = all, all, all
		return b
	}
	mk := func(s string) string {
		s = strings.TrimSpace(s)
		if rest != "" {
			s += "\n" + rest
		}
		return s
	}
	b.FooterTL = mk("% = " + segs[0])
	b.FooterEN = mk("% = " + segs[1])
	b.FooterJA = mk("% = " + segs[1])
	return b
}

// TopDropSummary builds the per-language top-drop summary (no emoji).
func TopDropSummary(sig *Signals) (tl, en, ja string) {
	if sig == nil || len(sig.Drops) == 0 {
		return "", "", ""
	}
	m := sig.Drops[0]
	tl = fmt.Sprintf("%s ₱%s (%s%%)", DisplayTL(m.Name), fmtPrice(m.DisplayPrice), fmtPct(m.Pct))
	en = fmt.Sprintf("%s ₱%s (%s%%)", DisplayEN(m.Name), fmtPrice(m.DisplayPrice), fmtPct(m.Pct))
	ja = fmt.Sprintf("%s ₱%s（%s%%）", DisplayJA(m.Name), fmtPrice(m.DisplayPrice), fmtPct(m.Pct))
	return tl, en, ja
}
