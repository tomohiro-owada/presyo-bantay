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

func RenderGrabFare(o *grabfare.Output, maps []image.Image, sample bool) (*image.RGBA, error) {
	if o == nil || !o.Visible || o.Fare == nil {
		return nil, fmt.Errorf("fbchart: grab fare not visible")
	}
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)
	dy := 0
	if sample {
		fillRect(dst, 0, 0, W, 76, red)
		banner := "SAMPLE — trial data, not a live post"
		bw := measure(banner, Bold, 28)
		drawString(dst, banner, Bold, 28, white, (W-bw)/2, 50)
		dy = 90
	}
	utilHeaderAt(dst, dy)
	utilTitleAt(dst, "Tantiya sa pamasahe ng Grab", "Zapote Bridge → Festival Mall · Alabang-Zapote Rd", dy)

	top := 256 + dy
	footerTop := H - 118
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
	}
	asofText := "Ayon sa Grab app noong " + hhmm + " · oras sa Maynila"
	asofSize := cfitSize(asofText, Medium, 28, 20, cardW)
	panelH := 24 + int(bigSize*1.35+0.5) + 12 + int(capSize*1.35+0.5) + 12 + pillH + int(asofSize*1.35+0.5) + 24
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

	numBlocks := 1 + 1
	if trafficH > 0 {
		numBlocks++
	}
	if mapH > 0 {
		numBlocks++
	}
	total := panelH + trendH + trafficH + mapH
	pad := (footerTop - top - total) / (numBlocks + 1)
	if pad < 8 {
		pad = 8
	}

	y := top + pad

	// panel
	billCard(dst, y, panelH)
	cy := y + 24
	// big
	tw := measure(big, ExtraBold, bigSize)
	bx := marginL + (W-marginL-marginR-tw)/2
	drawString(dst, big, ExtraBold, bigSize, green, bx, cy+int(bigSize*0.85+0.5))
	cy += int(bigSize*1.35+0.5) + 12
	// caption
	ctw := measure(capText, Medium, capSize)
	// use cfit? capText fixed
	cx := marginL + (W-marginL-marginR-ctw)/2
	cdraw(dst, capText, Medium, capSize, mutCol, cx, cy+int(capSize*0.85+0.5))
	cy += int(capSize*1.35+0.5) + 12
	// pill
	if o.Fare.Standard != nil {
		pillText := "Grab Standard " + grabfare.FormatPeso(*o.Fare.Standard)
		pw := cmeasure(pillText, Bold, 32) + 52
		px := marginL + (W-marginL-marginR-pw)/2
		pill(dst, pillText, px, cy, green)
		cy += 52 + 12
	}
	// asof
	atw := measure(asofText, Medium, asofSize)
	ax := marginL + (W-marginL-marginR-atw)/2
	cdraw(dst, asofText, Medium, asofSize, mutCol, ax, cy+int(asofSize*0.85+0.5))
	cy += int(asofSize*1.35+0.5) + 24
	_ = cy
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
			// clip outer corners
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					dx := 0
					if xx < x0+14 {
						dx = x0 + 14 - xx
					} else if xx >= x1-14 {
						dx = xx - (x1 - 14 - 1)
					}
					dy2 := 0
					if yy < y0+14 {
						dy2 = y0 + 14 - yy
					} else if yy >= y1-14 {
						dy2 = yy - (y1 - 14 - 1)
					}
					if dx > 0 && dy2 > 0 && dx*dx+dy2*dy2 > 14*14 {
						dst.Set(xx, yy, cream)
					}
				}
			}
		}
		label := "Map: Grab app screenshot"
		ls := cfitSize(label, Medium, 22, 14, W-2*marginL)
		cdraw(dst, label, Medium, ls, mutCol, marginL, stripY+tileH+16+19)
		y += mapH + pad
	}

	utilFooter(dst, "Grab app quote, not an official fare", "Hindi opisyal na pamasahe")
	_ = draw.Draw
	return dst, nil
}

// stripTrendGlyph removes the leading arrow glyph (the card draws its own
// triangle) from a trend text.
func stripTrendGlyph(s string) string {
	s = strings.TrimSpace(s)
	for _, g := range []string{"\u25b2", "\u25bc", "\uff1d"} {
		if strings.HasPrefix(s, g) {
			return strings.TrimSpace(strings.TrimPrefix(s, g))
		}
	}
	return s
}
