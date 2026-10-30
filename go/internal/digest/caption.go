package digest

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
)

// SiteBase is the public site root (trailing slash).
const SiteBase = "https://abalol.xyz/presyo-bantay/"

// DayURL is the per-day detail page that holds the regional posts.
func DayURL(asOf string) string { return SiteBase + "d/" + asOf + "/" }

func fmtPeso(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func fmtSigned(p int) string {
	switch {
	case p < 0:
		return "−" + strconv.Itoa(-p)
	case p > 0:
		return "+" + strconv.Itoa(p)
	}
	return "±0"
}

// Trend renders the trend glyph + signed percent, e.g. "▲+3%", "▼−2%",
// "→±0%", or "" when the trend is unknown.
func Trend(c *Cell) string {
	if c == nil || c.Pct == nil {
		return ""
	}
	g := "→"
	switch c.Dir {
	case DirUp:
		g = "▲"
	case DirDown:
		g = "▼"
	}
	p := *c.Pct
	if c.Dir == DirFlat {
		p = 0
	}
	return fmt.Sprintf("%s%s%%", g, fmtSigned(p))
}

func unitFor(it *Row, lang string) string {
	switch lang {
	case "tl":
		if it.Piece {
			return "/piraso"
		}
		return "/kg"
	case "en":
		if it.Piece {
			return "/pc"
		}
		return "/kg"
	}
	if it.Piece {
		return "/個"
	}
	return "/kg"
}

// dropNames lists dropped regions per language.
func (d *Digest) dropNames(lang string) string {
	var out []string
	for _, r := range d.Dropped() {
		switch lang {
		case "tl":
			out = append(out, r.TitleTL)
		case "en":
			out = append(out, r.TitleEN)
		default:
			out = append(out, r.TitleJA)
		}
	}
	return strings.Join(out, ", ")
}

// RenderCaption builds the trilingual caption (TL / EN / JA) from the
// digest only. Metro lines use the pooled median of every reporting market
// in the included regions; trends are like-for-like vs each region's
// previous report.
func RenderCaption(d *Digest) string {
	dt := fbregion.FormatDates(d.AsOf)
	prevTL, prevEN, prevJA := "nakaraang ulat", "previous report", "前回報告"
	if d.PrevAsOf != "" {
		pd := fbregion.FormatDates(d.PrevAsOf)
		prevTL, prevEN, prevJA = pd.TL, pd.EN, pd.JA
	}
	var b strings.Builder

	// Taglish.
	fmt.Fprintf(&b, "🛒 Presyo sa Metro Manila — noong %s\n", dt.TL)
	fmt.Fprintf(&b, "Median ng mga palengke, kumpara sa %s:\n", prevTL)
	for i := range d.Rows {
		r := &d.Rows[i]
		if r.Metro == nil {
			continue
		}
		line := fmt.Sprintf("• %s ₱%s%s", r.TL, fmtPeso(r.Metro.Price), unitFor(r, "tl"))
		if t := Trend(r.Metro); t != "" {
			line += " " + t
		}
		b.WriteString(line + "\n")
	}
	if d.Best != nil {
		fmt.Fprintf(&b, "⭐ Pinakamura kaysa usual: %s\n", fbregion.TopDealTL(d.Best.Deal))
	}
	if n := d.dropNames("tl"); n != "" {
		fmt.Fprintf(&b, "ℹ️ Wala pa sa digest: %s\n", n)
	}
	fmt.Fprintf(&b, "📍 Presyo kada rehiyon: %s\n", DayURL(d.AsOf))
	b.WriteString("\n" + fbregion.Separator + "\n")

	// English.
	fmt.Fprintf(&b, "🇬🇧 Metro Manila prices — %s\n", dt.EN)
	fmt.Fprintf(&b, "Market median vs %s:\n", prevEN)
	var segs []string
	for i := range d.Rows {
		r := &d.Rows[i]
		if r.Metro == nil {
			continue
		}
		s := fmt.Sprintf("%s ₱%s%s", r.EN, fmtPeso(r.Metro.Price), unitFor(r, "en"))
		if t := Trend(r.Metro); t != "" {
			s += " " + t
		}
		segs = append(segs, s)
	}
	b.WriteString(strings.Join(segs, " · ") + "\n")
	if d.Best != nil {
		fmt.Fprintf(&b, "⭐ Best deal vs usual: %s\n", fbregion.TopDealEN(d.Best.Deal))
	}
	if n := d.dropNames("en"); n != "" {
		fmt.Fprintf(&b, "ℹ️ Not in this digest yet: %s\n", n)
	}
	b.WriteString("\n" + fbregion.Separator + "\n")

	// Japanese.
	fmt.Fprintf(&b, "🇯🇵 メトロマニラの価格 — %s\n", dt.JA)
	fmt.Fprintf(&b, "市場の中央値、%s比：\n", prevJA)
	segs = segs[:0]
	for i := range d.Rows {
		r := &d.Rows[i]
		if r.Metro == nil {
			continue
		}
		s := fmt.Sprintf("%s ₱%s%s", r.JA, fmtPeso(r.Metro.Price), unitFor(r, "ja"))
		if t := Trend(r.Metro); t != "" {
			s += " " + t
		}
		segs = append(segs, s)
	}
	b.WriteString(strings.Join(segs, "、") + "\n")
	if d.Best != nil {
		fmt.Fprintf(&b, "⭐ いつもより安い：%s\n", fbregion.TopDealJA(d.Best.Deal))
	}
	if n := d.dropNames("ja"); n != "" {
		fmt.Fprintf(&b, "ℹ️ 今回は未掲載：%s\n", n)
	}
	b.WriteString("\n" + fbregion.Link + "\n")
	return b.String()
}

// AltText is the one-line trilingual image alt text.
func AltText(d *Digest) (tl, en, ja string) {
	dt := fbregion.FormatDates(d.AsOf)
	n := len(d.Included())
	return fmt.Sprintf("Presyo sa Metro Manila noong %s: %d rehiyon, mga pangunahing bilihin at trend.", dt.TL, n),
		fmt.Sprintf("Metro Manila prices on %s: %d regions, key staples with trend arrows.", dt.EN, n),
		fmt.Sprintf("%sのメトロマニラ価格：%d地域、主要食品と前回比の矢印。", dt.JA, n)
}

var (
	pesoRe = regexp.MustCompile(`₱([0-9][0-9,]*(?:\.[0-9]+)?)`)
	pctRe  = regexp.MustCompile(`([▲▼→]?)\s*([+−\-±])([0-9]+)\s*[%％]`)
)

// CheckReport is the digest checker result.
type CheckReport struct {
	Status  string   `json:"status"`
	Flagged []string `json:"flagged,omitempty"`
}

// CheckCaption verifies that every peso amount and percent in the caption
// comes from the digest (metro cells or the best deal) and that every
// trend glyph matches its sign. A failure blocks the post.
func CheckCaption(d *Digest, text string) *CheckReport {
	rep := &CheckReport{Status: fbregion.StatusPass}
	flag := func(s string) {
		rep.Status = fbregion.StatusFail
		rep.Flagged = append(rep.Flagged, s)
	}
	pesos := map[float64]bool{}
	pcts := map[int]bool{}
	for i := range d.Rows {
		if m := d.Rows[i].Metro; m != nil {
			pesos[m.Price] = true
			if m.Pct != nil {
				pcts[*m.Pct] = true
			}
		}
	}
	if d.Best != nil {
		pesos[d.Best.Deal.Price] = true
		pcts[d.Best.Deal.PctRounded] = true
	}
	if strings.Contains(text, "$") {
		flag("dollar sign")
	}
	for _, m := range pesoRe.FindAllStringSubmatch(text, -1) {
		v, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if !pesos[v] {
			flag("₱" + m[1] + ": not in digest")
		}
	}
	for _, m := range pctRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[3])
		switch m[2] {
		case "−", "-":
			n = -n
		}
		if !pcts[n] && !(n == 0 && m[2] == "±") {
			flag(fmt.Sprintf("%s%s%s%%: not in digest", m[1], m[2], m[3]))
			continue
		}
		switch m[1] {
		case "▲":
			if n <= 0 {
				flag("▲ with non-positive percent")
			}
		case "▼":
			if n >= 0 {
				flag("▼ with non-negative percent")
			}
		case "→":
			if n != 0 {
				flag("→ with non-zero percent")
			}
		}
	}
	return rep
}
