package fbchart

import (
	"image"
	"image/color"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

func bikeOutput(fresh bool, hourly *grabfare.HourlyOut) *grabfare.Output {
	o := grabOutput(nil, false, "flat")
	b := &grabfare.BikeOut{Label: "Motor taxi (Move It)", Fresh: fresh, Hourly: hourly}
	if fresh {
		b.Min, b.Max = f64(147), f64(147)
		b.AsOfHHMM = sp("12:29")
	}
	b.Text.Tl.Asof = "Walang bagong Move It quote sa nakaraang 90 minuto"
	o.Bike = b
	return o
}

func bikeHourly() *grabfare.HourlyOut {
	return &grabfare.HourlyOut{
		TodayDate: "2026-11-02", YesterdayDate: "2026-11-01",
		Today:     []grabfare.HourPoint{{H: 9, Min: 140, Max: 140}, {H: 12, Min: 147, Max: 147}},
		Yesterday: []grabfare.HourPoint{{H: 9, Min: 150, Max: 150}, {H: 12, Min: 155, Max: 155}},
	}
}

func TestRenderGrabFareWithBike(t *testing.T) {
	maps := []image.Image{mapTile(color.RGBA{0x12, 0x9A, 0xEE, 0xFF})}
	base, err := RenderGrabFare(grabOutput(nil, false, "flat"), maps, false)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]*grabfare.Output{
		"fresh with chart":      bikeOutput(true, bikeHourly()),
		"fresh single dot":      bikeOutput(true, &grabfare.HourlyOut{Today: []grabfare.HourPoint{{H: 12, Min: 147, Max: 147}}}),
		"fresh no chart":        bikeOutput(true, nil),
		"stale dash with chart": bikeOutput(false, bikeHourly()),
		"stale dash no chart":   bikeOutput(false, nil),
		"stale empty hourly":    bikeOutput(false, &grabfare.HourlyOut{}),
	}
	for name, o := range cases {
		img, err := RenderGrabFare(o, maps, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if img.Bounds().Dx() != W {
			t.Errorf("%s: width %d", name, img.Bounds().Dx())
		}
		if img.Bounds().Dy() < base.Bounds().Dy() {
			t.Errorf("%s: card did not grow for the bike block (%d < %d)", name, img.Bounds().Dy(), base.Bounds().Dy())
		}
	}
}

func TestRenderGrabFareBikeCarriedForward(t *testing.T) {
	// stale-by-age readings keep their price on the card and carried points draw as line only
	h := &grabfare.HourlyOut{Today: []grabfare.HourPoint{
		{H: 12, Min: 147, Max: 147},
		{H: 13, Min: 147, Max: 147, Carried: true},
		{H: 14, Min: 147, Max: 147, Carried: true},
	}}
	o := bikeOutput(false, h)
	o.Bike.Min, o.Bike.Max = f64(147), f64(147)
	o.Bike.AsOfHHMM = sp("12:29")
	o.Bike.Text.Tl.Asof = "Huling Move It quote: 12:29 (oras sa Maynila)"
	img, err := RenderGrabFare(o, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	dots := bikeOutput(true, &grabfare.HourlyOut{Today: []grabfare.HourPoint{{H: 12, Min: 147, Max: 147}}})
	if _, err := RenderGrabFare(dots, nil, false); err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W {
		t.Fatalf("width %d", img.Bounds().Dx())
	}
}

func TestRenderGrabFareNoBikeUnchanged(t *testing.T) {
	a, err := RenderGrabFare(grabOutput(nil, false, "flat"), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	o := bikeOutput(true, bikeHourly())
	o.Bike = nil
	b, err := RenderGrabFare(o, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds differ: %v vs %v", a.Bounds(), b.Bounds())
	}
}

func TestBikeChartAvailable(t *testing.T) {
	if bikeChartAvailable(nil) || bikeChartAvailable(&grabfare.HourlyOut{}) {
		t.Fatal("empty hourly must not offer a chart")
	}
	if !bikeChartAvailable(bikeHourly()) {
		t.Fatal("hourly with points must offer a chart")
	}
}

func TestBikeMapPixelsUnmodified(t *testing.T) {
	blue := color.RGBA{0x12, 0x9A, 0xEE, 0xFF}
	img, err := RenderGrabFare(bikeOutput(true, bikeHourly()), []image.Image{mapTile(blue)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := countColor(img, blue); n < 100000 {
		t.Fatalf("map pixels altered or missing: %d", n)
	}
}
