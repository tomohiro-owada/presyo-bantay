package fbchart

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

func hp(hours ...int) []grabfare.HourPoint {
	var o []grabfare.HourPoint
	for _, h := range hours {
		o = append(o, grabfare.HourPoint{H: h, Min: float64(280 + h), Max: float64(340 + h)})
	}
	return o
}

func hourlyOut(today, yesterday []grabfare.HourPoint) *grabfare.Output {
	o := grabOutput(nil, false, "up")
	o.Hourly = &grabfare.HourlyOut{TodayDate: "2026-11-02", YesterdayDate: "2026-11-01", Today: today, Yesterday: yesterday}
	return o
}

func TestSelectChartSeriesHideRule(t *testing.T) {
	if _, y, ok := selectChartSeries(hp(0, 1, 2), hp(21, 22, 23)); !ok || y != nil {
		t.Errorf("short yesterday must be hidden: ok=%v y=%v", ok, y)
	}
	if _, y, ok := selectChartSeries(hp(0, 1), hp(0, 1, 2, 3, 4, 5)); !ok || len(y) != 6 {
		t.Errorf("6 points keep yesterday: %v", y)
	}
	if _, _, ok := selectChartSeries(nil, hp(21, 22)); ok {
		t.Error("empty today + short yesterday must give no chart")
	}
	if tt, y, ok := selectChartSeries(nil, hp(0, 1, 2, 3, 4, 5, 6)); !ok || len(tt) != 0 || len(y) != 7 {
		t.Error("full yesterday alone is drawable")
	}
	if tt, _, _ := selectChartSeries(hp(5, 2, 3), nil); tt[0].H != 2 || tt[2].H != 5 {
		t.Errorf("not sorted: %v", tt)
	}
	if grabChartAvailable(grabOutput(nil, false, "up")) {
		t.Error("nil Hourly must be unavailable")
	}
}

func TestSplitRunsBreaksAtGaps(t *testing.T) {
	var got [][]int
	for _, r := range splitRuns(hp(0, 1, 2, 4, 5, 8)) {
		var hs []int
		for _, p := range r {
			hs = append(hs, p.H)
		}
		got = append(got, hs)
	}
	if want := [][]int{{0, 1, 2}, {4, 5}, {8}}; !reflect.DeepEqual(got, want) {
		t.Errorf("runs %v", got)
	}
}

func TestChartYRange(t *testing.T) {
	lo, hi, step := chartYRange(hp(0, 5), nil)
	if (hi-lo)/step > chartMaxSteps+0.001 || (hi-lo)/step < 2 {
		t.Errorf("bad axis %v %v %v", lo, hi, step)
	}
	if lo > 280 || hi < 345 {
		t.Errorf("axis must cover the data: %v %v", lo, hi)
	}
	flat := []grabfare.HourPoint{{H: 0, Min: 300, Max: 300}, {H: 1, Min: 300, Max: 300}}
	lo, hi, step = chartYRange(flat, nil)
	if hi-lo < 2*step || lo > 300 || hi < 300 {
		t.Errorf("flat axis %v %v %v", lo, hi, step)
	}
	wide := []grabfare.HourPoint{{H: 0, Min: 200, Max: 700}}
	lo, hi, step = chartYRange(wide, nil)
	if (hi-lo)/step > chartMaxSteps+0.001 {
		t.Errorf("too many grid lines %v %v %v", lo, hi, step)
	}
}

func diffRect(a, b *image.RGBA, r image.Rectangle) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return true
			}
		}
	}
	return false
}

func TestCardWithChartSameDimensionsAndOutsidePanelUnchanged(t *testing.T) {
	blue := color.RGBA{0x12, 0x9A, 0xEE, 0xFF}
	mag := color.RGBA{0xFF, 0x00, 0xFF, 0xFF}
	thumb := mapTile(blue)
	cases := []struct {
		name  string
		maps  []image.Image
		focus image.Image
	}{
		{"focus+3 maps", []image.Image{thumb, thumb, thumb}, solid(720, 280, mag)},
		{"focus only", nil, solid(720, 280, mag)},
		{"3 maps", []image.Image{thumb, thumb, thumb}, nil},
		{"fare only", nil, nil},
	}
	series := []struct {
		name string
		o    *grabfare.Output
	}{
		{"two lines", hourlyOut(hp(0, 1, 2, 3, 4, 5, 6, 7), hp(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 13, 14))},
		{"gap", hourlyOut(hp(0, 1, 2, 5, 6, 9), nil)},
		{"today only", hourlyOut(hp(0, 1, 2), hp(22, 23))},
		{"single point", hourlyOut(hp(0), nil)},
	}
	for _, c := range cases {
		plain, err := RenderGrabFareFocus(grabOutput(nil, false, "up"), c.maps, c.focus, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range series {
			img, err := RenderGrabFareFocus(s.o, c.maps, c.focus, false)
			if err != nil {
				t.Fatalf("%s/%s: %v", c.name, s.name, err)
			}
			if img.Bounds() != plain.Bounds() || img.Bounds().Dx() != 1080 {
				t.Fatalf("%s/%s: size %v vs %v", c.name, s.name, img.Bounds(), plain.Bounds())
			}
			// everything below the panel (y >= 700) is identical to the card without chart
			belowY := 700
			if c.name == "fare only" {
				belowY = 860 // the lone panel sits lower on a fare-only card
			}
			below := image.Rect(0, belowY, 1080, plain.Bounds().Dy())
			if diffRect(plain, img, below) {
				t.Errorf("%s/%s: pixels below the panel changed", c.name, s.name)
			}
			// the right half of the panel has drawn pixels
			if !diffRect(plain, img, image.Rect(560, 270, 1000, 860)) {
				t.Errorf("%s/%s: chart drew nothing in the right half", c.name, s.name)
			}
		}
	}
}

func TestChartAbsentIsIdenticalToNoHourly(t *testing.T) {
	a, _ := RenderGrabFare(grabOutput(nil, false, "up"), nil, false)
	o := hourlyOut(nil, hp(21, 22)) // nothing to draw
	b, _ := RenderGrabFare(o, nil, false)
	if diffRect(a, b, a.Bounds()) {
		t.Error("no-chart hourly must render exactly like no hourly")
	}
}

func TestLeftHalfTextsStayLeftOfChart(t *testing.T) {
	o := hourlyOut(hp(0, 1, 2, 3), nil)
	o.Fare.Min, o.Fare.Max = 1288, 1346
	std := 1290.0
	o.Fare.Standard = &std
	img, err := RenderGrabFare(o, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// divider at the panel middle; left of it only fare text (dark green/muted), right only chart.
	// Ensure the big number's green pixels never cross into the right half's first 10 px.
	mid := 540
	for y := 270; y < 560; y++ {
		for x := mid - 6; x < mid+1; x++ {
			c := img.RGBAAt(x, y)
			if c == green {
				t.Fatalf("text crosses divider at %d,%d", x, y)
			}
		}
	}
}

func TestBottomYLabelDoesNotCollideWithXLabels(t *testing.T) {
	o := hourlyOut(hp(0, 1, 2, 3, 4, 5, 6, 7), nil)
	today, yest, _ := grabChartSeries(o)
	lo, hi, step := chartYRange(today, yest)
	zone := image.Rect(560, 290, 1000, 575)
	g := chartGeom(zone, lo, hi, step)
	dst := image.NewRGBA(image.Rect(0, 0, W, 700))
	fillRect(dst, 0, 0, W, 700, white)
	drawGrabChart(dst, zone, o)

	nonWhite := func(r image.Rectangle) int {
		n := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if dst.RGBAAt(x, y) != white {
					n++
				}
			}
		}
		return n
	}
	// y gutter around and below the bottom gridline (left of the "00" label): nothing drawn
	botY := int(g.y(lo))
	gutter := image.Rect(zone.Min.X, botY-int(g.tickSize), g.left-24, g.bottom+int(g.tickSize*1.4))
	if n := nonWhite(gutter); n != 0 {
		t.Errorf("bottom y label (or something else) in the gutter overlaps the x labels: %d px", n)
	}
	// the next gridline is still labelled
	above := int(g.y(lo + step))
	if n := nonWhite(image.Rect(zone.Min.X, above-int(g.tickSize*0.6), g.left-8, above+int(g.tickSize*0.6))); n == 0 {
		t.Error("second y label missing")
	}
	// the "00" x label is drawn
	if n := nonWhite(image.Rect(g.left-14, g.bottom+6, g.left+14, g.bottom+int(g.tickSize*1.4))); n == 0 {
		t.Error("00 x label missing")
	}
}

func TestSplitRunsBreaksOnEraChange(t *testing.T) {
	pts := []grabfare.HourPoint{
		{H: 11, Min: 280, Max: 340, Era: 1},
		{H: 12, Min: 280, Max: 340, Era: 0},
		{H: 13, Min: 280, Max: 340, Era: 0},
	}
	var got [][]int
	for _, r := range splitRuns(pts) {
		var hs []int
		for _, p := range r {
			hs = append(hs, p.H)
		}
		got = append(got, hs)
	}
	if want := [][]int{{11}, {12, 13}}; !reflect.DeepEqual(got, want) {
		t.Errorf("runs %v", got)
	}
	plain := []grabfare.HourPoint{
		{H: 11, Min: 280, Max: 340},
		{H: 12, Min: 280, Max: 340},
		{H: 13, Min: 280, Max: 340},
	}
	var got2 [][]int
	for _, r := range splitRuns(plain) {
		var hs []int
		for _, p := range r {
			hs = append(hs, p.H)
		}
		got2 = append(got2, hs)
	}
	if want := [][]int{{11, 12, 13}}; !reflect.DeepEqual(got2, want) {
		t.Errorf("runs %v", got2)
	}
}

func TestGrabChartEraBoundaryMarker(t *testing.T) {
	today := []grabfare.HourPoint{
		{H: 10, Min: 100, Max: 120, Era: 1},
		{H: 11, Min: 110, Max: 130, Era: 1},
		{H: 12, Min: 200, Max: 220, Era: 0},
		{H: 13, Min: 210, Max: 230, Era: 0},
	}
	o := hourlyOut(today, nil)
	zone := image.Rect(560, 290, 1000, 575)
	lo, hi, step := chartYRange(today, nil)
	g := chartGeom(zone, lo, hi, step)
	dst := image.NewRGBA(image.Rect(0, 0, W, 700))
	for y := 0; y < 700; y++ {
		for x := 0; x < W; x++ {
			dst.Set(x, y, white)
		}
	}
	drawGrabChart(dst, zone, o)

	xmid := int(math.Round((g.x(11) + g.x(12)) / 2))
	mutCount := 0
	for y := g.top; y < g.bottom; y++ {
		for dx := -2; dx <= 2; dx++ {
			if dst.RGBAAt(xmid+dx, y) == mutCol {
				mutCount++
			}
		}
	}
	if mutCount < (g.bottom-g.top)/4 {
		t.Errorf("mutCol marker pixels too few: %d", mutCount)
	}
	consecutive := 0
	maxConsec := 0
	for y := g.top; y < g.bottom; y++ {
		if dst.RGBAAt(xmid, y) == mutCol {
			consecutive++
			if consecutive > maxConsec {
				maxConsec = consecutive
			}
		} else {
			consecutive = 0
		}
	}
	if maxConsec > 8 {
		t.Errorf("marker not dashed: max consecutive %d", maxConsec)
	}
	greenCount := 0
	for y := g.top + 12; y < g.bottom; y++ {
		for dx := -2; dx <= 2; dx++ {
			if dst.RGBAAt(xmid+dx, y) == green {
				greenCount++
			}
		}
	}
	if greenCount != 0 {
		t.Errorf("strong green in boundary band: %d", greenCount)
	}
}

func TestGrabChartAllEraZeroIdenticalNoMarker(t *testing.T) {
	mk := func(set bool) []grabfare.HourPoint {
		pts := []grabfare.HourPoint{
			{H: 10, Min: 100, Max: 120},
			{H: 11, Min: 110, Max: 130},
			{H: 12, Min: 200, Max: 220},
			{H: 13, Min: 210, Max: 230},
		}
		if set {
			for i := range pts {
				pts[i].Era = 0
			}
		}
		return pts
	}
	o1 := hourlyOut(mk(false), nil)
	o2 := hourlyOut(mk(true), nil)
	zone := image.Rect(560, 290, 1000, 575)
	render := func(o *grabfare.Output) *image.RGBA {
		dst := image.NewRGBA(image.Rect(0, 0, W, 700))
		for y := 0; y < 700; y++ {
			for x := 0; x < W; x++ {
				dst.Set(x, y, white)
			}
		}
		drawGrabChart(dst, zone, o)
		return dst
	}
	a := render(o1)
	b := render(o2)
	if a.Bounds() != b.Bounds() || !reflect.DeepEqual(a.Pix, b.Pix) {
		t.Error("all-era-0 renders are not byte-identical")
	}
	today := o1.Hourly.Today
	lo, hi, step := chartYRange(today, nil)
	g := chartGeom(zone, lo, hi, step)
	for i := 0; i+1 < len(today); i++ {
		x0 := int(math.Round(g.x(today[i].H)))
		x1 := int(math.Round(g.x(today[i+1].H)))
		for x := x0 + 1; x < x1; x++ {
			n := 0
			for y := g.top; y < g.bottom; y++ {
				if a.RGBAAt(x, y) == mutCol {
					n++
				}
			}
			if n > (g.bottom-g.top)/6 {
				t.Errorf("mutCol dashed column at x=%d: %d", x, n)
			}
		}
	}
}

func TestGrabChartEraDotLighter(t *testing.T) {
	today0 := []grabfare.HourPoint{{H: 12, Min: 280, Max: 340, Era: 0}}
	zone := image.Rect(560, 290, 1000, 575)
	lo, hi, step := chartYRange(today0, nil)
	g := chartGeom(zone, lo, hi, step)
	render := func(era int) *image.RGBA {
		today := []grabfare.HourPoint{{H: 12, Min: 280, Max: 340, Era: era}}
		o := hourlyOut(today, nil)
		dst := image.NewRGBA(image.Rect(0, 0, W, 700))
		for y := 0; y < 700; y++ {
			for x := 0; x < W; x++ {
				dst.Set(x, y, white)
			}
		}
		drawGrabChart(dst, zone, o)
		return dst
	}
	dst0 := render(0)
	dst1 := render(1)
	midv := 310.0
	px := int(math.Round(g.x(12)))
	py := int(math.Round(g.y(midv)))
	greenFound := false
	for dy := -8; dy <= 8; dy++ {
		for dx := -8; dx <= 8; dx++ {
			if dst0.RGBAAt(px+dx, py+dy) == green {
				greenFound = true
			}
		}
	}
	if !greenFound {
		t.Error("era0 ring has no exact green pixel")
	}
	lighterFound := false
	for dy := -8; dy <= 8; dy++ {
		for dx := -8; dx <= 8; dx++ {
			c := dst1.RGBAAt(px+dx, py+dy)
			if c == green {
				t.Errorf("era1 ring contains strong green at %d,%d", px+dx, py+dy)
			}
			if c != white && c != green {
				lighterFound = true
			}
		}
	}
	if !lighterFound {
		t.Error("era1 ring has no lighter pixel")
	}
}
