package utilities

import (
	"os"
	"path/filepath"
	"testing"
)

func testdata(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture %s missing: %v", name, err)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func mustLines(t *testing.T, pdf string) []Line {
	t.Helper()
	lines, err := PDFLines(testdata(t, pdf))
	if err != nil {
		t.Fatalf("PDFLines(%s): %v", pdf, err)
	}
	if len(lines) == 0 {
		t.Fatalf("PDFLines(%s): no lines", pdf)
	}
	return lines
}

func TestPDFLinesAllFixtures(t *testing.T) {
	pdfs := []string{
		"oilmon_20oct.pdf", "oilmon_13oct.pdf", "oilmon_06oct.pdf",
		"oilmon_29sep.pdf", "oilmon_22sep.pdf", "oilmon_15sep.pdf",
		"ncr_pump_1013.pdf", "ncr_pump_1006.pdf",
		"ncr_lpg_oct.pdf", "ncr_lpg_sep.pdf",
	}
	for _, pdf := range pdfs {
		lines := mustLines(t, pdf)
		// Reading order: non-decreasing (page, top).
		for i := 1; i < len(lines); i++ {
			a, b := lines[i-1], lines[i]
			if b.Page < a.Page || (b.Page == a.Page && b.Top < a.Top-1e-9) {
				t.Fatalf("%s: lines out of order at %d", pdf, i)
			}
			if len(b.Words) == 0 || b.Text == "" {
				t.Fatalf("%s: empty line at %d", pdf, i)
			}
		}
	}
}

func TestParseAttachments(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(testdata(t, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	mon := ParseAttachments(read("doe_oil_monitor_page.html"), "https://doe.gov.ph/oil-monitor")
	if len(mon) == 0 {
		t.Fatal("no oil monitor attachments")
	}
	if mon[0].Title != "Oil Monitor 20 October" {
		t.Fatalf("first oil monitor = %q", mon[0].Title)
	}
	pump := ParseAttachments(read("doe_ncr_pump_page.html"), "https://doe.gov.ph/ncr-pump")
	if len(pump) < 7 {
		t.Fatalf("only %d pump attachments", len(pump))
	}
	// Document order is NOT date order: Sep 29 lists before Oct 13.
	if pump[0].Title != "September 29 to October 5" {
		t.Fatalf("first pump = %q", pump[0].Title)
	}
	found1013 := false
	for _, a := range pump {
		if a.Title == "October 13 to 19" {
			found1013 = true
		}
	}
	if !found1013 {
		t.Fatal("October 13 to 19 missing")
	}
	lpg := ParseAttachments(read("doe_ncr_lpg_page.html"), "https://doe.gov.ph/ncr-lpg")
	if len(lpg) < 2 || lpg[0].Title != "October 1 to 8" || lpg[1].Title != "September 1 to 7" {
		t.Fatalf("lpg attachments = %+v", lpg[:2])
	}
	pa := ParseAttachments(read("doe_price_adjustments_page.html"), "https://doe.gov.ph/adjustments")
	if len(pa) == 0 {
		t.Fatal("no price adjustment attachments")
	}
	// Dedupe: Download / Open-in-new-tab anchors share the URL path.
	for _, list := range [][]Attachment{mon, pump, lpg, pa} {
		seen := map[string]int{}
		for _, a := range list {
			seen[a.URL]++
			if seen[a.URL] > 1 {
				t.Fatalf("duplicate URL %q", a.URL)
			}
		}
		_ = seen
	}
}

func TestParseOilMonitor(t *testing.T) {
	type want struct {
		from, to      string
		no            int
		gas, die, ker *Range
	}
	r := func(lo, hi float64) *Range { return &Range{Lo: lo, Hi: hi} }
	cases := map[string]want{
		"oilmon_20oct.pdf": {"2026-10-20", "2026-10-26", 42, r(-4.30, -4.30), r(2.60, 2.60), r(-1.00, -1.00)},
		"oilmon_13oct.pdf": {"2026-10-13", "2026-10-19", 41, r(1.50, 1.56), r(-3.00, -3.02), r(1.70, 1.77)},
		"oilmon_06oct.pdf": {"2026-10-06", "2026-10-12", 40, r(1.90, 1.93), r(-1.30, -1.30), r(3.30, 3.30)},
		"oilmon_29sep.pdf": {"2026-09-29", "2026-10-05", 39, r(-0.24, -0.50), r(-7.55, -8.00), r(-5.85, -5.90)},
		"oilmon_22sep.pdf": {"2026-09-22", "2026-09-29", 38, r(4.80, 4.88), r(8.80, 8.82), r(6.40, 6.47)},
		"oilmon_15sep.pdf": {"2026-09-15", "2026-09-21", 37, r(5.00, 5.70), r(4.00, 4.40), r(4.60, 4.62)},
	}
	asOf := map[string]string{
		"oilmon_20oct.pdf": "2026-10-20", "oilmon_13oct.pdf": "2026-10-13",
		"oilmon_06oct.pdf": "2026-10-06", "oilmon_29sep.pdf": "2026-09-29",
		"oilmon_22sep.pdf": "2026-09-22", "oilmon_15sep.pdf": "2026-09-15",
	}
	for pdf, w := range cases {
		adj, err := ParseOilMonitor(mustLines(t, pdf))
		if err != nil {
			t.Fatalf("%s: %v", pdf, err)
		}
		if adj.AsOf != asOf[pdf] || adj.EffectiveFrom != w.from || adj.EffectiveTo != w.to || adj.MovementNo != w.no {
			t.Fatalf("%s: got %+v", pdf, adj)
		}
		check := func(name string, got, wantR *Range) {
			t.Helper()
			if wantR == nil {
				if got != nil {
					t.Fatalf("%s %s: got %+v, want nil", pdf, name, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("%s %s: got nil, want %+v", pdf, name, wantR)
			}
			if *got != *wantR {
				t.Fatalf("%s %s: got %+v, want %+v", pdf, name, got, wantR)
			}
		}
		_ = check
		check("gasoline", adj.Gasoline, w.gas)
		check("diesel", adj.Diesel, w.die)
		check("kerosene", adj.Kerosene, w.ker)
	}
}

func TestParseNCRPumpSummary(t *testing.T) {
	sum, err := ParseNCRPumpSummary(mustLines(t, "ncr_pump_1013.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if sum.WeekFrom != "2026-10-13" || sum.WeekTo != "2026-10-19" {
		t.Fatalf("week = %s..%s", sum.WeekFrom, sum.WeekTo)
	}
	want := map[string][3]float64{
		"RON97/100": {84.83, 105.40, 98.80}, "RON95": {80.00, 102.80, 88.90},
		"RON91": {79.20, 98.07, 87.90}, "Diesel": {85.70, 96.20, 91.40},
		"Diesel Plus": {90.70, 100.17, 95.60}, "Kerosene": {115.00, 146.34, 124.40},
	}
	if len(sum.Prices) != len(want) {
		t.Fatalf("got %d prices: %+v", len(sum.Prices), sum.Prices)
	}
	for _, p := range sum.Prices {
		w, ok := want[p.Product]
		if !ok {
			t.Fatalf("unexpected product %q", p.Product)
		}
		if [3]float64{p.Low, p.High, p.Common} != w {
			t.Fatalf("%s = %v, want %v", p.Product, [3]float64{p.Low, p.High, p.Common}, w)
		}
	}

	sum2, err := ParseNCRPumpSummary(mustLines(t, "ncr_pump_1006.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if sum2.WeekFrom != "2026-10-06" || sum2.WeekTo != "2026-10-12" {
		t.Fatalf("week = %s..%s", sum2.WeekFrom, sum2.WeekTo)
	}
	want2 := map[string][3]float64{
		"RON97/100": {84.28, 105.90, 97.30}, "RON95": {79.60, 101.80, 88.40},
		"RON91": {78.60, 96.50, 88.30}, "Diesel": {83.00, 99.40, 93.70},
		"Diesel Plus": {90.24, 103.17, 98.60}, "Kerosene": {115.00, 144.57, 122.70},
	}
	for _, p := range sum2.Prices {
		if w := want2[p.Product]; [3]float64{p.Low, p.High, p.Common} != w {
			t.Fatalf("%s = %v, want %v", p.Product, [3]float64{p.Low, p.High, p.Common}, w)
		}
	}
}

func TestParseNCRLPG(t *testing.T) {
	oct, err := ParseNCRLPG(mustLines(t, "ncr_lpg_oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if oct.PeriodFrom != "2026-10-01" || oct.PeriodTo != "2026-10-08" {
		t.Fatalf("period = %s..%s", oct.PeriodFrom, oct.PeriodTo)
	}
	if oct.Low != 1268 || oct.High != 1710 || oct.Common != 1430 || oct.AutoLPG != 58.40 {
		t.Fatalf("summary = %+v", oct)
	}
	if oct.CityLow != 1368 || oct.CityHigh != 1569 || oct.CityCommon != nil {
		t.Fatalf("city = %+v", oct)
	}
	if oct.City == "" {
		t.Fatal("city name empty")
	}

	sep, err := ParseNCRLPG(mustLines(t, "ncr_lpg_sep.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if sep.PeriodFrom != "2026-09-01" || sep.PeriodTo != "2026-09-07" {
		t.Fatalf("period = %s..%s", sep.PeriodFrom, sep.PeriodTo)
	}
	if sep.Low != 1025 || sep.High != 1535 || sep.Common != 1341 {
		t.Fatalf("summary = %+v", sep)
	}
	if sep.CityLow != 1185 || sep.CityHigh != 1393 || sep.CityCommon != nil {
		t.Fatalf("city = %+v", sep)
	}
}
