package utilities

import (
	"os"
	"strings"
	"testing"
	"time"
)

// powerOct7 builds the Oct 7 Las Piñas alert from the city215 fixture.
func powerOct7(t *testing.T) PowerAlert {
	t.Helper()
	b := readPowerFixture(t, "meralco_ms/city215_p0.html")
	pg, err := ParseMeralcoMaintenance(b)
	if err != nil {
		t.Fatal(err)
	}
	p := &PowerStatus{CheckedAt: "2026-10-29T12:00:00+08:00", Items: pg.Items, HistoryFrom: "2026-09-01"}
	alerts := BuildPowerAlerts(p, time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC))
	for _, a := range alerts {
		if a.ID == "e8535cbefb82" {
			return a
		}
	}
	t.Fatal("Oct 7 alert missing")
	return PowerAlert{}
}

func powerSnap(a PowerAlert) *Snapshot {
	return &Snapshot{PowerAlerts: []PowerAlert{a}}
}

func TestRenderPowerAlertPostOct7(t *testing.T) {
	a := powerOct7(t)
	got := RenderPowerAlertPost(a, false)
	for _, want := range []string{
		"⚡ Walang kuryente sa ilang bahagi ng Las Piñas",
		"📅 Miy Okt 7, 12:01 AM hanggang 12:30 AM, at 4:30 AM hanggang 5:00 AM",
		"📍 Almanza I, Almanza II · Alabang-Zapote Rd, Investment Dr",
		"🔧 Dahilan: maintenance ng substation",
		"Ihanda ang gadgets at flashlight.",
		"🇬🇧 Power interruption in parts of Las Piñas",
		"every night", // must NOT appear for a non-daily alert
		"Details: https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/october-7-2026-las-pinas-city-almanza-i-and-almanza-ii",
		"Source: Meralco maintenance schedule · Hindi opisyal na Meralco page",
		"substation maintenance",
		"変電所メンテナンス",
	} {
		if want == "every night" {
			if strings.Contains(got, want) {
				t.Fatalf("non-daily caption contains %q:\n%s", want, got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Fatalf("caption missing %q:\n%s", want, got)
		}
	}
	if problems := CheckPost(got, powerSnap(a), "power"); len(problems) != 0 {
		t.Fatalf("check problems: %v", problems)
	}
	// Separators: 4 blocks, no nested parens, no markdown, no $.
	parts := strings.Split(got, Separator)
	if len(parts) != 4 {
		t.Fatalf("blocks = %d, want 4", len(parts))
	}
	for _, ln := range strings.Split(got, "\n") {
		if strings.Contains(ln, "$") || strings.Contains(ln, "NaN") {
			t.Fatalf("lint hit in %q", ln)
		}
		if nestedParens(ln) {
			t.Fatalf("nested parens in %q", ln)
		}
	}
}

func TestRenderPowerAlertDaily(t *testing.T) {
	start := time.Date(2026, 10, 29, 21, 0, 0, 0, manilaLoc)
	var wins []TimeWindow
	for d := 29; d <= 30; d++ {
		s := time.Date(2026, 10, d, 21, 0, 0, 0, manilaLoc)
		e := time.Date(2026, 10, d, 23, 59, 0, 0, manilaLoc)
		_ = start
		wins = append(wins, TimeWindow{Start: s, End: e})
	}
	a := PowerAlert{
		ID: "dailytest001", City: "Las Piñas",
		Start: wins[0].Start, End: wins[len(wins)-1].End, Windows: wins,
		Daily: true, DailyFrom: "2026-10-29", DailyTo: "2026-10-30",
		Barangays: []string{"Almanza I"}, Roads: []string{"Alabang-Zapote Road"},
		ReasonKey: PowerReasonLine, SourceURL: "https://example.invalid/d",
	}
	text := RenderPowerAlertPost(a, false)
	for _, want := range []string{"bawat gabi", "every night", "毎晩", "9:00 PM to 11:59 PM", "Okt 29"} {
		if !strings.Contains(text, want) {
			t.Fatalf("daily caption missing %q:\n%s", want, text)
		}
	}
	if problems := CheckPost(text, powerSnap(a), "power"); len(problems) != 0 {
		t.Fatalf("daily check: %v", problems)
	}
}

func TestPowerTrendLine(t *testing.T) {
	mk := func(id, date string, h1, m1, h2, m2 int) PowerItem {
		d, _ := time.Parse("2006-01-02", date)
		return PowerItem{
			ID: id, URL: "https://example.invalid/" + id, Title: id,
			Cities: []string{"Las Piñas"}, DateFrom: date, DateTo: date,
			Blocks: []PowerBlock{{Circuit: "X 1AA", Windows: []TimeWindow{{
				Start: time.Date(d.Year(), d.Month(), d.Day(), h1, m1, 0, 0, manilaLoc),
				End:   time.Date(d.Year(), d.Month(), d.Day(), h2, m2, 0, 0, manilaLoc),
			}}}},
			ReasonKey: PowerReasonLine,
		}
	}
	p := &PowerStatus{
		CheckedAt: "2026-10-29T12:00:00+08:00", HistoryFrom: "2026-09-01",
		Items: []PowerItem{
			mk("oct5", "2026-10-05", 10, 0, 12, 0),
			mk("oct7a", "2026-10-07", 0, 1, 0, 30),
			mk("oct7b", "2026-10-07", 4, 30, 5, 0),
			mk("sep9", "2026-09-09", 9, 0, 11, 0),
		},
	}
	alerts := BuildPowerAlerts(p, time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC))
	var second *PowerAlert
	for i := range alerts {
		if alerts[i].ID == "oct7a" {
			second = &alerts[i]
		}
	}
	if second == nil {
		t.Fatal("oct7a missing")
	}
	if second.NthThisMonth != 2 {
		t.Fatalf("N = %d, want 2", second.NthThisMonth)
	}
	text := RenderPowerAlertPost(*second, false)
	for _, want := range []string{
		"📊 Ika-2 na planadong brownout sa Las Piñas ngayong Oktubre, 1 noong Setyembre",
		"📊 2nd planned brownout in Las Piñas this October, 1 last September",
		"📊 今月ラスピニャスで2回目の計画停電（先月1回）",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("trend missing %q:\n%s", want, text)
		}
	}
	if problems := CheckPost(text, powerSnap(*second), "power"); len(problems) != 0 {
		t.Fatalf("trend check: %v", problems)
	}
	// A lone alert shows the 1st-this-month trend line.
	single := PowerAlert{
		ID: "s1", City: "Las Piñas",
		Start:     time.Date(2026, 10, 5, 10, 0, 0, 0, manilaLoc),
		End:       time.Date(2026, 10, 5, 12, 0, 0, 0, manilaLoc),
		Barangays: []string{"Almanza I"}, ReasonKey: PowerReasonLine,
		SourceURL: "https://example.invalid/s1", NthThisMonth: 1,
	}
	t1 := RenderPowerAlertPost(single, false)
	for _, want := range []string{
		"📊 Ika-1 na planadong brownout sa Las Piñas ngayong Oktubre",
		"📊 1st planned brownout in Las Piñas this October",
		"📊 今月ラスピニャスで1回目の計画停電",
	} {
		if !strings.Contains(t1, want) {
			t.Fatalf("1st trend missing %q:\n%s", want, t1)
		}
	}
	if problems := CheckPost(t1, powerSnap(single), "power"); len(problems) != 0 {
		t.Fatalf("1st trend check: %v", problems)
	}
}

func TestPowerPastSample(t *testing.T) {
	a := powerOct7(t)
	got := RenderPowerAlertPost(a, true)
	first := "⚠️ SAMPLE — lumang datos / past data, hindi kasalukuyang abiso"
	if !strings.HasPrefix(got, first+"\n") {
		t.Fatalf("no sample banner: %q", got[:80])
	}
	if n := strings.Count(got, first); n != 3 {
		t.Fatalf("banner in %d blocks, want 3", n)
	}
	if problems := CheckPost(got, powerSnap(a), "power"); len(problems) != 0 {
		t.Fatalf("sample check: %v", problems)
	}
}

func TestCheckPowerTampered(t *testing.T) {
	a := powerOct7(t)
	text := RenderPowerAlertPost(a, false)
	snap := powerSnap(a)
	cases := []struct {
		name string
		bad  string
	}{
		{"time", strings.Replace(text, "12:01 AM", "12:02 AM", 1)},
		{"day", strings.Replace(text, "Okt 7", "Okt 8", 1)},
		{"area", strings.Replace(text, "Almanza II", "Almanza III", 1)},
		{"road", strings.Replace(text, "Alabang-Zapote Rd", "Alabang-Zapote Ave", 1)},
		{"reason", strings.Replace(text, "substation maintenance", "tree trimming", 1)},
		{"trend unexpected", text + "\n📊 Ika-9 na planadong brownout sa Las Piñas ngayong Oktubre"},
		{"details", strings.Replace(text, "Details: https://", "Details: https://evil.example/", 1)},
		{"number", strings.Replace(text, "12:01 AM", "12:01 AM 9999", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if problems := CheckPost(tc.bad, snap, "power"); len(problems) == 0 {
				t.Fatal("tampered caption passed")
			}
		})
	}
	// No alert in snapshot.
	if problems := CheckPost(text, &Snapshot{}, "power"); len(problems) == 0 {
		t.Fatal("missing alert passed")
	}
	// Unknown kind still rejected.
	if problems := CheckPost(text, snap, "bogus"); len(problems) == 0 {
		t.Fatal("bogus kind passed")
	}
}

func readPowerFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(testdata(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
