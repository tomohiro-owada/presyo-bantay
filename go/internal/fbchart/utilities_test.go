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
	lpg2Lines, err := utilities.PDFLines(utilFixture(t, "ncr_lpg_sep.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	lpg2, err := utilities.ParseNCRLPG(lpg2Lines)
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
	fcda, err := os.ReadFile(utilFixture(t, "maynilad_fcda_posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	rates, err := utilities.ParseFCDAPosts(fcda)
	if err != nil {
		t.Fatal(err)
	}
	return &utilities.Snapshot{
		Fuel: utilities.FuelStatus{
			Adjustments: []utilities.OilAdjustment{adj},
			NCRPump:     []utilities.PumpSummary{pump},
		},
		LPG:        []utilities.LPGMonth{lpg, lpg2},
		Meralco:    []utilities.MeralcoRate{mer},
		WaterRates: rates,
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
		lastWeek := map[int]string{}
		if trial == 3 {
			// Exercise the muted last-week row texts once.
			lastWeek[0] = "nakaraan: +₱1.50–1.56"
			lastWeek[1] = "nakaraan: −₱3.00–3.02"
		}
		for i, f := range fuels {
			r := tr[i]
			if r == nil {
				continue
			}
			lbl := fuelBarLabel(r)
			g, _, ok := placeFuelRow(400, 110, zx, unit, f.name, lastWeek[i], r)
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
			// Muted last-week text, when present, stays in the column.
			if g.sub != "" {
				sx0, sx1, sy0, sy1 := labelInkBox(t, g.sub, g.ssize, g.sx, g.sy)
				if sx0 < 0 || sx1 > barX0-4 || sy0 < 400 || sy1 > 400+110 {
					t.Errorf("trial %d: last-week ink out of column: %q", trial, g.sub)
				}
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

// TestHistoryNoOverlaps replicates the renderer history layout for
// the real 6-fixture set and requires every value label's ink (+1 px)
// to be disjoint from every other label and every foreign bar, and
// every bar inside the row margins.
func TestHistoryNoOverlaps(t *testing.T) {
	files := []string{"oilmon_20oct.pdf", "oilmon_13oct.pdf", "oilmon_06oct.pdf",
		"oilmon_29sep.pdf", "oilmon_22sep.pdf", "oilmon_15sep.pdf"}
	var newest []utilities.OilAdjustment
	for _, f := range files {
		lines, err := utilities.PDFLines(utilFixture(t, f))
		if err != nil {
			t.Fatal(err)
		}
		adj, err := utilities.ParseOilMonitor(lines)
		if err != nil {
			t.Fatal(err)
		}
		newest = append(newest, adj)
	}
	// Display order: oldest last like the renderer.
	weeks := append([]utilities.OilAdjustment(nil), newest...)
	for i, j := 0, len(weeks)-1; i < j; i, j = i+1, j-1 {
		weeks[i], weeks[j] = weeks[j], weeks[i]
	}
	hMax := 0.0
	for _, w := range weeks {
		for _, r := range []*utilities.Range{w.Gasoline, w.Diesel} {
			if m := fmag(r); m > hMax {
				hMax = m
			}
		}
	}
	miniX0, miniX1 := 250, 990
	miniMid := (miniX0 + miniX1) / 2
	unit := fitMiniUnit(miniX0, miniX1, miniMid, weeks, float64((miniX1-miniX0)/2-10)/hMax)
	_, histRows := fuelRowHeights(len(weeks), 3)
	if len(histRows) != len(weeks) {
		t.Fatalf("hist rows = %d, want %d", len(histRows), len(weeks))
	}
	type box struct {
		own    int // bar index this label belongs to, or own index
		x0, x1 int
		y0, y1 int
	}
	var labels []box
	var bars []box
	yy := 0
	for i, w := range weeks {
		rowH := histRows[i]
		if rowH < fuelHistMin {
			t.Fatalf("history row below minimum: %d", rowH)
		}
		if gsize := miniLabelSize; gsize < 18 {
			t.Fatalf("mini label below 18px: %g", gsize)
		}
		cy := yy + rowH/2
		mh := miniBarH(rowH)
		off := mh/2 + 2
		for di, r := range []*utilities.Range{w.Gasoline, w.Diesel} {
			ccy := cy - off
			if di == 1 {
				ccy = cy + off
			}
			g, lbl, ok := placeMiniBar(r, ccy, mh, miniX0, miniX1, miniMid, unit)
			if !ok {
				t.Fatal("no placement")
			}
			if g.size < 18 {
				t.Errorf("mini label below 18px: %q", lbl)
			}
			bi := len(bars)
			bars = append(bars, box{own: bi, x0: g.bx0, x1: g.bx1, y0: g.by0, y1: g.by1})
			ix0, ix1, iy0, iy1 := labelInkBox(t, lbl, g.size, g.lx, g.ly)
			labels = append(labels, box{own: bi, x0: ix0 - 1, x1: ix1 + 1, y0: iy0 - 1, y1: iy1 + 1})
		}
		yy += rowH
	}
	overlap := func(a, b box) bool {
		return a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1
	}
	for i := range labels {
		if labels[i].x0 < miniX0 || labels[i].x1 > miniX1 {
			t.Errorf("label escapes row margins: %+v", labels[i])
		}
		for j := range labels {
			if i == j {
				continue
			}
			if overlap(labels[i], labels[j]) {
				t.Errorf("label-label overlap: %+v vs %+v", labels[i], labels[j])
			}
		}
		for j := range bars {
			if j == labels[i].own {
				continue
			}
			if overlap(labels[i], bars[j]) {
				t.Errorf("label-foreign-bar overlap: %+v vs %+v", labels[i], bars[j])
			}
		}
	}
	for _, b := range bars {
		if b.x0 < miniX0 || b.x1 > miniX1 {
			t.Errorf("bar escapes row margins: %+v", b)
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

func TestPumpTable(t *testing.T) {
	snap := utilSnapshot(t)
	pump2Lines, err := utilities.PDFLines(utilFixture(t, "ncr_pump_1006.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	pump2, err := utilities.ParseNCRPumpSummary(pump2Lines)
	if err != nil {
		t.Fatal(err)
	}
	now := snap.Fuel.NCRPump[0]
	rows, vsize, csize := layoutPumpTable(&now, &pump2)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (NCR + 2 cities)", len(rows))
	}
	if rows[0].label != "NCR" || rows[1].label != "Parañaque" || rows[2].label != "Muntinlupa" {
		t.Fatalf("labels = %q %q %q", rows[0].label, rows[1].label, rows[2].label)
	}
	// NCR RON91: ₱87.90, delta −0.40 (decrease → green).
	ncr91 := rows[0].cells[0]
	if ncr91.value != "₱87.90" || ncr91.delta != "−0.40" || !ncr91.dneg || ncr91.dzero {
		t.Errorf("NCR RON91 cell = %+v", ncr91)
	}
	// NCR RON95: +0.50 (increase, red).
	if ncr95 := rows[0].cells[1]; ncr95.delta != "+0.50" || ncr95.dneg || ncr95.dzero {
		t.Errorf("NCR RON95 cell = %+v", ncr95)
	}
	// Parañaque Diesel: −3.00.
	if pd := rows[1].cells[2]; pd.value != "₱91.80" || pd.delta != "−3.00" {
		t.Errorf("Parañaque Diesel cell = %+v", pd)
	}
	// Muntinlupa RON91: +2.30.
	if mr := rows[2].cells[0]; mr.value != "₱91.00" || mr.delta != "+2.30" {
		t.Errorf("Muntinlupa RON91 cell = %+v", mr)
	}
	if vsize < 20 || csize < 16 {
		t.Errorf("sizes too small: %g %g", vsize, csize)
	}
	// Geometry: every value + chip fits its column and the canvas.
	for _, r := range rows {
		for _, c := range r.cells {
			if c.empty {
				continue
			}
			if c.vx < 0 || c.cx+c.cw > W-16 {
				t.Errorf("cell escapes canvas: %+v", c)
			}
			ix0, ix1, _, _ := labelInkBox(t, c.value, vsize, c.vx, 600)
			if ix0 < c.vx || ix1 > c.vx+c.colW {
				t.Errorf("value ink escapes column: %q [%d,%d]", c.value, ix0, ix1)
			}
			jx0, jx1, _, _ := labelInkBox(t, c.delta, csize, c.cx+14, 600)
			if jx0 < c.cx || jx1 > c.cx+c.cw {
				t.Errorf("chip ink escapes chip: %q [%d,%d]", c.delta, jx0, jx1)
			}
		}
	}
	// Caption carries both weeks.
	if cap := pumpTableCaption(&now, &pump2); cap != "Pump price DOE Okt 13–19 vs Okt 6–12 · Parañaque at Muntinlupa: malapit sa Las Piñas" {
		t.Errorf("caption = %q", cap)
	}
	if tableHeadSize < 20 {
		t.Errorf("table header below 20px: %g", tableHeadSize)
	}
	// Without a previous week every delta is grey n/a.
	rows1, _, _ := layoutPumpTable(&now, nil)
	if d := rows1[0].cells[0]; d.delta != "n/a" || !d.dzero {
		t.Errorf("no-prev delta = %+v", d)
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
// line always carries its sign (+₱64, −₱8 with U+2212), and the chips
// always include their comparison period or origin.
func TestMeralcoCardLines(t *testing.T) {
	if got := meralcoImpactLine(64); got != "200 kWh: mga +₱64" {
		t.Errorf("impact line = %q", got)
	}
	if got := meralcoImpactLine(-8); got != "200 kWh: mga −₱8" {
		t.Errorf("impact line = %q", got)
	}
	if got := meralcoChipText(0.3191, "2026-10"); got != "+₱0.3191 vs Set" {
		t.Errorf("meralco chip = %q", got)
	}
	if got := lpgChipText(89, "Set 1–7"); got != "+₱89 vs Set 1–7" {
		t.Errorf("lpg chip = %q", got)
	}
	if got := waterChipText(0.24, "Okt 1"); got != "+₱0.24 mula Okt 1" {
		t.Errorf("water chip = %q", got)
	}
	if got := waterChipText(-0.01, "Okt 1"); got != "−₱0.01 mula Okt 1" {
		t.Errorf("water chip = %q", got)
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

// TestBillCardsReachFooter renders bills and requires card white
// within a few px above the footer band: no blank gap left behind
// (even distribution leaves at most a rounding remainder).
func TestBillCardsReachFooter(t *testing.T) {
	img := mustRenderBills(t, utilSnapshot(t))
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	n := 0
	for y := H - 118 - 10; y < H-118; y++ {
		for x := 0; x < W; x++ {
			if img.RGBAAt(x, y) == white {
				n++
			}
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

// TestBillsCards checks the 4-card content: old→new big lines, chips
// with comparison periods, family breakdown.
func TestBillsCards(t *testing.T) {
	snap := utilSnapshot(t)
	cards := buildBillsCards(snap, 64)
	if len(cards) != 4 {
		t.Fatalf("got %d cards", len(cards))
	}
	if cards[0].title != "Meralco" || cards[1].title != "LPG 11 kg NCR" ||
		cards[2].title != "Maynilad Las Piñas" || cards[3].title != "Pamilya kada buwan" {
		t.Fatalf("titles = %q %q %q %q", cards[0].title, cards[1].title, cards[2].title, cards[3].title)
	}
	if cards[0].chip != "+₱0.3191 vs Set" {
		t.Errorf("meralco chip = %q", cards[0].chip)
	}
	if cards[0].lines[0].text != "₱14.7424 → ₱15.0615/kWh" {
		t.Errorf("meralco big = %q", cards[0].lines[0].text)
	}
	if cards[1].chip != "+₱89 vs Set 1–7" {
		t.Errorf("lpg chip = %q", cards[1].chip)
	}
	if cards[1].lines[0].text != "₱1,341 → ₱1,430" {
		t.Errorf("lpg big = %q", cards[1].lines[0].text)
	}
	if cards[1].lines[2].text != "Set: ₱1,185–₱1,393" {
		t.Errorf("lpg prev range = %q", cards[1].lines[2].text)
	}
	if cards[2].chip != "+₱0.24 mula Okt 1" {
		t.Errorf("water chip = %q", cards[2].chip)
	}
	if cards[2].lines[0].text != "₱52.61 → ₱52.85/cu.m" {
		t.Errorf("water big = %q", cards[2].lines[0].text)
	}
	if cards[3].chip != "+₱157.62" {
		t.Errorf("family chip = %q", cards[3].chip)
	}
	if cards[3].lines[0].text != "₱5,341.68 → ₱5,499.30" {
		t.Errorf("family big = %q", cards[3].lines[0].text)
	}
	wantBreak := "kuryente +₱63.82 · LPG +₱89.00 · tubig +₱4.80"
	if cards[3].lines[1].text != wantBreak {
		t.Errorf("family breakdown = %q", cards[3].lines[1].text)
	}
	// Every line fits its card.
	const cardW = W - marginL - marginR - 60
	for _, cd := range cards {
		for _, ln := range cd.lines {
			if cmeasure(ln.text, ln.w, ln.size) > cardW {
				t.Errorf("card %q line overflows: %q", cd.title, ln.text)
			}
		}
		if cd.hasChip && cmeasure(cd.chip, Bold, 28)+36 > cardW {
			t.Errorf("card %q chip overflows", cd.title)
		}
	}
}
