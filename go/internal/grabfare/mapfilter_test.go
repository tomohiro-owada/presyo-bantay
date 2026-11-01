package grabfare

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"testing"
)

func loadFixture(t *testing.T, name string) image.Image {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	img, err := DecodeWebp(b)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestDecodeWebpColoursAreStudioSwingCorrect(t *testing.T) {
	img := loadFixture(t, "clean.webp")
	r, g, b, _ := img.At(20, 20).RGBA()
	if r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Errorf("white decoded as %d,%d,%d (studio-swing conversion broken)", r>>8, g>>8, b>>8)
	}
	r, g, b, _ = img.At(150, 100).RGBA()
	if int(r>>8) > 12 || abs8(int(g>>8)-170) > 12 || abs8(int(b>>8)-80) > 14 {
		t.Errorf("green decoded as %d,%d,%d, want ~0,170,80", r>>8, g>>8, b>>8)
	}
}

func abs8(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestHasDeviceMarkerRealCodec(t *testing.T) {
	if HasDeviceMarker(loadFixture(t, "clean.webp")) {
		t.Error("clean image flagged")
	}
	if !HasDeviceMarker(loadFixture(t, "marker.webp")) {
		t.Error("my-location dot not flagged")
	}
	if HasDeviceMarker(loadFixture(t, "pin.webp")) {
		t.Error("route pin blue must not be flagged")
	}
}

func disc(w, h, cx, cy, r int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
				img.Set(x, y, c)
			}
		}
	}
	return img
}

func TestHasDeviceMarkerSynthetic(t *testing.T) {
	if !HasDeviceMarker(disc(300, 300, 150, 150, 12, color.RGBA{66, 133, 244, 255})) {
		t.Error("blue dot radius 12 should be flagged")
	}
	if HasDeviceMarker(disc(300, 300, 150, 150, 12, color.RGBA{0, 140, 255, 255})) {
		t.Error("pin blue should not be flagged")
	}
	if HasDeviceMarker(disc(300, 300, 150, 150, 3, color.RGBA{66, 133, 244, 255})) {
		t.Error("tiny speck should not be flagged")
	}
	if HasDeviceMarker(nil) {
		t.Error("nil image")
	}
}

func TestPickMaps(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	mk := func(ks ...string) map[string]image.Image {
		m := map[string]image.Image{}
		for _, k := range ks {
			m[k] = im
		}
		return m
	}
	cases := []struct {
		have []string
		want string
	}{
		{[]string{"map1", "map2", "map3", "map4", "map"}, "map1,map2,map4"},
		{[]string{"map1", "map2", "map4"}, "map1,map2,map4"},
		{[]string{"map1", "map4"}, "map1,map4"},
		{[]string{"map2", "map"}, "map2,map"},
		{[]string{"map"}, "map"},
		{nil, ""},
	}
	for _, c := range cases {
		got := ""
		for i, k := range PickMaps(mk(c.have...)) {
			if i > 0 {
				got += ","
			}
			got += k
		}
		if got != c.want {
			t.Errorf("have %v: got %q want %q", c.have, got, c.want)
		}
	}
}
