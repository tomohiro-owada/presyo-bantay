package fbchart

import (
	"bytes"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

func testWeeklySignals() *weekly.Signals {
	return &weekly.Signals{
		Week:    "2026-W43",
		Covered: []string{"2026-10-19", "2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23"},
		Missing: []string{"2026-10-24"},
		Drops: []weekly.MetroEntry{
			{Name: "Chayote", Unit: "per kg", Pct: -19, DisplayPrice: 60},
			{Name: "Pechay Baguio", Unit: "per kg", Pct: -16, DisplayPrice: 120},
		},
		Rises: []weekly.MetroEntry{
			{Name: "Red Chili (Local)", Unit: "per kg", Pct: 16, DisplayPrice: 341},
		},
		DealsByRegion: []weekly.RegionDeals{
			{Region: "south", TitleTL: "South Metro", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Alabang", Pct: -48}}},
			{Region: "manila", TitleTL: "Maynila at Pasay", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Cartimar", Pct: -64}}},
			{Region: "qc", TitleTL: "Quezon City", Deals: []weekly.DealEntry{}},
			{Region: "north", TitleTL: "CAMANAVA", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Malabon Central", Pct: -49}}},
			{Region: "east", TitleTL: "Silangan", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Poblacion", Pct: -46}}},
		},
	}
}

func TestRenderWeeklySize(t *testing.T) {
	img, err := RenderWeekly(testWeeklySignals())
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %dx%d, want 1080x1350", img.Bounds().Dx(), img.Bounds().Dy())
	}
	var buf bytes.Buffer
	if err := EncodeCompact(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("empty compact png")
	}
}

func TestRenderWeeklyEmpty(t *testing.T) {
	sig := testWeeklySignals()
	sig.Drops = nil
	sig.Rises = nil
	sig.DealsByRegion = nil
	img, err := RenderWeekly(sig)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %v", img.Bounds())
	}
}

// maxWeeklySignals builds the densest chart case: 5 drops + 3 rises +
// 5 region deals with the longest current titles/markets.
func maxWeeklySignals() *weekly.Signals {
	return &weekly.Signals{
		Week:    "2026-W43",
		Covered: []string{"2026-10-19", "2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23"},
		Drops: []weekly.MetroEntry{
			{Name: "Chayote", Unit: "per kg", Pct: -45, DisplayPrice: 45},
			{Name: "Pechay Baguio", Unit: "per kg", Pct: -30, DisplayPrice: 80},
			{Name: "Red Chili (Local)", Unit: "per kg", Pct: -25, DisplayPrice: 300},
			{Name: "White Potato (Local)", Unit: "per kg", Pct: -16, DisplayPrice: 150},
			{Name: "Eggplant", Unit: "per kg", Pct: -12, DisplayPrice: 90},
		},
		Rises: []weekly.MetroEntry{
			{Name: "Tomato", Unit: "per kg", Pct: 30, DisplayPrice: 120},
			{Name: "Onion", Unit: "per kg", Pct: 20, DisplayPrice: 140},
			{Name: "Garlic", Unit: "per kg", Pct: 15, DisplayPrice: 200},
		},
		DealsByRegion: []weekly.RegionDeals{
			{Region: "south", TitleTL: "South Metro", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Alabang", Pct: -48}}},
			{Region: "manila", TitleTL: "Maynila at Pasay", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Cartimar", Pct: -64}}},
			{Region: "qc", TitleTL: "Quezon City", Deals: []weekly.DealEntry{{Name: "Pechay Baguio", MarketShort: "Commonwealth", Pct: -40}}},
			{Region: "north", TitleTL: "CAMANAVA", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Malabon Central", Pct: -49}}},
			{Region: "east", TitleTL: "Silangan", Deals: []weekly.DealEntry{{Name: "Chayote", MarketShort: "Poblacion", Pct: -46}}},
		},
	}
}

type inkBox struct {
	what           string
	x0, x1, y0, y1 int
}

func boxesOverlap(a, b inkBox) bool {
	return a.x0 <= b.x1 && b.x0 <= a.x1 && a.y0 <= b.y1 && b.y0 <= a.y1
}

// TestRenderWeeklyMaxFits renders the max case (5 drops + 3 rises +
// 5 regions) and asserts no text/ink overlaps between rows and nothing
// below the footer top.
func TestRenderWeeklyMaxFits(t *testing.T) {
	sig := maxWeeklySignals()
	img, err := RenderWeekly(sig)
	if err != nil {
		t.Fatal(err)
	}
	footerTop := H - 118

	// Rebuild the metro scale exactly as RenderWeekly does.
	var deals, pricier []rowItem
	for _, m := range sig.Drops {
		deals = append(deals, rowItem{
			name:  weekly.DisplayTL(m.Name),
			price: formatTestPrice(m.DisplayPrice, m.Unit),
			pct:   m.Pct, deal: true,
		})
	}
	for _, m := range sig.Rises {
		pricier = append(pricier, rowItem{
			name:  weekly.DisplayTL(m.Name),
			price: formatTestPrice(m.DisplayPrice, m.Unit),
			pct:   m.Pct, deal: false,
		})
	}
	maxNeg, maxPos := 0, 0
	for _, r := range deals {
		if a := abs(r.pct); a > maxNeg {
			maxNeg = a
		}
	}
	for _, r := range pricier {
		if a := abs(r.pct); a > maxPos {
			maxPos = a
		}
	}
	labelSize, labelX0, unit, zx := weeklyBarLayout(deals, pricier, maxNeg, maxPos)

	// Vertical budget (mirrors RenderWeekly).
	const dealPillH = 52 + 14
	const metroPillBlock = 8 + 52 + 14
	const areaPillBlock = 8 + 52 + 14
	y := 256
	fixed := dealPillH + metroPillBlock + areaPillBlock + len(sig.DealsByRegion)*weeklyAreaH + weeklyBottomGap
	availForBars := footerTop - y - fixed
	needShrink := len(deals)*weeklyDealNat + len(pricier)*weeklyPrNat
	dealH, prH := weeklyBarHeights(availForBars, needShrink, len(deals), len(pricier))

	var boxes []inkBox
	addLabel := func(what, s string, size float64, lx, ly int) {
		if s == "" {
			return
		}
		x0, x1, y0, y1 := labelInkBox(t, s, size, lx, ly)
		boxes = append(boxes, inkBox{what, x0, x1, y0, y1})
	}

	y += dealPillH
	// Metro rows (mirror drawWeeklyRow: single uniform line, name Bold +
	// price SemiBold sharing labelSize and baseline).
	addBarLabel := func(what string, r rowItem, rowH, yy int) {
		maxW := labelX0 - marginL - 12
		name := weeklyEllipsizeName(r.name, r.price, labelSize, maxW)
		by := yy + rowH/2 + 8
		addLabel(what+"-name "+r.name, name, labelSize, marginL, by)
		nx := marginL + measure(name, Bold, labelSize) + measure(" ", Bold, labelSize)
		// Price width measured in its own SemiBold face; approximate its
		// ink box with the Bold metric used by labelInkBox (same size,
		// widths differ by <2px, boxes stay disjoint from bar labels).
		addLabel(what+"-price "+r.name, r.price, labelSize, nx, by)
	}
	for _, r := range deals {
		addBarLabel("deal", r, dealH, y)
		lbl := fmtPct(r.pct)
		g := placeDealBar(y, dealH, zx, barLen(r.pct, unit), lbl)
		addLabel("deal-pct "+r.name, lbl, g.size, g.lx, g.ly)
		y += dealH
	}
	y += metroPillBlock
	for _, r := range pricier {
		addBarLabel("rise", r, prH, y)
		lbl := fmtPct(r.pct)
		g := placePricierBar(y, prH, zx, barLen(r.pct, unit), lbl)
		addLabel("rise-pct "+r.name, lbl, g.size, g.lx, g.ly)
		y += prH
	}
	metroBottom := y
	y += areaPillBlock
	regionTop := y
	for _, rd := range sig.DealsByRegion {
		d := rd.Deals[0]
		detail := detailTest(rd.TitleTL, d)
		_ = detail
		chip := fmtPct(d.Pct)
		cw := measure(chip, Bold, 26) + 28
		cx1 := W - marginR - cw
		maxW := cx1 - 12 - marginL
		ts := fitSize(rd.TitleTL, Bold, 26, 20, maxW)
		tt := ellipsize(rd.TitleTL, Bold, ts, maxW)
		if tt != rd.TitleTL {
			t.Errorf("region %q ellipsized to %q (current data must fit)", rd.TitleTL, tt)
		}
		ds := fitSize(detail, Medium, 24, 18, maxW)
		dt := ellipsize(detail, Medium, ds, maxW)
		if dt != detail {
			t.Errorf("detail %q ellipsized to %q (current data must fit)", detail, dt)
		}
		addLabel("region-title "+rd.TitleTL, tt, ts, marginL, y+28)
		addLabel("region-detail "+rd.TitleTL, dt, ds, marginL, y+62)
		cy := y + weeklyAreaH/2
		addLabel("region-chip "+rd.TitleTL, chip, 26, cx1+14, cy+9)
		y += weeklyAreaH
	}
	regionBottom := y

	if regionBottom > footerTop-weeklyBottomGap {
		t.Errorf("regionBottom %d exceeds footerTop-gap %d", regionBottom, footerTop-weeklyBottomGap)
	}
	if metroBottom >= regionTop {
		t.Errorf("metroBottom %d must be above regionTop %d (zero line must stop)", metroBottom, regionTop)
	}
	// Pairwise overlap check.
	for i := 0; i < len(boxes); i++ {
		if boxes[i].y1 >= footerTop {
			t.Errorf("%s ink box [%d,%d]x[%d,%d] reaches footer (>= %d)",
				boxes[i].what, boxes[i].x0, boxes[i].x1, boxes[i].y0, boxes[i].y1, footerTop)
		}
		for j := i + 1; j < len(boxes); j++ {
			// Bar labels live in the bar area (x >= barX0) while
			// names/prices/titles/details live in the text column;
			// they are disjoint by construction, but region chips
			// vs text are checked via maxW. Check everything.
			if boxesOverlap(boxes[i], boxes[j]) {
				// Allow a bar's own pct label vs nothing else: any
				// overlap is a bug (text column vs bar area never
				// share x, region text vs chip never share x).
				t.Errorf("ink overlap: %s [%d,%d]x[%d,%d] vs %s [%d,%d]x[%d,%d]",
					boxes[i].what, boxes[i].x0, boxes[i].x1, boxes[i].y0, boxes[i].y1,
					boxes[j].what, boxes[j].x0, boxes[j].x1, boxes[j].y0, boxes[j].y1)
			}
		}
	}

	// Image checks: the cream gap above the footer must be clean and the
	// zero line must not run through the region section.
	for yy := footerTop - weeklyBottomGap; yy < footerTop; yy++ {
		for xx := marginL; xx < W-marginR; xx++ {
			if img.RGBAAt(xx, yy) != cream {
				t.Fatalf("gap row y=%d x=%d not cream (region overflows into footer gap)", yy, xx)
			}
		}
	}
	for yy := regionTop; yy < regionBottom && yy < footerTop; yy++ {
		if img.RGBAAt(zx, yy) == zeroCol {
			t.Fatalf("zero line runs through region section at y=%d (must stop at %d)", yy, metroBottom)
		}
	}
}

// weeklyBarRows builds the metro rowItems exactly as RenderWeekly does.
func weeklyBarRows(sig *weekly.Signals) (deals, pricier []rowItem) {
	for _, m := range sig.Drops {
		deals = append(deals, rowItem{
			name:  weekly.DisplayTL(m.Name),
			price: formatTestPrice(m.DisplayPrice, m.Unit),
			pct:   m.Pct, deal: true,
		})
	}
	for _, m := range sig.Rises {
		pricier = append(pricier, rowItem{
			name:  weekly.DisplayTL(m.Name),
			price: formatTestPrice(m.DisplayPrice, m.Unit),
			pct:   m.Pct, deal: false,
		})
	}
	return deals, pricier
}

// TestWeeklyBarLabelsUniform asserts the compact bar-row labels use ONE
// uniform size in [20,28] and none is ellipsized, for W43-like data and
// the max case. It also exercises the widen path with a pathological name.
func TestWeeklyBarLabelsUniform(t *testing.T) {
	for _, sig := range []*weekly.Signals{testWeeklySignals(), maxWeeklySignals()} {
		deals, pricier := weeklyBarRows(sig)
		all := append(append([]rowItem{}, deals...), pricier...)
		if len(all) == 0 {
			t.Fatal("no metro rows")
		}
		maxNeg, maxPos := 0, 0
		for _, r := range deals {
			if a := abs(r.pct); a > maxNeg {
				maxNeg = a
			}
		}
		for _, r := range pricier {
			if a := abs(r.pct); a > maxPos {
				maxPos = a
			}
		}
		labelSize, x0, _, _ := weeklyBarLayout(deals, pricier, maxNeg, maxPos)
		if labelSize < weeklyLabelMin || labelSize > weeklyLabelMax {
			t.Errorf("week %s labelSize %g outside [%g,%g]", sig.Week, labelSize, weeklyLabelMin, weeklyLabelMax)
		}
		maxW := x0 - marginL - 12
		for _, r := range all {
			if w := weeklyLabelWidth(r.name, r.price, labelSize); w > maxW {
				t.Errorf("week %s row %q %q width %d exceeds label column %d at size %g (would ellipsize)",
					sig.Week, r.name, r.price, w, maxW, labelSize)
			}
			if got := weeklyEllipsizeName(r.name, r.price, labelSize, maxW); got != r.name {
				t.Errorf("week %s row %q ellipsized to %q at size %g", sig.Week, r.name, got, labelSize)
			}
		}
		// Uniformity: every row reports the same size (single layout size).
		for _, r := range all {
			_ = r
		}
		if got := weeklyUniformSize(all, maxW); got != labelSize {
			t.Errorf("week %s recomputed uniform size %g != layout size %g (not uniform/stable)", sig.Week, got, labelSize)
		}
		// Current data must fit without ellipsis; the max case may widen
		// the label column (shift the zero axis right) instead of
		// ellipsizing.
		if x0 < barX0 || barX1-x0 < 160 {
			t.Errorf("week %s label column x0=%d leaves unusable bar area", sig.Week, x0)
		}
	}
	// Widen path: a long name that cannot fit at 20px in the default
	// column must shift the zero axis right (shrinking bar scale) so it
	// fits at 20px without ellipsis.
	deals := []rowItem{{name: "Pechay Baguio Extra Mahaba", price: "₱12345/kg", pct: -50, deal: true}}
	pricier := []rowItem{{name: "Siling pula", price: "₱350/kg", pct: 10}}
	labelSize, x0, _, _ := weeklyBarLayout(deals, pricier, 50, 10)
	if labelSize != weeklyLabelMin {
		t.Errorf("widen case size = %g, want %g", labelSize, weeklyLabelMin)
	}
	if x0 <= barX0 {
		t.Errorf("widen case x0 = %d, want > %d", x0, barX0)
	}
	maxW := x0 - marginL - 12
	for _, r := range append(append([]rowItem{}, deals...), pricier...) {
		if got := weeklyEllipsizeName(r.name, r.price, labelSize, maxW); got != r.name {
			t.Errorf("widen case row %q still ellipsized to %q (column %d)", r.name, got, maxW)
		}
	}
}

func formatTestPrice(v float64, unit string) string {
	return "₱" + fmtMoney(v) + unitSuffix(unit)
}

func detailTest(title string, d weekly.DealEntry) string {
	_ = title
	return weekly.DisplayTL(d.Name) + " · " + d.MarketShort
}
