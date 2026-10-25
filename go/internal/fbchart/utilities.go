package fbchart

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"golang.org/x/image/font/sfnt"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// Household-utilities charts (UTILITIES_SPEC.md section 2): 1080x1350
// PNGs in the same visual style as the weekly chart (cream background,
// icon + "Presyo Bantay" header, pills, rounded bars, embedded fonts,
// footer bar). Only new functions are added; the daily/weekly render
// paths are untouched.

// drawable reports whether r draws with weight w (Montserrat or the
// Noto Sans fallback). Emoji and Japanese fall outside both fonts.
func drawable(w Weight, r rune) bool {
	if montHas(w, r) {
		return true
	}
	var buf sfnt.Buffer
	idx, err := weights[w].fbs.GlyphIndex(&buf, r)
	return err == nil && idx != 0
}

// dropMissing removes runes that would render as tofu (emoji when the
// embedded fonts lack them) and collapses whitespace.
func dropMissing(s string, w Weight) string {
	var b strings.Builder
	for _, r := range s {
		if drawable(w, r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// cdraw draws s (emoji dropped) with baseline at (x, y).
func cdraw(dst *image.RGBA, s string, w Weight, size float64, col color.Color, x, y int) int {
	return drawString(dst, dropMissing(s, w), w, size, col, x, y)
}

// cmeasure measures s after the emoji drop.
func cmeasure(s string, w Weight, size float64) int {
	return measure(dropMissing(s, w), w, size)
}

// cfitSize shrinks size until the cleaned string fits maxW.
func cfitSize(s string, w Weight, size, minSize float64, maxW int) float64 {
	return fitSize(dropMissing(s, w), w, size, minSize, maxW)
}

// cellipsize truncates the cleaned string with "…" to maxW.
func cellipsize(s string, w Weight, size float64, maxW int) string {
	return ellipsize(dropMissing(s, w), w, size, maxW)
}

// header draws the centered icon + "Presyo Bantay" row, returning
// nothing (same geometry as the daily chart).
func utilHeader(dst *image.RGBA) {
	icon := Icon()
	headText := "Presyo Bantay"
	headSize := 44.0
	tw := measure(headText, SemiBold, headSize)
	rowW := tw
	if icon != nil {
		rowW = icon.Bounds().Dx() + 18 + tw
	}
	hx := (W - rowW) / 2
	hy := 28
	if icon != nil {
		draw.Draw(dst, image.Rect(hx, hy, hx+icon.Bounds().Dx(), hy+icon.Bounds().Dy()),
			icon, image.Point{}, draw.Over)
		hx += icon.Bounds().Dx() + 18
	}
	drawString(dst, headText, SemiBold, headSize, green, hx, hy+34+18)
}

// utilTitle draws the centered ExtraBold title + Medium subtitle.
func utilTitle(dst *image.RGBA, title, sub string) {
	ts := fitSize(title, ExtraBold, 64, 28, W-2*marginL-60)
	tx := (W - measure(title, ExtraBold, ts)) / 2
	drawString(dst, title, ExtraBold, ts, green, tx, 184)
	ss := fitSize(sub, Medium, 30, 20, W-marginL-marginR)
	sx := (W - measure(sub, Medium, ss)) / 2
	drawString(dst, sub, Medium, ss, green, sx, 226)
}

// pill draws a section pill at (x, y), returning its width.
func pill(dst *image.RGBA, text string, x, y int, col color.RGBA) int {
	pw := cmeasure(text, Bold, 32) + 52
	fillRounded(dst, x, y, x+pw, y+52, 12, col)
	cdraw(dst, text, Bold, 32, white, x+26, y+37)
	return pw
}

// footerLayout fits the footer band: the URL stays right-aligned, the
// divider sits 20 px left of it, and the left texts shrink (min 14 px,
// then ellipsize) so they never cross the divider.
func footerLayout(left1, left2 string) (size float64, divX, footX int, t1, t2 string) {
	foot := "abalol.xyz/presyo-bantay"
	fw := measure(foot, Bold, 26)
	footX = W - marginR - fw
	divX = footX - 20
	maxW := divX - 20 - marginL
	size = 24.0
	for size > 14 && (measure(left1, SemiBold, size) > maxW || measure(left2, SemiBold, size) > maxW) {
		size -= 2
	}
	t1, t2 = left1, left2
	if measure(t1, SemiBold, size) > maxW {
		t1 = ellipsize(t1, SemiBold, size, maxW)
	}
	if measure(t2, SemiBold, size) > maxW {
		t2 = ellipsize(t2, SemiBold, size, maxW)
	}
	return size, divX, footX, t1, t2
}

// utilFooter draws the footer band with custom left texts.
func utilFooter(dst *image.RGBA, left1, left2 string) {
	footerTop := H - 118
	fillRect(dst, 0, footerTop, W, H, greenDk)
	size, divX, footX, t1, t2 := footerLayout(left1, left2)
	midY := footerTop + (H-footerTop)/2
	drawString(dst, t1, SemiBold, size, white, marginL, midY-4)
	drawString(dst, t2, SemiBold, size, white, marginL, midY+28)
	drawString(dst, "abalol.xyz/presyo-bantay", Bold, 26, white, footX, midY+9)
	vline(dst, divX, footerTop+20, H-20, 2, footDiv)
}

// wrapLines splits s into at most maxLines lines fitting maxW,
// shrinking from size to minSize and ellipsizing the last line.
func wrapLines(s string, w Weight, size, minSize float64, maxW, maxLines int) ([]string, float64) {
	s = dropMissing(s, w)
	if maxLines < 1 {
		maxLines = 1
	}
	for sz := size; ; sz -= 2 {
		if sz < minSize {
			sz = minSize
		}
		var lines []string
		cur := ""
		for _, wd := range strings.Fields(s) {
			t := wd
			if cur != "" {
				t = cur + " " + wd
			}
			if cur == "" || measure(t, w, sz) <= maxW {
				cur = t
			} else {
				lines = append(lines, cur)
				cur = wd
			}
		}
		lines = append(lines, cur)
		if len(lines) <= maxLines || sz <= minSize {
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				lines[maxLines-1] = ellipsize(lines[maxLines-1], w, sz, maxW)
			}
			for i, ln := range lines {
				if measure(ln, w, sz) > maxW {
					lines[i] = ellipsize(ln, w, sz, maxW)
				}
			}
			return lines, sz
		}
	}
}

// Fuel chart helpers.

// fmag returns the Hi magnitude of a fuel range (0 when nil).
func fmag(r *utilities.Range) float64 {
	if r == nil {
		return 0
	}
	if r.Hi < 0 {
		return -r.Hi
	}
	return r.Hi
}

// fuelSign reports whether the range is a rollback (negative).
func fuelNeg(r *utilities.Range) bool {
	return r != nil && r.Hi < 0
}

// f2 formats a signed 2-decimal peso value with U+2212 minus.
func f2(v float64) string {
	if v < 0 {
		return fmt.Sprintf("−₱%.2f", -v)
	}
	return fmt.Sprintf("+₱%.2f", v)
}

// stripPeso removes a leading "+₱"/"−₱" (compact range second half).
func stripPeso(s string) string {
	s = strings.TrimPrefix(s, "+₱")
	s = strings.TrimPrefix(s, "−₱")
	return strings.TrimPrefix(s, "₱")
}

// fuelBarLabel is the big-bar label ("−₱4.30/L", ranges "−₱3.00–3.02/L").
func fuelBarLabel(r *utilities.Range) string {
	if r.Lo == r.Hi {
		return f2(r.Lo) + "/L"
	}
	return f2(r.Lo) + "–" + stripPeso(f2(r.Hi)) + "/L"
}

// fuelValLabel is the history-bar label ("−₱4.30", "−₱3.00–3.02").
func fuelValLabel(r *utilities.Range) string {
	if r.Lo == r.Hi {
		return f2(r.Lo)
	}
	return f2(r.Lo) + "–" + stripPeso(f2(r.Hi))
}

// fscaleUnit mirrors scaleUnit for float magnitudes.
func fscaleUnit(maxNeg, maxPos float64) float64 {
	barW := barX1 - barX0
	span := barW - 2*pad
	if span < 60 {
		span = 60
	}
	switch {
	case maxNeg > 0 && maxPos > 0:
		shareN := maxNeg / (maxNeg + maxPos)
		if shareN < 0.2 {
			shareN = 0.2
		}
		if shareN > 0.8 {
			shareN = 0.8
		}
		negW := float64(span) * shareN
		posW := float64(span) - negW
		unit := negW / maxNeg
		if u := posW / maxPos; u < unit {
			unit = u
		}
		return unit
	case maxNeg > 0:
		return float64(span) / maxNeg
	default:
		if maxPos == 0 {
			maxPos = 1
		}
		return float64(span) / maxPos
	}
}

// fzeroX mirrors zeroX for float magnitudes.
func fzeroX(maxNeg, maxPos float64, unit float64) int {
	switch {
	case maxNeg > 0 && maxPos > 0:
		return barX0 + pad + int(unit*maxNeg)
	case maxNeg > 0:
		return barX1 - pad
	default:
		return barX0 + pad
	}
}

// Fuel bar geometry: bars are centered on their row, names share the
// row middle, and value labels sit fully inside the bar when they fit
// (10 px padding) or outside on the far side from zero.
const (
	fuelBarH    = 46
	fuelInSize  = 26.0
	fuelOutSize = 30.0
	fuelGap     = 6
)

// fuelRowPlace is one main-section row: bar, value label, and the
// vertically centered product name.
type fuelRowPlace struct {
	bx0, bx1, by0, by1 int
	lx, ly, lw         int
	size               float64
	inside             bool
	nx, ny, nw         int // name baseline origin + width
	nsize              float64
}

// placeFuelRow computes one main-section row at row middle cy.
func placeFuelRow(y, rowH, zx int, unit float64, name string, r *utilities.Range) (fuelRowPlace, string, bool) {
	var g fuelRowPlace
	if r == nil {
		return g, "", false
	}
	cy := y + rowH/2
	barH := fuelBarH
	if barH > rowH-20 {
		barH = rowH - 20
	}
	length := int(fmag(r) * unit)
	if length < 6 {
		length = 6
	}
	neg := fuelNeg(r)
	var bx0, bx1 int
	if neg {
		bx1, bx0 = zx-fuelGap, zx-fuelGap-length
	} else {
		bx0, bx1 = zx+fuelGap, zx+fuelGap+length
	}
	lbl := fuelBarLabel(r)
	lwIn := cmeasure(lbl, Bold, fuelInSize)
	lwOut := cmeasure(lbl, Bold, fuelOutSize)
	g = fuelRowPlace{bx0: bx0, bx1: bx1, by0: cy - barH/2, by1: cy + barH/2}
	if lwIn+20 <= length && barH >= 34 {
		g.inside = true
		g.size, g.lw, g.ly = fuelInSize, lwIn, cy+9
		if neg {
			g.lx = bx0 + 10
		} else {
			g.lx = bx1 - lwIn - 10
		}
	} else {
		g.size, g.lw, g.ly = fuelOutSize, lwOut, cy+10
		if neg {
			g.lx = bx0 - lwOut - 10
		} else {
			g.lx = bx1 + 10
		}
	}
	// Product name, vertically centered on the same row.
	nsize := 32.0
	maxW := barX0 - marginL - 12
	for nsize > 24 && cmeasure(name, Bold, nsize) > maxW {
		nsize -= 2
	}
	g.nx, g.ny, g.nw, g.nsize = marginL, cy+11, cmeasure(name, Bold, nsize), nsize
	return g, lbl, true
}

// fuelItem feeds the scale fitter.
type fuelItem struct {
	neg   bool
	mag   float64
	lbl   string
	nameW int
}

// fitFuelUnit shrinks the common scale so every outside label clears
// the name column (rollback side) or the canvas edge (increase side)
// by >= 8 px, never touching a bar edge or the zero line.
func fitFuelUnit(maxNeg, maxPos float64, items []fuelItem, unit float64, zx int) (float64, int) {
	for round := 0; round < 10; round++ {
		worst := -1.0
		for _, it := range items {
			if it.mag <= 0 {
				continue
			}
			length := int(it.mag * unit)
			if length < 6 {
				length = 6
			}
			lwOut := cmeasure(it.lbl, Bold, fuelOutSize)
			lwIn := cmeasure(it.lbl, Bold, fuelInSize)
			if lwIn+20 <= length {
				continue // inside: always clear
			}
			lengthF := float64(length)
			var cap float64
			if it.neg {
				nameEnd := float64(marginL + it.nameW)
				cap = (float64(zx-fuelGap-10) - float64(lwOut) - 8 - nameEnd) / it.mag
				_ = lengthF
			} else {
				cap = (float64(W-16-10) - float64(lwOut) - float64(zx+fuelGap)) / it.mag
			}
			if cap < unit && (worst < 0 || cap < worst) {
				worst = cap
			}
		}
		if worst < 0 {
			return unit, zx
		}
		if worst < 0.1 {
			worst = 0.1
		}
		unit = worst
		zx = fzeroX(maxNeg, maxPos, unit)
	}
	return unit, zx
}

// RenderFuelWeekly builds the 1080x1350 fuel chart: this week's three
// fuel bars, up to 6 weeks of gasoline/diesel history (newest last),
// and the NCR common-price chips. Row heights grow to fill the space
// above the footer instead of leaving a blank gap.
func RenderFuelWeekly(s *utilities.Snapshot) (*image.RGBA, error) {
	if len(s.Fuel.Adjustments) == 0 {
		return nil, fmt.Errorf("fbchart: no oil adjustments")
	}
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)
	utilHeader(dst)

	adj := s.Fuel.Adjustments[0]
	utilTitle(dst, "Presyo ng langis",
		fmt.Sprintf("Epektibo %s · per litro", utilities.DayRangeTL(adj.EffectiveFrom, adj.EffectiveTo)))

	type fuel struct {
		name string
		r    *utilities.Range
	}
	fuels := []fuel{
		{"Gasolina", adj.Gasoline}, {"Diesel", adj.Diesel}, {"Kerosene", adj.Kerosene},
	}
	maxNeg, maxPos := 0.0, 0.0
	var items []fuelItem
	for _, f := range fuels {
		if f.r == nil {
			continue
		}
		if fuelNeg(f.r) {
			if m := fmag(f.r); m > maxNeg {
				maxNeg = m
			}
		} else if m := fmag(f.r); m > maxPos {
			maxPos = m
		}
		items = append(items, fuelItem{
			neg: fuelNeg(f.r), mag: fmag(f.r), lbl: fuelBarLabel(f.r),
			nameW: cmeasure(f.name, Bold, 32),
		})
	}
	unit := fscaleUnit(maxNeg, maxPos)
	zx := fzeroX(maxNeg, maxPos, unit)
	unit, zx = fitFuelUnit(maxNeg, maxPos, items, unit, zx)

	// History (up to 6, newest last).
	hist := s.Fuel.Adjustments
	if len(hist) > 6 {
		hist = hist[:6]
	}
	weeks := append([]utilities.OilAdjustment(nil), hist...)
	for i, j := 0, len(weeks)-1; i < j; i, j = i+1, j-1 {
		weeks[i], weeks[j] = weeks[j], weeks[i]
	}
	pump := basePumpWeek(s)

	y := 256
	footerTop := H - 118

	// Vertical budget: pills, gaps, legend, and chips are fixed; row
	// heights grow (round-robin) to fill the leftover space.
	const pillBlock = 52 + 14
	const gapBlock = 8
	const legendH = 34
	const chipsH = 8 + 52 + 14 + 64
	const bigNat, histNat = 110, 70
	fixed := pillBlock + gapBlock + pillBlock + legendH
	if pump != nil {
		fixed += chipsH // gap + pill + chips row
	}
	avail := footerTop - y - fixed
	need := 3*bigNat + len(weeks)*histNat
	bigH, histH := bigNat, histNat
	if need > 0 {
		f := float64(avail) / float64(need)
		bigH = int(float64(bigNat) * f)
		histH = int(float64(histNat) * f)
		if bigH > 170 {
			bigH = 170
		}
		if histH > 120 {
			histH = 120
		}
	}
	for bigH > 84 && 3*bigH+len(weeks)*histH > avail {
		bigH--
	}
	for histH > 56 && 3*bigH+len(weeks)*histH > avail {
		histH--
	}
	rows := []int{bigH, bigH, bigH}
	for range weeks {
		rows = append(rows, histH)
	}
	for leftover := avail - (3*bigH + len(weeks)*histH); leftover > 0; leftover-- {
		rows[leftover%len(rows)]++
	}
	bigRows, histRows := rows[:3], rows[3:]

	// Section 1: this week's movement.
	pill(dst, "Galaw ngayong linggo", marginL, y, green)
	y += pillBlock
	rowsTop := y
	for i, f := range fuels {
		rowH := bigRows[i]
		g, lbl, ok := placeFuelRow(y, rowH, zx, unit, f.name, f.r)
		col := green
		barCol := green
		if f.r != nil && !fuelNeg(f.r) {
			col, barCol = redName, red
		}
		if !ok {
			cdraw(dst, "—", SemiBold, 27, mutCol, marginL, y+rowH/2+9)
		} else {
			cdraw(dst, cellipsize(f.name, Bold, g.nsize, barX0-marginL-12), Bold, g.nsize, col, g.nx, g.ny)
			fillRounded(dst, g.bx0, g.by0, g.bx1, g.by1, 8, barCol)
			if g.inside {
				drawString(dst, lbl, Bold, g.size, white, g.lx, g.ly)
			} else if fuelNeg(f.r) {
				drawString(dst, lbl, Bold, g.size, green, g.lx, g.ly)
			} else {
				drawString(dst, lbl, Bold, g.size, red, g.lx, g.ly)
			}
		}
		y += rowH
	}
	rowsBottom := y
	vline(dst, zx, rowsTop, rowsBottom, 3, zeroCol)

	// Section 2: history (gasoline solid above, diesel light below).
	y += gapBlock
	pill(dst, "Nakaraang mga linggo", marginL, y, greenDk)
	y += pillBlock
	cdraw(dst, historyLegend(), Medium, 22, mutCol, marginL, y+24)
	y += legendH
	histTop := y
	// Common mini scale over the shown weeks, fitted so outside
	// labels clear the row margins.
	hMax := 0.0
	for _, w := range weeks {
		for _, r := range []*utilities.Range{w.Gasoline, w.Diesel} {
			if m := fmag(r); m > hMax {
				hMax = m
			}
		}
	}
	if hMax == 0 {
		hMax = 1
	}
	miniX0, miniX1 := 250, 990
	miniMid := (miniX0 + miniX1) / 2
	miniUnit := fitMiniUnit(miniX0, miniX1, miniMid, weeks, float64((miniX1-miniX0)/2-10)/hMax)
	for i, w := range weeks {
		rowH := histRows[i]
		label := utilities.DayLabelTL(w.EffectiveFrom)
		cdraw(dst, label, Medium, 24, green, marginL, y+rowH/2+8)
		cy := y + rowH/2
		mh := miniBarH(rowH)
		off := mh/2 + 2
		drawMiniBar(dst, w.Gasoline, false, cy-off, mh, miniX0, miniX1, miniMid, miniUnit)
		drawMiniBar(dst, w.Diesel, true, cy+off, mh, miniX0, miniX1, miniMid, miniUnit)
		y += rowH
	}
	vline(dst, miniMid, histTop, y, 2, zeroCol)

	// Section 3: NCR common-price chips.
	if pump != nil {
		y += gapBlock
		pill(dst, fmt.Sprintf("Karaniwang presyo sa NCR (DOE, %s)",
			utilities.ShortRangeTL(pump.WeekFrom, pump.WeekTo)), marginL, y, green)
		y += pillBlock
		chips := pumpChips(*pump)
		drawChips(dst, chips, y)
	}

	utilFooter(dst, "Source: DOE (doe.gov.ph)", "Hindi opisyal na DOE page")
	return dst, nil
}

// historyLegend is the history key: position tells the fuel.
func historyLegend() string {
	return "Itaas: Gasolina · Ibaba: Diesel"
}

// miniBarH sizes history bars to their row.
func miniBarH(rowH int) int {
	h := rowH - 30
	if h < 22 {
		h = 22
	}
	if h > 40 {
		h = 40
	}
	return h
}

// miniPlace is one history mini bar plus its value label origin.
type miniPlace struct {
	bx0, bx1, by0, by1 int
	lx, ly             int // label baseline origin
	lw                 int // label width at the drawn size
	size               float64
	inside             bool
}

// History value labels are 20 px (readable, min 18 px): inside only
// when the label plus 10 px padding on each side fits the bar,
// otherwise outside the bar end on the far side from zero.
const miniLabelSize = 20.0

// placeMiniBar computes one history mini bar (h thick) centered on cy
// with its value label fully inside when it fits, else outside.
func placeMiniBar(r *utilities.Range, cy, h, x0, x1, mid int, unit float64) (miniPlace, string, bool) {
	if r == nil {
		return miniPlace{}, "", false
	}
	length := int(fmag(r) * unit)
	if length < 4 && fmag(r) > 0 {
		length = 4
	}
	lbl := fuelValLabel(r)
	lw := cmeasure(lbl, Bold, miniLabelSize)
	neg := fuelNeg(r)
	var bx0, bx1 int
	if neg {
		bx1, bx0 = mid-4, mid-4-length
	} else {
		bx0, bx1 = mid+4, mid+4+length
	}
	g := miniPlace{bx0: bx0, bx1: bx1, by0: cy - h/2, by1: cy + h/2, lw: lw, size: miniLabelSize}
	if lw+20 <= length {
		g.inside = true
		g.ly = cy + 7
		if neg {
			g.lx = bx0 + 10
		} else {
			g.lx = bx1 - lw - 10
		}
	} else if neg {
		g.lx, g.ly = bx0-8-lw, cy+7
	} else {
		g.lx, g.ly = bx1+8, cy+7
	}
	return g, lbl, true
}

// miniItem feeds the history scale fitter.
type miniItem struct {
	neg bool
	mag float64
	lbl string
}

// fitMiniUnit shrinks the history scale so every outside label clears
// the row margins (x0/x1) with room to spare.
func fitMiniUnit(x0, x1, mid int, weeks []utilities.OilAdjustment, unit float64) float64 {
	for round := 0; round < 10; round++ {
		worst := -1.0
		for _, w := range weeks {
			for _, r := range []*utilities.Range{w.Gasoline, w.Diesel} {
				if r == nil || fmag(r) <= 0 {
					continue
				}
				length := int(fmag(r) * unit)
				if length < 4 {
					length = 4
				}
				lw := cmeasure(fuelValLabel(r), Bold, miniLabelSize)
				if lw+20 <= length {
					continue // inside: always clear
				}
				var cap float64
				if fuelNeg(r) {
					cap = float64(mid-4-8-lw-x0) / fmag(r)
				} else {
					cap = float64(x1-8-lw-(mid+4)) / fmag(r)
				}
				if cap < unit && (worst < 0 || cap < worst) {
					worst = cap
				}
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

// histBarFill picks the history fill: gasoline solid by sign, diesel
// the same hue but lighter so the pair stays distinguishable.
func histBarFill(neg, diesel bool) color.RGBA {
	if diesel {
		if neg {
			return greenLt
		}
		return redLt
	}
	if neg {
		return green
	}
	return red
}

// drawMiniBar draws one history mini bar with its value label. Inside
// labels on light (diesel) bars use the dark hue for contrast.
func drawMiniBar(dst *image.RGBA, r *utilities.Range, diesel bool, cy, h, x0, x1, mid int, unit float64) {
	g, lbl, ok := placeMiniBar(r, cy, h, x0, x1, mid, unit)
	if !ok {
		return
	}
	neg := fuelNeg(r)
	fillRounded(dst, g.bx0, cy-h/2, g.bx1, cy+h/2, 6, histBarFill(neg, diesel))
	if g.inside {
		if diesel {
			if neg {
				cdraw(dst, lbl, Bold, g.size, green, g.lx, g.ly)
			} else {
				cdraw(dst, lbl, Bold, g.size, redName, g.lx, g.ly)
			}
		} else {
			cdraw(dst, lbl, Bold, g.size, white, g.lx, g.ly)
		}
	} else if neg {
		cdraw(dst, lbl, Bold, g.size, green, g.lx, g.ly)
	} else {
		cdraw(dst, lbl, Bold, g.size, red, g.lx, g.ly)
	}
}

// basePumpWeek mirrors utilities.basePump for charts (newest pump week
// before the current adjustment).
func basePumpWeek(s *utilities.Snapshot) *utilities.PumpSummary {
	if len(s.Fuel.Adjustments) == 0 {
		return nil
	}
	eff := s.Fuel.Adjustments[0].EffectiveFrom
	var best *utilities.PumpSummary
	for i := range s.Fuel.NCRPump {
		p := &s.Fuel.NCRPump[i]
		if p.WeekTo < eff && (best == nil || p.WeekFrom > best.WeekFrom) {
			best = p
		}
	}
	return best
}

// pumpChips builds the RON 91 / RON 95 / Diesel value chips.
func pumpChips(sum utilities.PumpSummary) []string {
	got := map[string]float64{}
	for _, p := range sum.Prices {
		got[p.Product] = p.Common
	}
	out := []string{}
	for _, key := range []string{"RON91", "RON95", "Diesel"} {
		v, ok := got[key]
		if !ok {
			continue
		}
		// Price levels print unsigned.
		out = append(out, fmt.Sprintf("%s ₱%s", chipName(key), stripPeso(f2(v))))
	}
	return out
}

// chipName renders product keys for chips.
func chipName(key string) string {
	switch key {
	case "RON91":
		return "RON 91"
	case "RON95":
		return "RON 95"
	}
	return key
}

// drawChips draws value chips in one row (green fill, white text).
func drawChips(dst *image.RGBA, chips []string, y int) {
	if len(chips) == 0 {
		return
	}
	const gap = 16
	size := 30.0
	for size > 20 {
		total := 0
		for _, c := range chips {
			total += cmeasure(c, Bold, size) + 44
		}
		total += gap * (len(chips) - 1)
		if total <= W-marginL-marginR {
			break
		}
		size -= 2
	}
	x := marginL
	for _, c := range chips {
		cw := cmeasure(c, Bold, size) + 44
		fillRounded(dst, x, y, x+cw, y+64, 12, green)
		cdraw(dst, c, Bold, size, white, x+22, y+41)
		x += cw + gap
	}
}

// Bills chart.

// f4 formats a signed 4-decimal peso value.
func f4(v float64) string {
	if v < 0 {
		return fmt.Sprintf("−₱%.4f", -v)
	}
	return fmt.Sprintf("+₱%.4f", v)
}

// wholePeso formats whole pesos with thousands separators (signed).
func wholePeso(v float64) string {
	n := int64(v + 0.5)
	if v < 0 {
		n = int64(v - 0.5)
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "−₱" + s
	}
	return "₱" + s
}

// billChip draws a small signed-amount chip, red for increases and
// green for decreases, returning its width.
func billChip(dst *image.RGBA, text string, negative bool, x, y int) int {
	col := red
	if negative {
		col = green
	}
	cw := cmeasure(text, Bold, 28) + 36
	fillRounded(dst, x, y, x+cw, y+52, 12, col)
	cdraw(dst, text, Bold, 28, white, x+18, y+36)
	return cw
}

// billCard frames a card body (white rounded rect).
func billCard(dst *image.RGBA, y, h int) {
	fillRounded(dst, marginL, y, W-marginR, y+h, 16, white)
}

// signedWholePeso formats whole pesos with an explicit sign (+₱64,
// −₱8 with U+2212), matching the caption text.
func signedWholePeso(v float64) string {
	if v < 0 {
		return wholePeso(v)
	}
	return "+" + wholePeso(v)
}

// meralcoImpactLine is the Meralco card's 200 kWh line, always signed.
func meralcoImpactLine(impact int) string {
	return fmt.Sprintf("200 kWh: mga %s", signedWholePeso(float64(impact)))
}

// meralcoPrevLine is the Meralco card's second line: prev rate plus
// the announced date ("mula ₱14.7424 · Okt 13").
func meralcoPrevLine(prevRate float64, announced string) string {
	prev := "₱" + stripPeso(f4(prevRate))
	if day := utilities.DayLabelTL(announced); day != "" {
		return fmt.Sprintf("mula %s · %s", prev, day)
	}
	return fmt.Sprintf("mula %s", prev)
}

// billLine is one card content line.
type billLine struct {
	text string
	w    Weight
	size float64
	col  color.Color
}

// billCardGeom lays out a card: header plus content lines with
// 26 px top/bottom padding. It returns the header baseline, the
// content baselines, and the content height (without extras).
func billCardGeom(title string, lines []billLine) (headerBase int, bases []int, contentH int) {
	const padTB = 26
	_ = title
	sizes := []float64{32}
	for _, ln := range lines {
		sizes = append(sizes, ln.size)
	}
	y := padTB
	bases = make([]int, 0, len(sizes))
	for i, sz := range sizes {
		y += int(sz*0.85 + 0.5)
		bases = append(bases, y)
		if i+1 < len(sizes) {
			y += int(sizes[i]*0.5 + 0.5)
		}
	}
	headerBase = bases[0]
	bases = bases[1:]
	contentH = y + int(sizes[len(sizes)-1]*0.5+0.5) + padTB
	return headerBase, bases, contentH
}

// drawBillCard frames one card and draws its header, optional chip,
// and content lines. extra is the even share of leftover space added
// at the bottom. It returns the next card top (gap excluded).
func drawBillCard(dst *image.RGBA, y int, title, chip string, chipNeg, hasChip bool, lines []billLine, extra int) int {
	const padX = 30
	cardW := W - marginL - marginR - 2*padX
	headerBase, bases, contentH := billCardGeom(title, lines)
	cardH := contentH + extra
	billCard(dst, y, cardH)
	hx := marginL + padX
	maxTitle := cardW
	chipW := 0
	if hasChip {
		chipW = cmeasure(chip, Bold, 28) + 36
		maxTitle = cardW - chipW - 16
		billChip(dst, chip, chipNeg, W-marginR-padX-chipW, y+20)
	}
	cdraw(dst, cellipsize(title, Bold, 32, maxTitle), Bold, 32, green, hx, y+headerBase)
	for i, ln := range lines {
		cdraw(dst, cellipsize(ln.text, ln.w, ln.size, cardW), ln.w, ln.size, ln.col, hx, y+bases[i])
	}
	return y + cardH
}

// RenderBills builds the 1080x1350 household-bills chart: Meralco,
// LPG and Maynilad cards sized to their content with leftover space
// distributed evenly.
func RenderBills(s *utilities.Snapshot) (*image.RGBA, error) {
	if len(s.Meralco) == 0 {
		return nil, fmt.Errorf("fbchart: no meralco rates")
	}
	if len(s.LPG) == 0 {
		return nil, fmt.Errorf("fbchart: no LPG months")
	}
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)
	utilHeader(dst)

	mer := s.Meralco[0]
	lpg := s.LPG[0]
	month, ok := utilities.MonthTitleTL(mer.Month)
	if !ok {
		return nil, fmt.Errorf("fbchart: bad meralco month %q", mer.Month)
	}
	utilTitle(dst, "Bayarin sa bahay", month)

	const padX = 30
	cardW := W - marginL - marginR - 2*padX

	// Card 1: Meralco.
	big := wholeRate(mer.Rate) + "/kWh"
	bs := cfitSize(big, ExtraBold, 64, 28, cardW)
	merLines := []billLine{
		{big, ExtraBold, bs, green},
		{meralcoPrevLine(mer.PrevRate, mer.Announced), Medium, 28, mutCol},
		{meralcoImpactLine(mer.Impact200), Medium, 30, mutCol},
	}
	// Card 2: LPG 11 kg NCR.
	diff := 0.0
	if len(s.LPG) > 1 {
		diff = lpg.Common - s.LPG[1].Common
	}
	chipTxt := signedWholePeso(diff)
	bigLPG := wholePeso(lpg.Common)
	bl := cfitSize(bigLPG, ExtraBold, 64, 28, cardW)
	lpgLine := fmt.Sprintf("Las Piñas %s–%s · DOE %s",
		wholePeso(lpg.CityLow), stripPeso(wholePeso(lpg.CityHigh)),
		utilities.ShortRangeTL(lpg.PeriodFrom, lpg.PeriodTo))
	lpgLines := []billLine{
		{bigLPG, ExtraBold, bl, green},
		{lpgLine, Medium, cfitSize(lpgLine, Medium, 30, 20, cardW), mutCol},
	}
	// Card 3: Maynilad Las Piñas at the LPG detail size, wrapped.
	var mayLines []billLine
	if s.Water == nil {
		ls, sz := wrapLines("walang datos — hindi natiningnan", Medium, 30, 22, cardW, 2)
		for _, ln := range ls {
			mayLines = append(mayLines, billLine{ln, Medium, sz, mutCol})
		}
	} else if rows := utilWaterRows(s.Water); len(rows) > 0 {
		for _, r := range rows {
			mayLines = append(mayLines, billLine{r, Medium, 30, mutCol})
		}
	} else {
		ctl, _, _ := utilities.ShortDay(s.Water.CheckedAt)
		ls, sz := wrapLines(fmt.Sprintf("walang naka-schedule na interruption — tiningnan %s", ctl), Medium, 30, 22, cardW, 2)
		for _, ln := range ls {
			mayLines = append(mayLines, billLine{ln, Medium, sz, mutCol})
		}
	}

	// Content-sized cards; leftover space shared evenly.
	_, _, h1 := billCardGeom("Meralco", merLines)
	_, _, h2 := billCardGeom("LPG 11 kg NCR", lpgLines)
	_, _, h3 := billCardGeom("Maynilad Las Piñas", mayLines)
	const cardGap = 24
	y := 256
	footerTop := H - 118
	leftover := footerTop - y - (h1 + h2 + h3) - 2*cardGap
	extra := 0
	if leftover > 0 {
		extra = leftover / 3
	}
	y = drawBillCard(dst, y, "Meralco", f4(mer.Adjustment), mer.Adjustment < 0, true, merLines, extra) + cardGap
	y = drawBillCard(dst, y, "LPG 11 kg NCR", chipTxt, diff < 0, true, lpgLines, extra) + cardGap
	drawBillCard(dst, y, "Maynilad Las Piñas", "", false, false, mayLines, extra)

	utilFooter(dst, "Source: DOE · Meralco · Maynilad", "Hindi opisyal na page")
	return dst, nil
}

// wholeRate formats a Meralco rate with 4 decimals (unsigned ₱).
func wholeRate(v float64) string {
	if v < 0 {
		return fmt.Sprintf("−₱%.4f", -v)
	}
	return fmt.Sprintf("₱%.4f", v)
}

// utilWaterRows summarizes up to 2 Maynilad city rows for the card.
func utilWaterRows(w *utilities.WaterStatus) []string {
	var out []string
	push := func(barangays, when string) {
		if len(out) >= 2 {
			return
		}
		out = append(out, fmt.Sprintf("%s — %s", barangays, when))
	}
	for _, list := range [][]utilities.Interruption{w.Scheduled, w.Emergency, w.Rotational} {
		for _, in := range list {
			when := in.Time
			if when == "" {
				when = in.From
			} else if in.From != "" {
				when = in.From + ", " + in.Time
			}
			push(in.Barangays, when)
		}
	}
	for _, m := range w.Maintenance {
		push(m.Barangays, m.Schedule)
	}
	return out
}
