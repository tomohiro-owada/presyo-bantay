package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

func utilTestdata(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "internal", "utilities", "testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture %s missing: %v", name, err)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

// renderTestSnapshot builds a snapshot from the saved fixtures
// (parse the testdata files directly; no network) and writes it to
// a temp file.
func renderTestSnapshot(t *testing.T) string {
	t.Helper()
	adjLines, err := utilities.PDFLines(utilTestdata(t, "oilmon_20oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	adj, err := utilities.ParseOilMonitor(adjLines)
	if err != nil {
		t.Fatal(err)
	}
	pumpLines, err := utilities.PDFLines(utilTestdata(t, "ncr_pump_1013.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	pump, err := utilities.ParseNCRPumpSummary(pumpLines)
	if err != nil {
		t.Fatal(err)
	}
	lpgLines, err := utilities.PDFLines(utilTestdata(t, "ncr_lpg_oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	lpg, err := utilities.ParseNCRLPG(lpgLines)
	if err != nil {
		t.Fatal(err)
	}
	lpg2Lines, err := utilities.PDFLines(utilTestdata(t, "ncr_lpg_sep.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	lpg2, err := utilities.ParseNCRLPG(lpg2Lines)
	if err != nil {
		t.Fatal(err)
	}
	merHTML, err := os.ReadFile(utilTestdata(t, "meralco_2026_10.html"))
	if err != nil {
		t.Fatal(err)
	}
	mer, err := utilities.ParseMeralcoRelease(merHTML)
	if err != nil {
		t.Fatal(err)
	}
	snap := utilities.Snapshot{
		FetchedAt: "2026-10-26T20:00:00+08:00",
		Fuel: utilities.FuelStatus{
			Adjustments: []utilities.OilAdjustment{adj},
			NCRPump:     []utilities.PumpSummary{pump},
		},
		LPG:     []utilities.LPGMonth{lpg, lpg2},
		Meralco: []utilities.MeralcoRate{mer},
		Water: &utilities.WaterStatus{
			City:      "Las Piñas",
			CheckedAt: "2026-10-26T20:00:00+08:00",
		},
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUtilitiesRenderFuel(t *testing.T) {
	snap := renderTestSnapshot(t)
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-snapshot", snap, "-kind", "fuel", "-out-dir", out}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, name := range []string{"fuel.txt", "fuel.png", "fuel.check.json"} {
		if fi, err := os.Stat(filepath.Join(out, name)); err != nil || fi.Size() == 0 {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	checkData, err := os.ReadFile(filepath.Join(out, "fuel.check.json"))
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
	txt, err := os.ReadFile(filepath.Join(out, "fuel.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(txt), "Presyo ng langis") {
		t.Fatalf("bad fuel.txt: %q", txt[:100])
	}
}

func TestUtilitiesRenderBills(t *testing.T) {
	snap := renderTestSnapshot(t)
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-snapshot", snap, "-kind", "bills", "-out-dir", out}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	checkData, err := os.ReadFile(filepath.Join(out, "bills.check.json"))
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
}

// TestUtilitiesRenderCheckFails renders a snapshot whose own data trips
// the checker (nested parentheses inside an interruption reason):
// artifacts are still written, exit code is 3.
func TestUtilitiesRenderCheckFails(t *testing.T) {
	snapPath := renderTestSnapshot(t)
	data, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap utilities.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	snap.Water = &utilities.WaterStatus{
		City:      "Las Piñas",
		CheckedAt: "2026-10-26T20:00:00+08:00",
		Scheduled: []utilities.Interruption{{
			Kind: "scheduled", City: "Las Piñas",
			Barangays: "Brgy. Almanza Uno",
			From:      "2026-10-27", FromRaw: "October 27, 2026",
			Time: "10:00 PM to 6:00 AM", Reason: "Repair (Phase 2 (Urgent))",
		}},
	}
	tampered, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-snapshot", badPath, "-kind", "bills", "-out-dir", out}); code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
	for _, name := range []string{"bills.txt", "bills.png", "bills.check.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	checkData, err := os.ReadFile(filepath.Join(out, "bills.check.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc utilitiesCheckDoc
	if err := json.Unmarshal(checkData, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OK {
		t.Fatal("check unexpectedly ok")
	}
}

func TestUtilitiesRenderUsage(t *testing.T) {
	if code := run([]string{"utilities"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if code := run([]string{"utilities", "bogus"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if code := run([]string{"utilities", "render", "-kind", "fuel"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}
