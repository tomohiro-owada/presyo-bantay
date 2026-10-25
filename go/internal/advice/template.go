package advice

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Texts holds the three deterministic fallback texts (FB-ready plain text,
// no markdown). They mirror the prompt's line structure.
type Texts struct {
	TL string `json:"tl"`
	EN string `json:"en"`
	JA string `json:"ja"`
}

// RenderTemplate builds the deterministic fallback texts from signals only.
func RenderTemplate(sig *signals.Signals) Texts {
	asOf := sig.AsOf
	var tl, en, ja []string
	tl = append(tl, "🛒 Sulit ngayon sa palengke ("+asOf+")")
	en = append(en, "🛒 Today's market deals ("+asOf+")")
	ja = append(ja, "🛒 今日のお買い得（"+asOf+"）")

	if len(sig.Deals) == 0 {
		tl = append(tl, "Walang kapansin-pansing mura ngayon.")
		en = append(en, "No notable deals today.")
		ja = append(ja, "今日は特に目立つお買い得はありません。")
	} else {
		for _, d := range sig.Deals {
			tl = append(tl, dealLineTL(d))
			en = append(en, dealLineEN(d))
			ja = append(ja, dealLineJA(d))
		}
	}

	pricier := sig.Pricier
	if len(pricier) > 3 {
		pricier = pricier[:3]
	}
	if len(pricier) > 0 {
		tl = append(tl, "Mas mahal ngayon:")
		en = append(en, "Pricier than usual:")
		ja = append(ja, "いつもより高め：")
		for _, p := range pricier {
			tl = append(tl, pricierLineTL(p))
			en = append(en, pricierLineEN(p))
			ja = append(ja, pricierLineJA(p))
		}
	}

	tlClose := "Batay sa DA price monitoring, palengke ng Las Piñas."
	enClose := "Based on DA price monitoring, Las Piñas public market."
	jaClose := "農業省（DA）の価格調査、ラス・ピニャス市場の価格。"
	if displayedUsesNearby(sig) {
		tlClose += " Ang may (kalapit na palengke) ay galing sa Alabang/La Huerta/Bicutan/Pasay."
		enClose += " Items marked (nearby markets) use Alabang/La Huerta/Bicutan/Pasay."
		jaClose += "（近隣市場）はアラバン・ラ・ウエルタ・ビクタン・パサイの価格。"
	}
	tl = append(tl, tlClose)
	en = append(en, enClose)
	ja = append(ja, jaClose)

	return Texts{
		TL: strings.Join(tl, "\n"),
		EN: strings.Join(en, "\n"),
		JA: strings.Join(ja, "\n"),
	}
}

// RenderTemplateJSON parses signals JSON then renders.
func RenderTemplateJSON(data []byte) (Texts, error) {
	var sig signals.Signals
	if err := json.Unmarshal(data, &sig); err != nil {
		return Texts{}, err
	}
	return RenderTemplate(&sig), nil
}

func isPiece(unit string) bool {
	return strings.Contains(strings.ToLower(unit), "piece")
}

func fmtMoney(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "-0" {
		s = "0"
	}
	return s
}

func fmtPct(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if strings.HasSuffix(s, ".0") {
		s = s[:len(s)-2]
	}
	return s
}

func nearbyTL(basis string) string {
	if basis == signals.BasisNearbyMedian {
		return " (kalapit na palengke)"
	}
	return ""
}

func nearbyEN(basis string) string {
	if basis == signals.BasisNearbyMedian {
		return " (nearby markets)"
	}
	return ""
}

func nearbyJA(basis string) string {
	if basis == signals.BasisNearbyMedian {
		return "（近隣市場）"
	}
	return ""
}

func fmtPesoDiff(v float64) string {
	m := fmtMoney(absF(v))
	if v < 0 {
		return "-₱" + m
	}
	return "₱" + m
}

func fmtTrendJA(t *float64) string {
	if t == nil {
		return ""
	}
	if *t >= 0 {
		return "+" + fmtPct(*t)
	}
	return fmtPct(*t)
}

// displayedUsesNearby reports whether any actually displayed item (all
// deals, first 3 pricier) uses the nearby basis. The template footnote
// decision uses this, not the signals-level flag (which may cover
// pricier #4/#5 that are never shown).
func displayedUsesNearby(sig *signals.Signals) bool {
	for _, d := range sig.Deals {
		if d.Basis == signals.BasisNearbyMedian {
			return true
		}
	}
	pricier := sig.Pricier
	if len(pricier) > 3 {
		pricier = pricier[:3]
	}
	for _, p := range pricier {
		if p.Basis == signals.BasisNearbyMedian {
			return true
		}
	}
	return false
}

func dealLineTL(d signals.DealEntry) string {
	unit := "/kg"
	if isPiece(d.Unit) {
		unit = "/piraso"
	}
	s := fmt.Sprintf("• %s: ₱%s%s — %s%% mas mura kaysa karaniwan (%s)",
		DisplayTL(d.Name), fmtMoney(d.Price), unit, fmtPct(absF(d.PctVsMedian30d)), fmtPesoDiff(d.DiffVsMedian30d))
	if d.SharpDropThisWeek && d.Trend7dPct != nil {
		s += fmt.Sprintf(", bumaba ng %s%% mula noong nakaraang linggo", fmtPct(absF(*d.Trend7dPct)))
	}
	if d.CheapestNearby != nil {
		s += fmt.Sprintf("; pinakamura sa %s: ₱%s", d.CheapestNearby.Market, fmtMoney(d.CheapestNearby.Price))
	}
	s += nearbyTL(d.Basis)
	return s
}

func dealLineEN(d signals.DealEntry) string {
	unit := "/kg"
	if isPiece(d.Unit) {
		unit = "/pc"
	}
	s := fmt.Sprintf("• %s: ₱%s%s — %s%% below usual (%s)",
		DisplayEN(d.Name), fmtMoney(d.Price), unit, fmtPct(absF(d.PctVsMedian30d)), fmtPesoDiff(d.DiffVsMedian30d))
	if d.SharpDropThisWeek && d.Trend7dPct != nil {
		s += fmt.Sprintf(", down %s%% vs last week", fmtPct(absF(*d.Trend7dPct)))
	}
	if d.CheapestNearby != nil {
		s += fmt.Sprintf("; cheapest at %s: ₱%s", d.CheapestNearby.Market, fmtMoney(d.CheapestNearby.Price))
	}
	s += nearbyEN(d.Basis)
	return s
}

func dealLineJA(d signals.DealEntry) string {
	junit := "1kg "
	if isPiece(d.Unit) {
		junit = "1個 "
	}
	s := fmt.Sprintf("• %s: %s₱%s — いつもより%s%%安い（%s）",
		DisplayJA(d.Name), junit, fmtMoney(d.Price), fmtPct(absF(d.PctVsMedian30d)), fmtPesoDiff(d.DiffVsMedian30d))
	if d.SharpDropThisWeek && d.Trend7dPct != nil {
		s += fmt.Sprintf("、先週比 %s%%（今週大きく下がった）", fmtTrendJA(d.Trend7dPct))
	}
	if d.CheapestNearby != nil {
		s += fmt.Sprintf("。%sが最安 ₱%s", MarketDisplayJA(d.CheapestNearby.Market), fmtMoney(d.CheapestNearby.Price))
	}
	s += nearbyJA(d.Basis)
	return s
}

func pricierLineTL(p signals.DealEntry) string {
	unit := "/kg"
	if isPiece(p.Unit) {
		unit = "/piraso"
	}
	s := fmt.Sprintf("• %s: ₱%s%s — %s%% mas mahal kaysa karaniwan",
		DisplayTL(p.Name), fmtMoney(p.Price), unit, fmtPct(absF(p.PctVsMedian30d)))
	if p.SharpRiseThisWeek && p.Trend7dPct != nil {
		s += fmt.Sprintf(", tumaas ng %s%% mula noong nakaraang linggo", fmtPct(absF(*p.Trend7dPct)))
	}
	s += nearbyTL(p.Basis)
	return s
}

func pricierLineEN(p signals.DealEntry) string {
	unit := "/kg"
	if isPiece(p.Unit) {
		unit = "/pc"
	}
	s := fmt.Sprintf("• %s: ₱%s%s — %s%% above usual",
		DisplayEN(p.Name), fmtMoney(p.Price), unit, fmtPct(absF(p.PctVsMedian30d)))
	if p.SharpRiseThisWeek && p.Trend7dPct != nil {
		s += fmt.Sprintf(", up %s%% vs last week", fmtPct(absF(*p.Trend7dPct)))
	}
	s += nearbyEN(p.Basis)
	return s
}

func pricierLineJA(p signals.DealEntry) string {
	junit := "1kg "
	if isPiece(p.Unit) {
		junit = "1個 "
	}
	s := fmt.Sprintf("• %s: %s₱%s — いつもより%s%%高い",
		DisplayJA(p.Name), junit, fmtMoney(p.Price), fmtPct(absF(p.PctVsMedian30d)))
	if p.SharpRiseThisWeek && p.Trend7dPct != nil {
		s += fmt.Sprintf("、先週比 %s%%（今週大きく上がった）", fmtTrendJA(p.Trend7dPct))
	}
	s += nearbyJA(p.Basis)
	return s
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// CombinedAdvice joins the three texts with section headers (for Check).
func (t Texts) CombinedAdvice() string {
	return "## Taglish\n" + t.TL + "\n## English\n" + t.EN + "\n## 日本語\n" + t.JA + "\n"
}
