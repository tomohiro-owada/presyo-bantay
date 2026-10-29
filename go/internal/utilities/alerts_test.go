package utilities

import (
	"regexp"
	"strings"
	"testing"
)

// sampleAlert builds the Aug-2026 Las Piñas maintenance alert from the
// saved fixture (no network).
func sampleAlert(t *testing.T) WaterAlert {
	t.Helper()
	b := readFixture(t, "maynilad_maintenance_posts.json")
	rows, err := ParseMaintenancePosts(b, "Las Pi")
	if err != nil {
		t.Fatal(err)
	}
	alerts := BuildWaterAlerts(&WaterStatus{City: "Las Piñas", Maintenance: rows})
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts", len(alerts))
	}
	return alerts[0]
}

func TestRenderWaterAlertPost(t *testing.T) {
	a := sampleAlert(t)
	got := RenderWaterAlertPost(a, false)
	want := `💧 Walang tubig sa ilang bahagi ng Las Piñas
📅 Miy Ago 26, 10:00 PM hanggang Huw Ago 27, 1:00 PM
📍 Almanza Uno, Pilar, Talon Uno, Talon Dos, Talon Singko
🔧 Dahilan: maintenance ng pasilidad
📊 Ika-1 na water interruption sa Las Piñas ngayong Agosto
Mag-ipon ng tubig bago magsimula.
━━━━━━━━━━
🇬🇧 Water interruption in parts of Las Piñas
Wed Aug 26, 10:00 PM to Thu Aug 27, 1:00 PM · Almanza Uno, Pilar, Talon Uno, Talon Dos, Talon Singko · Reason: facility maintenance. Store water beforehand.
📊 1st water interruption in Las Piñas this August
━━━━━━━━━━
🇯🇵 ラスピニャスの一部で断水
8/26（水）22:00〜8/27（木）13:00・Almanza Uno、Pilar、Talon Uno、Talon Dos、Talon Singko・理由：設備メンテナンス。事前に水の確保を。
📊 今月ラスピニャスで1回目の断水
━━━━━━━━━━
Details: https://www.mayniladwater.com.ph/maintenance-activities-for-the-upcoming-week-146/
Source: Maynilad advisory · Hindi opisyal na Maynilad page`
	if got != want {
		t.Fatalf("alert post mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderWaterAlertPastSample(t *testing.T) {
	a := sampleAlert(t)
	got := RenderWaterAlertPost(a, true)
	first := "⚠️ SAMPLE — lumang datos / past data, hindi kasalukuyang abiso"
	if !strings.HasPrefix(got, first+"\n") {
		t.Fatalf("missing sample banner: %q", got[:120])
	}
	if n := strings.Count(got, first); n != 3 {
		t.Fatalf("banner in %d blocks, want 3", n)
	}
}

func TestBuildWaterAlerts(t *testing.T) {
	a := sampleAlert(t)
	if len(a.Areas) != 5 || a.Areas[0] != "Almanza Uno" || a.Areas[4] != "Talon Singko" {
		t.Fatalf("areas = %q", a.Areas)
	}
	if a.ReasonKey != "facility" || a.Kind != "maintenance" || a.City != "Las Piñas" {
		t.Fatalf("alert = %+v", a)
	}
	if a.Start.Format("2006-01-02 15:04") != "2026-08-26 22:00" || a.End.Format("2006-01-02 15:04") != "2026-08-27 13:00" {
		t.Fatalf("window = %v..%v", a.Start, a.End)
	}
	// Stable 12-hex ID.
	again := sampleAlert(t)
	if a.ID != again.ID {
		t.Fatal("unstable alert ID")
	}
	if len(a.ID) != 12 {
		t.Fatalf("id = %q", a.ID)
	}
	if _, err := regexp.Compile(`^[0-9a-f]{12}$`); err != nil {
		t.Fatal(err)
	}
	matched, _ := regexp.MatchString(`^[0-9a-f]{12}$`, a.ID)
	if !matched {
		t.Fatalf("id = %q", a.ID)
	}

	// Interruption rows: areas split, times parsed, own wording.
	qcb := readFixture(t, "maynilad_emergency_qc.json")
	rows, err := ParseInterruptions(qcb, "emergency")
	if err != nil {
		t.Fatal(err)
	}
	w := &WaterStatus{City: "Quezon City", Emergency: rows}
	alerts := BuildWaterAlerts(w)
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts", len(alerts))
	}
	qa := alerts[0]
	if len(qa.Areas) != 2 || qa.Areas[0] != "Brgys. Sauyo" || qa.Areas[1] != "Talipapa" {
		t.Fatalf("areas = %q", qa.Areas)
	}
	if qa.ReasonKey != "repair" {
		t.Fatalf("reason = %q", qa.ReasonKey)
	}
	if qa.Start.Format("2006-01-02 15:04") != "2026-10-25 22:00" || qa.End.Format("2006-01-02 15:04") != "2026-10-26 06:00" {
		t.Fatalf("window = %v..%v", qa.Start, qa.End)
	}
	if qa.SourceURL != MayniladAdvisoriesURL {
		t.Fatalf("url = %q", qa.SourceURL)
	}
	text := RenderWaterAlertPost(qa, false)
	if !strings.Contains(text, "pagkukumpuni ng linya") || !strings.Contains(text, "pipe repair") || !strings.Contains(text, "配管修理") {
		t.Fatalf("own wording missing:\n%s", text)
	}

	// Reason mapping table.
	for raw, want := range map[string]string{
		"Facility Maintenance":             "facility",
		"Network Maintenance":              "network",
		"Network Repair Activity":          "repair",
		"Network and Facility Limitations": "limited",
		"Several Maintenance Activities":   "generic",
		"something entirely different":     "generic",
	} {
		if got := reasonKeyOf(raw); got != want {
			t.Fatalf("reasonKeyOf(%q) = %q, want %q", raw, got, want)
		}
	}
}
