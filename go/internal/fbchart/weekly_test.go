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
