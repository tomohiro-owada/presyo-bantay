package fbchart

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Dimensions of the Facebook post chart.
const (
	W = 1080
	H = 1350
)

const (
	marginL = 70
	marginR = 70
	// Text column runs marginL..barX0 (max ~330 px, names shrink to
	// fit); bar area runs barX0..barX1.
	barX0 = 370
	barX1 = 1030
	// pad is the gap between bars/labels/zero line; barGap separates
	// bars from the shared zero line.
	pad    = 10
	barGap = 6
	// barH is the bar thickness. Inside labels use labelInSize white
	// bold, vertically centred in the bar with >= 10 px padding from the
	// bar's outer end; a label goes inside only when label width + 24 px
	// fits in the bar, else just outside the bar end.
	barH         = 46
	labelInSize  = 26.0
	labelOutSize = 30.0
)

// fmtMoney mirrors the post text: whole amounts print without decimals.
func fmtMoney(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

// fmtPct formats an integer percent with U+2212 minus / plus.
func fmtPct(p int) string {
	if p < 0 {
		return fmt.Sprintf("−%d%%", -p)
	}
	return fmt.Sprintf("+%d%%", p)
}

func unitSuffix(unit string) string {
	if len(unit) >= 5 && (unit[:5] == "piece" || unit == "per piece") {
		return "/piraso"
	}
	for _, u := range []string{"piece", "pc", "piraso"} {
		if containsFold(unit, u) {
			return "/piraso"
		}
	}
	return "/kg"
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		eq := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				eq = false
				break
			}
		}
		if eq {
			return true
		}
	}
	return false
}

// rowItem is one rendered bar row.
type rowItem struct {
	name   string
	price  string
	market string // short, empty for pricier display
	pct    int
	deal   bool
}

// Render builds the 1080x1350 chart for region signals.
func Render(sig *signals.RegionSignals) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)

	d := fbregion.FormatDates(sig.AsOf)

	// Header row: icon + "Presyo Bantay", centered.
	icon := Icon()
	headText := "Presyo Bantay"
	headSize := 44.0
	tw := measure(headText, SemiBold, headSize)
	rowW := 0
	if icon != nil {
		rowW = icon.Bounds().Dx() + 18 + tw
	} else {
		rowW = tw
	}
	hx := (W - rowW) / 2
	hy := 28
	if icon != nil {
		draw.Draw(dst, image.Rect(hx, hy, hx+icon.Bounds().Dx(), hy+icon.Bounds().Dy()),
			icon, image.Point{}, draw.Over)
		hx += icon.Bounds().Dx() + 18
	}
	ascent := 34
	drawString(dst, headText, SemiBold, headSize, green, hx, hy+ascent+18)

	// Title + second line (report date is the DA report date).
	title := fmt.Sprintf("Sulit sa %s", sig.Region.TitleTL)
	ts := fitSize(title, ExtraBold, 64, 28, W-2*marginL-60)
	tx := (W - measure(title, ExtraBold, ts)) / 2
	drawString(dst, title, ExtraBold, ts, green, tx, 184)
	sub := fmt.Sprintf("Presyo noong %s · vs usual (30-day median), per kg", d.TL)
	ss := fitSize(sub, Medium, 30, 20, W-marginL-marginR)
	sx := (W - measure(sub, Medium, ss)) / 2
	drawString(dst, sub, Medium, ss, green, sx, 226)

	// Rows.
	var deals, pricier []rowItem
	for _, dl := range sig.Deals {
		deals = append(deals, rowItem{
			name:   fbregion.DisplayTL(dl.Name),
			price:  fmt.Sprintf("₱%s%s", fmtMoney(dl.Price), unitSuffix(dl.Unit)),
			market: fmt.Sprintf("(%s)", dl.MarketShort),
			pct:    dl.PctRounded, deal: true,
		})
	}
	for _, p := range sig.Pricier {
		pricier = append(pricier, rowItem{
			name:  fbregion.DisplayTL(p.Name),
			price: fmt.Sprintf("₱%s%s", fmtMoney(p.PriceMedian), unitSuffix(p.Unit)),
			pct:   p.PctRounded, deal: false,
		})
	}

	y := 256
	footerTop := H - 118

	// Shared zero line + common scale. The negative side gets width
	// proportional to max|deal pct| and the positive side to max pricier
	// pct (minimum 20% share each when both sections are present); the
	// scale is (bar area width)/(sum of maxima) so the longest bar on
	// each side spans most of its side. Pct labels go inside the bar
	// (white bold, near the outer end) when it is long enough, else just
	// outside the bar end.
	maxNeg, maxPos := 0, 0
	for _, r := range deals {
		if a := abs(r.pct); a > maxNeg {
			maxNeg = a
		}
	}
	for _, r := range pricier {
		if a := abs(r.pct); a > maxPos {
			maxPos = a
		}
	}
	var unit float64
	var zx int
	unit, zx = barScale(maxNeg, maxPos)
	// Guarantee every outside pricier label fits with >= 16 px right
	// canvas margin (a dominant negative side can otherwise push short
	// red bars' labels off-canvas).
	unit = fitScaleRight(maxNeg, maxPos, pricier, unit)
	zx = zeroX(maxNeg, maxPos, unit)

	// Row heights from the row counts: deals rows carry 3 lines (~110),
	// pricier rows 2 lines (~85); scaled down only if they overflow.
	const dealNat, prNat = 110, 85
	nDealRows := len(deals)
	noteH := 0
	if nDealRows == 0 {
		nDealRows = 1
		noteH = 60 // single-line empty-section note
	}
	nPrRows := len(pricier)
	pillH := 52 + 14 // pill + gap
	reserve := pillH
	if nPrRows > 0 {
		reserve += 8 + pillH // gap + pricier pill + gap
	}
	dealH, prH := dealNat, prNat
	need := nDealRows*dealH + nPrRows*prH
	if noteH > 0 {
		need = noteH + nPrRows*prH
	}
	if avail := footerTop - y - reserve; need > avail && need > 0 {
		f := float64(avail) / float64(need)
		dealH = int(float64(dealH) * f)
		prH = int(float64(prH) * f)
		if dealH < 80 {
			dealH = 80
		}
		if prH < 64 {
			prH = 64
		}
	}

	// Deals pill.
	pillText := "Mas mura kaysa usual"
	pillW := measure(pillText, Bold, 32) + 52
	fillRounded(dst, marginL, y, marginL+pillW, y+52, 12, green)
	drawString(dst, pillText, Bold, 32, white, marginL+26, y+37)
	y += 52 + 14

	rowsTop := y
	if len(deals) == 0 {
		drawString(dst, "Walang kapansin-pansing mura ngayon.", Medium, 28, green, marginL, y+40)
		y += noteH
	}
	for i, r := range deals {
		y = drawDealRow(dst, r, y, dealH, zx, unit, i == len(deals)-1 && len(pricier) == 0)
	}
	var rowsBottom int
	if len(pricier) > 0 {
		// Pricier pill.
		ppill := "Mas mahal ngayon"
		ppillW := measure(ppill, Bold, 32) + 52
		y += 8
		fillRounded(dst, marginL, y, marginL+ppillW, y+52, 12, red)
		drawString(dst, ppill, Bold, 32, white, marginL+26, y+37)
		y += 52 + 14
		for i, r := range pricier {
			y = drawPricierRow(dst, r, y, prH, zx, unit, i == len(pricier)-1)
		}
		rowsBottom = y
	} else {
		rowsBottom = y
	}

	// ONE shared zero line for both sections.
	if len(deals) > 0 || len(pricier) > 0 {
		vline(dst, zx, rowsTop, rowsBottom, 3, zeroCol)
	}

	// Footer band: two clear columns, vertically centered.
	fillRect(dst, 0, footerTop, W, H, greenDk)
	midY := footerTop + (H-footerTop)/2
	left1 := "Source: DA Price Monitoring"
	left2 := "Hindi opisyal na DA page"
	drawString(dst, left1, SemiBold, 24, white, marginL, midY-4)
	drawString(dst, left2, SemiBold, 24, white, marginL, midY+28)
	foot := "abalol.xyz/presyo-bantay"
	fw := measure(foot, Bold, 26)
	footX := W - marginR - fw
	drawString(dst, foot, Bold, 26, white, footX, midY+9)
	leftEnd := marginL + measure(left1, SemiBold, 24)
	if w := measure(left2, SemiBold, 24); marginL+w > leftEnd {
		leftEnd = marginL + w
	}
	if divX := (leftEnd + footX) / 2; divX > leftEnd+20 && divX < footX-20 {
		vline(dst, divX, footerTop+20, H-20, 2, footDiv)
	}
	return dst, nil
}

// drawDealRow renders one 3-line deals row (name, price, market) with the
// bar + pct label, returning the next y. last controls the separator.
func drawDealRow(dst *image.RGBA, r rowItem, y, rowH, zx int, unit float64, last bool) int {
	name := fitName(r.name)
	drawString(dst, name.text, Bold, name.size, green, marginL, y+38)
	drawString(dst, r.price, SemiBold, 27, green, marginL, y+72)
	drawString(dst, r.market, Medium, 22, mutCol, marginL, y+101)
	lbl := fmtPct(r.pct)
	g := placeDealBar(y, rowH, zx, barLen(r.pct, unit), lbl)
	fillRounded(dst, g.bx0, g.by0, g.bx1, g.by1, 8, green)
	if g.inside {
		drawString(dst, lbl, Bold, g.size, white, g.lx, g.ly)
	} else {
		drawString(dst, lbl, Bold, g.size, green, g.lx, g.ly)
	}
	if !last {
		hline(dst, marginL, W-marginR, y+rowH, 2, sepCol)
	}
	return y + rowH
}

// drawPricierRow renders one 2-line pricier row (dark-red name + price)
// with the bar + pct label, returning the next y.
func drawPricierRow(dst *image.RGBA, r rowItem, y, rowH, zx int, unit float64, last bool) int {
	name := fitName(r.name)
	drawString(dst, name.text, Bold, name.size, redName, marginL, y+36)
	drawString(dst, r.price, SemiBold, 27, redName, marginL, y+70)
	lbl := fmtPct(r.pct)
	g := placePricierBar(y, rowH, zx, barLen(r.pct, unit), lbl)
	fillRounded(dst, g.bx0, g.by0, g.bx1, g.by1, 8, red)
	if g.inside {
		drawString(dst, lbl, Bold, g.size, white, g.lx, g.ly)
	} else {
		drawString(dst, lbl, Bold, g.size, red, g.lx, g.ly)
	}
	if !last {
		hline(dst, marginL, W-marginR, y+rowH, 2, sepCol)
	}
	return y + rowH
}

// barScale computes the common bar scale (px per pct point) and the
// shared zero-line x for the given maxima. Each side's width is
// proportional to its maximum (minimum 20% share each when both sides
// are present); the scale is (bar area width)/(sum of maxima) so the
// longest bar on each side spans most of its side.
func barScale(maxNeg, maxPos int) (unit float64, zx int) {
	unit = scaleUnit(maxNeg, maxPos)
	return unit, zeroX(maxNeg, maxPos, unit)
}

// scaleUnit computes the common bar scale (px per pct point).
func scaleUnit(maxNeg, maxPos int) (unit float64) {
	barW := barX1 - barX0
	span := barW - 2*pad
	if span < 60 {
		span = 60
	}
	switch {
	case maxNeg > 0 && maxPos > 0:
		shareN := float64(maxNeg) / float64(maxNeg+maxPos)
		if shareN < 0.2 {
			shareN = 0.2
		}
		if shareN > 0.8 {
			shareN = 0.8
		}
		negW := float64(span) * shareN
		posW := float64(span) - negW
		unit = negW / float64(maxNeg)
		if u := posW / float64(maxPos); u < unit {
			unit = u
		}
	case maxNeg > 0:
		unit = float64(span) / float64(maxNeg)
	default:
		if maxPos == 0 {
			maxPos = 1
		}
		unit = float64(span) / float64(maxPos)
	}
	return unit
}

// zeroX returns the shared zero-line x for a scale.
func zeroX(maxNeg, maxPos int, unit float64) int {
	switch {
	case maxNeg > 0 && maxPos > 0:
		return barX0 + pad + int(unit*float64(maxNeg))
	case maxNeg > 0:
		return barX1 - pad
	default:
		return barX0 + pad
	}
}

// fitScaleRight shrinks the common scale (to a fixpoint) so every
// outside pricier label fits with >= 16 px right canvas margin. Each
// round fixes the worst current violator exactly; fixed bars stay fixed
// as the scale only shrinks, so this terminates.
func fitScaleRight(maxNeg, maxPos int, pricier []rowItem, unit float64) float64 {
	for round := 0; round < 10; round++ {
		worst := -1.0
		for _, r := range pricier {
			lbl := fmtPct(r.pct)
			length := barLen(r.pct, unit)
			if labelInside(length, measure(lbl, Bold, labelInSize)) {
				continue
			}
			lwOut := measure(lbl, Bold, labelOutSize)
			need := float64(W - 16 - pad - lwOut - (barX0 + pad + barGap))
			denom := float64(maxNeg + r.pct)
			if denom <= 0 || need <= 0 {
				continue
			}
			if cap := need / denom; cap < unit && (worst < 0 || cap < worst) {
				worst = cap
			}
		}
		if worst < 0 {
			return unit
		}
		if worst < 0.1 {
			worst = 0.1
		}
		unit = worst
	}
	return unit
}

// labelInside reports whether a label of width lw goes inside a bar of
// the given length: the bar is at least 110 px long with >= 10 px
// padding from the bar's outer end.
func labelInside(length, lw int) bool {
	return length >= 110 && length >= lw+pad
}

// barPlace is the computed geometry of one bar and its pct label,
// shared by the draw code and the bounds test.
type barPlace struct {
	bx0, bx1, by0, by1 int // bar rect
	lx, ly             int // label baseline origin
	lw                 int // label width at the drawn size
	size               float64
	inside             bool
}

// rowCenter returns the bar centre y for a row: the midpoint of the
// name/price text block, clamped so the 46 px bar stays in the row.
func rowCenter(y, rowH int, nameBl, priceBl int) int {
	cy := y + (nameBl+priceBl)/2
	if cy < y+barH/2 {
		cy = y + barH/2
	}
	if cy > y+rowH-barH/2 {
		cy = y + rowH - barH/2
	}
	return cy
}

// placeDealBar computes the geometry of a deals bar (extends left of the
// zero line) and its label: inside (white, near the outer/left end with
// >= 10 px padding) when it fits, else just outside the bar end.
func placeDealBar(y, rowH, zx, length int, lbl string) barPlace {
	cy := rowCenter(y, rowH, 38, 72)
	x1 := zx - barGap
	x0 := x1 - length
	lwIn := measure(lbl, Bold, labelInSize)
	if labelInside(length, lwIn) {
		return barPlace{bx0: x0, bx1: x1, by0: cy - barH/2, by1: cy + barH/2,
			lx: x0 + pad, ly: cy + 9, lw: lwIn, size: labelInSize, inside: true}
	}
	lwOut := measure(lbl, Bold, labelOutSize)
	return barPlace{bx0: x0, bx1: x1, by0: cy - barH/2, by1: cy + barH/2,
		lx: x0 - lwOut - pad, ly: cy + 10, lw: lwOut, size: labelOutSize, inside: false}
}

// placePricierBar computes the geometry of a pricier bar (extends right
// of the zero line) and its label.
func placePricierBar(y, rowH, zx, length int, lbl string) barPlace {
	cy := rowCenter(y, rowH, 36, 70)
	x0 := zx + barGap
	x1 := x0 + length
	lwIn := measure(lbl, Bold, labelInSize)
	if labelInside(length, lwIn) {
		return barPlace{bx0: x0, bx1: x1, by0: cy - barH/2, by1: cy + barH/2,
			lx: x1 - lwIn - pad, ly: cy + 9, lw: lwIn, size: labelInSize, inside: true}
	}
	lwOut := measure(lbl, Bold, labelOutSize)
	return barPlace{bx0: x0, bx1: x1, by0: cy - barH/2, by1: cy + barH/2,
		lx: x1 + pad, ly: cy + 10, lw: lwOut, size: labelOutSize, inside: false}
}

// fitText is a shrink-to-fit string with its drawing size.
type fitText struct {
	text string
	size float64
}

// fitName shrinks a 32px item name to fit the text column (then
// ellipsizes); the column is ~300 px wide.
func fitName(s string) fitText {
	maxW := barX0 - marginL - 12
	size := 32.0
	for size > 24 && measure(s, Bold, size) > maxW {
		size -= 2
	}
	return fitText{ellipsize(s, Bold, size, maxW), size}
}

// barLen scales |pct| by unit, keeping tiny non-zero bars visible.
func barLen(pct int, unit float64) int {
	length := int(float64(abs(pct)) * unit)
	if length < 6 && pct != 0 {
		length = 6
	}
	return length
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
