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
	fcdaB, err := os.ReadFile(utilTestdata(t, "maynilad_fcda_posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	rates, err := utilities.ParseFCDAPosts(fcdaB)
	if err != nil {
		t.Fatal(err)
	}
	snap := utilities.Snapshot{
		FetchedAt: "2026-10-26T20:00:00+08:00",
		Fuel: utilities.FuelStatus{
			Adjustments: []utilities.OilAdjustment{adj},
			NCRPump:     []utilities.PumpSummary{pump},
		},
		LPG:        []utilities.LPGMonth{lpg, lpg2},
		Meralco:    []utilities.MeralcoRate{mer},
		WaterRates: rates,
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

func TestUtilitiesRenderWaterOffline(t *testing.T) {
	maint := utilTestdata(t, "maynilad_maintenance_posts.json")
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-kind", "water",
		"-maintenance-json", maint, "-city", "Las Pi", "-out-dir", out}); code != 0 {
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
	if prefix == "" || !strings.HasPrefix(prefix, "water-") {
		t.Fatalf("water files missing: %v", entries)
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
	if !strings.Contains(string(txt), "Walang tubig sa ilang bahagi ng Las Pi") {
		t.Fatalf("bad water.txt: %q", txt[:100])
	}
}

func TestUtilitiesRenderWaterPastSample(t *testing.T) {
	maint := utilTestdata(t, "maynilad_maintenance_posts.json")
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-kind", "water",
		"-maintenance-json", maint, "-city", "Las Pi", "-past-sample", "-out-dir", out}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	// Prefix water-alert_PAST-SAMPLE_<start date> (2026-08-26 fixture).
	want := "water-alert_PAST-SAMPLE_2026-08-26"
	for _, ext := range []string{".txt", ".png", ".check.json"} {
		if _, err := os.Stat(filepath.Join(out, want+ext)); err != nil {
			t.Fatalf("%s%s missing: %v", want, ext, err)
		}
	}
	txt, err := os.ReadFile(filepath.Join(out, want+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(txt), "⚠️ SAMPLE — lumang datos") {
		t.Fatalf("no sample banner: %q", txt[:80])
	}
}

func TestUtilitiesRenderWaterAlertID(t *testing.T) {
	maint := utilTestdata(t, "maynilad_maintenance_posts.json")
	// Unknown alert id fails without network.
	out := t.TempDir()
	if code := run([]string{"utilities", "render", "-kind", "water",
		"-maintenance-json", maint, "-city", "Las Pi", "-alert-id", "bogus",
		"-out-dir", out}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	// City with no rows fails offline.
	if code := run([]string{"utilities", "render", "-kind", "water",
		"-maintenance-json", maint, "-city", "Nowhere",
		"-out-dir", t.TempDir()}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestUtilitiesFetchSectionsValidation(t *testing.T) {
	// No network touched: validation fails first.
	if code := run([]string{"utilities", "fetch", "-cache", t.TempDir(), "-out", filepath.Join(t.TempDir(), "s.json"), "-sections", "bogus"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

func TestUtilpagesCommand(t *testing.T) {
	snap := renderTestSnapshot(t)
	site := t.TempDir()
	if code := run([]string{"utilpages", "-snapshot", snap, "-site-public", site, "-now", "2026-10-26T12:00:00Z"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, name := range []string{
		"u/index.html", "data/utilities.json",
		"u/fuel/2026-10-20/index.html", "u/fuel/2026-10-20/post.json", "u/fuel/2026-10-20/fuel.png",
		"u/bills/2026-10/index.html", "u/bills/2026-10/post.json", "u/bills/2026-10/bills.png",
	} {
		if fi, err := os.Stat(filepath.Join(site, name)); err != nil || fi.Size() == 0 {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	if code := run([]string{"utilpages", "-snapshot", "/nonexistent.json", "-site-public", site}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if code := run([]string{"utilpages"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}
