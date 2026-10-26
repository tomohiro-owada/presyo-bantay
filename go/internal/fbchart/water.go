package fbchart

import (
	"fmt"
	"image"
	"image/draw"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// Water alert chart (UTILITIES_SPEC_V2.md A5 + polish): 1080x1350 PNG
// in the weekly style. Date window ~48 px bold in a rounded panel,
// areas ~34 px, reason ~32 px, a prominent "Mag-ipon ng tubig" pill,
// and a "Detalye:" line; content is vertically distributed. With
// pastSample a red "PAST DATA SAMPLE" banner tops the image.

// pastBanner is the sample banner text.
const pastBanner = "PAST DATA SAMPLE — not a current advisory"

// waterDetalye is the chart source line (domain only, per polish).
const waterDetalye = "Detalye: mayniladwater.com.ph/service-advisories-2"

// Water chart type sizes.
const (
	waterWindowSize = 48.0
	waterWindowMin  = 30.0
	waterAreasSize  = 34.0
	waterAreasMin   = 24.0
	waterReasonSize = 32.0
	waterReasonMin  = 22.0
	waterPillSize   = 30.0
	waterDetSize    = 24.0
)

// utilHeaderAt draws the centered icon + "Presyo Bantay" row shifted
// by dy (same geometry as utilHeader when dy is 0).
func utilHeaderAt(dst *image.RGBA, dy int) {
	icon := Icon()
	headText := "Presyo Bantay"
	headSize := 44.0
	tw := measure(headText, SemiBold, headSize)
	rowW := tw
	if icon != nil {
		rowW = icon.Bounds().Dx() + 18 + tw
	}
	hx := (W - rowW) / 2
	hy := 28 + dy
	if icon != nil {
		draw.Draw(dst, image.Rect(hx, hy, hx+icon.Bounds().Dx(), hy+icon.Bounds().Dy()),
			icon, image.Point{}, draw.Over)
		hx += icon.Bounds().Dx() + 18
	}
	drawString(dst, headText, SemiBold, headSize, green, hx, hy+34+18)
}

// utilTitleAt draws the centered title + subtitle shifted by dy.
func utilTitleAt(dst *image.RGBA, title, sub string, dy int) {
	ts := fitSize(title, ExtraBold, 64, 28, W-2*marginL-60)
	tx := (W - measure(title, ExtraBold, ts)) / 2
	drawString(dst, title, ExtraBold, ts, green, tx, 184+dy)
	ss := fitSize(sub, Medium, 30, 20, W-marginL-marginR)
	sx := (W - measure(sub, Medium, ss)) / 2
	drawString(dst, sub, Medium, ss, green, sx, 226+dy)
}

// waterWindowLines returns the big window lines at ~48 px bold,
// shrinking to fit (parsed alerts yield exactly two lines).
func waterWindowLines(a *utilities.WaterAlert, maxW int) ([]string, float64) {
	base := utilities.AlertWindowTL(a)
	if len(base) == 2 {
		size := waterWindowSize
		for size > waterWindowMin {
			if cmeasure(base[0], ExtraBold, size) <= maxW && cmeasure(base[1], ExtraBold, size) <= maxW {
				break
			}
			size -= 2
		}
		return base, size
	}
	return wrapLines(strings.Join(base, " "), ExtraBold, waterWindowSize, waterWindowMin, maxW, 2)
}

// waterAdvicePill draws the prominent advice pill, returning its width.
func waterAdvicePill(dst *image.RGBA, x, y int) int {
	text := "Mag-ipon ng tubig bago magsimula."
	pw := cmeasure(text, Bold, waterPillSize) + 56
	fillRounded(dst, x, y, x+pw, y+64, 14, green)
	cdraw(dst, text, Bold, waterPillSize, white, x+28, y+42)
	return pw
}

// RenderWaterAlert builds the 1080x1350 water alert chart.
func RenderWaterAlert(a *utilities.WaterAlert, pastSample bool) (*image.RGBA, error) {
	if a == nil || len(a.Areas) == 0 {
		return nil, fmt.Errorf("fbchart: no water alert")
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
	utilTitleAt(dst, "Abiso sa tubig", a.City, dy)

	const padX = 30
	cardW := W - 2*marginL - 2*padX
	top := 256 + dy
	footerTop := H - 118

	// Window panel (white rounded rect behind the ~48 px window).
	win, wsize := waterWindowLines(a, cardW-48)
	winH := 0
	for range win {
		winH += int(wsize*1.35 + 0.5)
	}
	panelH := winH + 48

	// Areas (~34 px, wrapped, capped by budget).
	areasText := strings.Join(a.Areas, ", ")
	rest := footerTop - top - panelH - 120 - 90 - 60 - 40 // pill + reason + detalye + gaps reserve
	maxAreaLines := rest / 46
	if maxAreaLines < 2 {
		maxAreaLines = 2
	}
	if maxAreaLines > 8 {
		maxAreaLines = 8
	}
	als, asz := wrapLines(areasText, Medium, waterAreasSize, waterAreasMin, cardW, maxAreaLines)

	// Reason (~32 px).
	rl := fmt.Sprintf("Dahilan: %s", utilities.AlertReason(a.ReasonKey, 0))
	rs := cfitSize(rl, Medium, waterReasonSize, waterReasonMin, cardW)
	rl = cellipsize(rl, Medium, rs, cardW)

	// Block heights, then even vertical distribution.
	areasH := 0
	for range als {
		areasH += int(asz*1.35 + 0.5)
	}
	reasonH := int(rs*1.35 + 0.5)
	blocks := []int{panelH, areasH, reasonH, 64 + 20, 40}
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
	// Reason.
	cdraw(dst, rl, Medium, rs, mutCol, marginL+padX, y+int(rs*0.85+0.5))
	y += reasonH + pad
	// Advice pill (prominent, left-aligned).
	waterAdvicePill(dst, marginL+padX, y)
	y += 64 + 20 + pad
	// Detalye line.
	ds := cfitSize(waterDetalye, Medium, waterDetSize, 18, cardW)
	cdraw(dst, cellipsize(waterDetalye, Medium, ds, cardW), Medium, ds, mutCol, marginL+padX, y+int(ds*0.85+0.5))

	utilFooter(dst, "Source: Maynilad advisory", "Hindi opisyal na Maynilad page")
	return dst, nil
}
