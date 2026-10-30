package utilities

import (
	"strings"
	"testing"
	"time"
)

// openEndedInbox builds a minimal X inbox: one emergency post with no
// parsable end time, one scheduled image-only post with no window, and
// one emergency post with a parsed resumption end.
func openEndedInbox(posted time.Time) *XInbox {
	mk := func(id string, at time.Time, text string) XPost {
		return XPost{ID: id, CreatedAt: at, Text: text, URL: "https://x.com/maynilad/status/" + id}
	}
	return &XInbox{
		Version: 1, Account: "maynilad", AccountID: "2992779308",
		FetchedAt: posted,
		Posts: []XPost{
			mk("open1", posted, "The low pressure to no water supply you may be experiencing in portions of Las Piñas City is due to emergency works at PAGCOR Pumping Station. We are deploying mobile water tankers."),
			mk("sched1", posted, "Maynilad regularly carries out maintenance activities to keep our water distribution system in the West Zone safe, reliable, and in good condition."),
			mk("withend1", posted, "The low pressure to no water supply you may be experiencing in portions of Las Piñas City is due to emergency works at PAGCOR Pumping Station. Estimated time of water resumption is 2:00 pm of October 16, 2026."),
		},
	}
}

func TestParseXOpenEnded(t *testing.T) {
	posted := time.Date(2026, 10, 16, 9, 51, 22, 0, manilaLoc)
	in := openEndedInbox(posted)
	now := posted.Add(2 * time.Hour)
	advs := ParseXAdvisories(in, "Las Piñas", now)
	byID := map[string]XAdvisory{}
	for _, a := range advs {
		byID[a.PostID] = a
	}
	// Emergency without an end: kept open-ended.
	open, ok := byID["open1"]
	if !ok {
		t.Fatalf("open-ended emergency dropped: %+v", advs)
	}
	if !open.OpenEnded || !open.End.IsZero() {
		t.Fatalf("open-ended flags wrong: %+v", open)
	}
	if !open.Start.Equal(posted) {
		t.Fatalf("open-ended start = %v, want post created_at %v", open.Start, posted)
	}
	if open.Kind != "emergency" {
		t.Fatalf("kind = %q, want emergency", open.Kind)
	}
	// Scheduled without a window: stays dropped.
	if _, ok := byID["sched1"]; ok {
		t.Fatal("scheduled advisory without a window was kept")
	}
	// Emergency with a parsed end: normal, not open-ended.
	ended, ok := byID["withend1"]
	if !ok {
		t.Fatalf("emergency with end dropped: %+v", advs)
	}
	if ended.OpenEnded || ended.End.IsZero() {
		t.Fatalf("parsed-end advisory wrong: %+v", ended)
	}
	// After 24h the open-ended advisory is no longer due; the
	// parsed-end one (resumption 2pm) is still returned before then
	// and dropped after its end.
	if got := ParseXAdvisories(in, "Las Piñas", posted.Add(25*time.Hour)); len(got) != 0 {
		t.Fatalf("stale advisories returned: %+v", got)
	}
}

func TestParseXOpenEndedRestoration(t *testing.T) {
	posted := time.Date(2026, 10, 16, 9, 51, 22, 0, manilaLoc)
	now := posted.Add(2 * time.Hour)
	for _, tc := range []struct {
		name string
		text string
		want bool // true = one open-ended advisory expected
	}{
		{
			name: "active emergency without time",
			text: "The low pressure to no water supply you may be experiencing in portions of Las Piñas City is due to emergency works at PAGCOR Pumping Station. We are deploying mobile water tankers.",
			want: true,
		},
		{
			name: "restored",
			text: "The low pressure in portions of Las Piñas City due to emergency works at PAGCOR Pumping Station has been restored. Thank you for your patience.",
			want: false,
		},
		{
			name: "has resumed",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Water supply has resumed as of this morning.",
			want: false,
		},
		{
			name: "have resumed",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Services have resumed normally.",
			want: false,
		},
		{
			name: "back to normal",
			text: "Low pressure in portions of Las Piñas City due to emergency works. Supply is back to normal.",
			want: false,
		},
		{
			name: "normal na",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Normal na ang supply ng tubig.",
			want: false,
		},
		{
			name: "naibalik",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Naibalik na ang supply.",
			want: false,
		},
		{
			name: "bumalik na",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Bumalik na ang supply ng tubig.",
			want: false,
		},
		{
			name: "tapos na",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Tapos na ang pagkukumpuni.",
			want: false,
		},
		{
			name: "lifted",
			text: "Low pressure in portions of Las Piñas City due to emergency works. The advisory is now lifted.",
			want: false,
		},
		{
			name: "balik-normal",
			text: "Emergency works in portions of Las Piñas City caused low pressure. Balik-normal na ang supply.",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &XInbox{
				Version: 1, Account: "maynilad", AccountID: "2992779308",
				FetchedAt: posted,
				Posts:     []XPost{{ID: "p1", CreatedAt: posted, Text: tc.text, URL: "https://x.com/maynilad/status/p1"}},
			}
			got := ParseXAdvisories(in, "Las Piñas", now)
			if tc.want && len(got) != 1 {
				t.Fatalf("want 1 advisory, got %d: %+v", len(got), got)
			}
			if !tc.want && len(got) != 0 {
				t.Fatalf("want no advisory, got %+v", got)
			}
			if tc.want && !got[0].OpenEnded {
				t.Fatalf("advisory not open-ended: %+v", got[0])
			}
		})
	}
}

func TestOpenEndedAlertBuildCaptionCheck(t *testing.T) {
	posted := time.Date(2026, 10, 16, 9, 51, 22, 0, manilaLoc)
	in := openEndedInbox(posted)
	now := posted.Add(2 * time.Hour)
	advs := ParseXAdvisories(in, "Las Piñas", now)
	w := &WaterStatus{City: "Las Piñas", CheckedAt: now.Format(time.RFC3339), Advisories: advs}
	alerts := BuildWaterAlerts(w)
	var open *WaterAlert
	for i := range alerts {
		if alerts[i].OpenEnded {
			open = &alerts[i]
		}
	}
	if open == nil {
		t.Fatalf("no open-ended alert built: %+v", alerts)
	}
	if len(open.Areas) != 1 || open.Areas[0] != XAreasTL {
		t.Fatalf("areas = %q", open.Areas)
	}
	text := RenderWaterAlertPost(*open, false)
	for _, want := range []string{
		"hanggang may abiso",
		"until further notice",
		"追って通知があるまで",
		"📍 " + XAreasTL,
		"Details: https://x.com/maynilad/status/open1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("caption missing %q:\n%s", want, text)
		}
	}
	snap := &Snapshot{Water: w, Alerts: []WaterAlert{*open}}
	if problems := CheckPost(text, snap, "water"); len(problems) != 0 {
		t.Fatalf("problems: %v\n%s", problems, text)
	}
	// Tampering with the open-ended wording fails the check.
	bad := strings.Replace(text, "until further notice", "soon", 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("tampered open-ended wording passed")
	}
	// Tampering with the start time fails the check.
	bad = strings.Replace(text, "9:51 AM", "10:51 AM", 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("tampered open-ended start passed")
	}
	// AlertWindowTL yields the chart pair, not raw text.
	win := AlertWindowTL(open)
	if len(win) != 2 || !strings.Contains(win[1], "hanggang may abiso") {
		t.Fatalf("window lines = %q", win)
	}
}
