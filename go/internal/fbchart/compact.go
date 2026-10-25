package fbchart

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"sort"
)

// EncodeCompact writes img as a paletted PNG with png.BestCompression.
// The palette is the exact image histogram when it holds at most 256
// colours, else the 256 most frequent colours (ties broken by colour
// value, deterministic); every other pixel maps to its nearest palette
// colour by squared RGB distance (memoised). No dithering. Output is
// byte-deterministic for a given input image.
func EncodeCompact(w io.Writer, img image.Image) error {
	b := img.Bounds()
	counts := map[uint32]int{}
	keys := make([]uint32, 0, b.Dx()*b.Dy())
	pix := make([]uint32, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			k := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
			if _, ok := counts[k]; !ok {
				keys = append(keys, k)
			}
			counts[k]++
			pix = append(pix, k)
		}
	}
	var pal []uint32
	if len(keys) <= 256 {
		pal = append([]uint32(nil), keys...)
		sort.Slice(pal, func(i, j int) bool { return pal[i] < pal[j] })
	} else {
		pal = append([]uint32(nil), keys...)
		sort.Slice(pal, func(i, j int) bool {
			if counts[pal[i]] != counts[pal[j]] {
				return counts[pal[i]] > counts[pal[j]]
			}
			return pal[i] < pal[j]
		})
		pal = pal[:256]
	}
	palette := make(color.Palette, len(pal))
	index := make(map[uint32]uint8, len(pal))
	for i, k := range pal {
		palette[i] = color.RGBA{R: uint8(k >> 16), G: uint8(k >> 8), B: uint8(k), A: 0xFF}
		index[k] = uint8(i)
	}
	nearest := func(k uint32) uint8 {
		kr, kg, kb := int(k>>16)&0xFF, int(k>>8)&0xFF, int(k)&0xFF
		best, bestD := 0, -1
		for i, p := range pal {
			pr, pg, pb := int(p>>16)&0xFF, int(p>>8)&0xFF, int(p)&0xFF
			dr, dg, db := kr-pr, kg-pg, kb-pb
			d := dr*dr + dg*dg + db*db
			if bestD < 0 || d < bestD {
				best, bestD = i, d
			}
		}
		return uint8(best)
	}
	memo := map[uint32]uint8{}
	dst := image.NewPaletted(b, palette)
	pos := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			k := pix[pos]
			pos++
			i, ok := index[k]
			if !ok {
				if m, hit := memo[k]; hit {
					i = m
				} else {
					i = nearest(k)
					memo[k] = i
				}
			}
			dst.SetColorIndex(x, y, i)
		}
	}
	return (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(w, dst)
}
