package utilities

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// feedRows loads one feed fixture via the detailed parser.
func feedRows(t *testing.T, name string) ([]Maintenance, int) {
	t.Helper()
	b, err := os.ReadFile(testdata(t, "maynilad_feed/"+name))
	if err != nil {
		t.Fatal(err)
	}
	rows, skipped, err := ParseMayniladFeedDetailed(b)
	if err != nil {
		t.Fatal(err)
	}
	return rows, skipped
}

func findFeedRow(rows []Maintenance, city, barangaySub string) *Maintenance {
	for i := range rows {
		if rows[i].City == city && strings.Contains(rows[i].Barangays, barangaySub) {
			return &rows[i]
		}
	}
	return nil
}

// Feed p3 posts 2026-08-21: Las Piñas Almanza Uno, Pilar, Talon Uno,
// Talon Dos, Talon Singko, 10:00 p.m. of August 26 to 1:00 p.m. of
// August 27, 2026, Facility Maintenance.
func TestFeedP3LasPinas(t *testing.T) {
	rows, skipped := feedRows(t, "feed_p3.xml")
	if skipped != 0 {
		t.Fatalf("skipped = %d", skipped)
	}
	r := findFeedRow(rows, "Las Piñas", "Almanza Uno")
	if r == nil {
		t.Fatalf("Las Piñas row missing in %d rows", len(rows))
	}
	if !strings.Contains(r.Barangays, "Talon Singko") {
		t.Fatalf("barangays = %q", r.Barangays)
	}
	if r.Schedule != "10:00 p.m. of August 26, 2026 to 1:00 p.m. of August 27, 2026" {
		t.Fatalf("schedule = %q", r.Schedule)
	}
	if r.Activity != "Facility Maintenance" {
		t.Fatalf("activity = %q", r.Activity)
	}
	if !strings.Contains(r.Location, "Marcos Alvarez") {
		t.Fatalf("location = %q", r.Location)
	}
	if r.PostDate != "2026-08-21" {
		t.Fatalf("post date = %q", r.PostDate)
	}
	if !strings.HasSuffix(r.PostURL, "maintenance-activities-for-the-upcoming-week-146/") {
		t.Fatalf("post url = %q", r.PostURL)
	}
	if len(r.Windows) != 1 || r.Daily {
		t.Fatalf("windows = %+v daily = %v", r.Windows, r.Daily)
	}
	if got := r.Windows[0].Start.Format("2006-01-02 15:04"); got != "2026-08-26 22:00" {
		t.Fatalf("start = %s", got)
	}
	if got := r.Windows[0].End.Format("2006-01-02 15:04"); got != "2026-08-27 13:00" {
		t.Fatalf("end = %s", got)
	}
	// Same-day short window in the same post.
	short := findFeedRow(rows, "Parañaque", "Don Bosco")
	if short == nil {
		t.Fatal("Parañaque short-window row missing")
	}
	if short.Schedule != "1:00 p.m. to 7:00 p.m. of August 24, 2026" {
		t.Fatalf("schedule = %q", short.Schedule)
	}
	if len(short.Windows) != 1 {
		t.Fatalf("windows = %+v", short.Windows)
	}
	if got := short.Windows[0].Start.Format("2006-01-02 15:04"); got != "2026-08-24 13:00" {
		t.Fatalf("start = %s", got)
	}
	if got := short.Windows[0].End.Format("2006-01-02 15:04"); got != "2026-08-24 19:00" {
		t.Fatalf("end = %s", got)
	}
	// Bacoor city normalisation ("Bacoor, Cavite" -> "Bacoor").
	bacoor := findFeedRow(rows, "Bacoor", "Molino II")
	if bacoor == nil {
		t.Fatal("Bacoor row missing")
	}
	// Singular "activity" title (feed_p2 151) still counts.
	rows2, _ := feedRows(t, "feed_p2.xml")
	found := false
	for _, r := range rows2 {
		if strings.Contains(r.PostURL, "week-151") {
			found = true
		}
	}
	if !found {
		t.Fatal("singular-title post 151 missing")
	}
}

// Feed p8 posts 2026-03-20: Las Piñas Pamplona Uno, Pulang Lupa Uno
// and Pulang Lupa Dos, 10:00 p.m. of March 24 to 6:00 a.m. of March
// 25, 2026; Caloocan daily 11:01 p.m. to 3:00 a.m. daily March 22-29
// = 8 nights; Manila rowspan rows.
func TestFeedP8Rows(t *testing.T) {
	rows, skipped := feedRows(t, "feed_p8.xml")
	if skipped != 0 {
		t.Fatalf("skipped = %d", skipped)
	}
	lp := findFeedRow(rows, "Las Piñas", "Pamplona Uno")
	if lp == nil {
		t.Fatalf("Las Piñas row missing in %d rows", len(rows))
	}
	if !strings.Contains(lp.Barangays, "Pulang Lupa Dos") {
		t.Fatalf("barangays = %q", lp.Barangays)
	}
	if len(lp.Windows) != 1 {
		t.Fatalf("windows = %+v", lp.Windows)
	}
	if got := lp.Windows[0].Start.Format("2006-01-02 15:04"); got != "2026-03-24 22:00" {
		t.Fatalf("start = %s", got)
	}
	if got := lp.Windows[0].End.Format("2006-01-02 15:04"); got != "2026-03-25 06:00" {
		t.Fatalf("end = %s", got)
	}
	// Swapped Activity/Schedule columns are mapped by header name.
	if lp.Activity != "Network Diagnostic / Leak Localization" {
		t.Fatalf("activity = %q (header swap not honoured)", lp.Activity)
	}
	// Caloocan daily range: 8 nights, end next day each night.
	var cal *Maintenance
	for i := range rows {
		if rows[i].City == "Caloocan" && strings.Contains(rows[i].Schedule, "March 22 to March 29, 2026") && strings.Contains(rows[i].Barangays, "Brgy. 36") {
			cal = &rows[i]
		}
	}
	if cal == nil {
		t.Fatal("Caloocan daily row missing")
	}
	if !cal.Daily || len(cal.Windows) != 8 {
		t.Fatalf("daily = %v windows = %d", cal.Daily, len(cal.Windows))
	}
	if cal.DailyFrom != "2026-03-22" || cal.DailyTo != "2026-03-29" {
		t.Fatalf("daily range = %q..%q", cal.DailyFrom, cal.DailyTo)
	}
	if got := cal.Windows[0].Start.Format("2006-01-02 15:04"); got != "2026-03-22 23:01" {
		t.Fatalf("first start = %s", got)
	}
	if got := cal.Windows[0].End.Format("2006-01-02 15:04"); got != "2026-03-23 03:00" {
		t.Fatalf("first end = %s", got)
	}
	if got := cal.Windows[7].Start.Format("2006-01-02 15:04"); got != "2026-03-29 23:01" {
		t.Fatalf("last start = %s", got)
	}
	if got := cal.Windows[7].End.Format("2006-01-02 15:04"); got != "2026-03-30 03:00" {
		t.Fatalf("last end = %s", got)
	}
	// Manila rowspan rows: the carried city covers multiple
	// schedules sharing one location block.
	manila := 0
	for _, r := range rows {
		if r.City == "Manila" && r.PostDate == "2026-03-20" {
			manila++
		}
	}
	if manila < 4 {
		t.Fatalf("manila rowspan rows = %d, want >= 4", manila)
	}
}

// Feed p1 (Oct 2026 posts): Muntinlupa Tunasan; the Imus row also
// lists Bacoor (AlsoCities).
func TestFeedP1Nearby(t *testing.T) {
	rows, skipped := feedRows(t, "feed_p1.xml")
	if skipped != 0 {
		t.Fatalf("skipped = %d", skipped)
	}
	mun := findFeedRow(rows, "Muntinlupa", "Tunasan")
	if mun == nil {
		t.Fatal("Muntinlupa Tunasan row missing")
	}
	if mun.Schedule != "10:00 p.m. of October 21, 2026 to 6:00 a.m. of October 22, 2026" {
		t.Fatalf("schedule = %q", mun.Schedule)
	}
	imus := findFeedRow(rows, "Imus City", "Molino")
	if imus == nil {
		t.Fatal("Imus row missing")
	}
	if len(imus.AlsoCities) != 1 || imus.AlsoCities[0] != "Bacoor" {
		t.Fatalf("also_cities = %q", imus.AlsoCities)
	}
	if got := imus.Windows[0].Start.Format("2006-01-02 15:04"); got != "2026-10-19 22:00" {
		t.Fatalf("start = %s", got)
	}
}

// Valenzuela: rowspan on city and other columns; daily rows carry,
// and one Valenzuela row holds 3 schedules sharing a location.
func TestFeedValenzuelaRowspan(t *testing.T) {
	rows, _ := feedRows(t, "feed_p8.xml")
	var singles int
	for _, r := range rows {
		if r.City == "Valenzuela" && r.PostDate == "2026-03-20" && !r.Daily {
			singles++
		}
	}
	// Post 124 lists March 22..27 single schedules for Valenzuela.
	if singles < 5 {
		t.Fatalf("valenzuela singles = %d, want >= 5", singles)
	}
	rows3, _ := feedRows(t, "feed_p3.xml")
	foundDaily := false
	for _, r := range rows3 {
		if r.City == "Valenzuela" && r.Daily && len(r.Windows) == 5 {
			foundDaily = true
		}
	}
	if !foundDaily {
		t.Fatal("Valenzuela Aug 10-14 daily (5 nights) missing")
	}
}

func TestParseFeedNotRSS(t *testing.T) {
	if _, err := ParseMayniladFeed([]byte("not xml at all <<<")); err == nil {
		t.Fatal("expected error for non-RSS body")
	}
	// Zero weekly posts is an empty (not error) result.
	empty := `<?xml version="1.0"?><rss version="2.0"><channel><title>x</title><item><title>Desludging services</title><link>https://example.invalid/a</link><pubDate>Fri, 23 Oct 2026 09:32:45 +0000</pubDate></item></channel></rss>`
	rows, err := ParseMayniladFeed([]byte(empty))
	if err != nil {
		t.Fatalf("empty feed err = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestParseMaintScheduleVariants(t *testing.T) {
	// Single window.
	w, m := ParseMaintSchedule("10:00 p.m. of August 26, 2026 to 1:00 p.m. of August 27, 2026")
	if m.Daily || len(w) != 1 {
		t.Fatalf("single = %+v %+v", w, m)
	}
	if got := w[0].Start.Format("2006-01-02 15:04"); got != "2026-08-26 22:00" {
		t.Fatalf("start = %s", got)
	}
	if got := w[0].End.Format("2006-01-02 15:04"); got != "2026-08-27 13:00" {
		t.Fatalf("end = %s", got)
	}
	// Same-day short window (date once at the end).
	w, _ = ParseMaintSchedule("1:00 p.m. to 7:00 p.m. of August 24, 2026")
	if len(w) != 1 || w[0].Start.Format("2006-01-02 15:04") != "2026-08-24 13:00" || w[0].End.Format("2006-01-02 15:04") != "2026-08-24 19:00" {
		t.Fatalf("short = %+v", w)
	}
	// Daily range across months with a year on both ends.
	w, m = ParseMaintSchedule("11:01 p.m. to 3:00 a.m. daily, from January 25, 2026 to February 1, 2026")
	if !m.Daily || len(w) != 8 || m.DailyFrom != "2026-01-25" || m.DailyTo != "2026-02-01" {
		t.Fatalf("jan-feb daily = %+v %+v", w, m)
	}
	// Daily range, second clock window.
	w, m = ParseMaintSchedule("12:01 a.m. to 3:00 a.m. daily, from March 23 to March 29, 2026")
	if !m.Daily || len(w) != 7 || m.DailyFrom != "2026-03-23" || m.DailyTo != "2026-03-29" {
		t.Fatalf("mar daily = %+v %+v", w, m)
	}
	// Odd variant: "10:00 p.m. from January 25, 2026 to 6:00 a.m. of January 26, 2026".
	w, _ = ParseMaintSchedule("10:00 p.m. from January 25, 2026 to 6:00 a.m. of January 26, 2026")
	if len(w) != 1 || w[0].Start.Format("2006-01-02 15:04") != "2026-01-25 22:00" || w[0].End.Format("2006-01-02 15:04") != "2026-01-26 06:00" {
		t.Fatalf("odd = %+v", w)
	}
	// Single date with a comma.
	w, _ = ParseMaintSchedule("12:01 a.m. to 4:00 a.m., June 12, 2026")
	if len(w) != 1 || w[0].Start.Format("2006-01-02 15:04") != "2026-06-12 00:01" {
		t.Fatalf("comma date = %+v", w)
	}
	// Unparsable keeps raw text and empty windows.
	w, _ = ParseMaintSchedule("sometime soon, please wait")
	if len(w) != 0 {
		t.Fatalf("garbage parsed = %+v", w)
	}
}

func TestSplitFeedRowsRetention(t *testing.T) {
	rows, _ := feedRows(t, "feed_p1.xml")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	main, nearby := SplitFeedRows(rows, now)
	for _, r := range main {
		if r.City != "Las Piñas" {
			for _, c := range r.AlsoCities {
				if c == "Las Piñas" {
					continue
				}
			}
			t.Fatalf("non-Las Piñas row in main: %+v", r)
		}
	}
	// Oct 27 keeps the Oct 26-27 Caloocan rows out of main (no Las
	// Piñas in p1) and keeps Muntinlupa Tunasan nearby.
	foundTunasan := false
	for _, r := range nearby {
		if r.City == "Muntinlupa" && strings.Contains(r.Barangays, "Tunasan") {
			foundTunasan = true
		}
	}
	if !foundTunasan {
		t.Fatalf("Tunasan missing from nearby (%d rows)", len(nearby))
	}
	foundBacoor := false
	for _, r := range nearby {
		if r.City == "Bacoor" || (len(r.AlsoCities) == 1 && r.AlsoCities[0] == "Bacoor") {
			foundBacoor = true
		}
	}
	if !foundBacoor {
		t.Fatal("Bacoor (via Imus AlsoCities) missing from nearby")
	}
	// Old rows expire: March rows are long gone by late October.
	oldRows, _ := feedRows(t, "feed_p8.xml")
	oldMain, oldNearby := SplitFeedRows(oldRows, now)
	if len(oldMain) != 0 || len(oldNearby) != 0 {
		t.Fatalf("expired rows kept: main=%d nearby=%d", len(oldMain), len(oldNearby))
	}
	// Unparsed rows from posts <= 8 days old are kept.
	unparsed := []Maintenance{{
		PostDate: "2026-10-23", PostURL: "https://example.invalid/p",
		City: "Las Piñas", Barangays: "Pilar", Schedule: "sometime soon",
		Activity: "Facility Maintenance", Location: "here",
	}}
	kept, _ := SplitFeedRows(unparsed, now)
	if len(kept) != 1 {
		t.Fatalf("fresh unparsed row dropped: %+v", kept)
	}
	stale := []Maintenance{{
		PostDate: "2026-10-01", PostURL: "https://example.invalid/p",
		City: "Las Piñas", Barangays: "Pilar", Schedule: "sometime soon",
		Activity: "Facility Maintenance", Location: "here",
	}}
	keptOld, _ := SplitFeedRows(stale, now)
	if len(keptOld) != 0 {
		t.Fatalf("stale unparsed row kept: %+v", keptOld)
	}
}

func TestBuildWaterAlertsDailySingle(t *testing.T) {
	rows, _ := feedRows(t, "feed_p8.xml")
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	main, _ := SplitFeedRows(rows, now)
	w := &WaterStatus{City: "Las Piñas", CheckedAt: "2026-03-21T20:00:00+08:00", Maintenance: main}
	alerts := BuildWaterAlerts(w)
	if len(alerts) == 0 {
		t.Fatal("no alerts")
	}
	// Las Piñas March rows are single windows: one alert each.
	for _, a := range alerts {
		if a.Daily {
			t.Fatalf("unexpected daily Las Piñas alert: %+v", a)
		}
	}
	// A daily Caloocan-style row yields ONE alert spanning first
	// start to last end with Daily set (never one alert per night).
	calRows, _ := feedRows(t, "feed_p3.xml")
	var val *Maintenance
	for i := range calRows {
		if calRows[i].City == "Valenzuela" && calRows[i].Daily {
			val = &calRows[i]
		}
	}
	if val == nil {
		t.Fatal("no daily fixture row")
	}
	// Valenzuela is neither main nor nearby: build the status
	// directly (SplitFeedRows only keeps Las Piñas + nearby).
	w2 := &WaterStatus{City: "Valenzuela", Maintenance: []Maintenance{*val}}
	a2 := BuildWaterAlerts(w2)
	if len(a2) != 1 {
		t.Fatalf("daily alerts = %d, want 1", len(a2))
	}
	if !a2[0].Daily || len(a2[0].Windows) != 5 {
		t.Fatalf("daily alert = %+v", a2[0])
	}
	if got := a2[0].Start.Format("2006-01-02 15:04"); got != "2026-08-10 00:01" {
		t.Fatalf("daily start = %s", got)
	}
	if got := a2[0].End.Format("2006-01-02 15:04"); got != "2026-08-14 04:00" {
		t.Fatalf("daily end = %s", got)
	}
	text := RenderWaterAlertPost(a2[0], false)
	for _, want := range []string{"bawat gabi", "every night", "毎晩", "12:01 AM to 4:00 AM", "Ago 10"} {
		if !strings.Contains(text, want) {
			t.Fatalf("daily caption missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "hanggang Huw Ago 14, 4:00 AM") {
		t.Fatalf("daily caption prints one continuous outage:\n%s", text)
	}
	snap := &Snapshot{Water: w2, Alerts: a2}
	if problems := CheckPost(text, snap, "water"); len(problems) != 0 {
		t.Fatalf("daily check problems: %v", problems)
	}
}

func TestBuildWaterAlertsDedupeX(t *testing.T) {
	// Feed Las Piñas window Oct 19 22:00 - Oct 20 06:00.
	windows, _ := ParseMaintSchedule("10:00 p.m. of October 19, 2026 to 6:00 a.m. of October 20, 2026")
	feedRows := []Maintenance{{
		PostDate: "2026-10-16", PostURL: "https://example.invalid/feed-post",
		City: "Las Piñas", Barangays: "Pilar", Schedule: "10:00 p.m. of October 19, 2026 to 6:00 a.m. of October 20, 2026",
		Activity: "Facility Maintenance", Location: "here",
		Windows: windows,
	}}
	// X advisory for the same city overlapping by > 50 %: dropped.
	xStart := time.Date(2026, 10, 19, 23, 0, 0, 0, manilaLoc)
	xEnd := time.Date(2026, 10, 20, 5, 0, 0, 0, manilaLoc)
	w := &WaterStatus{
		City: "Las Piñas", Maintenance: feedRows,
		Advisories: []XAdvisory{{
			PostID: "1", PostURL: "https://x.com/maynilad/status/1",
			Kind: "scheduled", City: "Las Piñas",
			Start: xStart, End: xEnd, WindowText: "x window", ReasonKey: "audit",
		}},
	}
	alerts := BuildWaterAlerts(w)
	for _, a := range alerts {
		if strings.HasPrefix(a.SourceURL, "https://x.com/") {
			t.Fatalf("overlapping X advisory not dropped: %+v", a)
		}
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1 feed winner", len(alerts))
	}
	// A disjoint X advisory survives.
	xStart2 := time.Date(2026, 10, 21, 10, 0, 0, 0, manilaLoc)
	xEnd2 := time.Date(2026, 10, 21, 14, 0, 0, 0, manilaLoc)
	w.Advisories = append(w.Advisories, XAdvisory{
		PostID: "2", PostURL: "https://x.com/maynilad/status/2",
		Kind: "scheduled", City: "Las Piñas",
		Start: xStart2, End: xEnd2, WindowText: "x window 2", ReasonKey: "audit",
	})
	alerts = BuildWaterAlerts(w)
	xCount := 0
	for _, a := range alerts {
		if strings.HasPrefix(a.SourceURL, "https://x.com/") {
			xCount++
		}
	}
	if xCount != 1 {
		t.Fatalf("disjoint X advisory missing: %+v", alerts)
	}
	// Per-night daily drop: an X window inside one nightly window of
	// a daily feed row is dropped even though the overall daily span
	// dwarfs it.
	dailyWindows, dailyMeta := ParseMaintSchedule("11:01 p.m. to 3:00 a.m. daily, from March 22 to March 29, 2026")
	dailyRow := Maintenance{
		PostDate: "2026-03-20", PostURL: "https://example.invalid/daily",
		City: "Las Piñas", Barangays: "Pilar", Schedule: "11:01 p.m. to 3:00 a.m. daily, from March 22 to March 29, 2026",
		Activity: "Network Diagnostic / Leak Localization", Location: "here",
		Windows: dailyWindows, Daily: dailyMeta.Daily,
		DailyFrom: dailyMeta.DailyFrom, DailyTo: dailyMeta.DailyTo,
	}
	wd := &WaterStatus{
		City: "Las Piñas", Maintenance: []Maintenance{dailyRow},
		Advisories: []XAdvisory{{
			PostID: "9", PostURL: "https://x.com/maynilad/status/9",
			Kind: "scheduled", City: "Las Piñas",
			Start:      time.Date(2026, 3, 24, 23, 30, 0, 0, manilaLoc),
			End:        time.Date(2026, 3, 25, 1, 30, 0, 0, manilaLoc),
			WindowText: "x nightly", ReasonKey: "audit",
		}},
	}
	if got := BuildWaterAlerts(wd); len(got) != 1 {
		t.Fatalf("nightly X not dropped: %+v", got)
	}
}

func TestWaterTrendLine(t *testing.T) {
	mk := func(day int) Maintenance {
		sched := time.Date(2026, 10, day, 22, 0, 0, 0, manilaLoc)
		end := time.Date(2026, 10, day+1, 6, 0, 0, 0, manilaLoc)
		if day == 31 {
			end = time.Date(2026, 11, 1, 6, 0, 0, 0, manilaLoc)
		}
		return Maintenance{
			PostDate: "2026-10-16", PostURL: "https://example.invalid/p",
			City: "Las Piñas", Barangays: "Pilar",
			Schedule: "x", Activity: "Facility Maintenance", Location: "here",
			Windows: []TimeWindow{{Start: sched, End: end}},
		}
	}
	// Two October alerts plus one September alert with a September
	// post covering last month: N=2 shows the line with last month.
	sep := Maintenance{
		PostDate: "2026-09-18", PostURL: "https://example.invalid/sep",
		City: "Las Piñas", Barangays: "Talon Uno",
		Schedule: "x", Activity: "Facility Maintenance", Location: "here",
		Windows: []TimeWindow{{
			Start: time.Date(2026, 9, 20, 22, 0, 0, 0, manilaLoc),
			End:   time.Date(2026, 9, 21, 6, 0, 0, 0, manilaLoc),
		}},
	}
	w := &WaterStatus{City: "Las Piñas", Maintenance: []Maintenance{mk(19), mk(21), sep}}
	alerts := BuildWaterAlerts(w)
	if len(alerts) != 3 {
		t.Fatalf("alerts = %d", len(alerts))
	}
	var second *WaterAlert
	for i := range alerts {
		if alerts[i].NthThisMonth == 2 {
			second = &alerts[i]
		}
	}
	if second == nil {
		t.Fatalf("no N=2 alert: %+v", alerts)
	}
	if !second.HasPrev || second.PrevMonthCount != 1 {
		t.Fatalf("prev = %+v", second)
	}
	text := RenderWaterAlertPost(*second, false)
	for _, want := range []string{"Ika-2", "2nd water interruption", "2回目", "1 noong Setyembre", "1 last September", "先月1回"} {
		if !strings.Contains(text, want) {
			t.Fatalf("trend missing %q:\n%s", want, text)
		}
	}
	snap := &Snapshot{Water: w, Alerts: alerts}
	var snapSecond Snapshot
	for _, a := range alerts {
		if a.NthThisMonth == 2 {
			snapSecond = Snapshot{Water: w, Alerts: []WaterAlert{a}}
		}
	}
	if problems := CheckPost(text, &snapSecond, "water"); len(problems) != 0 {
		t.Fatalf("trend check problems: %v", problems)
	}
	_ = snap
	// A lone alert shows the 1st-this-month trend line.
	w1 := &WaterStatus{City: "Las Piñas", Maintenance: []Maintenance{mk(19)}}
	a1 := BuildWaterAlerts(w1)
	if len(a1) != 1 || a1[0].NthThisMonth != 1 {
		t.Fatalf("single = %+v", a1)
	}
	t1 := RenderWaterAlertPost(a1[0], false)
	for _, want := range []string{"Ika-1 na water interruption", "1st water interruption in Las Piñas this October", "1回目の断水"} {
		if !strings.Contains(t1, want) {
			t.Fatalf("N=1 trend missing %q:\n%s", want, t1)
		}
	}
	snap1 := &Snapshot{Water: w1, Alerts: a1}
	if problems := CheckPost(t1, snap1, "water"); len(problems) != 0 {
		t.Fatalf("single check problems: %v", problems)
	}
	// Tampered trend numbers fail the checker.
	bad := strings.Replace(text, "Ika-2", "Ika-3", 1)
	if problems := CheckPost(bad, &snapSecond, "water"); len(problems) == 0 {
		t.Fatal("tampered trend passed")
	}
}

func TestFetchMayniladFeedFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	tmp := filepath.Join(t.TempDir(), "feed.xml")
	b, err := os.ReadFile(testdata(t, "maynilad_feed/feed_p1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:           srv.Client(),
		Now:              time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		MinDelay:         -1,
		MayniladBase:     srv.URL,
		MayniladFeedFile: tmp,
		Sections:         []string{"water"},
		Sleep:            func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Water == nil {
		t.Fatalf("no water, errors=%v", snap.Errors)
	}
	foundTunasan := false
	for _, r := range snap.Water.Nearby {
		if strings.Contains(r.Barangays, "Tunasan") {
			foundTunasan = true
		}
	}
	if !foundTunasan {
		t.Fatalf("Tunasan missing from nearby: %+v errors=%v", snap.Water.Nearby, snap.Errors)
	}
	for _, src := range snap.Sources {
		if src.Name == "maynilad-feed" && src.HTTPStatus != 200 {
			t.Fatalf("feed source = %+v", src)
		}
	}
	// Las Piñas alerts build from the feed (empty here: p1 has no
	// Las Piñas rows, but the section still stamps).
	if snap.SectionFetchedAt["water"] == "" {
		t.Fatal("water section not stamped")
	}
}

func TestFetchMayniladFeedHTTP(t *testing.T) {
	b, err := os.ReadFile(testdata(t, "maynilad_feed/feed_p1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write(b)
	}))
	defer srv.Close()
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:       srv.Client(),
		Now:          time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		MinDelay:     -1,
		MayniladBase: srv.URL,
		MayniladFeed: true,
		Sections:     []string{"water"},
		Sleep:        func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Water == nil {
		t.Fatalf("no water, errors=%v", snap.Errors)
	}
	if len(snap.Water.Nearby) == 0 {
		t.Fatalf("no nearby rows: %+v", snap.Water)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %v, want exactly 1 (page 1 has weekly posts)", requests)
	}
	// Page 1 without weekly posts walks to ?paged=2 (max 2 requests).
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, "second:"+r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/rss+xml")
		if r.URL.Query().Get("paged") == "2" {
			w.Write(b)
			return
		}
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>x</title><item><title>Desludging</title><link>https://example.invalid/a</link><pubDate>Fri, 23 Oct 2026 09:32:45 +0000</pubDate></item></channel></rss>`))
	}))
	defer srv2.Close()
	requests = nil
	snap2, err := Fetch(context.Background(), FetchOptions{
		Client:       srv2.Client(),
		Now:          time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		MinDelay:     -1,
		MayniladBase: srv2.URL,
		MayniladFeed: true,
		Sections:     []string{"water"},
		Sleep:        func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap2.Water == nil || len(snap2.Water.Nearby) == 0 {
		t.Fatalf("no water from page 2: %+v errors=%v", snap2.Water, snap2.Errors)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %v, want 2", requests)
	}
}

func TestFetchMayniladFeedBlocked(t *testing.T) {
	// 403 + cf-mitigated: challenge + "Just a moment" body: feed
	// blocked fallback — Source recorded, ONE stderr notice, nothing
	// in Errors, X inbox kept.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(403)
		w.Write([]byte("<html><body>Just a moment...</body></html>"))
	}))
	defer srv.Close()
	inboxSrc, err := os.ReadFile(testdata(t, "maynilad_x/inbox-2026-10-28.json"))
	if err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(t.TempDir(), "inbox.json")
	if err := os.WriteFile(inbox, inboxSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:         srv.Client(),
		Now:            time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC),
		MinDelay:       -1,
		MayniladBase:   srv.URL,
		MayniladFeed:   true,
		MayniladXInbox: inbox,
		Sections:       []string{"water"},
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Water == nil || len(snap.Water.Advisories) != 1 {
		t.Fatalf("X inbox lost on blocked feed: %+v errors=%v", snap.Water, snap.Errors)
	}
	for _, e := range snap.Errors {
		if strings.Contains(e, "maynilad-feed") {
			t.Fatalf("blocked feed in Errors: %v", snap.Errors)
		}
	}
	blocked := false
	for _, src := range snap.Sources {
		if src.Name == "maynilad-feed" {
			blocked = true
			if src.HTTPStatus != 403 {
				t.Fatalf("feed source status = %d", src.HTTPStatus)
			}
			if src.Err != "blocked; using X inbox" {
				t.Fatalf("feed source err = %q", src.Err)
			}
		}
	}
	if !blocked {
		t.Fatalf("no maynilad-feed source: %+v", snap.Sources)
	}
}
