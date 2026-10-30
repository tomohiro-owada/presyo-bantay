package fbchart

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/digest"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
)

// digestLabels are the short region column headers on the digest card.
var digestLabels = map[string]string{
	"south": "South", "manila": "Maynila", "qc": "QC", "north": "CAMANAVA", "east": "East",
}

func digestLabel(id, fallback string) string {
	if s, ok := digestLabels[id]; ok {
		return s
	}
	return fallback
}

var (
	rowTint = color.RGBA{0xF3, 0xEC, 0xD8, 0xFF}
	bestBg  = color.RGBA{0xDD, 0xEB, 0xDF, 0xFF}
)

// fillTriangle fills an up (up=true) or down triangle of the given width
// centred horizontally on cx, with its vertical middle at cy.
func fillTriangle(dst *image.RGBA, cx, cy, w, h int, up bool, c color.Color) {
	x0, x1 := cx-w/2, cx+w/2
	y0, y1 := cy-h/2, cy+h/2
	for y := y0; y <= y1; y++ {
		t := float64(y-y0) / float64(h) // 0 top .. 1 bottom
		var half float64
		if up {
			half = float64(w) / 2 * t
		} else {
			half = float64(w) / 2 * (1 - t)
		}
		for x := x0; x <= x1; x++ {
			if float64(x-cx) >= -half && float64(x-cx) <= half {
				dst.Set(x, y, c)
			}
		}
	}
}

// RenderDigest draws the 1080x1350 multi-region digest card: key staples
// (rows) by included region (columns), each cell a median price and a
// trend triangle vs the region's previous report. Price rises are red,
// falls green, flat grey.
func RenderDigest(d *digest.Digest) (*image.RGBA, error) {
	var cols []digest.RegionStatus
	for _, r := range d.Regions {
		if r.Included {
			cols = append(cols, r)
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("digest: no included regions")
	}
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(dst, 0, 0, W, H, cream)
	dt := fbregion.FormatDates(d.AsOf)

	// Header (same as the regional chart).
	icon := Icon()
	headText := "Presyo Bantay"
	headSize := 44.0
	tw := measure(headText, SemiBold, headSize)
	rowW := tw
	if icon != nil {
		rowW = icon.Bounds().Dx() + 18 + tw
	}
	hx := (W - rowW) / 2
	hy := 28
	if icon != nil {
		draw.Draw(dst, image.Rect(hx, hy, hx+icon.Bounds().Dx(), hy+icon.Bounds().Dy()), icon, image.Point{}, draw.Over)
		hx += icon.Bounds().Dx() + 18
	}
	drawString(dst, headText, SemiBold, headSize, green, hx, hy+34+18)

	title := "Presyo sa Metro Manila"
	ts := fitSize(title, ExtraBold, 64, 28, W-2*marginL)
	drawString(dst, title, ExtraBold, ts, green, (W-measure(title, ExtraBold, ts))/2, 184)
	prev := "nakaraang ulat"
	if d.PrevAsOf != "" {
		prev = fbregion.FormatDates(d.PrevAsOf).TL
	}
	sub := fmt.Sprintf("Presyo noong %s · pagbabago vs %s · median bawat rehiyon", dt.TL, prev)
	ss := fitSize(sub, Medium, 28, 18, W-2*40)
	drawString(dst, sub, Medium, ss, green, (W-measure(sub, Medium, ss))/2, 226)

	// Table geometry.
	const tx0, tx1 = 30, 1050
	nameW := 250
	colW := (tx1 - tx0 - nameW) / len(cols)
	cellX := func(i int) int { return tx0 + nameW + i*colW + colW/2 }
	hdrY0, hdrY1 := 252, 312
	fillRounded(dst, tx0, hdrY0, tx1, hdrY1, 14, green)
	for i, c := range cols {
		lab := digestLabel(c.ID, c.TitleEN)
		sz := fitSize(lab, Bold, 28, 16, colW-12)
		drawString(dst, lab, Bold, sz, white, cellX(i)-measure(lab, Bold, sz)/2, hdrY0+40)
	}
	drawString(dst, "Bilihin", Bold, 26, white, tx0+18, hdrY0+40)

	const rowH = 96
	y := hdrY1 + 8
	rowsTop := y
	for ri := range d.Rows {
		row := &d.Rows[ri]
		if ri%2 == 0 {
			fillRect(dst, tx0, y, tx1, y+rowH, rowTint)
		}
		name := row.TL
		ns := fitSize(name, Bold, 30, 18, nameW-24)
		drawString(dst, name, Bold, ns, green, tx0+18, y+42)
		unit := "per kg"
		if row.Piece {
			unit = "per piraso"
		}
		en := row.EN + " · " + unit
		es := fitSize(en, Medium, 20, 14, nameW-24)
		drawString(dst, en, Medium, es, mutCol, tx0+18, y+74)
		for i, c := range cols {
			cx := cellX(i)
			cell := row.Cells[c.ID]
			if cell == nil {
				drawString(dst, "—", Bold, 30, grey, cx-measure("—", Bold, 30)/2, y+56)
				continue
			}
			ptxt := "₱" + fmtMoney(cell.Price)
			ps := fitSize(ptxt, Bold, 34, 20, colW-10)
			drawString(dst, ptxt, Bold, ps, green, cx-measure(ptxt, Bold, ps)/2, y+44)
			drawTrend(dst, cx, y+75, cell)
		}
		y += rowH
	}
	rowsBottom := y
	hline(dst, tx0, tx1, rowsBottom, 2, sepCol)
	_ = rowsTop

	// Best deal + legend + dropped note.
	footerTop := H - 118
	by := rowsBottom + 18
	if d.Best != nil {
		line := "Pinakamura kaysa usual: " + fbregion.TopDealTL(d.Best.Deal)
		bs := fitSize(line, SemiBold, 26, 16, W-2*tx0-36)
		fillRounded(dst, tx0, by, tx1, by+52, 12, bestBg)
		drawString(dst, line, SemiBold, bs, green, tx0+18, by+35)
		by += 52 + 12
	}
	legend := "Berde = bumaba ang presyo · Pula = tumaas · Abo = pareho (vs nakaraang ulat)"
	ls := fitSize(legend, Medium, 22, 14, W-2*tx0)
	drawString(dst, legend, Medium, ls, mutCol, tx0, by+26)
	by += 34
	if dr := d.Dropped(); len(dr) > 0 && by+30 < footerTop {
		note := "Wala pa sa digest: "
		for i, r := range dr {
			if i > 0 {
				note += ", "
			}
			note += digestLabel(r.ID, r.TitleEN)
		}
		ns := fitSize(note, Medium, 22, 14, W-2*tx0)
		drawString(dst, note, Medium, ns, redName, tx0, by+26)
	}

	// Footer band (same as the regional chart).
	fillRect(dst, 0, footerTop, W, H, greenDk)
	midY := footerTop + (H-footerTop)/2
	left1 := "Source: DA Price Monitoring"
	left2 := "Hindi opisyal na DA page"
	drawString(dst, left1, SemiBold, 24, white, marginL, midY-4)
	drawString(dst, left2, SemiBold, 24, white, marginL, midY+28)
	foot := "abalol.xyz/presyo-bantay"
	fw := measure(foot, Bold, 26)
	footX := W - marginR - fw
	drawString(dst, foot, Bold, 26, white, footX, midY+9)
	return dst, nil
}

// drawTrend draws the triangle + percent under a price, centred on cx with
// its text baseline near y+8.
func drawTrend(dst *image.RGBA, cx, y int, c *digest.Cell) {
	if c.Pct == nil {
		drawString(dst, "—", SemiBold, 22, grey, cx-measure("—", SemiBold, 22)/2, y+8)
		return
	}
	var col color.RGBA
	txt := ""
	p := *c.Pct
	switch c.Dir {
	case digest.DirUp:
		col, txt = red, fmt.Sprintf("+%d%%", p)
	case digest.DirDown:
		col, txt = green, fmt.Sprintf("−%d%%", -p)
	default:
		col, txt = grey, "0%"
	}
	const tri = 22
	w := measure(txt, SemiBold, 24)
	total := tri + 8 + w
	x := cx - total/2
	switch c.Dir {
	case digest.DirUp:
		fillTriangle(dst, x+tri/2, y, tri, tri-4, true, col)
	case digest.DirDown:
		fillTriangle(dst, x+tri/2, y, tri, tri-4, false, col)
	default:
		fillRect(dst, x, y-2, x+tri, y+3, col)
	}
	drawString(dst, txt, SemiBold, 24, col, x+tri+8, y+8)
}
