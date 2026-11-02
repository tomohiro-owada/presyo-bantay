package fbchart

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

var amber = colorRGB(0xC9, 0x8A, 0x00)

func colorRGB(r, g, b byte) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 0xFF} }

// MapTopCutPx is the height (at the 720px native width of the cropped map
// images) of the top strip that holds the Grab group-ride bar and the back
// arrow. The card renderer leaves this strip out.
const MapTopCutPx = 130

func mapTopCut(b image.Rectangle) int {
	if b.Dx() <= 0 {
		return 0
	}
	c := MapTopCutPx * b.Dx() / 720
	if c > b.Dy()/3 { // never eat more than a third of a (small/odd) image
		c = b.Dy() / 3
	}
	return c
}

// defaultCardSubtitle is used only when the data carries no route at all.
const defaultCardSubtitle = "Old Zapote Public Market → Festival Mall · Alabang-Zapote Rd"

// grabCardSubtitle is the route line under the title; the pickup comes from
// the data (route.from = the current pickup), never from a constant.
func grabCardSubtitle(o *grabfare.Output) string {
	if s := grabfare.CardSubtitle(o.Route); s != "" {
		return s
	}
	return defaultCardSubtitle
}

// RenderGrabFare renders the card without a focus band (see RenderGrabFareFocus).
func RenderGrabFare(o *grabfare.Output, maps []image.Image, sample bool) (*image.RGBA, error) {
	return RenderGrabFareFocus(o, maps, nil, sample)
}

// focusLabel is the caption under the wide focus band.
const focusLabel = "Colours · SM Southmall · Village Square"

// Focus band geometry: full content width, never taller than focusMaxH
// (a taller image is scaled down to fit and centred, never cropped).
const (
	focusMaxH   = 380
	focusLabelH = 34
	focusMinPad = 14
)

// RenderGrabFareFocus renders the card. With a focus image (the wide
// Colours/SM Southmall band, already cropped) it is drawn as a full-width
// strip under the fare/trend rows and the usual map row stays at its normal
// size below it. The card is 1080x1350 when it fits (spacing is reclaimed
// first); otherwise it grows taller by exactly what is needed.
func RenderGrabFareFocus(o *grabfare.Output, maps []image.Image, focus image.Image, sample bool) (*image.RGBA, error) {
	if o == nil || !o.Visible || o.Fare == nil {
		return nil, fmt.Errorf("fbchart: grab fare not visible")
	}
	dy := 0
	if sample {
		dy = 90
	}

	top := 256 + dy
	cardW := W - 2*marginL - 60

	hhmm := ""
	if o.AsOfHHMM != nil {
		hhmm = *o.AsOfHHMM
	}

	// panel height
	big := grabfare.FormatRange(o.Fare.Min, o.Fare.Max)
	bigSize := cfitSize(big, ExtraBold, 108, 60, cardW)
	capText := "Metered taxi · Grab app quote"
	capSize := cfitSize(capText, Medium, 30, 20, cardW)
	pillH := 0
	if o.Fare.Standard != nil {
		pillH = 52 + 12
		if focus != nil {
			pillH = 52 + 8
		}
	}
	asofText := "Ayon sa Grab app noong " + hhmm + " · oras sa Maynila"
	asofSize := cfitSize(asofText, Medium, 28, 20, cardW)
	// With a focus band the panel's inner padding is tightened (sizes of
	// the texts are unchanged) to reclaim whitespace.
	padIn, gapIn := 24, 12
	if focus != nil {
		padIn, gapIn = 16, 8
	}
	panelH := padIn + int(bigSize*1.35+0.5) + gapIn + int(capSize*1.35+0.5) + gapIn + pillH + int(asofSize*1.35+0.5) + padIn
	if panelH < 200 {
		panelH = 200
	}

	trendRows := 1
	if o.VsYesterday != nil {
		trendRows = 2
	}
	trendH := trendRows * 44

	trafficH := 0
	if o.Traffic != nil {
		trafficH = 64
	}

	nMaps := len(maps)
	if nMaps > 3 {
		nMaps = 3
	}
	mapH := 0
	var tileW, tileH int
	if nMaps > 0 {
		tileW = (W - 2*marginL - (nMaps-1)*16) / nMaps
		b0 := maps[0].Bounds()
		tileH = tileW * (b0.Dy() - mapTopCut(b0)) / b0.Dx()
		mapH = tileH + 16 + 30
	}

	// Fare-only card: a small honest note instead of an empty strip.
	noteH := 0
	if nMaps == 0 {
		noteH = 64
	}

	// focus band
	focusH, focusW := 0, 0
	focusBlockH := 0
	if focus != nil {
		fb := focus.Bounds()
		focusW = W - 2*marginL
		focusH = focusW * fb.Dy() / fb.Dx()
		if focusH > focusMaxH {
			focusW = focusW * focusMaxH / focusH
			focusH = focusMaxH
		}
		focusBlockH = focusH + 12 + focusLabelH
		// With a focus band and no thumbnails the focus caption also
		// carries the source label.
	}

	// Fare-only note is for cards without any picture.
	if focus != nil {
		noteH = 0
	}

	numBlocks := 1 + 1
	if trafficH > 0 {
		numBlocks++
	}
	if noteH > 0 {
		numBlocks++
	}
	if mapH > 0 {
		numBlocks++
	}
	if focusBlockH > 0 {
		numBlocks++
	}
	total := panelH + trendH + trafficH + noteH + focusBlockH + mapH
	h := H
	footerTop := h - 118
	minPad := 8
	if focus != nil {
		minPad = focusMinPad
	}
	pad := (footerTop - top - total) / (numBlocks + 1)
	if pad < minPad {
		// Not enough room even with the reclaimed spacing: grow the card
		// by exactly what is missing (nothing is made smaller).
		pad = minPad
		h = top + total + (numBlocks+1)*pad + 118
		footerTop = h - 118
	}

	dst := image.NewRGBA(image.Rect(0, 0, W, h))
	fillRect(dst, 0, 0, W, h, cream)
	if sample {
		fillRect(dst, 0, 0, W, 76, red)
		banner := "SAMPLE — trial data, not a live post"
		bw := measure(banner, Bold, 28)
		drawString(dst, banner, Bold, 28, white, (W-bw)/2, 50)
	}
	utilHeaderAt(dst, dy)
	utilTitleAt(dst, "Tantiya sa pamasahe ng Grab", grabCardSubtitle(o), dy)

	y := top + pad

	// panel
	billCard(dst, y, panelH)
	hasChart := grabChartAvailable(o)
	if !hasChart {
		cy := y + padIn
		// big
		tw := measure(big, ExtraBold, bigSize)
		bx := marginL + (W-marginL-marginR-tw)/2
		drawString(dst, big, ExtraBold, bigSize, green, bx, cy+int(bigSize*0.85+0.5))
		cy += int(bigSize*1.35+0.5) + gapIn
		// caption
		ctw := measure(capText, Medium, capSize)
		cx := marginL + (W-marginL-marginR-ctw)/2
		cdraw(dst, capText, Medium, capSize, mutCol, cx, cy+int(capSize*0.85+0.5))
		cy += int(capSize*1.35+0.5) + gapIn
		// pill
		if o.Fare.Standard != nil {
			pillText := "Grab Standard " + grabfare.FormatPeso(*o.Fare.Standard)
			pw := cmeasure(pillText, Bold, 32) + 52
			px := marginL + (W-marginL-marginR-pw)/2
			pill(dst, pillText, px, cy, green)
			cy += pillH
		}
		// asof
		atw := measure(asofText, Medium, asofSize)
		ax := marginL + (W-marginL-marginR-atw)/2
		cdraw(dst, asofText, Medium, asofSize, mutCol, ax, cy+int(asofSize*0.85+0.5))
		cy += int(asofSize*1.35+0.5) + 24
		_ = cy
	} else {
		drawGrabPanelHalf(dst, y, panelH, padIn, gapIn, big, bigSize, capText, capSize, asofText, hhmm, o)
	}
	y += panelH + pad

	// trend rows
	ty := y
	for r := 0; r < trendRows; r++ {
		var dir string
		var text string
		if r == 0 {
			dir = o.Trend.Direction
			text = o.Text.Tl.Trend
		} else {
			if o.VsYesterday != nil {
				dir = o.VsYesterday.Direction
				text = grabfare.YesterdayTextTl(*o.VsYesterday)
			}
		}
		cy2 := ty + 44/2 + 3
		triX := marginL + 30
		switch dir {
		case "up":
			fillTriangle(dst, triX+14, cy2, 28, 24, true, red)
		case "down":
			fillTriangle(dst, triX+14, cy2, 28, 24, false, green)
		case "flat":
			fillRect(dst, triX, cy2-3, triX+28, cy2+3, grey)
		default:
			fillRounded(dst, triX+8, cy2-6, triX+20, cy2+6, 6, grey)
		}
		cdraw(dst, stripTrendGlyph(text), Medium, 30, mutCol, triX+44, ty+int(30*0.85+0.5)+4)
		ty += 44
	}
	y += trendH + pad

	// traffic chip
	if trafficH > 0 {
		level := o.Traffic.Level
		tlWord := level
		switch strings.ToLower(level) {
		case "light":
			tlWord = "magaan"
		case "moderate":
			tlWord = "katamtaman"
		case "heavy":
			tlWord = "mabigat"
		case "severe":
			tlWord = "sobrang bigat"
		}
		chipText := "Trapiko: " + tlWord
		var fill color.RGBA
		switch strings.ToLower(level) {
		case "light":
			fill = green
		case "moderate":
			fill = amber
		case "heavy":
			fill = red
		case "severe":
			fill = redDk
		default:
			fill = grey
		}
		cw := cmeasure(chipText, Bold, 32) + 52
		px := marginL + 30
		fillRounded(dst, px, y, px+cw, y+52, 12, fill)
		cdraw(dst, chipText, Bold, 32, white, px+26, y+37)
		y += trafficH + pad
	}

	// fare-only note
	if noteH > 0 {
		noteText := "Fare lang ngayong oras · walang mapa"
		cw := cmeasure(noteText, Bold, 30) + 52
		px := marginL + 30
		fillRounded(dst, px, y, px+cw, y+52, 12, grey)
		cdraw(dst, noteText, Bold, 30, white, px+26, y+36)
		y += noteH + pad
	}

	// focus band (wide, already cropped: no top cut)
	if focus != nil {
		x0 := marginL + (W-2*marginL-focusW)/2
		fillRounded(dst, x0, y, x0+focusW, y+focusH, 14, sepCol)
		inner := image.Rect(x0+2, y+2, x0+focusW-2, y+focusH-2)
		draw.CatmullRom.Scale(dst, inner, focus, focus.Bounds(), draw.Over, nil)
		roundCorners(dst, x0, y, x0+focusW, y+focusH, 14)
		lbl := focusLabel
		if mapH == 0 {
			lbl += " · Map: Grab app screenshot"
		}
		ls := cfitSize(lbl, Medium, 24, 14, W-2*marginL)
		cdraw(dst, lbl, Medium, ls, mutCol, marginL, y+focusH+12+22)
		y += focusBlockH + pad
	}

	// map strip
	if mapH > 0 {
		stripY := y
		for i := 0; i < nMaps; i++ {
			x0 := marginL + i*(tileW+16)
			y0 := stripY
			x1 := x0 + tileW
			y1 := y0 + tileH
			fillRounded(dst, x0, y0, x1, y1, 14, sepCol)
			inner := image.Rect(x0+2, y0+2, x1-2, y1-2)
			// The top strip (Grab group-ride bar + back arrow) is left out by
			// scaling only the sub-rectangle below it; the source is not modified.
			sb := maps[i].Bounds()
			sb.Min.Y += mapTopCut(sb)
			draw.CatmullRom.Scale(dst, inner, maps[i], sb, draw.Over, nil)
			roundCorners(dst, x0, y0, x1, y1, 14)
		}
		label := "Map: Grab app screenshot"
		ls := cfitSize(label, Medium, 22, 14, W-2*marginL)
		cdraw(dst, label, Medium, ls, mutCol, marginL, stripY+tileH+16+19)
		y += mapH + pad
	}

	grabFooter(dst, h, "Grab app quote, not an official fare", "Hindi opisyal na pamasahe")
	_ = draw.Draw
	return dst, nil
}

// stripTrendGlyph removes the leading arrow glyph (the card draws its own
// triangle) from a trend text.
// drawGrabPanelHalf draws the fare texts in the LEFT half of the panel and the
// hour-of-day chart in the RIGHT half. Text sizes shrink to fit the half
// width; the as-of line wraps onto two lines.
func drawGrabPanelHalf(dst *image.RGBA, y, panelH, padIn, gapIn int, big string, bigSize float64, capText string, capSize float64, asofText, hhmm string, o *grabfare.Output) {
	px0, px1 := marginL, W-marginR
	midX := (px0 + px1) / 2
	leftMaxW := midX - px0 - 2*padIn - 8
	centreX := (px0 + midX) / 2

	capS := cfitSize(capText, Medium, 26, 18, leftMaxW)
	asof1 := "Ayon sa Grab app noong " + hhmm
	asof2 := "oras sa Maynila"
	asofS := cfitSize(asof1, Medium, 26, 18, leftMaxW)
	pillText := ""
	pillS := 0.0
	pillBlock := 0
	if o.Fare.Standard != nil {
		pillText = "Grab Standard " + grabfare.FormatPeso(*o.Fare.Standard)
		pillS = cfitSize(pillText, Bold, 30, 18, leftMaxW-52)
		pillBlock = 52 + gapIn
	}
	rest := int(capS*1.3+0.5) + gapIn + pillBlock + 2*int(asofS*1.3+0.5)
	avail := panelH - 2*padIn
	bs := cfitSize(big, ExtraBold, bigSize, 40, leftMaxW)
	for bs > 40 && int(bs*1.3+0.5)+gapIn+rest > avail {
		bs -= 2
	}
	contentH := int(bs*1.3+0.5) + gapIn + rest
	cy := y + (panelH-contentH)/2
	if cy < y+padIn {
		cy = y + padIn
	}
	tw := cmeasure(big, ExtraBold, bs)
	cdraw(dst, big, ExtraBold, bs, green, centreX-tw/2, cy+int(bs*0.85+0.5))
	cy += int(bs*1.3+0.5) + gapIn
	cw := cmeasure(capText, Medium, capS)
	cdraw(dst, capText, Medium, capS, mutCol, centreX-cw/2, cy+int(capS*0.85+0.5))
	cy += int(capS*1.3+0.5) + gapIn
	if pillText != "" {
		pw := cmeasure(pillText, Bold, pillS) + 52
		fillRounded(dst, centreX-pw/2, cy, centreX-pw/2+pw, cy+52, 12, green)
		cdraw(dst, pillText, Bold, pillS, white, centreX-pw/2+26, cy+26+int(pillS*0.35+0.5))
		cy += pillBlock
	}
	for _, ln := range []string{asof1, asof2} {
		lw := cmeasure(ln, Medium, asofS)
		cdraw(dst, ln, Medium, asofS, mutCol, centreX-lw/2, cy+int(asofS*0.85+0.5))
		cy += int(asofS*1.3 + 0.5)
	}
	// divider + chart zone
	fillRect(dst, midX, y+padIn, midX+1, y+panelH-padIn, sepCol)
	drawGrabChart(dst, image.Rect(midX+padIn, y+padIn, px1-padIn, y+panelH-padIn), o)
}

func stripTrendGlyph(s string) string {
	s = strings.TrimSpace(s)
	for _, g := range []string{"\u25b2", "\u25bc", "\uff1d"} {
		if strings.HasPrefix(s, g) {
			return strings.TrimSpace(strings.TrimPrefix(s, g))
		}
	}
	return s
}

// roundCorners paints the outer corners of the rect back to the cream
// background so a pasted image gets rounded corners.
func roundCorners(dst *image.RGBA, x0, y0, x1, y1, r int) {
	for yy := y0; yy < y1; yy++ {
		for xx := x0; xx < x1; xx++ {
			dx := 0
			if xx < x0+r {
				dx = x0 + r - xx
			} else if xx >= x1-r {
				dx = xx - (x1 - r - 1)
			}
			dy2 := 0
			if yy < y0+r {
				dy2 = y0 + r - yy
			} else if yy >= y1-r {
				dy2 = yy - (y1 - r - 1)
			}
			if dx > 0 && dy2 > 0 && dx*dx+dy2*dy2 > r*r {
				dst.Set(xx, yy, cream)
			}
		}
	}
}

// grabFooter is utilFooter for a card of height h.
func grabFooter(dst *image.RGBA, h int, left1, left2 string) {
	footerTop := h - 118
	fillRect(dst, 0, footerTop, W, h, greenDk)
	size, divX, footX, t1, t2 := footerLayout(left1, left2)
	midY := footerTop + (h-footerTop)/2
	drawString(dst, t1, SemiBold, size, white, marginL, midY-4)
	drawString(dst, t2, SemiBold, size, white, marginL, midY+28)
	drawString(dst, "abalol.xyz/presyo-bantay", Bold, 26, white, footX, midY+9)
	vline(dst, divX, footerTop+20, h-20, 2, footDiv)
}
