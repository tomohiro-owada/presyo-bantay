package fbchart

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

func utilFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "utilities", "testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture %s missing: %v", name, err)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func utilSnapshot(t *testing.T) *utilities.Snapshot {
	t.Helper()
	adjLines, err := utilities.PDFLines(utilFixture(t, "oilmon_20oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	adj, err := utilities.ParseOilMonitor(adjLines)
	if err != nil {
		t.Fatal(err)
	}
	pumpLines, err := utilities.PDFLines(utilFixture(t, "ncr_pump_1013.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	pump, err := utilities.ParseNCRPumpSummary(pumpLines)
	if err != nil {
		t.Fatal(err)
	}
	lpgLines, err := utilities.PDFLines(utilFixture(t, "ncr_lpg_oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	lpg, err := utilities.ParseNCRLPG(lpgLines)
	if err != nil {
		t.Fatal(err)
	}
	merHTML, err := os.ReadFile(utilFixture(t, "meralco_2026_10.html"))
	if err != nil {
		t.Fatal(err)
	}
	mer, err := utilities.ParseMeralcoRelease(merHTML)
	if err != nil {
		t.Fatal(err)
	}
	return &utilities.Snapshot{
		Fuel: utilities.FuelStatus{
			Adjustments: []utilities.OilAdjustment{adj},
			NCRPump:     []utilities.PumpSummary{pump},
		},
		LPG:     []utilities.LPGMonth{lpg},
		Meralco: []utilities.MeralcoRate{mer},
		Water: &utilities.WaterStatus{
			City:      "Las Piñas",
			CheckedAt: "2026-10-26T20:00:00+08:00",
		},
	}
}

func TestFuelSize(t *testing.T) {
	img, err := RenderFuelWeekly(utilSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %v, want 1080x1350", img.Bounds())
	}
}

func TestBillsSize(t *testing.T) {
	img, err := RenderBills(utilSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %v, want 1080x1350", img.Bounds())
	}
}

func TestFuelEmpty(t *testing.T) {
	if _, err := RenderFuelWeekly(&utilities.Snapshot{}); err == nil {
		t.Error("expected error for empty snapshot")
	}
	if _, err := RenderBills(&utilities.Snapshot{}); err == nil {
		t.Error("expected error for empty snapshot")
	}
}

// TestUtilitiesDeterministic renders twice and requires byte-identical
// compact PNGs.
func TestUtilitiesDeterministic(t *testing.T) {
	snap := utilSnapshot(t)
	for _, tc := range []struct {
		name   string
		render func() (*image.RGBA, error)
	}{
		{"fuel", func() (*image.RGBA, error) { return RenderFuelWeekly(snap) }},
		{"bills", func() (*image.RGBA, error) { return RenderBills(snap) }},
	} {
		var first []byte
		for i := 0; i < 2; i++ {
			img, err := tc.render()
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := EncodeCompact(&buf, img); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				first = buf.Bytes()
			} else if !bytes.Equal(first, buf.Bytes()) {
				t.Errorf("%s: renders differ (%d vs %d bytes)", tc.name, len(first), buf.Len())
			}
		}
	}
}

// TestFuelBarBounds checks the main-section row geometry for every
// fuel sign combination: names centered in the left column, value ink
// fully inside the bar when it fits, else outside on the far side
// without touching the bar edge, the zero line, or the canvas edge.
func TestFuelBarBounds(t *testing.T) {
	snap := utilSnapshot(t)
	adj := snap.Fuel.Adjustments[0]
	type item struct {
		name string
		r    *utilities.Range
	}
	fuels := []item{{"Gasolina", adj.Gasoline}, {"Diesel", adj.Diesel}, {"Kerosene", adj.Kerosene}}
	for trial := 0; trial < 4; trial++ {
		// Trial flips exercise all-negative/all-positive mixes by
		// negating whole ranges (negated Lo/Hi keep magnitude order).
		tr := make([]*utilities.Range, len(fuels))
		for i, f := range fuels {
			r := f.r
			if r != nil && trial != 3 && (trial == 1) != fuelNeg(r) {
				r = &utilities.Range{Lo: -r.Hi, Hi: -r.Lo}
			}
			tr[i] = r
		}
		maxNeg, maxPos := 0.0, 0.0
		var items []fuelItem
		for i, f := range fuels {
			r := tr[i]
			if r == nil {
				continue
			}
			if fuelNeg(r) {
				if m := fmag(r); m > maxNeg {
					maxNeg = m
				}
			} else if m := fmag(r); m > maxPos {
				maxPos = m
			}
			items = append(items, fuelItem{
				neg: fuelNeg(r), mag: fmag(r), lbl: fuelBarLabel(r),
				nameW: cmeasure(f.name, Bold, 32),
			})
		}
		if maxNeg == 0 && maxPos == 0 {
			maxPos = 1
		}
		unit := fscaleUnit(maxNeg, maxPos)
		zx := fzeroX(maxNeg, maxPos, unit)
		unit, zx = fitFuelUnit(maxNeg, maxPos, items, unit, zx)
		for i, f := range fuels {
			r := tr[i]
			if r == nil {
				continue
			}
			lbl := fuelBarLabel(r)
			g, _, ok := placeFuelRow(400, 110, zx, unit, f.name, r)
			if !ok {
				t.Fatal("no placement")
			}
			if g.bx0 < barX0 || g.bx1 > barX1 || g.by1-g.by0 != fuelBarH {
				t.Errorf("trial %d: bad bar rect %+v", trial, g)
			}
			// Name ink inside the left column.
			nx0, nx1, ny0, ny1 := labelInkBox(t, f.name, g.nsize, g.nx, g.ny)
			if nx0 < 0 || nx1 > barX0-4 || ny0 < 400 || ny1 > 400+110 {
				t.Errorf("trial %d: name ink out of column: %q [%d,%d]x[%d,%d]", trial, f.name, nx0, nx1, ny0, ny1)
			}
			ix0, ix1, iy0, iy1 := labelInkBox(t, lbl, g.size, g.lx, g.ly)
			if ix0 < 0 || ix1 > W-16 || iy0 < 400 || iy1 > 400+110 {
				t.Errorf("trial %d: label ink outside row: %q [%d,%d]x[%d,%d]", trial, lbl, ix0, ix1, iy0, iy1)
			}
			if g.inside {
				if ix0 < g.bx0 || ix1 > g.bx1 || iy0 < g.by0 || iy1 > g.by1 {
					t.Errorf("trial %d: inside ink outside bar: %q", trial, lbl)
				}
				// >= 10 px padding from the outer (far-from-zero) end.
				if fuelNeg(r) {
					if ix0-g.bx0 < 10 {
						t.Errorf("trial %d: inside padding < 10: %q %+v", trial, lbl, g)
					}
				} else if g.bx1-ix1 < 10 {
					t.Errorf("trial %d: inside padding < 10: %q %+v", trial, lbl, g)
				}
			} else {
				// Outside on the far side, clear of bar edge + zero line.
				if fuelNeg(r) {
					if !(ix1 <= g.bx0-4 && ix1 <= zx-4) {
						t.Errorf("trial %d: outside label touches bar/zero: %q %+v zx=%d", trial, lbl, g, zx)
					}
				} else if !(ix0 >= g.bx1+4 && ix0 >= zx+4) {
					t.Errorf("trial %d: outside label touches bar/zero: %q %+v zx=%d", trial, lbl, g, zx)
				}
			}
		}
	}
}

// TestMiniBarBounds checks history mini-bar labels across all fixture
// weeks: labels are >= 18 px, fully inside the bar when they fit, else
// outside the bar end within the row margins.
func TestMiniBarBounds(t *testing.T) {
	files := []string{"oilmon_20oct.pdf", "oilmon_13oct.pdf", "oilmon_06oct.pdf",
		"oilmon_29sep.pdf", "oilmon_22sep.pdf", "oilmon_15sep.pdf"}
	var weeks []utilities.OilAdjustment
	for _, f := range files {
		lines, err := utilities.PDFLines(utilFixture(t, f))
		if err != nil {
			t.Fatal(err)
		}
		adj, err := utilities.ParseOilMonitor(lines)
		if err != nil {
			t.Fatal(err)
		}
		weeks = append(weeks, adj)
	}
	hMax := 0.0
	for _, w := range weeks {
		for _, r := range []*utilities.Range{w.Gasoline, w.Diesel} {
			if m := fmag(r); m > hMax {
				hMax = m
			}
		}
	}
	unit := fitMiniUnit(250, 990, 620, weeks, float64((990-250)/2-10)/hMax)
	for _, w := range weeks {
		for di, r := range []*utilities.Range{w.Gasoline, w.Diesel} {
			g, lbl, ok := placeMiniBar(r, 600, 30, 250, 990, 620, unit)
			if !ok {
				t.Fatal("no placement")
			}
			_ = di
			if g.size < 18 {
				t.Errorf("mini label below 18px: %q size=%g", lbl, g.size)
			}
			if g.bx0 < 250 || g.bx1 > 990 {
				t.Errorf("mini bar escapes row margins: %+v", g)
			}
			ix0, ix1, iy0, iy1 := labelInkBox(t, lbl, g.size, g.lx, g.ly)
			if ix0 < 250 || ix1 > 990 {
				t.Errorf("mini label escapes row margins: %q [%d,%d]", lbl, ix0, ix1)
			}
			if iy0 < 600-40 || iy1 > 600+40 {
				t.Errorf("mini label outside row band: %q [%d,%d]", lbl, iy0, iy1)
			}
			if g.inside {
				if ix0 < g.bx0 || ix1 > g.bx1 || iy0 < g.by0 || iy1 > g.by1 {
					t.Errorf("inside mini ink outside bar: %q", lbl)
				}
			} else if fuelNeg(r) {
				if ix1 > g.bx0-4 {
					t.Errorf("outside mini label touches bar: %q %+v", lbl, g)
				}
			} else if ix0 < g.bx1+4 {
				t.Errorf("outside mini label touches bar: %q %+v", lbl, g)
			}
		}
	}
}

// TestHistBarFill checks the history pair fills: gasoline solid by
// sign, diesel the same hue but lighter.
func TestHistBarFill(t *testing.T) {
	if histBarFill(true, false) != green || histBarFill(false, false) != red {
		t.Error("gasoline must be solid green/red")
	}
	if histBarFill(true, true) != greenLt || histBarFill(false, true) != redLt {
		t.Error("diesel must be the lighter hue")
	}
	if greenLt == green || redLt == red {
		t.Error("light fills must differ from solid fills")
	}
	if historyLegend() != "Itaas: Gasolina · Ibaba: Diesel" {
		t.Errorf("legend = %q", historyLegend())
	}
}

func TestDropMissing(t *testing.T) {
	if got := dropMissing("⚡ Meralco", Bold); got != "Meralco" {
		t.Errorf("emoji not dropped: %q", got)
	}
	for _, s := range []string{"Presyo ng langis", "Las Piñas", "−₱4.30/L", "RON 91 ₱87.90", "200 kWh: mga +₱64"} {
		if got := dropMissing(s, Bold); got != s {
			t.Errorf("latin mangled: %q -> %q", s, got)
		}
	}
}

func TestPumpChips(t *testing.T) {
	pump := utilSnapshot(t).Fuel.NCRPump[0]
	chips := pumpChips(pump)
	if len(chips) != 3 {
		t.Fatalf("chips = %v", chips)
	}
	if chips[0] != "RON 91 ₱87.90" {
		t.Errorf("chip0 = %q", chips[0])
	}
}

// TestChartsHaveInk guards against a silently blank render.
func TestChartsHaveInk(t *testing.T) {
	snap := utilSnapshot(t)
	for _, tc := range []struct {
		name string
		img  *image.RGBA
	}{
		{"fuel", mustRenderFuel(t, snap)},
		{"bills", mustRenderBills(t, snap)},
	} {
		n := 0
		for y := 0; y < H; y += 7 {
			for x := 0; x < W; x += 7 {
				c := tc.img.RGBAAt(x, y)
				if c != (color.RGBA{0xFB, 0xF6, 0xE9, 0xFF}) && c.A > 0 {
					n++
				}
			}
		}
		if n == 0 {
			t.Errorf("%s chart is blank", tc.name)
		}
	}
}

func mustRenderFuel(t *testing.T, snap *utilities.Snapshot) *image.RGBA {
	t.Helper()
	img, err := RenderFuelWeekly(snap)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func mustRenderBills(t *testing.T, snap *utilities.Snapshot) *image.RGBA {
	t.Helper()
	img, err := RenderBills(snap)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestMeralcoCardLines checks the signed card strings: the 200 kWh
// line always carries its sign (+₱64, −₱8 with U+2212), and the second
// line pairs the prev rate with the announced date.
func TestMeralcoCardLines(t *testing.T) {
	if got := meralcoImpactLine(64); got != "200 kWh: mga +₱64" {
		t.Errorf("impact line = %q", got)
	}
	if got := meralcoImpactLine(-8); got != "200 kWh: mga −₱8" {
		t.Errorf("impact line = %q", got)
	}
	if got := meralcoPrevLine(14.7424, "2026-10-13"); got != "mula ₱14.7424 · Okt 13" {
		t.Errorf("prev line = %q", got)
	}
}

// TestFooterBounds checks the fitted footer never crosses its divider:
// the left block ends >= 20 px before it, the URL starts >= 20 px
// after it.
func TestFooterBounds(t *testing.T) {
	for _, tc := range [][2]string{
		{"Source: DOE (doe.gov.ph)", "Hindi opisyal na DOE page"},
		{"Source: DOE · Meralco · Maynilad", "Hindi opisyal na page"},
	} {
		size, divX, footX, t1, t2 := footerLayout(tc[0], tc[1])
		if size < 14 {
			t.Errorf("footer size %g below minimum", size)
		}
		leftEnd := marginL + cmeasure(t1, SemiBold, size)
		if w := cmeasure(t2, SemiBold, size); marginL+w > leftEnd {
			leftEnd = marginL + w
		}
		if leftEnd > divX-20 {
			t.Errorf("left text crosses divider: end=%d divX=%d", leftEnd, divX)
		}
		if divX+20 > footX {
			t.Errorf("divider crosses URL: divX=%d footX=%d", divX, footX)
		}
		// Ink check on the longer bills line.
		ix0, ix1, _, _ := labelInkBox(t, t1, size, marginL, 1300)
		if ix1 > divX-20 {
			t.Errorf("left ink crosses divider: %q inkEnd=%d divX=%d", t1, ix1, divX)
		}
		_ = ix0
	}
}

// TestWrapLines checks the Maynilad status wrap: the long status fits
// in 2 lines at the LPG detail size, short text stays on one line,
// and every line fits the card.
func TestWrapLines(t *testing.T) {
	const cardW = W - marginL - marginR - 60
	long := "walang naka-schedule na interruption — tiningnan Okt 26"
	ls, sz := wrapLines(long, Medium, 30, 22, cardW, 2)
	if sz != 30 {
		t.Errorf("status shrank to %g, want 30", sz)
	}
	if len(ls) != 2 {
		t.Fatalf("status wraps to %d lines: %q", len(ls), ls)
	}
	for _, ln := range ls {
		if cmeasure(ln, Medium, sz) > cardW {
			t.Errorf("wrapped line overflows: %q", ln)
		}
	}
	short := "walang datos — hindi natiningnan"
	ls, _ = wrapLines(short, Medium, 30, 22, cardW, 2)
	if len(ls) != 1 {
		t.Errorf("short text wraps to %d lines: %q", len(ls), ls)
	}
}

// TestBillCardsReachFooter renders bills and requires content (card
// white) just above the footer band: no blank gap left behind.
func TestBillCardsReachFooter(t *testing.T) {
	img := mustRenderBills(t, utilSnapshot(t))
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	n := 0
	for x := 0; x < W; x++ {
		if img.RGBAAt(x, H-118-3) == white {
			n++
		}
	}
	if n == 0 {
		t.Error("no card pixels just above the footer: blank gap remains")
	}
}

// TestFuelReachesFooter renders fuel and requires chart ink (chips)
// just above the footer band.
func TestFuelReachesFooter(t *testing.T) {
	img := mustRenderFuel(t, utilSnapshot(t))
	n := 0
	for y := H - 118 - 70; y < H-118-2; y++ {
		for x := 0; x < W; x += 2 {
			if img.RGBAAt(x, y) == green {
				n++
			}
		}
	}
	if n == 0 {
		t.Error("no chart ink near the footer: blank gap remains")
	}
}
