package fbchart

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

// MinYesterdayPoints is the minimum number of yesterday readings needed to
// draw the yesterday line (same rule as the site chart, site/src/grabchart.ts).
const MinYesterdayPoints = 6

// chartSS is the supersampling factor of the chart scratch image.
const chartSS = 3

// selectChartSeries applies the hide rule: nothing to draw when today is empty
// and yesterday is too short; yesterday is dropped when it has fewer than
// MinYesterdayPoints points. Both outputs are sorted by hour.
func selectChartSeries(todayIn, yesterdayIn []grabfare.HourPoint) (today, yesterday []grabfare.HourPoint, ok bool) {
	if len(todayIn) == 0 && len(yesterdayIn) < MinYesterdayPoints {
		return nil, nil, false
	}
	today = append([]grabfare.HourPoint(nil), todayIn...)
	sort.Slice(today, func(i, j int) bool { return today[i].H < today[j].H })
	if len(yesterdayIn) >= MinYesterdayPoints {
		yesterday = append([]grabfare.HourPoint(nil), yesterdayIn...)
		sort.Slice(yesterday, func(i, j int) bool { return yesterday[i].H < yesterday[j].H })
	}
	return today, yesterday, true
}

// grabChartSeries returns the series to draw for o (ok=false: no chart).
func grabChartSeries(o *grabfare.Output) (today, yesterday []grabfare.HourPoint, ok bool) {
	if o == nil || o.Hourly == nil {
		return nil, nil, false
	}
	return selectChartSeries(o.Hourly.Today, o.Hourly.Yesterday)
}

// grabChartAvailable reports whether the card gets the half-width chart.
func grabChartAvailable(o *grabfare.Output) bool {
	_, _, ok := grabChartSeries(o)
	return ok
}

// splitRuns splits points into runs of consecutive hours; a missing hour
// breaks the line (no interpolation).
func splitRuns(pts []grabfare.HourPoint) [][]grabfare.HourPoint {
	var runs [][]grabfare.HourPoint
	var cur []grabfare.HourPoint
	for _, p := range pts {
		if len(cur) > 0 && p.H != cur[len(cur)-1].H+1 {
			runs = append(runs, cur)
			cur = nil
		}
		cur = append(cur, p)
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs
}

// chartMaxSteps is the most grid intervals the card chart shows (the plot is
// short, so few, round gridlines keep it readable at phone size).
const chartMaxSteps = 4

// chartYRange returns the y-axis bounds and grid step covering both series:
// padded a little, rounded to a round step (10/20/25/50/100/200) such that at
// most chartMaxSteps intervals are drawn (at least 2).
func chartYRange(today, yesterday []grabfare.HourPoint) (lo, hi, step float64) {
	all := append(append([]grabfare.HourPoint(nil), today...), yesterday...)
	if len(all) == 0 {
		return 0, 20, 10
	}
	mn, mx := all[0].Min, all[0].Max
	for _, p := range all {
		mn = math.Min(mn, p.Min)
		mx = math.Max(mx, p.Max)
	}
	for _, st := range []float64{10, 20, 25, 50, 100, 200, 500, 1000} {
		step = st
		lo = math.Floor((mn-st*0.15)/st) * st
		hi = math.Ceil((mx+st*0.15)/st) * st
		if hi-lo < st*2 {
			hi = lo + st*2
		}
		if (hi-lo)/st <= chartMaxSteps+0.001 {
			break
		}
	}
	return lo, hi, step
}

// yTickLabel is the y-axis label: whole pesos without the symbol (the last
// value label carries the peso sign).
func yTickLabel(v float64) string { return strings.TrimPrefix(wholeU(v), "₱") }

// premul builds a premultiplied colour with the given alpha (0-255).
func premul(c color.RGBA, a uint8) color.RGBA {
	return color.RGBA{
		R: uint8(int(c.R) * int(a) / 255),
		G: uint8(int(c.G) * int(a) / 255),
		B: uint8(int(c.B) * int(a) / 255),
		A: a,
	}
}

// overPx composites premultiplied src over the scratch pixel.
func overPx(img *image.RGBA, x, y int, src color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(img.Rect) {
		return
	}
	i := img.PixOffset(x, y)
	inv := 255 - int(src.A)
	img.Pix[i+0] = uint8(int(src.R) + int(img.Pix[i+0])*inv/255)
	img.Pix[i+1] = uint8(int(src.G) + int(img.Pix[i+1])*inv/255)
	img.Pix[i+2] = uint8(int(src.B) + int(img.Pix[i+2])*inv/255)
	img.Pix[i+3] = uint8(int(src.A) + int(img.Pix[i+3])*inv/255)
}

// fillCapsule draws a round-capped segment of radius r into the scratch.
// Opaque colours are written, translucent ones are composited.
func fillCapsule(img *image.RGBA, x0, y0, x1, y1, r float64, col color.RGBA) {
	dx, dy := x1-x0, y1-y0
	den := dx*dx + dy*dy
	minX := int(math.Floor(math.Min(x0, x1) - r - 1))
	maxX := int(math.Ceil(math.Max(x0, x1) + r + 1))
	minY := int(math.Floor(math.Min(y0, y1) - r - 1))
	maxY := int(math.Ceil(math.Max(y0, y1) + r + 1))
	for yy := minY; yy <= maxY; yy++ {
		for xx := minX; xx <= maxX; xx++ {
			px, py := float64(xx)+0.5, float64(yy)+0.5
			t := 0.0
			if den > 0 {
				t = math.Max(0, math.Min(1, ((px-x0)*dx+(py-y0)*dy)/den))
			}
			if math.Hypot(px-(x0+t*dx), py-(y0+t*dy)) <= r {
				setPx(img, xx, yy, col)
			}
		}
	}
}

func fillDisc(img *image.RGBA, cx, cy, r float64, col color.RGBA) {
	fillCapsule(img, cx, cy, cx, cy, r, col)
}

// setPx writes (opaque) or composites (translucent) one scratch pixel; a
// line is drawn as one shape so overlapping capsules of one line must not
// double-darken: callers draw translucent lines with the max-alpha rule.
func setPx(img *image.RGBA, x, y int, col color.RGBA) {
	if !(image.Point{X: x, Y: y}).In(img.Rect) {
		return
	}
	i := img.PixOffset(x, y)
	if col.A == 255 || int(col.A) > int(img.Pix[i+3]) {
		img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = col.R, col.G, col.B, col.A
	}
}

func fillPoly(sc *image.RGBA, pts [][2]float64, col color.RGBA) {
	if len(pts) < 3 {
		return
	}
	r := vector.NewRasterizer(sc.Bounds().Dx(), sc.Bounds().Dy())
	r.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
	for _, p := range pts[1:] {
		r.LineTo(float32(p[0]), float32(p[1]))
	}
	r.ClosePath()
	r.Draw(sc, sc.Bounds(), image.NewUniform(col), image.Point{})
}

// grabChartGeom is the pixel layout of the chart inside its zone.
type grabChartGeom struct {
	zone                            image.Rectangle // whole right half content area
	left, right, top, bottom        int             // plot area
	lo, hi                          float64
	titleSize, legendSize, tickSize float64
	titleBase, legendBase, tickBase int
	titleText                       string
}

func (g grabChartGeom) x(h int) float64 {
	return float64(g.left) + float64(h)/23*float64(g.right-g.left)
}

func (g grabChartGeom) y(v float64) float64 {
	return float64(g.top) + (1-(v-g.lo)/(g.hi-g.lo))*float64(g.bottom-g.top)
}

func chartGeom(zone image.Rectangle, lo, hi float64, step float64) grabChartGeom {
	g := grabChartGeom{zone: zone, lo: lo, hi: hi, titleText: "Pamasahe kada oras"}
	g.titleSize = cfitSize(g.titleText, Bold, 28, 20, zone.Dx())
	g.legendSize, g.tickSize = 24, 24
	g.titleBase = zone.Min.Y + int(g.titleSize*0.85+0.5)
	g.legendBase = zone.Min.Y + int(g.titleSize*1.3+0.5) + 4 + int(g.legendSize*0.85+0.5)
	// widest y label decides the left gutter
	gutter := 0
	for v := lo; v <= hi+0.001; v += step {
		if w := cmeasure(yTickLabel(v), Medium, g.tickSize); w > gutter {
			gutter = w
		}
	}
	g.left = zone.Min.X + gutter + 10
	g.right = zone.Max.X - 8
	g.top = zone.Min.Y + int(g.titleSize*1.3+0.5) + 4 + int(g.legendSize*1.3+0.5) + 14
	g.bottom = zone.Max.Y - int(g.tickSize*1.3+0.5) - 4
	g.tickBase = g.bottom + 6 + int(g.tickSize*0.85+0.5)
	return g
}

// drawGrabChart draws the hour-of-day chart into zone (inside a white panel).
func drawGrabChart(dst *image.RGBA, zone image.Rectangle, o *grabfare.Output) {
	today, yesterday, ok := grabChartSeries(o)
	if !ok {
		return
	}
	lo, hi, step := chartYRange(today, yesterday)
	g := chartGeom(zone, lo, hi, step)
	if g.right-g.left < 80 || g.bottom-g.top < 60 {
		return
	}

	// grid, y labels, x ticks (crisp, final resolution)
	for v := lo; v <= hi+0.001; v += step {
		yy := int(math.Round(g.y(v)))
		fillRect(dst, g.left, yy, g.right, yy+1, sepCol)
		lbl := yTickLabel(v)
		cdraw(dst, lbl, Medium, g.tickSize, mutCol, g.left-8-cmeasure(lbl, Medium, g.tickSize), yy+int(g.tickSize*0.35+0.5))
	}
	for h := 0; h <= 21; h += 3 {
		xx := int(math.Round(g.x(h)))
		fillRect(dst, xx, g.bottom, xx+1, g.bottom+5, mutCol)
		if h%6 != 0 {
			continue // minor tick, no label (keeps labels apart on the narrow plot)
		}
		lbl := fmt.Sprintf("%02d", h)
		cdraw(dst, lbl, Medium, g.tickSize, mutCol, xx-cmeasure(lbl, Medium, g.tickSize)/2, g.tickBase)
	}

	// supersampled lines
	ss := float64(chartSS)
	sc := image.NewRGBA(image.Rect(0, 0, zone.Dx()*chartSS, zone.Dy()*chartSS))
	sx := func(h int) float64 { return (g.x(h) - float64(zone.Min.X)) * ss }
	sy := func(v float64) float64 { return (g.y(v) - float64(zone.Min.Y)) * ss }
	mid := func(p grabfare.HourPoint) float64 { return (p.Min + p.Max) / 2 }

	for _, run := range splitRuns(today) {
		if len(run) < 2 {
			continue
		}
		var poly [][2]float64
		for _, p := range run {
			poly = append(poly, [2]float64{sx(p.H), sy(p.Max)})
		}
		for i := len(run) - 1; i >= 0; i-- {
			poly = append(poly, [2]float64{sx(run[i].H), sy(run[i].Min)})
		}
		fillPoly(sc, poly, premul(green, 0x30))
	}
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
		for i := 0; i+1 < len(run); i++ {
			fillCapsule(sc, sx(run[i].H), sy(mid(run[i])), sx(run[i+1].H), sy(mid(run[i+1])), 3.2*ss, green)
		}
	}
	for _, p := range today {
		fillDisc(sc, sx(p.H), sy(mid(p)), 5*ss, green)
		fillDisc(sc, sx(p.H), sy(mid(p)), 2.4*ss, white)
	}
	xdraw.CatmullRom.Scale(dst, zone, sc, sc.Bounds(), draw.Over, nil)

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
		y := int(g.y(last.Max)) - 12
		if y < g.top+int(ls*0.85) {
			y = g.top + int(ls*0.85)
		}
		cdraw(dst, lbl, Bold, ls, green, x, y)
	}
}
