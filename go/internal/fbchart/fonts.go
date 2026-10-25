package fbchart

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Embedded Montserrat statics (OFL, see fonts/OFL.txt) plus Noto Sans
// fallbacks (OFL, see fonts/NotoSans-OFL.txt) used per-rune when
// Montserrat lacks a glyph.
var (
	//go:embed fonts/Montserrat-ExtraBold.ttf
	montExtraBold []byte
	//go:embed fonts/Montserrat-Bold.ttf
	montBold []byte
	//go:embed fonts/Montserrat-SemiBold.ttf
	montSemiBold []byte
	//go:embed fonts/Montserrat-Medium.ttf
	montMedium []byte
	//go:embed fonts/NotoSans-Bold.ttf
	notoBold []byte
	//go:embed fonts/NotoSans-SemiBold.ttf
	notoSemiBold []byte
	//go:embed icon.png
	iconPNG []byte
)

// Weight selects a Montserrat static.
type Weight int

const (
	ExtraBold Weight = iota
	Bold
	SemiBold
	Medium
)

type weightFonts struct {
	mont *sfnt.Font // for glyph lookup
	face *opentype.Font
	fb   *opentype.Font // fallback drawing face family
	fbs  *sfnt.Font     // fallback glyph lookup
}

var (
	weights   [4]*weightFonts
	faceCache = map[string]font.Face{}
	faceMu    sync.Mutex
)

// montHas reports whether the Montserrat static for w has a glyph for r.
func montHas(w Weight, r rune) bool {
	wf := weights[w]
	var buf sfnt.Buffer
	idx, err := wf.mont.GlyphIndex(&buf, r)
	if err != nil {
		return false
	}
	return idx != 0
}

func mustParseSFNT(b []byte, name string) *sfnt.Font {
	f, err := sfnt.Parse(b)
	if err != nil {
		panic(fmt.Sprintf("fbchart: parse %s: %v", name, err))
	}
	return f
}

func mustParseOT(b []byte, name string) *opentype.Font {
	f, err := opentype.Parse(b)
	if err != nil {
		panic(fmt.Sprintf("fbchart: parse %s: %v", name, err))
	}
	return f
}

func init() {
	monts := []struct {
		w Weight
		b []byte
		n string
	}{
		{ExtraBold, montExtraBold, "Montserrat-ExtraBold"},
		{Bold, montBold, "Montserrat-Bold"},
		{SemiBold, montSemiBold, "Montserrat-SemiBold"},
		{Medium, montMedium, "Montserrat-Medium"},
	}
	fbOT := mustParseOT(notoBold, "NotoSans-Bold")
	fbOTSemi := mustParseOT(notoSemiBold, "NotoSans-SemiBold")
	fbS := mustParseSFNT(notoBold, "NotoSans-Bold")
	fbSSemi := mustParseSFNT(notoSemiBold, "NotoSans-SemiBold")
	for _, m := range monts {
		fb, fbs := fbOT, fbS
		if m.w == Medium || m.w == SemiBold {
			fb, fbs = fbOTSemi, fbSSemi
		}
		weights[m.w] = &weightFonts{
			mont: mustParseSFNT(m.b, m.n),
			face: mustParseOT(m.b, m.n),
			fb:   fb, fbs: fbs,
		}
	}
	// Check at init that Montserrat covers the currency and minus signs
	// the chart needs (₱ U+20B1, − U+2212); per-rune fallback covers the
	// rest.
	for _, r := range []rune{'₱', '−', '·', '•', '%', '+', '/', '(', ')'} {
		if !montHas(Bold, r) {
			panic(fmt.Sprintf("fbchart: Montserrat-Bold missing glyph U+%04X", r))
		}
	}
	if _, err := decodeIcon(); err != nil {
		panic(fmt.Sprintf("fbchart: decode icon: %v", err))
	}
}

// face returns the cached drawing face for w at size px (96 DPI).
func face(w Weight, size float64) font.Face {
	key := fmt.Sprintf("%d-%g", w, size)
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	f, err := opentype.NewFace(weights[w].face, &opentype.FaceOptions{
		Size: size, DPI: 96, Hinting: font.HintingFull,
	})
	if err != nil {
		panic(fmt.Sprintf("fbchart: face %v %g: %v", w, size, err))
	}
	faceCache[key] = f
	return f
}

// fbFace returns the cached fallback drawing face for w at size px.
func fbFace(w Weight, size float64) font.Face {
	key := fmt.Sprintf("fb%d-%g", w, size)
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[key]; ok {
		return f
	}
	f, err := opentype.NewFace(weights[w].fb, &opentype.FaceOptions{
		Size: size, DPI: 96, Hinting: font.HintingFull,
	})
	if err != nil {
		panic(fmt.Sprintf("fbchart: fallback face %v %g: %v", w, size, err))
	}
	faceCache[key] = f
	return f
}

// faceFor returns the drawing face for rune r: Montserrat when it has the
// glyph, else the Noto Sans fallback (per-rune fallback, no tofu).
func faceFor(w Weight, size float64, r rune) font.Face {
	if montHas(w, r) {
		return face(w, size)
	}
	return fbFace(w, size)
}

// measure returns the pixel width of s at w/size (face-switch aware).
func measure(s string, w Weight, size float64) int {
	var adv fixed.Int26_6
	var prev rune
	var prevFace font.Face
	for i, r := range s {
		f := faceFor(w, size, r)
		if i > 0 && prevFace == f {
			adv += f.Kern(prev, r)
		}
		a, _ := f.GlyphAdvance(r)
		adv += a
		prev, prevFace = r, f
	}
	return adv.Round()
}

// drawString draws s onto dst with baseline at (x, y), switching to the
// fallback face per rune when Montserrat lacks the glyph. It returns the
// pixel width drawn.
func drawString(dst *image.RGBA, s string, w Weight, size float64, col color.Color, x, y int) int {
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(col)}
	dot := fixed.P(x, y)
	var prev rune
	var prevFace font.Face
	start := dot.X
	for i, r := range s {
		f := faceFor(w, size, r)
		if i > 0 && prevFace == f {
			dot.X += f.Kern(prev, r)
		}
		d.Face = f
		d.Dot = dot
		d.DrawString(string(r))
		a, _ := f.GlyphAdvance(r)
		dot.X += a
		prev, prevFace = r, f
	}
	return (dot.X - start).Round()
}

// fitSize shrinks size until s fits maxW (down to minSize).
func fitSize(s string, w Weight, size, minSize float64, maxW int) float64 {
	for size > minSize && measure(s, w, size) > maxW {
		size -= 2
	}
	return size
}

// ellipsize truncates s with "…" (or "..." when … is missing) to maxW.
func ellipsize(s string, w Weight, size float64, maxW int) string {
	if measure(s, w, size) <= maxW {
		return s
	}
	ell := "…"
	if !montHas(w, '…') {
		ell = "..."
	}
	rs := []rune(s)
	for len(rs) > 0 && measure(string(rs)+ell, w, size) > maxW {
		rs = rs[:len(rs)-1]
	}
	return string(rs) + ell
}
