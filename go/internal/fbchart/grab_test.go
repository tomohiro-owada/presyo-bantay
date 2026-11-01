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

func TestRenderGrabFareCutsTopStrip(t *testing.T) {
	red := color.RGBA{0xFF, 0x00, 0xFF, 0xFF} // magenta: appears nowhere else on the card
	blue := color.RGBA{0x00, 0x00, 0xFF, 0xFF}
	src := image.NewRGBA(image.Rect(0, 0, 720, 744))
	for y := 0; y < 744; y++ {
		c := blue
		if y < MapTopCutPx {
			c = red // the group-ride bar strip
		}
		for x := 0; x < 720; x++ {
			src.Set(x, y, c)
		}
	}
	before := append([]uint8(nil), src.Pix...)
	img, err := RenderGrabFare(grabOutput(nil, false, "up"), []image.Image{src, src}, false)
	if err != nil {
		t.Fatal(err)
	}
	nRed, nBlue := 0, 0
	for y := 0; y < 1350; y++ {
		for x := 0; x < 1080; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 200 && p.G < 60 && p.B > 200 {
				nRed++
			}
			if p == blue {
				nBlue++
			}
		}
	}
	if nBlue < 2*250*200 {
		t.Errorf("map body missing: %d blue px", nBlue)
	}
	if nRed > 0 {
		t.Errorf("top strip leaked into the card: %d red px", nRed)
	}
	for i := range before {
		if src.Pix[i] != before[i] {
			t.Fatal("source image was modified")
		}
	}
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func countColor(img *image.RGBA, c color.RGBA) int {
	n := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y) == c {
				n++
			}
		}
	}
	return n
}

func TestRenderGrabFareFocusBand(t *testing.T) {
	blue := color.RGBA{0x12, 0x9A, 0xEE, 0xFF}
	mag := color.RGBA{0xFF, 0x00, 0xFF, 0xFF}
	o := grabOutput(nil, false, "up")
	thumb := mapTile(blue)

	base, err := RenderGrabFare(o, []image.Image{thumb, thumb, thumb}, false)
	if err != nil {
		t.Fatal(err)
	}
	withFocus, err := RenderGrabFareFocus(o, []image.Image{thumb, thumb, thumb}, solid(720, 280, mag), false)
	if err != nil {
		t.Fatal(err)
	}
	if base.Bounds().Dy() != 1350 || withFocus.Bounds().Dx() != 1080 {
		t.Fatalf("sizes %v %v", base.Bounds(), withFocus.Bounds())
	}
	if withFocus.Bounds().Dy() < 1350 {
		t.Errorf("focus card shorter than 1350: %d", withFocus.Bounds().Dy())
	}
	// no top cut on the focus band: the full 940 x 366 area is source colour
	if n := countColor(withFocus, mag); n < 930*360 {
		t.Errorf("focus band cut or shrunk: %d px", n)
	}
	// the thumbnails keep exactly their size: same source-coloured area as without focus
	if a, b := countColor(base, blue), countColor(withFocus, blue); b != a {
		t.Errorf("thumbnail area changed: %d -> %d", a, b)
	}
	// source untouched is covered by the other test; the footer is at the bottom of the taller card
	if got := withFocus.RGBAAt(5, withFocus.Bounds().Dy()-5); got == (color.RGBA{}) {
		t.Error("footer not drawn at the bottom")
	}
}

func TestRenderGrabFareFocusOnlyAndTall(t *testing.T) {
	mag := color.RGBA{0xFF, 0x00, 0xFF, 0xFF}
	o := grabOutput(nil, false, "down")
	// focus without thumbnails fits 1350
	img, err := RenderGrabFareFocus(o, nil, solid(720, 280, mag), false)
	if err != nil || img.Bounds().Dy() != 1350 {
		t.Fatalf("focus only: %v %v", err, img.Bounds())
	}
	// a very tall "focus" is scaled down to the cap, never cropped, and centred
	img, err = RenderGrabFareFocus(o, nil, solid(720, 1000, mag), false)
	if err != nil {
		t.Fatal(err)
	}
	if n := countColor(img, mag); n < 250*350 || n > 940*380 {
		t.Errorf("tall focus area %d", n)
	}
}
