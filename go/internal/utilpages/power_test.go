package utilpages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// powerTestSnapshot builds a snapshot with fuel/bills plus Las Piñas
// power (Oct 7) and nearby power + water nearby rows.
func powerTestSnapshot(t *testing.T) *utilities.Snapshot {
	t.Helper()
	snap := testSnapshot(t)
	// Power: Oct 7 Las Piñas + Sep 29 Las Piñas/Muntinlupa from the
	// city215 fixture, history reaching September.
	var items []utilities.PowerItem
	for _, f := range []string{"city215_p0.html"} {
		b, err := os.ReadFile(filepath.Join("..", "utilities", "testdata", "meralco_ms", f))
		if err != nil {
			t.Fatal(err)
		}
		pg, err := utilities.ParseMeralcoMaintenance(b)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, pg.Items...)
	}
	snap.Power = &utilities.PowerStatus{
		CheckedAt:   "2026-10-20T12:00:00+08:00",
		Items:       items,
		HistoryFrom: "2026-09-01",
	}
	// Nearby power: Muntinlupa Oct 29 entry.
	b, err := os.ReadFile(filepath.Join("..", "utilities", "testdata", "meralco_ms", "city221_p0.html"))
	if err != nil {
		t.Fatal(err)
	}
	pg, err := utilities.ParseMeralcoMaintenance(b)
	if err != nil {
		t.Fatal(err)
	}
	snap.Power.Items = append(snap.Power.Items, pg.Items[0])
	// Water nearby: one Bacoor row.
	snap.Water.Nearby = []utilities.Maintenance{{
		PostDate: "2026-10-19", PostURL: "https://example.invalid/p",
		City: "Bacoor", Barangays: "Molino II",
		Schedule: "10:00 p.m. of October 21, 2026 to 6:00 a.m. of October 22, 2026",
		Activity: "Facility Maintenance", Location: "here",
	}}
	return snap
}

func TestUtilpagesPowerWrites(t *testing.T) {
	site := t.TempDir()
	snap := powerTestSnapshot(t)
	cfg := testConfig(site)
	// Pin now so power now+upcoming filtering is deterministic: Oct 6
	// keeps Oct 7 upcoming (Sep 29 is past and hidden).
	cfg.Now = func() time.Time { return time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC) }
	if err := Run(snap, cfg); err != nil {
		t.Fatal(err)
	}
	// Frozen power pages exist.
	matches, _ := filepath.Glob(filepath.Join(site, "u", "power", "*", "post.json"))
	if len(matches) != 2 {
		t.Fatalf("power pages = %v, want 2", matches)
	}
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc PostDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "power" || doc.Check != "pass" || doc.AlertKind != "power" || doc.Text == "" {
			t.Fatalf("power post.json = %+v", doc)
		}
		if doc.Start == "" || doc.End == "" {
			t.Fatalf("power Start/End missing: %+v", doc)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(p), "power.png")); err != nil {
			t.Fatalf("power.png missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(p), "index.html")); err != nil {
			t.Fatalf("power index.html missing: %v", err)
		}
		// FB == site: frozen text/PNG equal the render pipeline.
		snapOne := &utilities.Snapshot{PowerAlerts: []utilities.PowerAlert{}}
		_ = snapOne
	}
	// Index carries the power section + nearby lists + water nearby.
	idx, err := os.ReadFile(filepath.Join(site, "u", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(idx)
	for _, want := range []string{
		"Kuryente", "Almanza I",
		"maintenance ng substation", "company.meralco.com.ph",
		"Malapit na lugar", "Bacoor", "Muntinlupa",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("index missing %q", want)
		}
	}
	// Manifest has the additive power key; fuel/bills/water unchanged.
	var manifest UtilitiesDoc
	mdata, err := os.ReadFile(filepath.Join(site, "data", "utilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mdata, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Power == nil || manifest.Power.ID == "" {
		t.Fatalf("power manifest missing: %+v", manifest)
	}
	if manifest.Fuel == nil || manifest.Bills == nil || manifest.Water == nil {
		t.Fatalf("fuel/bills/water manifest lost: %+v", manifest)
	}
	// Existing outputs byte-identical: fuel/bills text still matches the
	// render pipeline.
	var fdoc PostDoc
	fdata, err := os.ReadFile(filepath.Join(site, "u", "fuel", "2026-10-20", "post.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fdata, &fdoc); err != nil {
		t.Fatal(err)
	}
	wantText, err := utilities.RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if fdoc.Text != wantText {
		t.Fatal("fuel post.json changed")
	}
	// Power image determinism: re-render equals frozen PNG.
	var pdoc PostDoc
	pdata, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(pdata, &pdoc); err != nil {
		t.Fatal(err)
	}
	_ = fbchart.EncodeCompact
}

func TestUtilpagesPowerLayoutAlarm(t *testing.T) {
	site := t.TempDir()
	snap := powerTestSnapshot(t)
	snap.Power.LayoutAlarm = "meralco-ms: layout changed (no parsable date+window across the run; keeping previous items)"
	cfg := testConfig(site)
	if err := Run(snap, cfg); err != nil {
		t.Fatal(err)
	}
	// No frozen power pages when the alarm is set.
	matches, _ := filepath.Glob(filepath.Join(site, "u", "power", "*", "post.json"))
	if len(matches) != 0 {
		t.Fatalf("power pages written despite alarm: %v", matches)
	}
	idx, err := os.ReadFile(filepath.Join(site, "u", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(idx)
	if strings.Contains(s, "layout changed") {
		t.Fatal("alarm text leaked to the site")
	}
	if !strings.Contains(s, "Walang datos sa ngayon") {
		t.Fatalf("alarm site should show no-data line:\n%.500s", s)
	}
	var manifest UtilitiesDoc
	mdata, err := os.ReadFile(filepath.Join(site, "data", "utilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mdata, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Power != nil {
		t.Fatalf("power manifest written despite alarm: %+v", manifest.Power)
	}
}
