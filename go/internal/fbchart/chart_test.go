package fbchart

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

func testSignals() *signals.RegionSignals {
	return &signals.RegionSignals{
		AsOf:   "2026-10-23",
		Region: signals.RegionRef{ID: "south", TitleTL: "South Metro", TitleEN: "South Metro", TitleJA: "南部（ラス・ピニャス周辺）"},
		Deals: []signals.RegionDeal{
			{Name: "Chayote", MarketShort: "Las Piñas", Unit: "per kg", Price: 60, PctRounded: -33},
			{Name: "Egg (Medium)", MarketShort: "Alabang", Unit: "per piece", Price: 8.5, PctRounded: -15},
			{Name: "Tomato", MarketShort: "Bicutan", Unit: "per kg", Price: 90, PctRounded: -14},
		},
		Pricier: []signals.RegionPricier{
			{Name: "Red Chili (Local)", Unit: "per kg", PriceMedian: 450, PctRounded: 70},
			{Name: "White Potato (Local)", Unit: "per kg", PriceMedian: 200, PctRounded: 25},
		},
	}
}

// The face actually used for ₱ must have the glyph (no tofu): Montserrat
// carries U+20B1 and U+2212, fallback covers the rest per rune.
func TestPesoGlyphPresent(t *testing.T) {
	for _, r := range []rune{'₱', '−'} {
		if !montHas(Bold, r) {
			t.Errorf("Montserrat-Bold missing U+%04X, would fall back to Noto", r)
		}
		if f := faceFor(Bold, 32, r); f == nil {
			t.Errorf("no face for U+%04X", r)
		}
	}
	// A rune Montserrat lacks (e.g. あ) must resolve to the fallback.
	if montHas(Bold, 'あ') {
		t.Error("expected Montserrat-Bold to lack U+3042")
	}
}

func TestRenderSize(t *testing.T) {
	img, err := Render(testSignals())
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %dx%d, want 1080x1350", img.Bounds().Dx(), img.Bounds().Dy())
	}
	f, err := os.CreateTemp("", "chart-*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestRenderEmpty(t *testing.T) {
	sig := testSignals()
	sig.Deals = nil
	sig.Pricier = nil
	img, err := Render(sig)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %v", img.Bounds())
	}
}

func TestRenderLongTitleFits(t *testing.T) {
	sig := testSignals()
	sig.Region.TitleTL = "Maynila at Pasay"
	if _, err := Render(sig); err != nil {
		t.Fatal(err)
	}
	title := "Sulit ngayon sa Maynila at Pasay · Okt 23"
	if w := measure(title, ExtraBold, fitSize(title, ExtraBold, 64, 28, W-2*marginL)); w > W-2*marginL {
		t.Errorf("long title still overflows: %dpx", w)
	}
}
func TestRenderFull(t *testing.T) {
	sig := testSignals()
	sig.Deals = append(sig.Deals,
		signals.RegionDeal{Name: "Eggplant", MarketShort: "La Huerta", Unit: "per kg", Price: 80, PctRounded: -11},
		signals.RegionDeal{Name: "Ampalaya", MarketShort: "Suki Moonwalk", Unit: "per kg", Price: 70, PctRounded: -9},
	)
	sig.Pricier = append(sig.Pricier,
		signals.RegionPricier{Name: "Chili (Green)", Unit: "per kg", PriceMedian: 240, PctRounded: 20},
	)
	if _, err := Render(sig); err != nil {
		t.Fatal(err)
	}
}

// TestMarketLabelsHaveNoNestedParens covers all 5 regions: no short name
// may contain parentheses (else the chart's "(<short>)" label nests like
// "(Grace (Pateros))"), and every rendered chart row label must be free of
// nested parentheses.
func TestMarketLabelsHaveNoNestedParens(t *testing.T) {
	hasNested := func(s string) bool {
		depthLatin, depthJA := 0, 0
		for _, r := range s {
			switch r {
			case '(':
				depthLatin++
				if depthLatin > 1 {
					return true
				}
			case ')':
				if depthLatin > 0 {
					depthLatin--
				}
			case '（':
				depthJA++
				if depthJA > 1 {
					return true
				}
			case '）':
				if depthJA > 0 {
					depthJA--
				}
			}
		}
		return false
	}
	if len(regions.Regions) != 5 {
		t.Fatalf("got %d regions, want 5", len(regions.Regions))
	}
	for _, r := range regions.Regions {
		for _, m := range r.Markets {
			if strings.ContainsAny(m.Short, "()（）") {
				t.Errorf("region %s short %q contains parentheses", r.ID, m.Short)
			}
			if label := "(" + m.Short + ")"; hasNested(label) {
				t.Errorf("region %s PNG label %q is nested", r.ID, label)
			}
		}
		// Render one chart per region with that region's own shorts.
		sig := &signals.RegionSignals{
			AsOf:   "2026-10-23",
			Region: signals.RegionRef{ID: r.ID, TitleTL: r.TitleTL, TitleEN: r.TitleEN, TitleJA: r.TitleJA},
			Deals: []signals.RegionDeal{
				{Name: "Chayote", MarketShort: r.Markets[0].Short, Unit: "per kg", Price: 60, PctRounded: -33},
				{Name: "Egg (Medium)", MarketShort: r.Markets[1%len(r.Markets)].Short, Unit: "per piece", Price: 8.5, PctRounded: -15},
			},
			Pricier: []signals.RegionPricier{
				{Name: "Red Chili (Local)", Unit: "per kg", PriceMedian: 450, PctRounded: 70},
			},
		}
		if _, err := Render(sig); err != nil {
			t.Fatalf("region %s render: %v", r.ID, err)
		}
		// The row labels Render builds must not nest either.
		for _, dl := range sig.Deals {
			if label := "(" + dl.MarketShort + ")"; hasNested(label) {
				t.Errorf("region %s deal label %q is nested", r.ID, label)
			}
		}
	}
}

// labelInkBox draws the label at its placed origin on a scratch image
// and returns the tight ink bounding box (canvas coordinates).
func labelInkBox(t *testing.T, lbl string, size float64, lx, ly int) (x0, x1, y0, y1 int) {
	t.Helper()
	const m = 20
	w, h := measure(lbl, Bold, size)+2*m, 80
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	drawString(img, lbl, Bold, size, color.White, m, 40)
	x0, y0 = w, h
	x1, y1 = -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if img.RGBAAt(x, y).A > 0 {
				if x < x0 {
					x0 = x
				}
				if x > x1 {
					x1 = x
				}
				if y < y0 {
					y0 = y
				}
				if y > y1 {
					y1 = y
				}
			}
		}
	}
	if x1 < 0 {
		t.Fatalf("no ink for label %q", lbl)
	}
	return lx - m + x0, lx - m + x1, ly - 40 + y0, ly - 40 + y1
}

// TestLabelBounds checks every bar's label geometry across scale
// scenarios: an inside label's ink box must lie within the bar rect
// (with >= 10 px padding from the bar's outer end) and inside the canvas
// with >= 16 px right margin; an outside label must lie inside the
// canvas. Bar lengths never exceed their side's maximum (as in Render,
// where the maxima come from the data).
func TestLabelBounds(t *testing.T) {
	dealPcts := []int{8, 14, 21, 30, 40, 50, 72, 100}
	prPcts := []int{8, 14, 20, 25, 43, 50, 67, 72, 100}
	for _, maxNeg := range []int{0, 8, 25, 50, 72, 100} {
		for _, maxPos := range []int{0, 8, 25, 50, 72, 100} {
			if maxNeg == 0 && maxPos == 0 {
				continue
			}
			unit, _ := barScale(maxNeg, maxPos)
			// Mirror Render: shrink the scale so outside pricier
			// labels fit, then recompute the zero line.
			unit = fitScaleRight(maxNeg, maxPos, prItems(prPcts, maxPos), unit)
			zx := zeroX(maxNeg, maxPos, unit)
			for _, rowH := range []int{110, 80} {
				for _, pct := range dealPcts {
					if maxNeg == 0 || pct > maxNeg {
						continue
					}
					lbl := fmtPct(-pct)
					g := placeDealBar(400, rowH, zx, barLen(-pct, unit), lbl)
					checkBar(t, "deal", maxNeg, maxPos, -pct, rowH, lbl, g, false)
				}
				for _, pct := range prPcts {
					if maxPos == 0 || pct > maxPos {
						continue
					}
					lbl := fmtPct(pct)
					g := placePricierBar(400, rowH, zx, barLen(pct, unit), lbl)
					checkBar(t, "pricier", maxNeg, maxPos, pct, rowH, lbl, g, true)
				}
			}
		}
	}
	// Shrunk pricier rows take the clamp path too.
	unit, _ := barScale(50, 67)
	unit = fitScaleRight(50, 67, prItems(prPcts, 67), unit)
	zx := zeroX(50, 67, unit)
	for _, pct := range prPcts {
		if pct > 67 {
			continue
		}
		lbl := fmtPct(pct)
		g := placePricierBar(400, 64, zx, barLen(pct, unit), lbl)
		checkBar(t, "pricier", 50, 67, pct, 64, lbl, g, true)
	}
}

// prItems builds pricier row items capped at maxPos (as in Render, bar
// lengths never exceed their side's maximum).
func prItems(pcts []int, maxPos int) []rowItem {
	var out []rowItem
	for _, pct := range pcts {
		if pct > maxPos {
			continue
		}
		out = append(out, rowItem{pct: pct})
	}
	return out
}

func checkBar(t *testing.T, side string, maxNeg, maxPos, pct, rowH int, lbl string, g barPlace, right bool) {
	t.Helper()
	if g.bx1-g.bx0 <= 0 || g.by1-g.by0 != barH {
		t.Errorf("%s maxNeg=%d maxPos=%d pct=%d rowH=%d: bad bar rect %+v", side, maxNeg, maxPos, pct, rowH, g)
	}
	if g.bx0 < 0 || g.bx1 > W-16 {
		t.Errorf("%s maxNeg=%d maxPos=%d pct=%d rowH=%d: bar rect %+v escapes canvas (right margin 16)", side, maxNeg, maxPos, pct, rowH, g)
	}
	ix0, ix1, iy0, iy1 := labelInkBox(t, lbl, g.size, g.lx, g.ly)
	if g.inside {
		if right {
			if g.bx1-(g.lx+g.lw) < 10 {
				t.Errorf("pricier maxNeg=%d maxPos=%d pct=%d rowH=%d: inside label padding from outer end < 10: %+v", maxNeg, maxPos, pct, rowH, g)
			}
			if g.lx < g.bx0 {
				t.Errorf("pricier maxNeg=%d maxPos=%d pct=%d rowH=%d: inside label starts left of bar: %+v", maxNeg, maxPos, pct, rowH, g)
			}
		} else {
			if g.lx-g.bx0 < 10 {
				t.Errorf("deal maxNeg=%d maxPos=%d pct=%d rowH=%d: inside label padding from outer end < 10: %+v", maxNeg, maxPos, pct, rowH, g)
			}
			if g.lx+g.lw > g.bx1 {
				t.Errorf("deal maxNeg=%d maxPos=%d pct=%d rowH=%d: inside label overflows bar: %+v", maxNeg, maxPos, pct, rowH, g)
			}
		}
		if ix0 < g.bx0 || ix1 > g.bx1 || iy0 < g.by0 || iy1 > g.by1 {
			t.Errorf("%s maxNeg=%d maxPos=%d pct=%d rowH=%d: inside ink box [%d,%d]x[%d,%d] outside bar [%d,%d]x[%d,%d]",
				side, maxNeg, maxPos, pct, rowH, ix0, ix1, iy0, iy1, g.bx0, g.bx1, g.by0, g.by1)
		}
		if ix1 > W-16 {
			t.Errorf("%s maxNeg=%d maxPos=%d pct=%d rowH=%d: inside label within 16px of right edge", side, maxNeg, maxPos, pct, rowH)
		}
		return
	}
	// Outside: ink must stay inside the canvas.
	if ix0 < 0 || ix1 > W-16 {
		t.Errorf("%s maxNeg=%d maxPos=%d pct=%d rowH=%d: outside ink escapes canvas: [%d,%d]", side, maxNeg, maxPos, pct, rowH, ix0, ix1)
	}
	if iy0 < 0 || iy1 > H {
		t.Errorf("%s maxNeg=%d maxPos=%d pct=%d rowH=%d: outside ink outside canvas vertically", side, maxNeg, maxPos, pct, rowH)
	}
}
