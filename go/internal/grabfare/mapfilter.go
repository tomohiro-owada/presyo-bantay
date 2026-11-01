package grabfare

import (
	"bytes"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/webp"
)

// HasDeviceMarker reports whether the Android "my location" blue dot is
// visible in img.
func HasDeviceMarker(img image.Image) bool {
	if img == nil {
		return false
	}
	b := img.Bounds()
	count := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			r8 := int(r >> 8)
			g8 := int(g >> 8)
			b8 := int(bb >> 8)
			if math.Abs(float64(r8-66)) < 25 && math.Abs(float64(g8-133)) < 25 && math.Abs(float64(b8-244)) < 20 {
				count++
			}
		}
	}
	return count >= 150
}

// PickMaps chooses up to 3 clean map kinds in route order.
func PickMaps(clean map[string]image.Image) []string {
	chosen := []string{}
	available := []string{}
	for _, k := range []string{"map1", "map2", "map3", "map4"} {
		if _, ok := clean[k]; ok {
			available = append(available, k)
		}
	}
	switch len(available) {
	case 0, 1, 2, 3:
		chosen = append(chosen, available...)
	case 4:
		chosen = append(chosen, "map1", "map2", "map4")
	}
	if len(chosen) < 2 {
		if _, ok := clean["map"]; ok {
			chosen = append(chosen, "map")
		}
	}
	if len(chosen) > 3 {
		chosen = chosen[:3]
	}
	return chosen
}

// DecodeWebp decodes a WebP image to RGBA. golang.org/x/image/webp returns
// lossy images as *image.YCbCr in studio swing (Y 16..235), but image's own
// YCbCr->RGB conversion assumes full range, which would darken and wash out
// the screenshot; this converts with the BT.601 studio-swing matrix.
func DecodeWebp(data []byte) (image.Image, error) {
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	yc, ok := img.(*image.YCbCr)
	if !ok {
		return img, nil
	}
	b := yc.Bounds()
	out := image.NewRGBA(b)
	clamp := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			yi := yc.YOffset(x, y)
			ci := yc.COffset(x, y)
			yy := 1.164 * (float64(yc.Y[yi]) - 16)
			cb := float64(yc.Cb[ci]) - 128
			cr := float64(yc.Cr[ci]) - 128
			out.SetRGBA(x, y, color.RGBA{
				R: clamp(yy + 1.596*cr),
				G: clamp(yy - 0.392*cb - 0.813*cr),
				B: clamp(yy + 2.017*cb),
				A: 255,
			})
		}
	}
	return out, nil
}
