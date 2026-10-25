// Package regionrender is the single shared core used by both `presyo
// fbpost` (live Facebook posting) and `presyo daypages` (static daily
// report pages): region signals -> template text -> checker report ->
// chart image. Both commands must publish exactly the same numbers,
// text and chart pixels; the only differences are downstream (fbpost
// uploads to Facebook, daypages writes static files with a compact PNG
// encoder).
package regionrender

import (
	"image"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Render builds the template post text, runs the checker on it and
// renders the chart image for region signals. It performs no I/O.
func Render(sig *signals.RegionSignals) (text string, img *image.RGBA, rep *fbregion.Report, err error) {
	text = fbregion.RenderPost(sig)
	rep = fbregion.CheckPost(sig, text)
	img, err = fbchart.Render(sig)
	if err != nil {
		return "", nil, nil, err
	}
	return text, img, rep, nil
}
