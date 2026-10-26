package fbchart

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

func waterAlert(t *testing.T) *utilities.WaterAlert {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "utilities", "testdata", "maynilad_maintenance_posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := utilities.ParseMaintenancePosts(b, "Las Pi")
	if err != nil {
		t.Fatal(err)
	}
	alerts := utilities.BuildWaterAlerts(&utilities.WaterStatus{City: "Las Piñas", Maintenance: rows})
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts", len(alerts))
	}
	return &alerts[0]
}

func TestWaterSize(t *testing.T) {
	img, err := RenderWaterAlert(waterAlert(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %v, want 1080x1350", img.Bounds())
	}
	if _, err := RenderWaterAlert(nil, false); err == nil {
		t.Error("expected error for nil alert")
	}
}

func TestWaterDeterministic(t *testing.T) {
	a := waterAlert(t)
	var first []byte
	for i := 0; i < 2; i++ {
		img, err := RenderWaterAlert(a, true)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := EncodeCompact(&buf, img); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = buf.Bytes()
		} else if !bytes.Equal(first, buf.Bytes()) {
			t.Error("renders differ")
		}
	}
}

// TestWaterBanner requires the red band only with pastSample.
func TestWaterBanner(t *testing.T) {
	plain, err := RenderWaterAlert(waterAlert(t), false)
	if err != nil {
		t.Fatal(err)
	}
	past, err := RenderWaterAlert(waterAlert(t), true)
	if err != nil {
		t.Fatal(err)
	}
	redCount := func(img *image.RGBA) int {
		n := 0
		for y := 0; y < 76; y++ {
			for x := 0; x < W; x += 4 {
				if img.RGBAAt(x, y) == red {
					n++
				}
			}
		}
		return n
	}
	if n := redCount(plain); n != 0 {
		t.Errorf("banner without pastSample: %d red px", n)
	}
	if n := redCount(past); n == 0 {
		t.Error("no banner with pastSample")
	}
}

// TestWaterLinesFit checks every wrapped line fits its width, and
// the type sizes meet their minimums (window 48, areas 34, reason 32).
func TestWaterLinesFit(t *testing.T) {
	a := waterAlert(t)
	const cardW = W - 2*marginL - 60
	win, wsize := waterWindowLines(a, cardW-48)
	if wsize < waterWindowMin {
		t.Errorf("window size %g below minimum", wsize)
	}
	for _, ln := range win {
		if cmeasure(ln, ExtraBold, wsize) > cardW-48 {
			t.Errorf("window line overflows: %q", ln)
		}
	}
	als, asz := wrapLines("Almanza Uno, Pilar, Talon Uno, Talon Dos, Talon Singko", Medium, waterAreasSize, waterAreasMin, cardW, 8)
	if asz < waterAreasMin {
		t.Errorf("areas size %g below minimum", asz)
	}
	for _, ln := range als {
		if cmeasure(ln, Medium, asz) > cardW {
			t.Errorf("area line overflows: %q", ln)
		}
	}
	rl := "Dahilan: " + utilities.AlertReason(a.ReasonKey, 0)
	rs := cfitSize(rl, Medium, waterReasonSize, waterReasonMin, cardW)
	if rs < waterReasonMin {
		t.Errorf("reason size %g below minimum", rs)
	}
	// Advice pill + detalye fit the card.
	pillW := cmeasure("Mag-ipon ng tubig bago magsimula.", Bold, waterPillSize) + 56
	if pillW > cardW {
		t.Errorf("pill overflows: %d > %d", pillW, cardW)
	}
	if cmeasure(waterDetalye, Medium, waterDetSize) > cardW {
		t.Errorf("detalye overflows")
	}
	_ = image.Rect
}
