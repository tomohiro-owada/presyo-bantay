package fbchart

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

// TestEncodeCompactDeterministic encodes the same chart twice and
// requires byte-identical output.
func TestEncodeCompactDeterministic(t *testing.T) {
	img, err := Render(testSignals())
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := EncodeCompact(&a, img); err != nil {
		t.Fatal(err)
	}
	if err := EncodeCompact(&b, img); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("EncodeCompact output is not deterministic")
	}
}

// TestEncodeCompactSmaller requires the compact encoding to stay below
// the default RGBA encoding size for the same chart.
func TestEncodeCompactSmaller(t *testing.T) {
	img, err := Render(testSignals())
	if err != nil {
		t.Fatal(err)
	}
	var compact, rgba bytes.Buffer
	if err := EncodeCompact(&compact, img); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&rgba, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("compact=%d rgba=%d", compact.Len(), rgba.Len())
	if compact.Len() >= rgba.Len() {
		t.Errorf("compact=%d not below rgba=%d", compact.Len(), rgba.Len())
	}
}

// TestEncodeCompactNearLossless decodes the compact PNG and requires
// nearly every pixel to be within a tiny squared-RGB distance of the
// original (flat fills and text stay exact) and every pixel within a
// small bound (only rare decorative-icon blend pixels remap, and only
// to a nearby colour). The chart must stay crisp/readable.
func TestEncodeCompactNearLossless(t *testing.T) {
	img, err := Render(testSignals())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := EncodeCompact(&buf, img); err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Bounds() != img.Bounds() {
		t.Fatalf("bounds = %v, want %v", dec.Bounds(), img.Bounds())
	}
	const (
		tinyD2   = 1000  // flat fills / glyph interiors stay ~exact
		maxD2    = 12000 // every pixel, incl. rare icon blends, stays near
		tinyFrac = 0.998 // fraction of pixels required within tinyD2
	)
	total, tiny, worst := 0, 0, 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			a := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			c := color.NRGBAModel.Convert(dec.At(x, y)).(color.NRGBA)
			dr := int(a.R) - int(c.R)
			dg := int(a.G) - int(c.G)
			db := int(a.B) - int(c.B)
			d2 := dr*dr + dg*dg + db*db
			total++
			if d2 <= tinyD2 {
				tiny++
			}
			if d2 > worst {
				worst = d2
			}
		}
	}
	t.Logf("tiny=%d/%d worst d2=%d", tiny, total, worst)
	if float64(tiny)/float64(total) < tinyFrac {
		t.Errorf("only %d/%d pixels within d2<=%d, want >= %.3f", tiny, total, tinyD2, tinyFrac)
	}
	if worst > maxD2 {
		t.Errorf("worst d2 = %d, want <= %d", worst, maxD2)
	}
}
