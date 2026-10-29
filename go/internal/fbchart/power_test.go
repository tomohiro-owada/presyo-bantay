package fbchart

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

func testManila(y, m, d, h, mi int) time.Time {
	return time.Date(y, time.Month(m), d, h, mi, 0, 0, time.FixedZone("Asia/Manila", 8*3600))
}

func powerAlert(t *testing.T) *utilities.PowerAlert {
	t.Helper()
	// Reuse the Oct 7 alert via the utilities test helper: build from
	// the city215 fixture through the public API (no network).
	// We construct it via BuildPowerAlerts on the parsed fixture.
	// To avoid duplicating fixture parsing, use a synthetic alert with
	// two windows mirroring the Oct 7 shape.
	a := utilities.PowerAlert{
		ID:        "e8535cbefb82",
		City:      "Las Piñas",
		Barangays: []string{"Almanza I", "Almanza II"},
		Roads:     []string{"Alabang-Zapote Road", "Investment Drive", "Daang Hari Road"},
		ReasonKey: utilities.PowerReasonSubstation,
		SourceURL: "https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/october-7-2026-las-pinas-city-almanza-i-and-almanza-ii",
	}
	// Windows: Oct 7 00:01-00:30 and 04:30-05:00 Manila.
	mk := func(h1, mi1, h2, mi2 int) utilities.TimeWindow {
		return utilities.TimeWindow{
			Start: testManila(2026, 10, 7, h1, mi1),
			End:   testManila(2026, 10, 7, h2, mi2),
		}
	}
	a.Windows = []utilities.TimeWindow{mk(0, 1, 0, 30), mk(4, 30, 5, 0)}
	a.Start, a.End = a.Windows[0].Start, a.Windows[1].End
	a.NthThisMonth = 2
	a.PrevMonthCount = 1
	a.HistoryComplete = true
	return &a
}

func TestPowerSize(t *testing.T) {
	img, err := RenderPowerAlert(powerAlert(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != W || img.Bounds().Dy() != H {
		t.Errorf("size = %v, want 1080x1350", img.Bounds())
	}
	if _, err := RenderPowerAlert(nil, false); err == nil {
		t.Error("expected error for nil alert")
	}
}

func TestPowerDeterministic(t *testing.T) {
	a := powerAlert(t)
	var first []byte
	for i := 0; i < 2; i++ {
		img, err := RenderPowerAlert(a, true)
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

// TestPowerBanner requires the red band only with pastSample.
func TestPowerBanner(t *testing.T) {
	plain, err := RenderPowerAlert(powerAlert(t), false)
	if err != nil {
		t.Fatal(err)
	}
	past, err := RenderPowerAlert(powerAlert(t), true)
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

// TestPowerLinesFit checks wrapped lines fit their widths and type
// sizes meet their minimums.
func TestPowerLinesFit(t *testing.T) {
	a := powerAlert(t)
	const cardW = W - 2*marginL - 60
	win, wsize := powerWindowLines(a, cardW-48)
	if wsize < powerWindowMin {
		t.Errorf("window size %g below minimum", wsize)
	}
	for _, ln := range win {
		if cmeasure(ln, ExtraBold, wsize) > cardW-48 {
			t.Errorf("window line overflows: %q", ln)
		}
	}
	als, asz := wrapLines("Almanza I, Almanza II", Medium, powerAreasSize, powerAreasMin, cardW, 6)
	if asz < powerAreasMin {
		t.Errorf("areas size %g below minimum", asz)
	}
	for _, ln := range als {
		if cmeasure(ln, Medium, asz) > cardW {
			t.Errorf("area line overflows: %q", ln)
		}
	}
	rl := "Dahilan: " + utilities.PowerReason(a.ReasonKey, 0)
	rs := cfitSize(rl, Medium, powerReasonSize, powerReasonMin, cardW)
	if rs < powerReasonMin {
		t.Errorf("reason size %g below minimum", rs)
	}
	_ = image.Rect
}
