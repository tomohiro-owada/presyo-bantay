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
			truncated := false
			if len(lines) > maxLines {
				lines = lines[:maxLines]
				truncated = true
			}
			for i, ln := range lines {
				// A truncated tail must always show its ellipsis;
				// fitting lines pass through unchanged.
				if (truncated && i == len(lines)-1) || measure(ln, w, sz) > maxW {
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

// fuelRowPlace is one main-section row: bar, value label, the
// vertically centered product name, and the muted last-week line.
type fuelRowPlace struct {
	bx0, bx1, by0, by1 int
	lx, ly, lw         int
	size               float64
	inside             bool
	nx, ny, nw         int // name baseline origin + width
	nsize              float64
	sub                string // last-week text ("" when omitted)
	sx, sy, sw         int
	ssize              float64
}

// placeFuelRow computes one main-section row at row middle cy. The
// product name shares the row middle; lastWeek (already formatted,
// "" to omit) sits below it as small muted text.
func placeFuelRow(y, rowH, zx int, unit float64, name, lastWeek string, r *utilities.Range) (fuelRowPlace, string, bool) {
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
	// Product name shares the row middle; the muted last-week line
	// sits below it when present (two-line group stays centered).
	nsize := 32.0
	maxW := barX0 - marginL - 12
	for nsize > 24 && cmeasure(name, Bold, nsize) > maxW {
		nsize -= 2
	}
	ssize := 20.0
	sub := dropMissing(lastWeek, Medium)
	for ssize > 14 && cmeasure(sub, Medium, ssize) > maxW {
		ssize -= 2
	}
	if sub == "" {
		g.nx, g.ny, g.nw, g.nsize = marginL, cy+11, cmeasure(name, Bold, nsize), nsize
	} else {
		g.nx, g.ny, g.nw, g.nsize = marginL, cy-6, cmeasure(name, Bold, nsize), nsize
		g.sx, g.sy, g.sw, g.ssize, g.sub = marginL, cy+26, cmeasure(sub, Medium, ssize), ssize, sub
	}
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

// Fuel chart vertical budget constants: pills, gaps, legend, table
// chrome, natural row heights, and guaranteed minimums. Minimums
// (big 70, history 56, table 44) always fit by construction, so
// history labels keep their clearance (see TestHistoryNoOverlaps).
const (
	fuelPillBlock  = 52 + 14
	fuelGapBlock   = 6
	fuelLegendH    = 28
	fuelTableCapH  = 28
	fuelTableHeadH = 28
	fuelTableRowH  = 44
	fuelBigNat     = 110
	fuelHistNat    = 70
	fuelBigMin     = 70
	fuelHistMin    = 56
)

// fuelRowHeights distributes main + history row heights for nWeeks
// history rows and tableRows table rows: naturals scaled to the
// budget, clamped to minimums, leftover shared round-robin.
func fuelRowHeights(nWeeks, tableRows int) (bigRows, histRows []int) {
	tableH := 0
	if tableRows > 0 {
		tableH = fuelGapBlock + fuelPillBlock + fuelTableCapH + fuelTableHeadH + tableRows*fuelTableRowH
	}
	fixed := fuelPillBlock + fuelGapBlock + fuelPillBlock + fuelLegendH + tableH
	avail := (H - 118) - 256 - fixed
	need := 3*fuelBigNat + nWeeks*fuelHistNat
	bigH, histH := fuelBigNat, fuelHistNat
	if need > 0 {
		f := float64(avail) / float64(need)
		bigH = int(float64(fuelBigNat) * f)
		histH = int(float64(fuelHistNat) * f)
		if bigH > 170 {
			bigH = 170
		}
		if histH > 120 {
			histH = 120
		}
		if bigH < fuelBigMin {
			bigH = fuelBigMin
		}
		if histH < fuelHistMin {
			histH = fuelHistMin
		}
	}
	for bigH > fuelBigMin && 3*bigH+nWeeks*histH > avail {
		bigH--
	}
	for histH > fuelHistMin && 3*bigH+nWeeks*histH > avail {
		histH--
	}
	rows := []int{bigH, bigH, bigH}
	for i := 0; i < nWeeks; i++ {
		rows = append(rows, histH)
	}
	for leftover := avail - (3*bigH + nWeeks*histH); leftover > 0; leftover-- {
		rows[leftover%len(rows)]++
	}
	return rows[:3], rows[3:]
}

// RenderFuelWeekly builds the 1080x1350 fuel chart: this week's three
// fuel bars, up to 6 weeks of gasoline/diesel history (newest last),
// and the now-vs-previous pump price table. Row heights grow to fill
// the space above the footer instead of leaving a blank gap.
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
	var prevPump *utilities.PumpSummary
	if pump != nil {
		for i := range s.Fuel.NCRPump {
			p := &s.Fuel.NCRPump[i]
			if p.WeekFrom < pump.WeekFrom && (prevPump == nil || p.WeekFrom > prevPump.WeekFrom) {
				prevPump = p
			}
		}
	}
	// Last week's values for the muted row texts.
	lastWeek := map[string]string{}
	if len(s.Fuel.Adjustments) > 1 {
		for _, e := range []struct {
			name string
			r    *utilities.Range
		}{
			{"Gasolina", s.Fuel.Adjustments[1].Gasoline},
			{"Diesel", s.Fuel.Adjustments[1].Diesel},
			{"Kerosene", s.Fuel.Adjustments[1].Kerosene},
		} {
			if e.r != nil {
				lastWeek[e.name] = "nakaraan: " + fuelValLabel(e.r)
			}
		}
	}

	y := 256

	var tableRows []pumpRowSpec
	var tableV, tableC float64
	if pump != nil {
		tableRows, tableV, tableC = layoutPumpTable(pump, prevPump)
	}
	bigRows, histRows := fuelRowHeights(len(weeks), len(tableRows))

	// Section 1: this week's movement.
	pill(dst, "Galaw ngayong linggo", marginL, y, green)
	y += fuelPillBlock
	rowsTop := y
	for i, f := range fuels {
		rowH := bigRows[i]
		g, lbl, ok := placeFuelRow(y, rowH, zx, unit, f.name, lastWeek[f.name], f.r)
		col := green
		barCol := green
		if f.r != nil && !fuelNeg(f.r) {
			col, barCol = redName, red
		}
		if !ok {
			cdraw(dst, "—", SemiBold, 27, mutCol, marginL, y+rowH/2+9)
		} else {
			cdraw(dst, cellipsize(f.name, Bold, g.nsize, barX0-marginL-12), Bold, g.nsize, col, g.nx, g.ny)
			if g.sub != "" {
				cdraw(dst, cellipsize(g.sub, Medium, g.ssize, barX0-marginL-12), Medium, g.ssize, mutCol, g.sx, g.sy)
			}
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
	y += fuelGapBlock
	pill(dst, "Nakaraang mga linggo", marginL, y, greenDk)
	y += fuelPillBlock
	cdraw(dst, historyLegend(), Medium, 22, mutCol, marginL, y+20)
	y += fuelLegendH
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

	// Section 3: pump price table (now vs previous week).
	if len(tableRows) > 0 {
		y += fuelGapBlock
		pill(dst, "Presyo sa bomba", marginL, y, green)
		y += fuelPillBlock
		cap := pumpTableCaption(pump, prevPump)
		cs := cfitSize(cap, Medium, 24, 14, W-marginL-marginR)
		cdraw(dst, cellipsize(cap, Medium, cs, W-marginL-marginR), Medium, cs, mutCol, marginL, y+22)
		y += fuelTableCapH
		y = drawPumpTable(dst, tableRows, tableV, tableC, y, fuelTableRowH)
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
		// Outside on the far side; the cy+6 baseline keeps ink clear
		// of the adjacent rows (see TestHistoryNoOverlaps).
		g.lx, g.ly = bx0-8-lw, cy+6
	} else {
		g.lx, g.ly = bx1+8, cy+6
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

// pumpDeltaText formats a compact delta chip ("−0.40", no ₱).
func pumpDeltaText(d float64) string {
	if d < 0 {
		return fmt.Sprintf("−%.2f", -d)
	}
	if d > 0 {
		return fmt.Sprintf("+%.2f", d)
	}
	return "0.00"
}

// pumpCellSpec is one laid-out table cell.
type pumpCellSpec struct {
	value string // "₱87.90" or "—"
	delta string // "−0.40" or "n/a"
	dneg  bool   // delta is a decrease (green)
	dzero bool   // delta is zero/n/a (grey)
	vx    int    // value baseline origin x
	cx    int    // chip rect x
	cw    int    // chip width
	colW  int    // column width
	empty bool   // no data at all (skip cell)
}

// pumpRowSpec is one laid-out table row.
type pumpRowSpec struct {
	label string
	cells [3]pumpCellSpec
}

// layoutPumpTable lays out the NCR/Parañaque/Muntinlupa × RON91/95/
// Diesel table: equal value columns sharing the content width. Rows
// without any data this week are skipped. It returns the rows plus the
// fitted value/chip sizes.
func layoutPumpTable(now, prev *utilities.PumpSummary) (rows []pumpRowSpec, vsize, csize float64) {
	ncrOf := func(sum *utilities.PumpSummary, product string) (float64, bool) {
		if sum == nil {
			return 0, false
		}
		for _, p := range sum.Prices {
			if p.Product == product {
				return p.Common, true
			}
		}
		return 0, false
	}
	cityOf := func(sum *utilities.PumpSummary, city, product string) (*float64, bool) {
		if sum == nil {
			return nil, false
		}
		for i := range sum.Cities {
			if sum.Cities[i].City != city {
				continue
			}
			for j := range sum.Cities[i].Prices {
				if sum.Cities[i].Prices[j].Product == product {
					return sum.Cities[i].Prices[j].Common, true
				}
			}
		}
		return nil, false
	}
	keys := []string{"RON91", "RON95", "Diesel"}
	for _, e := range []struct {
		label, city string
	}{
		{"NCR", ""}, {"Parañaque", "Parañaque"}, {"Muntinlupa", "Muntinlupa"},
	} {
		var row pumpRowSpec
		row.label = e.label
		any := false
		for i, k := range keys {
			cell := pumpCellSpec{empty: true}
			var nv *float64
			var present bool
			if e.city == "" {
				if v, ok := ncrOf(now, k); ok {
					nv, present = &v, true
				}
			} else {
				nv, present = cityOf(now, e.city, k)
			}
			if !present {
				row.cells[i] = cell
				continue
			}
			any = true
			cell.empty = false
			var pv *float64
			if e.city == "" {
				if v, ok := ncrOf(prev, k); ok {
					pv = &v
				}
			} else if v, ok := cityOf(prev, e.city, k); ok {
				pv = v
			}
			if nv == nil {
				cell.value, cell.delta, cell.dzero = "—", "n/a", true
			} else {
				cell.value = "₱" + stripPeso(f2(*nv))
				if pv == nil {
					cell.delta, cell.dzero = "n/a", true
				} else {
					d := *nv - *pv
					cell.delta = pumpDeltaText(d)
					cell.dneg = d < 0
					cell.dzero = d == 0
				}
			}
			row.cells[i] = cell
		}
		if any {
			rows = append(rows, row)
		}
	}
	// Column geometry: label column + 3 equal value columns.
	// The label column fits "Muntinlupa" at 24 px with room to spare.
	const labelW = 210
	const colGap = 16
	colsX := marginL + labelW + 12
	colW := (W - marginR - colsX - 2*colGap) / 3
	vsize, csize = 26.0, 20.0
	for {
		ok := true
		for _, r := range rows {
			for _, c := range r.cells {
				if c.empty {
					continue
				}
				if cmeasure(c.value, Bold, vsize)+8+cmeasure(c.delta, Bold, csize)+28 > colW {
					ok = false
				}
			}
		}
		if ok || (vsize <= 20 && csize <= 16) {
			break
		}
		if vsize > 20 {
			vsize -= 2
		} else {
			csize -= 2
		}
	}
	for ri := range rows {
		x := colsX
		for i := range rows[ri].cells {
			c := &rows[ri].cells[i]
			c.colW = colW
			c.vx = x
			c.cw = cmeasure(c.delta, Bold, csize) + 28
			c.cx = x + cmeasure(c.value, Bold, vsize) + 8
			if c.cx+c.cw > x+colW {
				c.cx = x + colW - c.cw
			}
			x += colW + colGap
		}
	}
	return rows, vsize, csize
}

// tableHeader are the pump table column headers (at least 20 px).
var tableHeader = [3]string{"RON 91", "RON 95", "Diesel"}

// tableHeadSize is the pump table header text size.
const tableHeadSize = 20.0

// drawPumpTable draws the table header, rows (rowH each), and delta
// chips (green down / red up / grey zero or n/a). Empty cells stay
// blank. It returns the next y.
func drawPumpTable(dst *image.RGBA, rows []pumpRowSpec, vsize, csize float64, y, rowH int) int {
	if len(rows) == 0 {
		return y
	}
	const labelW = 210
	colsX := marginL + labelW + 12
	colW := rows[0].cells[0].colW
	for i, h := range tableHeader {
		cdraw(dst, h, Medium, tableHeadSize, mutCol, colsX+i*(colW+16), y+20)
	}
	y += 28
	for _, r := range rows {
		cdraw(dst, cellipsize(r.label, Bold, 24, labelW), Bold, 24, green, marginL, y+rowH/2+8)
		for _, c := range r.cells {
			if c.empty {
				continue
			}
			cdraw(dst, c.value, Bold, vsize, green, c.vx, y+rowH/2+9)
			var fill color.RGBA
			tcol := white
			switch {
			case c.dzero:
				fill, tcol = grey, white
			case c.dneg:
				fill = green
			default:
				fill = red
			}
			cy := y + rowH/2
			fillRounded(dst, c.cx, cy-17, c.cx+c.cw, cy+17, 9, fill)
			cdraw(dst, c.delta, Bold, csize, tcol, c.cx+14, cy+7)
		}
		y += rowH
	}
	return y
}

// pumpTableCaption is the caption above the table.
func pumpTableCaption(now, prev *utilities.PumpSummary) string {
	nr := utilities.ShortRangeTL(now.WeekFrom, now.WeekTo)
	vs := ""
	if prev != nil {
		vs = " vs " + utilities.ShortRangeTL(prev.WeekFrom, prev.WeekTo)
	}
	return fmt.Sprintf("Pump price DOE %s%s · Parañaque at Muntinlupa: malapit sa Las Piñas", nr, vs)
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

// billChip draws a small chip with the given fill, returning its width.
func billChip(dst *image.RGBA, text string, fill color.RGBA, x, y int) int {
	cw := cmeasure(text, Bold, 28) + 36
	fillRounded(dst, x, y, x+cw, y+52, 12, fill)
	cdraw(dst, text, Bold, 28, white, x+18, y+36)
	return cw
}

// chipFill picks red for increases, green for decreases, grey for zero.
func chipFill(neg, zero bool) color.RGBA {
	if zero {
		return grey
	}
	if neg {
		return green
	}
	return red
}

// meralcoChipText is the Meralco card chip ("+₱0.3191 vs Set").
func meralcoChipText(adj float64, month string) string {
	vs := ""
	if prev, ok := utilities.PrevMonthShortTL(month); ok {
		vs = " vs " + prev
	}
	return f4(adj) + vs
}

// lpgChipText is the LPG card chip ("+₱89 vs Set 1–7" — the comparison
// period is always included when known).
func lpgChipText(diff float64, prevRange string) string {
	chip := signedWholePeso(diff)
	if prevRange != "" {
		chip += " vs " + prevRange
	}
	return chip
}

// waterChipText is the water card chip ("+₱0.24 mula Okt 1").
func waterChipText(diff float64, day string) string {
	chip := fmt.Sprintf("+₱%.2f", diff)
	if diff < 0 {
		chip = fmt.Sprintf("−₱%.2f", -diff)
	}
	if day != "" {
		chip += " mula " + day
	}
	return chip
}

// wholeU formats unsigned whole pesos with thousands separators.
func wholeU(v float64) string {
	if v < 0 {
		v = -v
	}
	s := fmt.Sprintf("%.0f", v)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "₱" + s
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
func drawBillCard(dst *image.RGBA, y int, title, chip string, fill color.RGBA, hasChip bool, lines []billLine, extra int) int {
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
		billChip(dst, chip, fill, W-marginR-padX-chipW, y+20)
	}
	cdraw(dst, cellipsize(title, Bold, 32, maxTitle), Bold, 32, green, hx, y+headerBase)
	for i, ln := range lines {
		cdraw(dst, cellipsize(ln.text, ln.w, ln.size, cardW), ln.w, ln.size, ln.col, hx, y+bases[i])
	}
	return y + cardH
}

// billCardSpec is one card's content.
type billCardSpec struct {
	title   string
	chip    string
	fill    color.RGBA
	hasChip bool
	lines   []billLine
	big     int // index of the big old→new line in lines, or -1
}

// buildBillsCards assembles the 4 bills cards (Meralco, LPG, Water,
// Family). bigCap caps the big old→new line sizes (overflow guard).
// bigFit fits a big old→new line, reserving the chip width when the
// card has a chip so the two never overlap.
func bigFit(text, chip string, hasChip bool, nat, min, bigCap float64, cardW int) float64 {
	maxW := cardW
	if hasChip {
		maxW = cardW - (cmeasure(chip, Bold, 28) + 36) - 16
	}
	sz := cfitSize(text, ExtraBold, nat, min, maxW)
	if sz > bigCap {
		sz = bigCap
	}
	return sz
}

// prevCityLine is the LPG card's previous-period Las Piñas range
// ("Set: ₱1,185–₱1,393").
func prevCityLine(prev utilities.LPGMonth) string {
	mon, ok := utilities.MonthShortTL(prev.PeriodFrom)
	if !ok {
		mon = "nakaraan"
	}
	return fmt.Sprintf("%s: %s–%s", mon, wholeU(prev.CityLow), wholeU(prev.CityHigh))
}

func buildBillsCards(s *utilities.Snapshot, bigCap float64) []billCardSpec {
	const padX = 30
	cardW := W - marginL - marginR - 2*padX
	mer := s.Meralco[0]
	lpg := s.LPG[0]
	var cards []billCardSpec

	// Card 1: Meralco old → new.
	merChip := meralcoChipText(mer.Adjustment, mer.Month)
	big := wholeRate(mer.PrevRate) + " → " + wholeRate(mer.Rate) + "/kWh"
	bs := bigFit(big, merChip, true, 48, 28, bigCap, cardW)
	cards = append(cards, billCardSpec{
		title: "Meralco", chip: merChip,
		fill: chipFill(mer.Adjustment < 0, mer.Adjustment == 0), hasChip: true,
		lines: []billLine{
			{big, ExtraBold, bs, green},
			{meralcoImpactLine(mer.Impact200), Medium, 28, mutCol},
		},
		big: 0,
	})

	// Card 2: LPG old → new.
	var chip string
	var hasChip bool
	var fill color.RGBA
	var lpgLines []billLine
	if len(s.LPG) > 1 {
		prev2 := s.LPG[1]
		d := lpg.Common - prev2.Common
		chip = lpgChipText(d, utilities.ShortRangeTL(prev2.PeriodFrom, prev2.PeriodTo))
		fill = chipFill(d < 0, d == 0)
		hasChip = true
		bigLPG := wholeU(prev2.Common) + " → " + wholeU(lpg.Common)
		bl := bigFit(bigLPG, chip, true, 52, 28, bigCap, cardW)
		lpgLines = []billLine{
			{bigLPG, ExtraBold, bl, green},
			{fmt.Sprintf("Las Piñas %s–%s · DOE %s", wholeU(lpg.CityLow), wholeU(lpg.CityHigh),
				utilities.ShortRangeTL(lpg.PeriodFrom, lpg.PeriodTo)), Medium, 28, mutCol},
			{prevCityLine(prev2), Medium, 28, mutCol},
		}
	} else {
		bigLPG := wholeU(lpg.Common)
		bl := bigFit(bigLPG, "", false, 52, 28, bigCap, cardW)
		lpgLines = []billLine{
			{bigLPG, ExtraBold, bl, green},
			{fmt.Sprintf("Las Piñas %s–%s · DOE %s", wholeU(lpg.CityLow), wholeU(lpg.CityHigh),
				utilities.ShortRangeTL(lpg.PeriodFrom, lpg.PeriodTo)), Medium, 28, mutCol},
		}
	}
	cards = append(cards, billCardSpec{title: "LPG 11 kg NCR", chip: chip, fill: fill, hasChip: hasChip, lines: lpgLines, big: 0})

	// Card 3: Water old → new + interruption status.
	rateM := utilities.WaterRateFor(s.WaterRates, mer.Month)
	var mayLines []billLine
	wchip, whasChip, wfill := "", false, grey
	if rateM != nil {
		rateP := utilities.WaterRateFor(s.WaterRates, utilities.PrevMonth(mer.Month))
		if rateP != nil {
			d := rateM.Rate - rateP.Rate
			wchip = waterChipText(d, utilities.DayLabelTL(rateM.Effective))
			wfill = chipFill(d < 0, d == 0)
			whasChip = true
			bigW := fmtCents2(rateP.Rate) + " → " + fmtCents2(rateM.Rate) + "/cu.m"
			bw := bigFit(bigW, wchip, true, 44, 28, bigCap, cardW)
			mayLines = append(mayLines, billLine{bigW, ExtraBold, bw, green})
		} else {
			bigW := fmtCents2(rateM.Rate) + "/cu.m"
			bw := bigFit(bigW, "", false, 44, 28, bigCap, cardW)
			mayLines = append(mayLines, billLine{bigW, ExtraBold, bw, green})
		}
	}
	if s.Water == nil {
		ls, sz := wrapLines("walang datos — hindi natiningnan", Medium, 28, 22, cardW, 2)
		for _, ln := range ls {
			mayLines = append(mayLines, billLine{ln, Medium, sz, mutCol})
		}
	} else if rows := utilWaterRowPairs(s.Water); len(rows) > 0 {
		for _, r := range rows {
			als, asz := wrapLines(r.areas, Medium, 28, 22, cardW, 2)
			for _, ln := range als {
				mayLines = append(mayLines, billLine{ln, Medium, asz, green})
			}
			wls, wsz := wrapLines("— "+r.when, Medium, 24, 20, cardW, 2)
			for _, ln := range wls {
				mayLines = append(mayLines, billLine{ln, Medium, wsz, mutCol})
			}
		}
	} else {
		ctl, _, _ := utilities.ShortDay(s.Water.CheckedAt)
		ls, sz := wrapLines(fmt.Sprintf("walang naka-schedule na interruption — tiningnan %s", ctl), Medium, 28, 22, cardW, 2)
		for _, ln := range ls {
			mayLines = append(mayLines, billLine{ln, Medium, sz, mutCol})
		}
	}
	cards = append(cards, billCardSpec{title: "Maynilad Las Piñas", chip: wchip, fill: wfill, hasChip: whasChip, lines: mayLines, big: 0})

	// Card 4: family total.
	if fig, ok := utilities.FamilyFigures(s, rateM, utilities.WaterRateFor(s.WaterRates, utilities.PrevMonth(mer.Month))); ok {
		var flines []billLine
		bigF := fmtCents2(fig.Prev) + " → " + fmtCents2(fig.Cur)
		if !fig.HasPrev {
			bigF = fmtCents2(fig.Cur)
		}
		fchip, fhasChip, ffill := "", false, grey
		if fig.HasPrev {
			fchip = fmtCentsSigned2(fig.Change)
			ffill = chipFill(fig.Change < 0, fig.Change == 0)
			fhasChip = true
		}
		bf := bigFit(bigF, fchip, fhasChip, 40, 28, bigCap, cardW)
		flines = append(flines, billLine{bigF, ExtraBold, bf, green})
		if fig.HasPrev {
			br := fmt.Sprintf("kuryente %s · LPG %s · tubig %s",
				fmtCentsSigned2(fig.Power), fmtCentsSigned2(fig.LpgC), fmtCentsSigned2(fig.WaterC))
			flines = append(flines, billLine{br, Medium, cfitSize(br, Medium, 26, 20, cardW), mutCol})
		} else {
			flines = append(flines, billLine{"Walang datos ng nakaraang kabuuan", Medium, 26, mutCol})
		}
		flines = append(flines, billLine{"Palagay: 200 kWh · 1 LPG 11 kg · 20 cu.m tubig", Medium, cfitSize("Palagay: 200 kWh · 1 LPG 11 kg · 20 cu.m tubig", Medium, 24, 18, cardW), mutCol})
		cards = append(cards, billCardSpec{title: "Pamilya kada buwan", chip: fchip, fill: ffill, hasChip: fhasChip, lines: flines, big: 0})
	}
	return cards
}

// fmtCents2 formats unsigned with 2 decimals + thousands separators
// (chart twin of the caption helper).
func fmtCents2(v float64) string {
	if v < 0 {
		v = -v
	}
	ip := int(v)
	fp := int(v*100+0.5) - ip*100
	if fp >= 100 {
		ip++
		fp -= 100
	}
	s := fmt.Sprintf("%d", ip)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "₱" + s + fmt.Sprintf(".%02d", fp)
}

// fmtCentsSigned2 formats signed with 2 decimals.
func fmtCentsSigned2(v float64) string {
	if v < 0 {
		return "−" + fmtCents2(v)
	}
	return "+" + fmtCents2(v)
}

// RenderBills builds the 1080x1350 household-bills chart: Meralco, LPG,
// Water and Family cards sized to content with leftover space shared
// evenly (big lines shrink first when over budget).
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
	month, ok := utilities.MonthTitleTL(mer.Month)
	if !ok {
		return nil, fmt.Errorf("fbchart: bad meralco month %q", mer.Month)
	}
	utilTitle(dst, "Bayarin sa bahay", month)

	const cardGap = 20
	y := 256
	footerTop := H - 118
	avail := footerTop - y
	cards := buildBillsCards(s, 64)
	for step := 0; step < 5; step++ {
		total := 0
		for _, cd := range cards {
			_, _, h := billCardGeom(cd.title, cd.lines)
			total += h
		}
		total += (len(cards) - 1) * cardGap
		if total <= avail || step == 4 {
			break
		}
		// Over budget: shrink the big lines and rebuild.
		cards = buildBillsCards(s, 64-float64((step+1)*8))
	}
	leftover := avail
	for _, cd := range cards {
		_, _, h := billCardGeom(cd.title, cd.lines)
		leftover -= h
	}
	leftover -= (len(cards) - 1) * cardGap
	extra := 0
	if leftover > 0 {
		extra = leftover / len(cards)
	}
	for i, cd := range cards {
		y = drawBillCard(dst, y, cd.title, cd.chip, cd.fill, cd.hasChip, cd.lines, extra)
		if i+1 < len(cards) {
			y += cardGap
		}
	}

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

// waterRowPair is one Maynilad city row for the card.
type waterRowPair struct{ areas, when string }

// utilWaterRowPairs collects up to 2 Maynilad city rows.
func utilWaterRowPairs(w *utilities.WaterStatus) []waterRowPair {
	var out []waterRowPair
	push := func(barangays, when string) {
		if len(out) >= 2 {
			return
		}
		out = append(out, waterRowPair{barangays, when})
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
