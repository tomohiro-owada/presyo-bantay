package fbchart

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"sync"
)

// Palette sampled from ../prototype/advice/fb_post_chart.png.
var (
	cream   = color.RGBA{0xFB, 0xF6, 0xE9, 0xFF}
	green   = color.RGBA{0x1E, 0x56, 0x31, 0xFF}
	greenDk = color.RGBA{0x0E, 0x3D, 0x1C, 0xFF}
	red     = color.RGBA{0xE5, 0x33, 0x1C, 0xFF}
	redDk   = color.RGBA{0x7A, 0x1D, 0x12, 0xFF}
	redName = color.RGBA{0x8B, 0x1A, 0x1A, 0xFF}
	footDiv = color.RGBA{0x2E, 0x6B, 0x45, 0xFF}
	sepCol  = color.RGBA{0xD8, 0xCF, 0xB8, 0xFF}
	mutCol  = color.RGBA{0x3D, 0x6B, 0x4A, 0xFF}
	white   = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	zeroCol = color.RGBA{0x14, 0x35, 0x1F, 0xFF}
	// Light fills for the utilities history diesel bars (same hues as
	// green/red, readable on cream; inside labels use the dark hue).
	greenLt = color.RGBA{0x5C, 0x90, 0x6E, 0xFF}
	redLt   = color.RGBA{0xE8, 0x76, 0x58, 0xFF}
)

var (
	iconOnce sync.Once
	iconImg  image.Image
	iconErr  error
)

func decodeIcon() (image.Image, error) {
	iconOnce.Do(func() {
		iconImg, iconErr = png.Decode(bytes.NewReader(iconPNG))
	})
	return iconImg, iconErr
}

// Icon returns the embedded ~96px basket icon.
func Icon() image.Image {
	im, err := decodeIcon()
	if err != nil {
		return nil
	}
	return im
}

func fillRect(dst *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	r := image.Rect(x0, y0, x1, y1).Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.Set(x, y, c)
		}
	}
}

// fillRounded fills a rounded rectangle (radius rad) with c.
func fillRounded(dst *image.RGBA, x0, y0, x1, y1, rad int, c color.Color) {
	r := image.Rect(x0, y0, x1, y1).Intersect(dst.Bounds())
	if r.Empty() {
		return
	}
	rr := float64(rad)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dx := 0.0
			if x < x0+rad {
				dx = float64(x0 + rad - x)
			} else if x >= x1-rad {
				dx = float64(x - (x1 - rad - 1))
			}
			dy := 0.0
			if y < y0+rad {
				dy = float64(y0 + rad - y)
			} else if y >= y1-rad {
				dy = float64(y - (y1 - rad - 1))
			}
			if dx == 0 || dy == 0 || dx*dx+dy*dy <= rr*rr {
				dst.Set(x, y, c)
			}
		}
	}
}

func hline(dst *image.RGBA, x0, x1, y, w int, c color.Color) {
	fillRect(dst, x0, y-w/2, x1, y+w/2+1, c)
}

func vline(dst *image.RGBA, x, y0, y1, w int, c color.Color) {
	fillRect(dst, x-w/2, y0, x+w/2+1, y1, c)
}
