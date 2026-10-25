package fbchart

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

// Weekly region section layout: each region row spans the full content
// width with two text lines and a right-aligned chip.
const (
	weeklyAreaH     = 72
	weeklyDealNat   = 85
	weeklyPrNat     = 85
	weeklyMinBarH   = 48
	weeklyBottomGap = 8
)

// Compact metro bar-row labels ("Sayote ₱60/kg") share ONE uniform size:
// the largest size in [20,28] at which every row fits the label column.
// Name is Bold, price is SemiBold, same baseline and size, row colour.
const (
	weeklyLabelMax = 28.0
	weeklyLabelMin = 20.0
)

// weeklyLabelWidth is the drawn width of one compact bar-row label with
// the name in Bold and the price in SemiBold at the same size.
func weeklyLabelWidth(name, price string, size float64) int {
	return measure(name, Bold, size) + measure(" ", Bold, size) + measure(price, SemiBold, size)
}

// weeklyUniformSize returns the largest size in [weeklyLabelMin,
// weeklyLabelMax] at which every row fits maxW; weeklyLabelMin when even
// that does not fit (caller widens the label column first).
func weeklyUniformSize(rows []rowItem, maxW int) float64 {
	for s := weeklyLabelMax; s >= weeklyLabelMin; s-- {
		ok := true
		for _, r := range rows {
			if weeklyLabelWidth(r.name, r.price, s) > maxW {
				ok = false
				break
			}
		}
		if ok {
			return s
		}
	}
	return weeklyLabelMin
}

// weeklyWidenExtra returns how many px the label column must grow so the
// widest row fits at weeklyLabelMin (0 when it already fits).
func weeklyWidenExtra(rows []rowItem, maxW int) int {
	need := 0
	for _, r := range rows {
		if w := weeklyLabelWidth(r.name, r.price, weeklyLabelMin); w > need {
			need = w
		}
	}
	if need > maxW {
		return need - maxW
	}
	return 0
}

// weeklyEllipsizeName truncates the name with "…" so name+" "+price fits
// maxW at size (last resort after widening; current data never hits it).
func weeklyEllipsizeName(name, price string, size float64, maxW int) string {
	if weeklyLabelWidth(name, price, size) <= maxW {
		return name
	}
	ell := "…"
	if !montHas(Bold, '…') {
		ell = "..."
	}
	rs := []rune(name)
	for len(rs) > 0 && weeklyLabelWidth(string(rs)+ell, price, size) > maxW {
		rs = rs[:len(rs)-1]
	}
	return string(rs) + ell
}

// weeklyBarHeights computes shrink-to-fit bar row heights for the metro
// sections so that 5 drops + 3 rises + 5 region rows always end above the
// footer. Only the bar sections shrink; region rows keep weeklyAreaH.
func weeklyBarHeights(availForBars, needBars int, nDeal, nPr int) (dealH, prH int) {
	dealH, prH = weeklyDealNat, weeklyPrNat
	if needBars <= 0 || availForBars >= needBars {
		return dealH, prH
	}
	f := float64(availForBars) / float64(needBars)
	dealH = int(float64(weeklyDealNat) * f)
	prH = int(float64(weeklyPrNat) * f)
	if nDeal > 0 && dealH < weeklyMinBarH {
		dealH = weeklyMinBarH
	}
	if nPr > 0 && prH < weeklyMinBarH {
		prH = weeklyMinBarH
	}
	if nDeal == 0 {
		dealH = weeklyDealNat
	}
	if nPr == 0 {
		prH = weeklyPrNat
	}
	return dealH, prH
}

// weeklyScaleUnit is scaleUnit for a custom label/bar boundary x0 (bar
// area x0..barX1); scaleUnit itself is the barX0 case (daily unchanged).
func weeklyScaleUnit(x0, maxNeg, maxPos int) float64 {
	barW := barX1 - x0
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
		unit := negW / float64(maxNeg)
		if u := posW / float64(maxPos); u < unit {
			unit = u
		}
		return unit
	case maxNeg > 0:
		return float64(span) / float64(maxNeg)
	default:
		if maxPos == 0 {
			maxPos = 1
		}
		return float64(span) / float64(maxPos)
	}
}

// weeklyZeroX is zeroX for a custom boundary x0.
func weeklyZeroX(x0, maxNeg, maxPos int, unit float64) int {
	switch {
	case maxNeg > 0 && maxPos > 0:
		return x0 + pad + int(unit*float64(maxNeg))
	case maxNeg > 0:
		return barX1 - pad
	default:
		return x0 + pad
	}
}

// weeklyFitScaleRight is fitScaleRight for a custom boundary x0.
func weeklyFitScaleRight(x0, maxNeg, maxPos int, pricier []rowItem, unit float64) float64 {
	for round := 0; round < 10; round++ {
		worst := -1.0
		for _, r := range pricier {
			lbl := fmtPct(r.pct)
			length := barLen(r.pct, unit)
			if labelInside(length, measure(lbl, Bold, labelInSize)) {
				continue
			}
			lwOut := measure(lbl, Bold, labelOutSize)
			need := float64(W - 16 - pad - lwOut - (x0 + pad + barGap))
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

// weeklyBarLayout picks the single uniform compact-label size for all
// metro rows and, only when even weeklyLabelMin does not fit the default
// label column, widens the column (shifts the zero axis right, shrinking
// the bar scale) so it fits before any ellipsis.
func weeklyBarLayout(deals, pricier []rowItem, maxNeg, maxPos int) (labelSize float64, x0 int, unit float64, zx int) {
	all := make([]rowItem, 0, len(deals)+len(pricier))
	all = append(all, deals...)
	all = append(all, pricier...)
	if len(all) == 0 {
		unit, zx = barScale(maxNeg, maxPos)
		unit = fitScaleRight(maxNeg, maxPos, pricier, unit)
		zx = zeroX(maxNeg, maxPos, unit)
		return weeklyLabelMax, barX0, unit, zx
	}
	baseMaxW := barX0 - marginL - 12
	labelSize = weeklyUniformSize(all, baseMaxW)
	x0 = barX0
	if extra := weeklyWidenExtra(all, baseMaxW); extra > 0 {
		x0 = barX0 + extra
		// Keep a usable bar area; cap the shift and let the ellipsis
		// fallback cover pathological names (never hit in practice).
		const minBarArea = 160
		if barX1-x0 < minBarArea {
			x0 = barX1 - minBarArea
		}
		labelSize = weeklyLabelMin
	}
	unit = weeklyScaleUnit(x0, maxNeg, maxPos)
	unit = weeklyFitScaleRight(x0, maxNeg, maxPos, pricier, unit)
	zx = weeklyZeroX(x0, maxNeg, maxPos, unit)
	return labelSize, x0, unit, zx
}

// RenderWeekly builds the 1080x1350 Sunday report chart for weekly signals.
func RenderWeekly(sig *weekly.Signals) (*image.RGBA, error) {
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)

	rng := weekly.RangeOf(sig)

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
	drawString(dst, headText, SemiBold, headSize, green, hx, hy+34+18)

	title := "Lingguhang ulat"
	ts := fitSize(title, ExtraBold, 64, 28, W-2*marginL-60)
	tx := (W - measure(title, ExtraBold, ts)) / 2
	drawString(dst, title, ExtraBold, ts, green, tx, 184)
	sub := fmt.Sprintf("%s · Metro Manila · vs nakaraang linggo", rng.TL)
	ss := fitSize(sub, Medium, 30, 20, W-marginL-marginR)
	sx := (W - measure(sub, Medium, ss)) / 2
	drawString(dst, sub, Medium, ss, green, sx, 226)

	var deals, pricier []rowItem
	for _, m := range sig.Drops {
		deals = append(deals, rowItem{
			name:  weekly.DisplayTL(m.Name),
			price: fmt.Sprintf("₱%s%s", fmtMoney(m.DisplayPrice), unitSuffix(m.Unit)),
			pct:   m.Pct, deal: true,
		})
	}
	for _, m := range sig.Rises {
		pricier = append(pricier, rowItem{
			name:  weekly.DisplayTL(m.Name),
			price: fmt.Sprintf("₱%s%s", fmtMoney(m.DisplayPrice), unitSuffix(m.Unit)),
			pct:   m.Pct, deal: false,
		})
	}

	y := 256
	footerTop := H - 118

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
	labelSize, labelX0, unit, zx := weeklyBarLayout(deals, pricier, maxNeg, maxPos)

	// Vertical budget: pills + gaps + fixed region rows + bottom gap are
	// fixed; only the metro bar rows shrink.
	const dealPillH = 52 + 14
	const metroPillBlock = 8 + 52 + 14
	const areaPillBlock = 8 + 52 + 14
	areaRows := len(sig.DealsByRegion)
	if areaRows == 0 {
		areaRows = 1
	}
	noteH := 0
	if len(deals) == 0 {
		noteH = 60
	}
	prNoteH := 0
	if len(pricier) == 0 {
		prNoteH = 60
	}
	fixed := dealPillH + metroPillBlock + areaPillBlock + areaRows*weeklyAreaH + weeklyBottomGap
	needBars := len(deals)*weeklyDealNat + len(pricier)*weeklyPrNat
	if len(deals) == 0 {
		needBars += noteH
	}
	if len(pricier) == 0 {
		needBars += prNoteH
	}
	// Notes are fixed height; only bar rows shrink.
	needShrink := len(deals)*weeklyDealNat + len(pricier)*weeklyPrNat
	availForBars := footerTop - y - fixed
	if len(deals) == 0 {
		availForBars += 0 // note already in fixed? no, account below
	}
	// availForBars currently excludes notes; add them back for shrink math:
	// fixed includes pills+areas+gap but not notes, so bars avail is:
	// availForBars = footerTop - y - fixed - notes
	availForBars -= noteH + prNoteH
	dealH, prH := weeklyBarHeights(availForBars, needShrink, len(deals), len(pricier))
	_ = needBars

	// Deals pill.
	pillText := "Bumaba ngayong linggo"
	pillW := measure(pillText, Bold, 32) + 52
	fillRounded(dst, marginL, y, marginL+pillW, y+52, 12, green)
	drawString(dst, pillText, Bold, 32, white, marginL+26, y+37)
	y += 52 + 14

	rowsTop := y
	if len(deals) == 0 {
		drawString(dst, "Walang malaking pagbabago.", Medium, 28, green, marginL, y+40)
		y += noteH
	}
	for i, r := range deals {
		y = drawWeeklyRow(dst, r, y, dealH, zx, unit, labelSize, labelX0, i == len(deals)-1 && len(pricier) == 0)
	}
	metroBottom := y
	if len(pricier) > 0 {
		ppill := "Tumaas"
		ppillW := measure(ppill, Bold, 32) + 52
		y += 8
		fillRounded(dst, marginL, y, marginL+ppillW, y+52, 12, red)
		drawString(dst, ppill, Bold, 32, white, marginL+26, y+37)
		y += 52 + 14
		for i, r := range pricier {
			y = drawWeeklyRow(dst, r, y, prH, zx, unit, labelSize, labelX0, i == len(pricier)-1)
		}
		metroBottom = y
	} else {
		ppill := "Tumaas"
		ppillW := measure(ppill, Bold, 32) + 52
		y += 8
		fillRounded(dst, marginL, y, marginL+ppillW, y+52, 12, red)
		drawString(dst, ppill, Bold, 32, white, marginL+26, y+37)
		y += 52 + 14
		drawString(dst, "Walang malaking pagbabago.", Medium, 28, redName, marginL, y+40)
		y += prNoteH
		metroBottom = y
	}

	// ONE shared zero line for the metro sections only: it stops at the
	// end of the Tumaas bars and never runs through the region section.
	if len(deals) > 0 || len(pricier) > 0 {
		vline(dst, zx, rowsTop, metroBottom, 3, zeroCol)
	}

	// Area pill + region rows spanning the full content width.
	apill := "Pinakasulit kada rehiyon"
	apillW := measure(apill, Bold, 32) + 52
	y += 8
	fillRounded(dst, marginL, y, marginL+apillW, y+52, 12, greenDk)
	drawString(dst, apill, Bold, 32, white, marginL+26, y+37)
	y += 52 + 14
	if len(sig.DealsByRegion) == 0 {
		drawString(dst, "Walang kapansin-pansin.", Medium, 24, mutCol, marginL, y+40)
		y += weeklyAreaH
	}
	for i, rd := range sig.DealsByRegion {
		last := i == len(sig.DealsByRegion)-1
		if len(rd.Deals) == 0 {
			y = drawRegionRow(dst, rd.TitleTL, "walang kapansin-pansin", 0, false, y, weeklyAreaH, last)
			continue
		}
		d := rd.Deals[0]
		detail := fmt.Sprintf("%s · %s", weekly.DisplayTL(d.Name), d.MarketShort)
		y = drawRegionRow(dst, rd.TitleTL, detail, d.Pct, true, y, weeklyAreaH, last)
	}

	// Footer band (same as daily).
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

// drawRegionRow renders one region row: line 1 = region title (bold,
// dark green, ~26px), line 2 = "commodity · market" (~24px, muted), and a
// green % chip right-aligned and vertically centered. Text never overlaps
// the chip: it shrinks (fitSize) and only ellipsizes as a fallback.
func drawRegionRow(dst *image.RGBA, title, detail string, pct int, hasChip bool, y, rowH int, last bool) int {
	var chip string
	cw := 0
	cx1 := 0
	if hasChip {
		chip = fmtPct(pct)
		cw = measure(chip, Bold, 26) + 28
		cx1 = W - marginR - cw
	}
	maxW := W - marginL - marginR
	if hasChip {
		maxW = cx1 - 12 - marginL
	}
	tSize := fitSize(title, Bold, 26, 20, maxW)
	tText := ellipsize(title, Bold, tSize, maxW)
	dSize := fitSize(detail, Medium, 24, 18, maxW)
	dText := ellipsize(detail, Medium, dSize, maxW)
	drawString(dst, tText, Bold, tSize, green, marginL, y+28)
	drawString(dst, dText, Medium, dSize, mutCol, marginL, y+62)
	if hasChip {
		cy := y + rowH/2
		fillRounded(dst, cx1, cy-18, cx1+cw, cy+18, 10, green)
		drawString(dst, chip, Bold, 26, white, cx1+14, cy+9)
	}
	if !last {
		hline(dst, marginL, W-marginR, y+rowH, 2, sepCol)
	}
	return y + rowH
}

// drawWeeklyRow renders one metro row as a single line "name ₱price/unit"
// with the bar. All rows share the uniform labelSize from weeklyBarLayout:
// the name is Bold, the price SemiBold, same size and baseline, row colour
// as before. The label column (marginL..x0) is already widened when needed
// so no ellipsis occurs for current data; name truncation is a last resort.
func drawWeeklyRow(dst *image.RGBA, r rowItem, y, rowH, zx int, unit float64, labelSize float64, x0 int, last bool) int {
	col := green
	if !r.deal {
		col = redName
	}
	xi := int(x0)
	maxW := xi - marginL - 12
	name := weeklyEllipsizeName(r.name, r.price, labelSize, maxW)
	by := y + rowH/2 + 8
	drawString(dst, name, Bold, labelSize, col, marginL, by)
	nx := marginL + measure(name, Bold, labelSize) + measure(" ", Bold, labelSize)
	drawString(dst, r.price, SemiBold, labelSize, col, nx, by)
	lbl := fmtPct(r.pct)
	var g barPlace
	if r.deal {
		g = placeDealBar(y, rowH, zx, barLen(r.pct, unit), lbl)
		fillRounded(dst, g.bx0, g.by0, g.bx1, g.by1, 8, green)
	} else {
		g = placePricierBar(y, rowH, zx, barLen(r.pct, unit), lbl)
		fillRounded(dst, g.bx0, g.by0, g.bx1, g.by1, 8, red)
	}
	if g.inside {
		drawString(dst, lbl, Bold, g.size, white, g.lx, g.ly)
	} else if r.deal {
		drawString(dst, lbl, Bold, g.size, green, g.lx, g.ly)
	} else {
		drawString(dst, lbl, Bold, g.size, red, g.lx, g.ly)
	}
	if !last {
		hline(dst, marginL, W-marginR, y+rowH, 2, sepCol)
	}
	return y + rowH
}
