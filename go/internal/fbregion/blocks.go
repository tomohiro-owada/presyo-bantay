package fbregion

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// PostBlocks is the RenderPost output split on the separator: one block
// per language plus the shared footer (site link line excluded). The
// day-pages builder and the FB poster both derive their per-language
// text from here so the two never drift.
type PostBlocks struct {
	TL string
	EN string
	JA string
	// Footer holds the shared footer lines per language (comparison
	// note in that language plus the shared source line).
	FooterTL string
	FooterEN string
	FooterJA string
}

// SplitPost splits a RenderPost result into its language blocks. Each
// block reassembles byte-exactly: TL+"\n"+Sep+"\n"+EN+... (exactly one
// newline is stripped from each side of every part).
func SplitPost(post string) PostBlocks {
	parts := strings.Split(post, Separator)
	get := func(i int) string {
		if i >= len(parts) {
			return ""
		}
		return strings.TrimSuffix(strings.TrimPrefix(parts[i], "\n"), "\n")
	}
	b := PostBlocks{TL: get(0), EN: get(1), JA: get(2)}
	var footer []string
	if len(parts) >= 4 {
		for _, ln := range strings.Split(get(3), "\n") {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "📊") || strings.Contains(t, Link) {
				continue
			}
			footer = append(footer, t)
		}
	}
	if len(footer) == 0 {
		return b
	}
	// First footer line is trilingual ("a / b / c"); the rest (source
	// line) is shared verbatim.
	rest := ""
	if len(footer) > 1 {
		rest = strings.Join(footer[1:], "\n")
	}
	segs := strings.Split(footer[0], " / ")
	if len(segs) != 3 {
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
	b.FooterTL = mk(segs[0])
	b.FooterEN = mk("% = " + segs[1])
	b.FooterJA = mk("% = " + segs[2])
	return b
}

// BlockForLang returns the post block for tl|en|ja (default tl).
func (b PostBlocks) BlockForLang(lang string) string {
	switch lang {
	case "en":
		return b.EN
	case "ja":
		return b.JA
	default:
		return b.TL
	}
}

// FooterForLang returns the footer note for tl|en|ja (default tl).
func (b PostBlocks) FooterForLang(lang string) string {
	switch lang {
	case "en":
		return b.FooterEN
	case "ja":
		return b.FooterJA
	default:
		return b.FooterTL
	}
}

// DateLong holds the long per-language date strings for a data date.
type DateLong struct {
	TL string // "Okt 23, 2026"
	EN string // "Oct 23, 2026"
	JA string // "2026年10月23日"
}

// FormatDateLong formats asOf (YYYY-MM-DD) as long per-language dates.
func FormatDateLong(asOf string) DateLong {
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return DateLong{TL: asOf, EN: asOf, JA: asOf}
	}
	return DateLong{
		TL: fmt.Sprintf("%s %d, %d", tlMonths[t.Month()], t.Day(), t.Year()),
		EN: fmt.Sprintf("%s %d, %d", enMonths[t.Month()], t.Day(), t.Year()),
		JA: fmt.Sprintf("%d年%d月%d日", t.Year(), int(t.Month()), t.Day()),
	}
}

// MonthLong holds the per-language month headings for the archive page.
type MonthLong struct {
	TL string // "Oktubre 2026"
	EN string // "October 2026"
	JA string // "2026年10月"
}

var tlMonthsLong = map[time.Month]string{
	time.January: "Enero", time.February: "Pebrero", time.March: "Marso",
	time.April: "Abril", time.May: "Mayo", time.June: "Hunyo",
	time.July: "Hulyo", time.August: "Agosto", time.September: "Setyembre",
	time.October: "Oktubre", time.November: "Nobyembre", time.December: "Disyembre",
}

var enMonthsLong = map[time.Month]string{
	time.January: "January", time.February: "February", time.March: "March",
	time.April: "April", time.May: "May", time.June: "June",
	time.July: "July", time.August: "August", time.September: "September",
	time.October: "October", time.November: "November", time.December: "December",
}

// FormatMonthLong formats asOf (YYYY-MM-DD or YYYY-MM) as per-language
// month headings.
func FormatMonthLong(asOf string) MonthLong {
	s := asOf
	if len(s) >= 7 {
		s = s[:7]
	}
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return MonthLong{TL: asOf, EN: asOf, JA: asOf}
	}
	return MonthLong{
		TL: fmt.Sprintf("%s %d", tlMonthsLong[t.Month()], t.Year()),
		EN: fmt.Sprintf("%s %d", enMonthsLong[t.Month()], t.Year()),
		JA: fmt.Sprintf("%d年%d月", t.Year(), int(t.Month())),
	}
}

// altPct formats a rounded percent with U+2212 minus / plus, no parens.
func altPct(p int) string {
	if p < 0 {
		return "−" + strconv.Itoa(-p) + "%"
	}
	return "+" + strconv.Itoa(p) + "%"
}

// AltText builds the short per-language image alt sentence from the top
// 3 deals (no parentheses, so it can sit next to titles carrying them).
func AltText(sig *signals.RegionSignals, lang string) string {
	top := sig.Deals
	if len(top) > 3 {
		top = top[:3]
	}
	switch lang {
	case "en":
		if len(top) == 0 {
			return sig.Region.TitleEN + ": no notable deals today."
		}
		segs := make([]string, 0, len(top))
		for _, d := range top {
			segs = append(segs, DisplayEN(d.Name)+" "+altPct(d.PctRounded))
		}
		return sig.Region.TitleEN + ": cheaper than usual: " + strings.Join(segs, ", ")
	case "ja":
		if len(top) == 0 {
			return sig.Region.TitleJA + "：今日は目立つお買い得なし。"
		}
		segs := make([]string, 0, len(top))
		for _, d := range top {
			segs = append(segs, DisplayJA(d.Name)+" "+altPct(d.PctRounded))
		}
		return sig.Region.TitleJA + "のお買い得：" + strings.Join(segs, "、")
	default:
		if len(top) == 0 {
			return sig.Region.TitleTL + ": walang kapansin-pansing mura ngayon."
		}
		segs := make([]string, 0, len(top))
		for _, d := range top {
			segs = append(segs, DisplayTL(d.Name)+" "+altPct(d.PctRounded))
		}
		return sig.Region.TitleTL + ": mas mura ang " + strings.Join(segs, ", ")
	}
}
