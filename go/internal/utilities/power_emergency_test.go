package utilities

import (
	"strings"
	"testing"
	"time"
)

// meralcoXFixtureAlert builds the 2026-10-19 emergency alert from the
// testdata inbox (ParseMeralcoXEvents + BuildMeralcoXAlerts with
// now=2026-10-19T10:00+08:00).
func meralcoXFixtureAlert(t *testing.T) PowerAlert {
	t.Helper()
	in := mustLoadMeralcoXInbox(t)
	now := manila(t, "2026-10-19T10:00:00+08:00")
	evs := ParseMeralcoXEvents(in, now)
	if len(evs) != 1 {
		t.Fatalf("events = %+v, want 1", evs)
	}
	alerts := BuildMeralcoXAlerts(&MeralcoXStatus{
		CheckedAt: "2026-10-19T10:00:00+08:00",
		Events:    evs,
	}, nil, now)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %+v, want 1", alerts)
	}
	return alerts[0]
}

func meralcoXSnap(a PowerAlert) *Snapshot {
	return &Snapshot{PowerAlerts: []PowerAlert{a}}
}

const meralcoXWantEstimated = "⚡ Biglaang brownout sa ilang bahagi ng Las Piñas\n" +
	"📅 Lun Okt 19, 8:30 AM · tinatayang maibabalik 11:30 AM\n" +
	"📍 Ilang barangay — tingnan ang abiso ng Meralco\n" +
	"🔧 Dahilan: biglaang aberya sa linya\n" +
	"Ihanda ang gadgets at flashlight.\n" +
	"━━━━━━━━━━\n" +
	"🇬🇧 Emergency power outage in parts of Las Piñas\n" +
	"Mon Oct 19, 8:30 AM, est. restoration 11:30 AM · Some barangays — see the Meralco advisory · Reason: emergency line trouble\n" +
	"Charge gadgets and prepare flashlights.\n" +
	"━━━━━━━━━━\n" +
	"🇯🇵 ラスピニャスの一部で緊急停電\n" +
	"10/19（月）08:30〜復旧見込み11:30・一部の地区（詳細は告知をご確認ください）・理由：緊急の送電線トラブル\n" +
	"端末の充電と懐中電灯の準備を。\n" +
	"━━━━━━━━━━\n" +
	"Details: https://x.com/meralco/status/2111996138043957361\n" +
	"Source: Meralco on X (@meralco) · Hindi opisyal na Meralco page"

const meralcoXWantOpenEnded = "⚡ Biglaang brownout sa ilang bahagi ng Las Piñas\n" +
	"📅 Lun Okt 19, 8:30 AM hanggang may abiso\n" +
	"📍 Ilang barangay — tingnan ang abiso ng Meralco\n" +
	"🔧 Dahilan: biglaang aberya sa linya\n" +
	"Ihanda ang gadgets at flashlight.\n" +
	"━━━━━━━━━━\n" +
	"🇬🇧 Emergency power outage in parts of Las Piñas\n" +
	"Mon Oct 19, 8:30 AM, until further notice · Some barangays — see the Meralco advisory · Reason: emergency line trouble\n" +
	"Charge gadgets and prepare flashlights.\n" +
	"━━━━━━━━━━\n" +
	"🇯🇵 ラスピニャスの一部で緊急停電\n" +
	"10/19（月）08:30〜追って通知があるまで・一部の地区（詳細は告知をご確認ください）・理由：緊急の送電線トラブル\n" +
	"端末の充電と懐中電灯の準備を。\n" +
	"━━━━━━━━━━\n" +
	"Details: https://x.com/meralco/status/2111996138043957361\n" +
	"Source: Meralco on X (@meralco) · Hindi opisyal na Meralco page"

func TestRenderPowerEmergencyEstimated(t *testing.T) {
	a := meralcoXFixtureAlert(t)
	if a.Kind != "emergency" {
		t.Fatalf("kind = %q", a.Kind)
	}
	if got := RenderPowerAlertPost(a, false); got != meralcoXWantEstimated {
		t.Fatalf("caption mismatch:\ngot:\n%s\nwant:\n%s", got, meralcoXWantEstimated)
	}
	if problems := CheckPost(meralcoXWantEstimated, meralcoXSnap(a), "power"); len(problems) != 0 {
		t.Fatalf("check problems: %v", problems)
	}
}

func TestRenderPowerEmergencyOpenEnded(t *testing.T) {
	a := meralcoXFixtureAlert(t)
	a.OpenEnded = true
	// Zero the end: open-ended carries no end time.
	a.End = time.Time{}
	a.Windows = nil
	a.EstimatedEnd = false
	if got := RenderPowerAlertPost(a, false); got != meralcoXWantOpenEnded {
		t.Fatalf("open-ended caption mismatch:\ngot:\n%s\nwant:\n%s", got, meralcoXWantOpenEnded)
	}
	if problems := CheckPost(meralcoXWantOpenEnded, meralcoXSnap(a), "power"); len(problems) != 0 {
		t.Fatalf("open-ended check: %v", problems)
	}
}

func TestCheckPowerEmergencyTampered(t *testing.T) {
	a := meralcoXFixtureAlert(t)
	text := RenderPowerAlertPost(a, false)
	snap := meralcoXSnap(a)
	cases := []struct {
		name string
		bad  string
	}{
		{"time", strings.Replace(text, "8:30 AM", "9:30 AM", 1)},
		{"source", strings.Replace(text, "\nSource: Meralco on X (@meralco) · Hindi opisyal na Meralco page", "", 1)},
		{"details", strings.Replace(text, "Details: https://", "Details: https://evil.example/", 1)},
		{"number", text + " 9999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if problems := CheckPost(tc.bad, snap, "power"); len(problems) == 0 {
				t.Fatal("tampered caption passed")
			}
		})
	}
	// The open-ended variant rejects the same tampering.
	open := a
	open.OpenEnded = true
	open.End = time.Time{}
	open.Windows = nil
	open.EstimatedEnd = false
	openText := RenderPowerAlertPost(open, false)
	openSnap := meralcoXSnap(open)
	for _, tc := range []struct {
		name string
		bad  string
	}{
		{"time", strings.Replace(openText, "8:30 AM", "9:30 AM", 1)},
		{"source", strings.Replace(openText, "\nSource: Meralco on X (@meralco) · Hindi opisyal na Meralco page", "", 1)},
		{"details", strings.Replace(openText, "Details: https://", "Details: https://evil.example/", 1)},
		{"number", openText + " 9999"},
	} {
		t.Run("open/"+tc.name, func(t *testing.T) {
			if problems := CheckPost(tc.bad, openSnap, "power"); len(problems) == 0 {
				t.Fatal("tampered open-ended caption passed")
			}
		})
	}
}
