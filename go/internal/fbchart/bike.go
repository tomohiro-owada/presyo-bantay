package fbchart

import (
	"fmt"
	"image"
	"math"

	"golang.org/x/image/draw"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

// bikeBlockH is the height of the compact Move It motor-taxi block.
const bikeBlockH = 270

func bikeChartAvailable(hourly *grabfare.HourlyOut) bool {
	if hourly == nil {
		return false
	}
	_, _, ok := selectChartSeries(hourly.Today, hourly.Yesterday)
	return ok
}

func drawBikeBlock(dst *image.RGBA, y, h int, o *grabfare.Output) {
	if o == nil || o.Bike == nil {
		return
	}
	billCard(dst, y, h)
	bike := o.Bike
	padIn := 24
	gapIn := 10
	hourly := bike.Hourly
	chartOK := bikeChartAvailable(hourly)

	px0, px1 := marginL, W-marginR
	if !chartOK {
		maxW := px1 - px0 - 2*padIn
		centreX := (px0 + px1) / 2
		drawBikeLeft(dst, y, h, bike, maxW, centreX, padIn, gapIn)
		return
	}
	midX := (px0 + px1) / 2
	leftMaxW := midX - px0 - 2*padIn - 8
	centreX := (px0 + midX) / 2
	fillRect(dst, midX, y+padIn, midX+1, y+h-padIn, sepCol)
	zone := image.Rect(midX+padIn, y+padIn, px1-padIn, y+h-padIn)
	drawBikeChart(dst, zone, hourly)
	drawBikeLeft(dst, y, h, bike, leftMaxW, centreX, padIn, gapIn)
}

func drawBikeLeft(dst *image.RGBA, y, h int, bike *grabfare.BikeOut, maxW, centreX, padIn, gapIn int) {
	labelText := bike.Label
	if labelText == "" {
		labelText = "Motor taxi (Move It)"
	}
	labelCol := green
	labelS := cfitSize(labelText, Bold, 28, 20, maxW)

	priceText := "—"
	priceCol := mutCol
	if bike.Min != nil && bike.Max != nil {
		priceText = grabfare.FormatRange(*bike.Min, *bike.Max)
		priceCol = green
	}
	priceS := cfitSize(priceText, ExtraBold, 84, 40, maxW)

	var asofStr string
	if bike.Text.Tl.Asof != "" {
		asofStr = bike.Text.Tl.Asof
	} else if bike.AsOfHHMM != nil {
		asofStr = "Huling Move It quote: " + *bike.AsOfHHMM + " (oras sa Maynila)"
	}
	asofLines, asofS := wrapLines(asofStr, Medium, 24, 18, maxW, 2)

	gap := gapIn
	if gap < 8 {
		gap = 8
	}
	contentH := int(labelS*1.3+0.5) + gap + int(priceS*1.3+0.5) + gap + len(asofLines)*int(asofS*1.3+0.5)
	cy := y + (h-contentH)/2
	if cy < y+padIn {
		cy = y + padIn
	}
	tw := cmeasure(labelText, Bold, labelS)
	cdraw(dst, labelText, Bold, labelS, labelCol, centreX-tw/2, cy+int(labelS*0.85+0.5))
	cy += int(labelS*1.3+0.5) + gap
	tw = cmeasure(priceText, ExtraBold, priceS)
	cdraw(dst, priceText, ExtraBold, priceS, priceCol, centreX-tw/2, cy+int(priceS*0.85+0.5))
	cy += int(priceS*1.3+0.5) + gap
	for _, ln := range asofLines {
		lw := cmeasure(ln, Medium, asofS)
		cdraw(dst, ln, Medium, asofS, mutCol, centreX-lw/2, cy+int(asofS*0.85+0.5))
		cy += int(asofS*1.3 + 0.5)
	}
}

func drawBikeChart(dst *image.RGBA, zone image.Rectangle, h *grabfare.HourlyOut) {
	if h == nil {
		return
	}
	today, yesterday, ok := selectChartSeries(h.Today, h.Yesterday)
	if !ok {
		return
	}
	lo, hi, step := chartYRange(today, yesterday)
	g := chartGeom(zone, lo, hi, step)
	g.titleText = "Motor kada oras"
	g.titleSize = cfitSize(g.titleText, Bold, 28, 20, zone.Dx())
	g.titleBase = zone.Min.Y + int(g.titleSize*0.85+0.5)
	g.legendBase = zone.Min.Y + int(g.titleSize*1.3+0.5) + 4 + int(g.legendSize*0.85+0.5)
	g.top = zone.Min.Y + int(g.titleSize*1.3+0.5) + 4 + int(g.legendSize*1.3+0.5) + 14
	if g.right-g.left < 80 || g.bottom-g.top < 60 {
		return
	}

	// grid, y labels, x ticks
	for v := lo; v <= hi+0.001; v += step {
		yy := int(math.Round(g.y(v)))
		fillRect(dst, g.left, yy, g.right, yy+1, sepCol)
		if v < lo+step/2 {
			continue
		}
		lbl := yTickLabel(v)
		cdraw(dst, lbl, Medium, g.tickSize, mutCol, g.left-8-cmeasure(lbl, Medium, g.tickSize), yy+int(g.tickSize*0.35+0.5))
	}
	for hh := 0; hh <= 21; hh += 3 {
		xx := int(math.Round(g.x(hh)))
		fillRect(dst, xx, g.bottom, xx+1, g.bottom+5, mutCol)
		if hh%6 != 0 {
			continue
		}
		lbl := fmt.Sprintf("%02d", hh)
		cdraw(dst, lbl, Medium, g.tickSize, mutCol, xx-cmeasure(lbl, Medium, g.tickSize)/2, g.tickBase)
	}

	// supersampled lines
	ss := float64(chartSS)
	sc := image.NewRGBA(image.Rect(0, 0, zone.Dx()*chartSS, zone.Dy()*chartSS))
	sx := func(hh int) float64 { return (g.x(hh) - float64(zone.Min.X)) * ss }
	sy := func(v float64) float64 { return (g.y(v) - float64(zone.Min.Y)) * ss }
	mid := func(p grabfare.HourPoint) float64 { return (p.Min + p.Max) / 2 }

	if yesterday != nil {
		yc := premul(grey, 0xA0)
		for _, run := range splitRuns(yesterday) {
			if len(run) == 1 {
				fillDisc(sc, sx(run[0].H), sy(mid(run[0])), 2.5*ss, yc)
				continue
			}
			for i := 0; i+1 < len(run); i++ {
				fillCapsule(sc, sx(run[i].H), sy(mid(run[i])), sx(run[i+1].H), sy(mid(run[i+1])), 1.6*ss, yc)
			}
		}
	}
	for _, run := range splitRuns(today) {
		lineCol := green
		for i := 0; i+1 < len(run); i++ {
			fillCapsule(sc, sx(run[i].H), sy(mid(run[i])), sx(run[i+1].H), sy(mid(run[i+1])), 3.2*ss, lineCol)
		}
	}
	for _, p := range today {
		if p.Carried {
			continue
		}
		dotCol := green
		fillDisc(sc, sx(p.H), sy(mid(p)), 5*ss, dotCol)
		fillDisc(sc, sx(p.H), sy(mid(p)), 2.4*ss, white)
	}
	draw.CatmullRom.Scale(dst, zone, sc, sc.Bounds(), draw.Over, nil)

	// title + legend + latest value
	tw := cmeasure(g.titleText, Bold, g.titleSize)
	cdraw(dst, g.titleText, Bold, g.titleSize, green, zone.Min.X+(zone.Dx()-tw)/2, g.titleBase)
	lx := zone.Min.X + 4
	fillRounded(dst, lx, g.legendBase-9, lx+30, g.legendBase-5, 2, green)
	cdraw(dst, "Ngayon", Bold, g.legendSize, green, lx+38, g.legendBase)
	if yesterday != nil {
		x2 := lx + 38 + cmeasure("Ngayon", Bold, g.legendSize) + 22
		fillRect(dst, x2, g.legendBase-8, x2+30, g.legendBase-6, grey)
		cdraw(dst, "Kahapon", Medium, g.legendSize, mutCol, x2+38, g.legendBase)
	}
	if len(today) > 0 {
		last := today[len(today)-1]
		lbl := wholeU(mid(last))
		ls := 26.0
		w := cmeasure(lbl, Bold, ls)
		x := int(g.x(last.H)) - w/2
		if x+w > g.right {
			x = g.right - w
		}
		if x < g.left {
			x = g.left
		}
		top := last.Max
		if n := len(today); n > 1 && today[n-2].Max > top {
			top = today[n-2].Max
		}
		yv := int(g.y(top)) - 12
		if yv < g.top+int(ls*0.85) {
			yv = g.top + int(ls*0.85)
		}
		cdraw(dst, lbl, Bold, ls, green, x, yv)
	}
}
