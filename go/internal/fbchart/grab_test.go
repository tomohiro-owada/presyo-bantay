package fbchart

import (
	"image"
	"image/color"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

func f64(v float64) *float64 { return &v }
func sp(s string) *string    { return &s }

func grabOutput(traffic *grabfare.TrafficOut, yesterday bool, direction string) *grabfare.Output {
	o := &grabfare.Output{
		Visible: true, State: "fresh",
		AsOf: sp("2026-11-02T08:00:00+08:00"), AsOfHHMM: sp("08:00"),
		Fare:    &grabfare.FareOut{Min: 308, Max: 370, Standard: f64(309)},
		Traffic: traffic,
		Trend:   grabfare.TrendOut{Direction: direction, DeltaPesos: f64(12), DeltaPct: f64(4), PreviousHHMM: sp("07:00")},
	}
	if direction == "none" {
		o.Trend = grabfare.TrendOut{Direction: "none"}
	}
	if yesterday {
		o.VsYesterday = &grabfare.TrendOut{Direction: "down", DeltaPesos: f64(9), DeltaPct: f64(3), PreviousHHMM: sp("08:00")}
	}
	o.Text.Tl = grabfare.LangText{Title: "Tantiya", Headline: "x", Trend: "▲ ₱12 (+4%) kumpara sa 07:00"}
	return o
}

func mapTile(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 720, 744))
	for y := 0; y < 744; y++ {
		for x := 0; x < 720; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestRenderGrabFareVariants(t *testing.T) {
	traffic := &grabfare.TrafficOut{Level: "heavy", RedPct: f64(35)}
	cases := []struct {
		name string
		o    *grabfare.Output
		maps int
		samp bool
	}{
		{"3 maps traffic yesterday", grabOutput(traffic, true, "up"), 3, false},
		{"2 maps no traffic", grabOutput(nil, false, "down"), 2, false},
		{"no maps flat", grabOutput(nil, false, "flat"), 0, false},
		{"sample none-trend", grabOutput(traffic, false, "none"), 3, true},
	}
	for _, c := range cases {
		var maps []image.Image
		for i := 0; i < c.maps; i++ {
			maps = append(maps, mapTile(color.RGBA{0x12, 0x9A, 0xEE, 0xFF}))
		}
		img, err := RenderGrabFare(c.o, maps, c.samp)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if img.Bounds().Dx() != 1080 || img.Bounds().Dy() != 1350 {
			t.Errorf("%s: size %v", c.name, img.Bounds())
		}
	}
}

func TestRenderGrabFareMapPixelsUnmodified(t *testing.T) {
	want := color.RGBA{0x12, 0x9A, 0xEE, 0xFF}
	o := grabOutput(nil, false, "up")
	img, err := RenderGrabFare(o, []image.Image{mapTile(want), mapTile(want)}, false)
	if err != nil {
		t.Fatal(err)
	}
	// The strip must contain a large block of exactly the source colour
	// (maps are scaled but never tinted/cropped/overlaid).
	n := 0
	for y := 0; y < 1350; y++ {
		for x := 0; x < 1080; x++ {
			if img.RGBAAt(x, y) == want {
				n++
			}
		}
	}
	if n < 2*250*250 {
		t.Errorf("only %d source-coloured pixels", n)
	}
	// Nothing is drawn below the footer top into the content area edges.
	if got := img.RGBAAt(5, 1350-5); got == (color.RGBA{}) {
		t.Error("footer not drawn")
	}
}

func TestRenderGrabFareRejectsHidden(t *testing.T) {
	if _, err := RenderGrabFare(nil, nil, false); err == nil {
		t.Error("nil output must error")
	}
	o := grabOutput(nil, false, "up")
	o.Visible = false
	if _, err := RenderGrabFare(o, nil, false); err == nil {
		t.Error("hidden output must error")
	}
	o = grabOutput(nil, false, "up")
	o.Fare = nil
	if _, err := RenderGrabFare(o, nil, false); err == nil {
		t.Error("no fare must error")
	}
}

func TestRenderGrabFareLongNumbersNoPanic(t *testing.T) {
	o := grabOutput(nil, true, "up")
	o.Fare = &grabfare.FareOut{Min: 1234567, Max: 7654321, Standard: f64(9999999)}
	o.Text.Tl.Trend = "▲ ₱1,234,567 (+400%) kumpara sa 07:00 na may napakahabang teksto para subukan ang pagkakasya"
	if _, err := RenderGrabFare(o, nil, true); err != nil {
		t.Fatal(err)
	}
}

func TestStripTrendGlyph(t *testing.T) {
	for in, want := range map[string]string{
		"▲ ₱12 (+4%) kumpara sa 07:00": "₱12 (+4%) kumpara sa 07:00",
		"▼ ₱5":                         "₱5",
		"＝ Walang pagbabago":           "Walang pagbabago",
		"Wala pang naunang reading":    "Wala pang naunang reading",
	} {
		if got := stripTrendGlyph(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
}
