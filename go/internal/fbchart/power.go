package fbchart

import (
	"fmt"
	"image"
	"image/draw"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// Power alert chart (Part C): 1080x1350 PNG in the water-alert style.
// Title "Abiso sa kuryente", big date + window(s), barangays, roads,
// reason, trend line, advice pill, detalye line and the Meralco footer.
// With pastSample a red banner tops the image.

// powerDetalye is the chart source line (domain only).
const powerDetalye = "Detalye: company.meralco.com.ph/news-and-advisories/maintenance-schedule"

// Power chart type sizes (mirroring the water chart).
const (
	powerWindowSize = 44.0
	powerWindowMin  = 26.0
	powerAreasSize  = 34.0
	powerAreasMin   = 24.0
	powerReasonSize = 32.0
	powerReasonMin  = 22.0
	powerTrendSize  = 28.0
	powerTrendMin   = 20.0
	powerPillSize   = 30.0
	powerDetSize    = 24.0
)

// powerWindowLines returns the big window lines for a power alert: the
// TL date line wrapped to fit (up to 3 lines).
func powerWindowLines(a *utilities.PowerAlert, maxW int) ([]string, float64) {
	s := strings.TrimSpace(powerDateTL(a))
	if s == "" {
		return []string{""}, powerWindowMin
	}
	return wrapLines(s, ExtraBold, powerWindowSize, powerWindowMin, maxW, 3)
}

// powerDateTL mirrors the caption TL date line for the chart.
func powerDateTL(a *utilities.PowerAlert) string {
	// Reuse the caption renderer via a minimal TL extraction: the
	// caption's 📅 line without the emoji.
	text := utilities.RenderPowerAlertPost(*a, false)
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, "📅 ") {
			return strings.TrimPrefix(ln, "📅 ")
		}
	}
	return ""
}

// powerAdvicePill draws the prominent advice pill, returning its width.
func powerAdvicePill(dst *image.RGBA, x, y int) int {
	text := "Ihanda ang gadgets at flashlight."
	pw := cmeasure(text, Bold, powerPillSize) + 56
	fillRounded(dst, x, y, x+pw, y+64, 14, green)
	cdraw(dst, text, Bold, powerPillSize, white, x+28, y+42)
	return pw
}

// RenderPowerAlert builds the 1080x1350 power alert chart.
func RenderPowerAlert(a *utilities.PowerAlert, pastSample bool) (*image.RGBA, error) {
	if a == nil || len(a.Barangays) == 0 {
		return nil, fmt.Errorf("fbchart: no power alert")
	}
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)
	dy := 0
	if pastSample {
		fillRect(dst, 0, 0, W, 76, red)
		bw := measure(pastBanner, Bold, 28)
		drawString(dst, pastBanner, Bold, 28, white, (W-bw)/2, 50)
		dy = 90
	}
	utilHeaderAt(dst, dy)
	utilTitleAt(dst, "Abiso sa kuryente", a.City, dy)

	const padX = 30
	cardW := W - 2*marginL - 2*padX
	top := 256 + dy
	footerTop := H - 118

	// Window panel (white rounded rect behind the big date lines).
	win, wsize := powerWindowLines(a, cardW-48)
	winH := 0
	for range win {
		winH += int(wsize*1.35 + 0.5)
	}
	panelH := winH + 48

	// Areas (~34 px, wrapped, capped by budget).
	areasText := strings.Join(a.Barangays, ", ")
	rest := footerTop - top - panelH - 120 - 90 - 60 - 40 - 60 - 50 // pill + reason + roads + trend + detalye + gaps
	maxAreaLines := rest / 46
	if maxAreaLines < 2 {
		maxAreaLines = 2
	}
	if maxAreaLines > 6 {
		maxAreaLines = 6
	}
	als, asz := wrapLines(areasText, Medium, powerAreasSize, powerAreasMin, cardW, maxAreaLines)

	// Roads (~30 px, up to 2 lines).
	var rls []string
	rsz := powerReasonSize
	if len(a.Roads) > 0 {
		rls, rsz = wrapLines(strings.Join(a.Roads, ", "), Medium, 30, 20, cardW, 2)
	}

	// Reason (~32 px).
	rl := fmt.Sprintf("Dahilan: %s", utilities.PowerReason(a.ReasonKey, 0))
	rs := cfitSize(rl, Medium, powerReasonSize, powerReasonMin, cardW)
	rl = cellipsize(rl, Medium, rs, cardW)

	// Trend line (~28 px, TL only on the chart).
	var tls []string
	tsz := powerTrendSize
	hasTrend := a.NthThisMonth >= 1 && !a.Start.IsZero()
	if hasTrend {
		if ttl, _, _, ok := utilities.PowerTrendLines(a); ok {
			tls, tsz = wrapLines(ttl, Medium, powerTrendSize, powerTrendMin, cardW, 2)
		} else {
			hasTrend = false
		}
	}

	// Block heights, then even vertical distribution.
	areasH := 0
	for range als {
		areasH += int(asz*1.35 + 0.5)
	}
	roadsH := 0
	for range rls {
		roadsH += int(rsz*1.35 + 0.5)
	}
	reasonH := int(rs*1.35 + 0.5)
	trendH := 0
	for range tls {
		trendH += int(tsz*1.35 + 0.5)
	}
	blocks := []int{panelH, areasH, roadsH, reasonH, trendH, 64 + 20, 40}
	total := 0
	for _, h := range blocks {
		total += h
	}
	pad := (footerTop - top - total) / (len(blocks) + 1)
	if pad < 8 {
		pad = 8
	}
	y := top + pad
	// Panel + window.
	billCard(dst, y, panelH)
	wy := y + 24
	for _, ln := range win {
		cdraw(dst, cellipsize(ln, ExtraBold, wsize, cardW-48), ExtraBold, wsize, green, marginL+padX+24, wy+int(wsize*0.85+0.5))
		wy += int(wsize*1.35 + 0.5)
	}
	y += panelH + pad
	// Areas.
	for _, ln := range als {
		cdraw(dst, ln, Medium, asz, green, marginL+padX, y+int(asz*0.85+0.5))
		y += int(asz*1.35 + 0.5)
	}
	y += pad
	// Roads.
	for _, ln := range rls {
		cdraw(dst, ln, Medium, rsz, green, marginL+padX, y+int(rsz*0.85+0.5))
		y += int(rsz*1.35 + 0.5)
	}
	if len(rls) > 0 {
		y += pad
	}
	// Reason.
	cdraw(dst, rl, Medium, rs, mutCol, marginL+padX, y+int(rs*0.85+0.5))
	y += reasonH + pad
	// Trend.
	for _, ln := range tls {
		cdraw(dst, ln, Medium, tsz, mutCol, marginL+padX, y+int(tsz*0.85+0.5))
		y += int(tsz*1.35 + 0.5)
	}
	if hasTrend {
		y += pad
	}
	// Advice pill (prominent, left-aligned).
	powerAdvicePill(dst, marginL+padX, y)
	y += 64 + 20 + pad
	// Detalye line.
	ds := cfitSize(powerDetalye, Medium, powerDetSize, 18, cardW)
	cdraw(dst, cellipsize(powerDetalye, Medium, ds, cardW), Medium, ds, mutCol, marginL+padX, y+int(ds*0.85+0.5))

	utilFooter(dst, "Source: Meralco maintenance schedule", "Hindi opisyal na Meralco page")
	_ = draw.Draw
	return dst, nil
}
