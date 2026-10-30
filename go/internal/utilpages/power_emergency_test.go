package utilpages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// emergencyNow pins Run/Rebuild near the Oct 19 event so the alert is
// current (Run skips alerts ended >24h ago).
var emergencyNow = time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)

func emergencyMeralcoX(t *testing.T) *utilities.MeralcoXStatus {
	t.Helper()
	in, err := utilities.LoadXInbox(filepath.Join("..", "utilities", "testdata", "meralco_x", "inbox-2026-10-30.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := emergencyNow.In(manilaZone())
	evs := utilities.ParseMeralcoXEvents(in, now)
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want 1", evs)
	}
	return &utilities.MeralcoXStatus{
		CheckedAt: "2026-10-19T10:00:00+08:00",
		Events:    evs,
	}
}

func emergencyConfig(site string) Config {
	return Config{
		SitePublic: site,
		BaseURL:    "https://abalol.xyz/presyo-bantay/",
		Now:        func() time.Time { return emergencyNow },
	}
}

func checkEmergencyFrozen(t *testing.T, site string, snap *utilities.Snapshot) {
	t.Helper()
	const id = "x-2111996138043957361"
	docPath := filepath.Join(site, "u", "power", id, "post.json")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("missing %s: %v", docPath, err)
	}
	var doc PostDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "power" || doc.Check != "pass" || doc.AlertKind != "emergency" {
		t.Fatalf("power post.json = %+v", doc)
	}
	if doc.Start != "2026-10-19T08:30:00+08:00" || doc.End != "2026-10-19T11:30:00+08:00" {
		t.Fatalf("Start/End = %q/%q", doc.Start, doc.End)
	}
	if doc.OpenEnded {
		t.Fatal("estimated-end event frozen as open-ended")
	}
	// Frozen text equals the render pipeline output for the alert.
	alerts := utilities.BuildMeralcoXAlerts(snap.MeralcoX, nil, emergencyNow.In(manilaZone()))
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want 1", alerts)
	}
	if want := utilities.RenderPowerAlertPost(alerts[0], false); doc.Text != want {
		t.Fatalf("frozen text != RenderPowerAlertPost:\n%s\n---\n%s", doc.Text, want)
	}
	if _, err := os.Stat(filepath.Join(site, "u", "power", id, "power.png")); err != nil {
		t.Fatalf("power.png missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(site, "u", "power", id, "index.html")); err != nil {
		t.Fatalf("power index.html missing: %v", err)
	}
	// Manifest: the emergency page is the latest power entry.
	mdata, err := os.ReadFile(filepath.Join(site, "data", "utilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest UtilitiesDoc
	if err := json.Unmarshal(mdata, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Power == nil || manifest.Power.ID != id || manifest.Power.Path != "u/power/"+id+"/" {
		t.Fatalf("power manifest = %+v", manifest.Power)
	}
	// Index links the emergency page with a date label, never the raw
	// id as the label.
	idx, err := os.ReadFile(filepath.Join(site, "u", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(idx)
	if !strings.Contains(s, `href="power/`+id+`/`) {
		t.Fatalf("index missing emergency power link")
	}
	if strings.Contains(s, ">"+id+"</a>") {
		t.Fatalf("raw emergency id used as link label")
	}
	if !strings.Contains(s, "Okt 19, 2026") {
		t.Fatalf("emergency link date label missing")
	}
	// Rebuild from the snapshot succeeds (idempotent, x- id never
	// parsed as a date).
	var genDoc struct {
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(mdata, &genDoc); err != nil {
		t.Fatal(err)
	}
	if err := Rebuild(snap, site, "https://abalol.xyz/presyo-bantay/", genDoc.GeneratedAt); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
}

func TestUtilpagesPowerEmergencyNilPower(t *testing.T) {
	site := t.TempDir()
	snap := testSnapshot(t)
	snap.Power = nil
	snap.MeralcoX = emergencyMeralcoX(t)
	if err := Run(snap, emergencyConfig(site)); err != nil {
		t.Fatal(err)
	}
	checkEmergencyFrozen(t, site, snap)
}

func TestUtilpagesPowerEmergencyLayoutAlarm(t *testing.T) {
	site := t.TempDir()
	snap := testSnapshot(t)
	snap.Power = &utilities.PowerStatus{
		CheckedAt:   "2026-10-19T10:00:00+08:00",
		LayoutAlarm: "meralco-ms: layout changed (no parsable date+window across the run; keeping previous items)",
	}
	snap.MeralcoX = emergencyMeralcoX(t)
	if err := Run(snap, emergencyConfig(site)); err != nil {
		t.Fatal(err)
	}
	// No planned pages under the alarm: only the emergency page.
	matches, _ := filepath.Glob(filepath.Join(site, "u", "power", "*", "post.json"))
	if len(matches) != 1 || !strings.Contains(matches[0], "x-2111996138043957361") {
		t.Fatalf("power pages = %v, want only the emergency alert", matches)
	}
	checkEmergencyFrozen(t, site, snap)
}
