package fbchart

import (
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/digest"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
)

// testDigestDoc builds a hand-made digest with the given included regions.
func testDigestDoc(ids []string) *digest.Digest {
	inc := map[string]bool{}
	for _, id := range ids {
		inc[id] = true
	}
	var regs []digest.RegionStatus
	for _, r := range regions.Regions {
		st := digest.RegionStatus{
			ID: r.ID, TitleTL: r.TitleTL, TitleEN: r.TitleEN, TitleJA: r.TitleJA,
			Included: inc[r.ID], Markets: 3, Cells: len(digest.KeyItems),
			PrevAsOf: "2026-10-29",
		}
		if !inc[r.ID] {
			st.Reason = "test drop"
		}
		regs = append(regs, st)
	}
	var rows []digest.Row
	for i, it := range digest.KeyItems {
		cells := map[string]*digest.Cell{}
		for _, id := range ids {
			p := 100.0 + float64(i*7)
			var pct *int
			dir := digest.DirNone
			switch i % 4 {
			case 0:
				v := 3
				pct, dir = &v, digest.DirUp
			case 1:
				v := -2
				pct, dir = &v, digest.DirDown
			case 2:
				v := 0
				pct, dir = &v, digest.DirFlat
			}
			cells[id] = &digest.Cell{Price: p, Pct: pct, Dir: dir, Markets: 3}
		}
		z := 0
		rows = append(rows, digest.Row{
			Canonical: it.Canonical, TL: it.TL, EN: it.EN, JA: it.JA, Piece: it.Piece,
			Cells: cells,
			Metro: &digest.Cell{Price: 100.0 + float64(i*7), Pct: &z, Dir: digest.DirFlat, Markets: 9},
		})
	}
	return &digest.Digest{AsOf: "2026-10-30", PrevAsOf: "2026-10-29", Regions: regs, Rows: rows}
}

func TestRenderDigestSizes(t *testing.T) {
	for _, ids := range [][]string{
		{"south", "manila", "qc", "north", "east"},
		{"south", "manila", "qc"},
	} {
		img, err := RenderDigest(testDigestDoc(ids))
		if err != nil {
			t.Fatalf("ids %v: %v", ids, err)
		}
		if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
			t.Fatalf("ids %v: size = %v, want 1080x1350", ids, img.Bounds())
		}
		seen := map[[4]uint32]bool{}
		for y := 0; y < H; y += 37 {
			for x := 0; x < W; x += 41 {
				r, g, b, a := img.RGBAAt(x, y).RGBA()
				seen[[4]uint32{r, g, b, a}] = true
			}
		}
		if len(seen) < 10 {
			t.Errorf("ids %v: only %d distinct colors, want a non-uniform card", ids, len(seen))
		}
	}
}

func TestRenderDigestNoneIncluded(t *testing.T) {
	d := testDigestDoc(nil)
	if _, err := RenderDigest(d); err == nil {
		t.Error("no included regions must error")
	}
	d2 := &digest.Digest{AsOf: "2026-10-30"}
	if _, err := RenderDigest(d2); err == nil {
		t.Error("empty digest must error")
	}
}
