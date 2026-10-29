package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func powerFixtureDir(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "internal", "utilities", "testdata", "meralco_ms")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture dir missing: %v", err)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

// TestUtilitiesRenderPowerOffline builds a power sample from fixtures
// via -meralco-ms-dir (no network, no snapshot).
func TestUtilitiesRenderPowerOffline(t *testing.T) {
	out := t.TempDir()
	args := []string{"utilities", "render", "-kind", "power",
		"-meralco-ms-dir", powerFixtureDir(t),
		"-now", "2026-10-06T12:00:00+08:00",
		"-out-dir", out}
	if code := run(args); code != 0 {
		t.Fatalf("exit %d", code)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var prefix string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") {
			prefix = strings.TrimSuffix(e.Name(), ".txt")
		}
	}
	if prefix == "" || !strings.HasPrefix(prefix, "power-") {
		t.Fatalf("power files missing: %v", entries)
	}
	for _, ext := range []string{".txt", ".png", ".check.json"} {
		if fi, err := os.Stat(filepath.Join(out, prefix+ext)); err != nil || fi.Size() == 0 {
			t.Fatalf("%s%s missing: %v", prefix, ext, err)
		}
	}
	checkData, err := os.ReadFile(filepath.Join(out, prefix+".check.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc utilitiesCheckDoc
	if err := json.Unmarshal(checkData, &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.OK {
		t.Fatalf("check failed: %v", doc.Problems)
	}
	txt, err := os.ReadFile(filepath.Join(out, prefix+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(txt), "Walang kuryente sa ilang bahagi ng Las Piñas") {
		t.Fatalf("bad power.txt: %.100q", string(txt))
	}
}

// TestUtilitiesRenderPowerPastSample checks the PAST-SAMPLE prefix,
// banner and red-band PNG via fixtures.
func TestUtilitiesRenderPowerPastSample(t *testing.T) {
	out := t.TempDir()
	args := []string{"utilities", "render", "-kind", "power",
		"-meralco-ms-dir", powerFixtureDir(t),
		"-now", "2026-10-06T12:00:00+08:00",
		"-past-sample", "-out-dir", out}
	if code := run(args); code != 0 {
		t.Fatalf("exit %d", code)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "power-alert_PAST-SAMPLE_") && strings.HasSuffix(e.Name(), ".txt") {
			found = true
			txt, _ := os.ReadFile(filepath.Join(out, e.Name()))
			if !strings.HasPrefix(string(txt), "⚠️ SAMPLE — lumang datos") {
				t.Fatalf("no sample banner: %.80q", string(txt))
			}
		}
	}
	if !found {
		t.Fatalf("past-sample files missing: %v", entries)
	}
}

// TestUtilitiesRenderPowerAlertID checks -alert-id selection and unknown id.
func TestUtilitiesRenderPowerAlertID(t *testing.T) {
	dir := powerFixtureDir(t)
	// Discover one real id via offline fetch.
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-kind", "power",
		"-meralco-ms-dir", dir, "-now", "2026-10-06T12:00:00+08:00", "-out-dir", out}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	entries, _ := os.ReadDir(out)
	var id string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") && strings.HasPrefix(e.Name(), "power-") {
			id = strings.TrimSuffix(strings.TrimPrefix(e.Name(), "power-"), ".txt")
		}
	}
	if id == "" {
		t.Fatal("no power id")
	}
	out2 := t.TempDir()
	if code := run([]string{"utilities", "render", "-kind", "power",
		"-meralco-ms-dir", dir, "-now", "2026-10-06T12:00:00+08:00",
		"-alert-id", id, "-out-dir", out2}); code != 0 {
		t.Fatalf("alert-id exit %d", code)
	}
	if code := run([]string{"utilities", "render", "-kind", "power",
		"-meralco-ms-dir", dir, "-alert-id", "bogus", "-out-dir", t.TempDir()}); code != 1 {
		t.Fatalf("bogus id exit %d, want 1", code)
	}
}
