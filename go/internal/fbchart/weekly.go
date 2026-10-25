package fbchart

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

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
	unit, zx := barScale(maxNeg, maxPos)
	unit = fitScaleRight(maxNeg, maxPos, pricier, unit)
	zx = zeroX(maxNeg, maxPos, unit)

	const dealNat, prNat, areaH = 85, 85, 56
	nDealRows := len(deals)
	noteH := 0
	if nDealRows == 0 {
		nDealRows = 1
		noteH = 60
	}
	nPrRows := len(pricier)
	prNoteH := 0
	if nPrRows == 0 {
		nPrRows = 1
		prNoteH = 60
	}
	pillH := 52 + 14
	reserve := pillH + pillH // deals pill + area pill
	areaRows := len(sig.DealsByRegion)
	if areaRows == 0 {
		areaRows = 1
	}
	dealH, prH := dealNat, prNat
	need := nDealRows*dealH + nPrRows*prH + areaRows*areaH
	if noteH > 0 {
		need = noteH + nPrRows*prH + areaRows*areaH - (nDealRows-1)*dealH
	}
	_ = prNoteH
	if avail := footerTop - y - reserve; need > avail && need > 0 {
		f := float64(avail) / float64(need)
		dealH = int(float64(dealH) * f)
		prH = int(float64(prH) * f)
		if dealH < 64 {
			dealH = 64
		}
		if prH < 64 {
			prH = 64
		}
	}

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
		y = drawWeeklyRow(dst, r, y, dealH, zx, unit, i == len(deals)-1 && len(pricier) == 0)
	}
	var rowsBottom int
	if len(pricier) > 0 {
		ppill := "Tumaas"
		ppillW := measure(ppill, Bold, 32) + 52
		y += 8
		fillRounded(dst, marginL, y, marginL+ppillW, y+52, 12, red)
		drawString(dst, ppill, Bold, 32, white, marginL+26, y+37)
		y += 52 + 14
		for i, r := range pricier {
			y = drawWeeklyRow(dst, r, y, prH, zx, unit, i == len(pricier)-1)
		}
		rowsBottom = y
	} else {
		ppill := "Tumaas"
		ppillW := measure(ppill, Bold, 32) + 52
		y += 8
		fillRounded(dst, marginL, y, marginL+ppillW, y+52, 12, red)
		drawString(dst, ppill, Bold, 32, white, marginL+26, y+37)
		y += 52 + 14
		drawString(dst, "Walang malaking pagbabago.", Medium, 28, redName, marginL, y+40)
		y += prNoteH
		rowsBottom = y
	}

	// Area pill + compact rows (no zero line for these).
	apill := "Pinakasulit kada rehiyon"
	apillW := measure(apill, Bold, 32) + 52
	y += 8
	fillRounded(dst, marginL, y, marginL+apillW, y+52, 12, greenDk)
	drawString(dst, apill, Bold, 32, white, marginL+26, y+37)
	y += 52 + 14
	for i, rd := range sig.DealsByRegion {
		last := i == len(sig.DealsByRegion)-1
		if len(rd.Deals) == 0 {
			line := fmt.Sprintf("%s · walang kapansin-pansin", rd.TitleTL)
			ln := fitName(line)
			drawString(dst, ln.text, Bold, ln.size, green, marginL, y+36)
			if !last {
				hline(dst, marginL, W-marginR, y+areaH, 2, sepCol)
			}
			y += areaH
			continue
		}
		d := rd.Deals[0]
		line := fmt.Sprintf("%s · %s", rd.TitleTL, weekly.DisplayTL(d.Name))
		ln := fitName(line)
		drawString(dst, ln.text, Bold, ln.size, green, marginL, y+36)
		mkt := fmt.Sprintf("(%s)", d.MarketShort)
		drawString(dst, mkt, Medium, 22, mutCol, marginL, y+areaH-6)
		chip := fmtPct(d.Pct)
		cw := measure(chip, Bold, 26) + 28
		cx1 := W - marginR - cw
		if cx1 < marginL+measure(ln.text, Bold, ln.size)+20 {
			cx1 = marginL + measure(ln.text, Bold, ln.size) + 20
		}
		fillRounded(dst, cx1, y+6, cx1+cw, y+6+36, 10, green)
		drawString(dst, chip, Bold, 26, white, cx1+14, y+6+26)
		if !last {
			hline(dst, marginL, W-marginR, y+areaH, 2, sepCol)
		}
		y += areaH
	}
	rowsBottom = y

	// ONE shared zero line for the metro sections.
	if len(deals) > 0 || len(pricier) > 0 {
		vline(dst, zx, rowsTop, rowsBottom, 3, zeroCol)
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

// drawWeeklyRow renders one 2-line metro row (name + price) with the bar.
func drawWeeklyRow(dst *image.RGBA, r rowItem, y, rowH, zx int, unit float64, last bool) int {
	name := fitName(r.name)
	col := green
	if !r.deal {
		col = redName
	}
	drawString(dst, name.text, Bold, name.size, col, marginL, y+36)
	drawString(dst, r.price, SemiBold, 27, col, marginL, y+70)
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
