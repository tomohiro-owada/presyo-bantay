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

// Part B tests: Meralco maintenance-schedule parsing, the Fetch
// section power (request plan, offline dir, httptest, layout alarm)
// and Las Piñas BuildPowerAlerts. All expectations are taken from
// the fixtures in testdata/meralco_ms/ (captured 2026-10-29).

// msFixture parses one maintenance-schedule fixture.
func msFixture(t *testing.T, name string) MeralcoMSPage {
	t.Helper()
	b, err := os.ReadFile(testdata(t, "meralco_ms/"+name))
	if err != nil {
		t.Fatal(err)
	}
	pg, err := ParseMeralcoMaintenance(b)
	if err != nil {
		t.Fatal(err)
	}
	return pg
}

// msWindow formats one window for exact comparison.
func msWindow(w TimeWindow) string {
	return w.Start.Format("2006-01-02 15:04") + "->" + w.End.Format("2006-01-02 15:04")
}

// TestParseMeralcoOct7LasPinas covers the Las Piñas Oct 7 2026 entry:
// split windows, Almanza I & II barangays, the road triple and the
// substation reason.
func TestParseMeralcoOct7LasPinas(t *testing.T) {
	pg := msFixture(t, "city215_p0.html")
	if len(pg.Items) != 2 {
		t.Fatalf("items = %d", len(pg.Items))
	}
	it := pg.Items[0]
	if it.ID != "e8535cbefb82" {
		t.Fatalf("ID = %q", it.ID)
	}
	if it.Title != "October 7, 2026 - Las Piñas City (Almanza I And Almanza Ii)" {
		t.Fatalf("title = %q", it.Title)
	}
	if it.URL != "https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/october-7-2026-las-pinas-city-almanza-i-and-almanza-ii" {
		t.Fatalf("URL = %q", it.URL)
	}
	if len(it.Tags) != 1 || it.Tags[0] != "Las Pinas City" {
		t.Fatalf("tags = %q", it.Tags)
	}
	if len(it.Cities) != 2 || it.Cities[0] != "Las Piñas" || it.Cities[1] != "Muntinlupa" {
		t.Fatalf("cities = %q", it.Cities)
	}
	if it.DateFrom != "2026-10-07" || it.DateTo != "2026-10-07" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if len(it.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(it.Blocks))
	}
	bl := it.Blocks[0]
	if bl.Circuit != "AYALA ALABANG 425WR" {
		t.Fatalf("circuit = %q", bl.Circuit)
	}
	if len(bl.Windows) != 2 {
		t.Fatalf("windows = %d", len(bl.Windows))
	}
	if got := msWindow(bl.Windows[0]); got != "2026-10-07 00:01->2026-10-07 00:30" {
		t.Fatalf("window 0 = %s", got)
	}
	if got := msWindow(bl.Windows[1]); got != "2026-10-07 04:30->2026-10-07 05:00" {
		t.Fatalf("window 1 = %s", got)
	}
	if got := bl.Barangays["Las Piñas"]; len(got) != 2 || got[0] != "Almanza I" || got[1] != "Almanza II" {
		t.Fatalf("Las Piñas barangays = %q", got)
	}
	if got := bl.Barangays["Muntinlupa"]; len(got) != 2 || got[0] != "Alabang" || got[1] != "Ayala Alabang" {
		t.Fatalf("Muntinlupa barangays = %q", got)
	}
	if len(bl.Roads) != 3 || bl.Roads[0] != "Alabang-Zapote Road" || bl.Roads[1] != "Investment Drive" || bl.Roads[2] != "Daang Hari Road" {
		t.Fatalf("roads = %q", bl.Roads)
	}
	if it.Reason != "Maintenance works inside Meralco - Ayala Alabang substation." {
		t.Fatalf("reason = %q", it.Reason)
	}
	if it.ReasonKey != PowerReasonSubstation {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
	if it.Daily || it.DateAmbiguous {
		t.Fatalf("daily/ambiguous = %v/%v", it.Daily, it.DateAmbiguous)
	}
}

// TestParseMeralcoSep29LasPinasMuntinlupa covers the Sep 29 2026
// entry mentioning both Las Piñas and Muntinlupa.
func TestParseMeralcoSep29LasPinasMuntinlupa(t *testing.T) {
	pg := msFixture(t, "city215_p0.html")
	it := pg.Items[1]
	if it.ID != "124cd2c0d3de" {
		t.Fatalf("ID = %q", it.ID)
	}
	if it.Title != "September 29, 2026 - Las Piñas City; And Muntinlupa City" {
		t.Fatalf("title = %q", it.Title)
	}
	if len(it.Cities) != 2 || it.Cities[0] != "Las Piñas" || it.Cities[1] != "Muntinlupa" {
		t.Fatalf("cities = %q", it.Cities)
	}
	if it.DateFrom != "2026-09-29" || it.DateTo != "2026-09-29" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if len(it.Blocks) != 1 || len(it.Blocks[0].Windows) != 2 {
		t.Fatalf("blocks/windows = %+v", it.Blocks)
	}
	if got := msWindow(it.Blocks[0].Windows[0]); got != "2026-09-29 00:01->2026-09-29 00:30" {
		t.Fatalf("window 0 = %s", got)
	}
	if got := msWindow(it.Blocks[0].Windows[1]); got != "2026-09-29 03:30->2026-09-29 04:00" {
		t.Fatalf("window 1 = %s", got)
	}
	if got := it.Blocks[0].Barangays["Las Piñas"]; len(got) != 2 || got[0] != "Almanza 1" || got[1] != "Almanza 2" {
		t.Fatalf("Las Piñas barangays = %q", got)
	}
	if it.ReasonKey != PowerReasonLine {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
}

// TestParseMeralcoMuntinlupaOct29 covers the Muntinlupa Oct 29 2026
// entry: no extractable barangays or roads, facility+line reason.
func TestParseMeralcoMuntinlupaOct29(t *testing.T) {
	pg := msFixture(t, "city221_p0.html")
	it := pg.Items[0]
	if it.ID != "052ed3750e34" {
		t.Fatalf("ID = %q", it.ID)
	}
	if len(it.Cities) != 1 || it.Cities[0] != "Muntinlupa" {
		t.Fatalf("cities = %q", it.Cities)
	}
	if it.DateFrom != "2026-10-29" || it.DateTo != "2026-10-29" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if len(it.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(it.Blocks))
	}
	bl := it.Blocks[0]
	if bl.Circuit != "AYALA ALABANG 412WR" {
		t.Fatalf("circuit = %q", bl.Circuit)
	}
	if len(bl.Windows) != 1 {
		t.Fatalf("windows = %d", len(bl.Windows))
	}
	if got := msWindow(bl.Windows[0]); got != "2026-10-29 10:00->2026-10-29 15:00" {
		t.Fatalf("window = %s", got)
	}
	if len(bl.Barangays) != 0 {
		t.Fatalf("barangays = %q", bl.Barangays)
	}
	if len(bl.Roads) != 0 {
		t.Fatalf("roads = %q", bl.Roads)
	}
	// Installation + reconductoring: line works win.
	if it.ReasonKey != PowerReasonLine {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
}

// TestParseMeralcoCaviteBacoorEntries covers the Imus/Kawit entries
// on the BACOOR 412WX feeder (Oct 26 - 27, two blocks).
func TestParseMeralcoCaviteBacoorEntries(t *testing.T) {
	pg := msFixture(t, "city213_p2.html")
	if len(pg.Items) != 2 {
		t.Fatalf("items = %d", len(pg.Items))
	}
	it := pg.Items[1]
	if it.ID != "d1c54ddcf84c" {
		t.Fatalf("ID = %q", it.ID)
	}
	if it.DateFrom != "2026-10-26" || it.DateTo != "2026-10-27" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if len(it.Blocks) != 2 {
		t.Fatalf("blocks = %d", len(it.Blocks))
	}
	if it.Blocks[0].Circuit != "BACOOR 412WX" || it.Blocks[1].Circuit != "BACOOR 412WX" {
		t.Fatalf("circuits = %q/%q", it.Blocks[0].Circuit, it.Blocks[1].Circuit)
	}
	if got := msWindow(it.Blocks[0].Windows[0]); got != "2026-10-26 23:30->2026-10-27 01:30" {
		t.Fatalf("block 0 window = %s", got)
	}
	if got := msWindow(it.Blocks[1].Windows[0]); got != "2026-10-26 23:30->2026-10-27 04:30" {
		t.Fatalf("block 1 window = %s", got)
	}
	if it.ReasonKey != PowerReasonLine {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
	found := map[string]bool{}
	for _, c := range it.Cities {
		found[c] = true
	}
	for _, want := range []string{"Cavite", "Imus City", "Bacoor"} {
		if !found[want] {
			t.Fatalf("cities = %q, want %q", it.Cities, want)
		}
	}
}

// TestParseMeralcoBulacanInlineDates covers the Nov 3 - 4 Bulacan
// entry whose block carries inline dates.
func TestParseMeralcoBulacanInlineDates(t *testing.T) {
	pg := msFixture(t, "all_p0.html")
	it := pg.Items[0]
	if it.ID != "32b12afb46ed" {
		t.Fatalf("ID = %q", it.ID)
	}
	if it.DateFrom != "2026-11-03" || it.DateTo != "2026-11-04" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if len(it.Cities) != 1 || it.Cities[0] != "Bulacan" {
		t.Fatalf("cities = %q", it.Cities)
	}
	bl := it.Blocks[0]
	if bl.Circuit != "STA. MARIA 435YU" {
		t.Fatalf("circuit = %q", bl.Circuit)
	}
	if len(bl.Windows) != 1 {
		t.Fatalf("windows = %d", len(bl.Windows))
	}
	if got := msWindow(bl.Windows[0]); got != "2026-11-03 23:00->2026-11-04 04:00" {
		t.Fatalf("window = %s", got)
	}
	bgy := bl.Barangays["Bulacan"]
	found := false
	for _, b := range bgy {
		if b == "Bunsuran 1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Bulacan barangays = %q", bgy)
	}
	if len(bl.Roads) != 1 || bl.Roads[0] != "Malibong Matanda Road" {
		t.Fatalf("roads = %q", bl.Roads)
	}
	if it.ReasonKey != PowerReasonLine {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
}

// TestParseMeralcoCaviteMultiDay covers the Oct 29 - Nov 2
// Dasmariñas entry: inline dates resolve the multi-day title (not
// daily, not ambiguous), with the header-swallowed area line.
func TestParseMeralcoCaviteMultiDay(t *testing.T) {
	pg := msFixture(t, "all_p2.html")
	it := pg.Items[1]
	if it.ID != "7a70a7e891c2" {
		t.Fatalf("ID = %q", it.ID)
	}
	if it.DateFrom != "2026-10-29" || it.DateTo != "2026-11-02" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if it.Daily || it.DateAmbiguous {
		t.Fatalf("daily/ambiguous = %v/%v", it.Daily, it.DateAmbiguous)
	}
	if len(it.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(it.Blocks))
	}
	bl := it.Blocks[0]
	if bl.Circuit != "DASMARIÑAS 416DM" {
		t.Fatalf("circuit = %q", bl.Circuit)
	}
	if len(bl.Windows) != 2 {
		t.Fatalf("windows = %d", len(bl.Windows))
	}
	if got := msWindow(bl.Windows[0]); got != "2026-10-29 23:30->2026-10-29 23:59" {
		t.Fatalf("window 0 = %s", got)
	}
	if got := msWindow(bl.Windows[1]); got != "2026-11-02 06:00->2026-11-02 06:30" {
		t.Fatalf("window 1 = %s", got)
	}
	bgy := bl.Barangays["Cavite"]
	for _, want := range []string{"San Agustin I", "San Agustin II", "Burol I", "Burol II", "Burol III"} {
		found := false
		for _, b := range bgy {
			if b == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("barangays = %q, want %q", bgy, want)
		}
	}
	if len(bl.Roads) != 3 || bl.Roads[0] != "Gen. Emilio Aguinaldo Highway" || bl.Roads[1] != "Gov. E. Mangubat Ave" || bl.Roads[2] != "Congressional Ave" {
		t.Fatalf("roads = %q", bl.Roads)
	}
	if it.ReasonKey != PowerReasonSubstation {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
}

// TestParseMeralcoLagunaAmbiguousDaily covers the Oct 29 - 30 Laguna
// entry: no inline dates on a multi-day title, so nightly repeats
// flagged ambiguous (shown on the site, never posted).
func TestParseMeralcoLagunaAmbiguousDaily(t *testing.T) {
	pg := msFixture(t, "all_p4.html")
	it := pg.Items[0]
	if it.ID != "53002dfc7911" {
		t.Fatalf("ID = %q", it.ID)
	}
	if it.DateFrom != "2026-10-29" || it.DateTo != "2026-10-30" {
		t.Fatalf("dates = %q/%q", it.DateFrom, it.DateTo)
	}
	if !it.Daily || !it.DateAmbiguous {
		t.Fatalf("daily/ambiguous = %v/%v", it.Daily, it.DateAmbiguous)
	}
	if it.DailyFrom != "2026-10-29" || it.DailyTo != "2026-10-30" {
		t.Fatalf("daily range = %q/%q", it.DailyFrom, it.DailyTo)
	}
	if len(it.Blocks) != 1 || len(it.Blocks[0].Windows) != 2 {
		t.Fatalf("blocks/windows = %+v", it.Blocks)
	}
	if got := msWindow(it.Blocks[0].Windows[0]); got != "2026-10-29 21:00->2026-10-29 23:59" {
		t.Fatalf("window 0 = %s", got)
	}
	if got := msWindow(it.Blocks[0].Windows[1]); got != "2026-10-30 21:00->2026-10-30 23:59" {
		t.Fatalf("window 1 = %s", got)
	}
}

// TestParseMeralcoParanaqueThreeBlocks covers the Sep 16 - 17
// Parañaque entry: three dated blocks on one item.
func TestParseMeralcoParanaqueThreeBlocks(t *testing.T) {
	pg := msFixture(t, "city223_p1.html")
	it := pg.Items[1]
	if it.ID != "1889b91fee66" {
		t.Fatalf("ID = %q", it.ID)
	}
	if len(it.Blocks) != 3 {
		t.Fatalf("blocks = %d", len(it.Blocks))
	}
	for i, bl := range it.Blocks {
		if bl.Circuit != "BF-PARAÑAQUE 424WU" {
			t.Fatalf("block %d circuit = %q", i, bl.Circuit)
		}
		if len(bl.Windows) != 1 {
			t.Fatalf("block %d windows = %d", i, len(bl.Windows))
		}
	}
	for i, want := range []string{
		"2026-09-16 21:00->2026-09-16 21:30",
		"2026-09-17 05:00->2026-09-17 05:30",
		"2026-09-17 05:30->2026-09-17 06:00",
	} {
		if got := msWindow(it.Blocks[i].Windows[0]); got != want {
			t.Fatalf("block %d window = %s, want %s", i, got, want)
		}
	}
	if it.ReasonKey != PowerReasonLine {
		t.Fatalf("reason key = %q", it.ReasonKey)
	}
}

// TestParseMeralcoDedupe checks duplicate suppression: the Oct 31
// Pasay/Manila pair shares its date range and circuit+windows, so
// only the first URL is kept.
func TestParseMeralcoDedupe(t *testing.T) {
	pg := msFixture(t, "all_p1.html")
	if len(pg.Items) != 1 {
		t.Fatalf("items = %d, want 1 (duplicate dropped)", len(pg.Items))
	}
	if pg.Items[0].ID != "24f2f69d4a9f" {
		t.Fatalf("kept ID = %q", pg.Items[0].ID)
	}
	if !strings.Contains(pg.Items[0].URL, "october-31-2026-pasay-city") {
		t.Fatalf("kept URL = %q", pg.Items[0].URL)
	}
}

// TestParseMeralcoLayoutChanged checks the alarm fixture: a 200 page
// with no rows parses to zero items without an error (the fetch
// layer raises the alarm).
func TestParseMeralcoLayoutChanged(t *testing.T) {
	pg := msFixture(t, "layout_changed.html")
	if len(pg.Items) != 0 {
		t.Fatalf("items = %d", len(pg.Items))
	}
	if _, err := ParseMeralcoMaintenance([]byte("  ")); err == nil {
		t.Fatal("empty input should error")
	}
}

// msLasPinasPower builds the Las Piñas PowerStatus from the city215
// fixtures (Oct 7, Sep 29, Aug 14, Jul 22, two Jun 6 rows).
func msLasPinasPower(t *testing.T) *PowerStatus {
	t.Helper()
	var items []PowerItem
	for _, f := range []string{"city215_p0.html", "city215_p1.html", "city215_p2.html"} {
		items = append(items, msFixture(t, f).Items...)
	}
	if len(items) != 6 {
		t.Fatalf("items = %d, want 6", len(items))
	}
	return &PowerStatus{CheckedAt: "2026-10-29T12:00:00+08:00", Items: items, HistoryFrom: "2026-06-06"}
}

func msAlertByID(alerts []PowerAlert, id string) *PowerAlert {
	for i := range alerts {
		if alerts[i].ID == id {
			return &alerts[i]
		}
	}
	return nil
}

// TestBuildPowerAlertsLasPinas checks Las Piñas-only alerts from the
// city215 fixtures: ordering, windows, barangays/roads, reason keys
// and the monthly trend (distinct-date N, previous-month counts).
func TestBuildPowerAlertsLasPinas(t *testing.T) {
	now := time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC)
	alerts := BuildPowerAlerts(msLasPinasPower(t), now)
	if len(alerts) != 6 {
		t.Fatalf("alerts = %d, want 6", len(alerts))
	}
	// Ordered by Start: Jun 6 04:00, Jun 6 12:00, Jul 22, Aug 14,
	// Sep 29, Oct 7.
	if !alerts[0].Start.Before(alerts[1].Start) || !alerts[5].Start.After(alerts[4].Start) {
		t.Fatalf("not ordered by start: %+v", alerts)
	}
	oct := msAlertByID(alerts, "e8535cbefb82")
	if oct == nil {
		t.Fatal("Oct 7 alert missing")
	}
	if oct.City != "Las Piñas" {
		t.Fatalf("city = %q", oct.City)
	}
	if got := oct.Start.Format("2006-01-02 15:04"); got != "2026-10-07 00:01" {
		t.Fatalf("start = %s", got)
	}
	if got := oct.End.Format("2006-01-02 15:04"); got != "2026-10-07 05:00" {
		t.Fatalf("end = %s", got)
	}
	if len(oct.Windows) != 2 {
		t.Fatalf("windows = %d", len(oct.Windows))
	}
	if len(oct.Barangays) != 2 || oct.Barangays[0] != "Almanza I" || oct.Barangays[1] != "Almanza II" {
		t.Fatalf("barangays = %q", oct.Barangays)
	}
	if len(oct.Roads) != 3 || oct.Roads[0] != "Alabang-Zapote Road" {
		t.Fatalf("roads = %q", oct.Roads)
	}
	if oct.ReasonKey != PowerReasonSubstation {
		t.Fatalf("reason = %q", oct.ReasonKey)
	}
	if oct.SourceURL != "https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/october-7-2026-las-pinas-city-almanza-i-and-almanza-ii" {
		t.Fatalf("source = %q", oct.SourceURL)
	}
	// Only October Las Piñas date so far: N=1; September has the
	// Sep 29 date and history reaches September: prev=1, complete.
	if oct.NthThisMonth != 1 || oct.PrevMonthCount != 1 || !oct.HistoryComplete {
		t.Fatalf("trend = N=%d prev=%d complete=%v", oct.NthThisMonth, oct.PrevMonthCount, oct.HistoryComplete)
	}
	sep := msAlertByID(alerts, "124cd2c0d3de")
	if sep == nil {
		t.Fatal("Sep 29 alert missing")
	}
	if sep.NthThisMonth != 1 || sep.PrevMonthCount != 1 || !sep.HistoryComplete {
		t.Fatalf("sep trend = N=%d prev=%d complete=%v", sep.NthThisMonth, sep.PrevMonthCount, sep.HistoryComplete)
	}
	if len(sep.Barangays) != 2 || sep.Barangays[0] != "Almanza 1" || sep.Barangays[1] != "Almanza 2" {
		t.Fatalf("sep barangays = %q", sep.Barangays)
	}
	// The two June 6 rows share one distinct date: both are N=1,
	// with no previous-month coverage (history starts in June).
	jun := []*PowerAlert{msAlertByID(alerts, "0b8d5a4ab22c"), msAlertByID(alerts, "85081153b567")}
	for _, a := range jun {
		if a == nil {
			t.Fatal("Jun 6 alert missing")
		}
		if a.NthThisMonth != 1 {
			t.Fatalf("jun N = %d", a.NthThisMonth)
		}
		if a.PrevMonthCount != 0 || a.HistoryComplete {
			t.Fatalf("jun prev = %d complete = %v", a.PrevMonthCount, a.HistoryComplete)
		}
		// No barangay in the Jun 6 area lines: areas come from the
		// title parenthetical ("Talon V" / "Talon I; And Talon V").
		if g := strings.Join(a.Barangays, "|"); g != "Talon V" && g != "Talon I|Talon V" {
			t.Fatalf("jun barangays = %q", a.Barangays)
		}
	}
}

// TestBuildPowerAlertsRanking checks distinct-date N ranking,
// ambiguous/windowless/non-Las Piñas exclusion and the alarm gate
// with synthetic items.
func TestBuildPowerAlertsRanking(t *testing.T) {
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
	now := time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC)
	p := &PowerStatus{
		CheckedAt:   "2026-10-29T12:00:00+08:00",
		HistoryFrom: "2026-09-01",
		Items: []PowerItem{
			mk("oct5", "2026-10-05", 10, 0, 12, 0),
			mk("oct7a", "2026-10-07", 0, 1, 0, 30),
			mk("oct7b", "2026-10-07", 4, 30, 5, 0),
			mk("sep9", "2026-09-09", 9, 0, 11, 0),
		},
	}
	alerts := BuildPowerAlerts(p, now)
	if len(alerts) != 4 {
		t.Fatalf("alerts = %d", len(alerts))
	}
	byID := map[string]PowerAlert{}
	for _, a := range alerts {
		byID[a.ID] = a
	}
	if byID["oct7a"].NthThisMonth != 2 || byID["oct7b"].NthThisMonth != 2 {
		t.Fatalf("oct N = %d/%d, want 2/2", byID["oct7a"].NthThisMonth, byID["oct7b"].NthThisMonth)
	}
	if byID["oct5"].NthThisMonth != 1 {
		t.Fatalf("oct5 N = %d, want 1", byID["oct5"].NthThisMonth)
	}
	if !byID["oct7a"].HistoryComplete || byID["oct7a"].PrevMonthCount != 1 {
		t.Fatalf("oct trend = prev=%d complete=%v", byID["oct7a"].PrevMonthCount, byID["oct7a"].HistoryComplete)
	}
	// Ambiguous, windowless and non-Las Piñas items are excluded;
	// a set layout alarm yields no alerts at all.
	amb := mk("amb", "2026-10-20", 9, 0, 10, 0)
	amb.DateAmbiguous = true
	empty := mk("empty", "2026-10-21", 9, 0, 10, 0)
	empty.Blocks = nil
	other := mk("other", "2026-10-22", 9, 0, 10, 0)
	other.Cities = []string{"Muntinlupa"}
	p.Items = append(p.Items, amb, empty, other)
	if alerts := BuildPowerAlerts(p, now); len(alerts) != 4 {
		t.Fatalf("excluded items leaked: %d alerts", len(alerts))
	}
	p.LayoutAlarm = "meralco-ms: layout changed"
	if alerts := BuildPowerAlerts(p, now); len(alerts) != 0 {
		t.Fatalf("alarm still yields %d alerts", len(alerts))
	}
	if alerts := BuildPowerAlerts(nil, now); len(alerts) != 0 {
		t.Fatalf("nil power yields %d alerts", len(alerts))
	}
}

// TestPowerReasonKeys checks the reason mapping on fixture and
// synthetic phrases.
func TestPowerReasonKeys(t *testing.T) {
	for raw, want := range map[string]string{
		"Maintenance works inside Meralco - Ayala Alabang substation.": PowerReasonSubstation,
		"Line maintenance works along Alabang - Zapote Road.":          PowerReasonLine,
		"Installation of facilities along Chico St.":                   PowerReasonFacility,
		"Tree trimming along Sampaguita St.":                           PowerReasonTrimming,
		"Replacement and retirement of facilities.":                    PowerReasonLine,
		"Something entirely different.":                                PowerReasonMaintenance,
	} {
		if got := powerReasonKeyOf(raw); got != want {
			t.Fatalf("reason %q -> %q, want %q", raw, got, want)
		}
	}
}

// TestFetchPowerOffline reads the whole testdata/meralco_ms directory
// (all_pN.html / cityID_pN.html): dedupe, history, Beyond14d.
func TestFetchPowerOffline(t *testing.T) {
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:       httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })).Client(),
		Now:          time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC),
		MinDelay:     -1,
		MeralcoMSDir: testdata(t, "meralco_ms"),
		Sections:     []string{"power"},
		Sleep:        func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Errors) != 0 {
		t.Fatalf("errors = %v", snap.Errors)
	}
	p := snap.Power
	if p == nil {
		t.Fatal("no power section")
	}
	if p.LayoutAlarm != "" {
		t.Fatalf("alarm = %q", p.LayoutAlarm)
	}
	if p.HistoryFrom != "2026-07-22" {
		t.Fatalf("history from = %q", p.HistoryFrom)
	}
	byID := map[string]PowerItem{}
	for _, it := range p.Items {
		byID[it.ID] = it
	}
	oct7, ok := byID["e8535cbefb82"]
	if !ok {
		t.Fatalf("Oct 7 Las Piñas missing in %d items", len(p.Items))
	}
	if oct7.Beyond14d {
		t.Fatal("Oct 7 wrongly beyond +14d")
	}
	// The Muntinlupa-tagged Oct 7 duplicate (same date range and
	// circuit+windows) is deduped: first URL wins.
	if _, dup := byID["9364ce6ead1b"]; dup {
		t.Fatal("Oct 7 duplicate not deduped")
	}
	// The Cavite Imus/Kawit "-0" duplicate is deduped too.
	nImus := 0
	for _, it := range p.Items {
		if it.DateFrom == "2026-10-29" && it.DateTo == "2026-10-30" && strings.Contains(it.Title, "Imus City") {
			nImus++
		}
	}
	if nImus != 2 {
		t.Fatalf("imus/kawit items = %d, want 2 (DILA + IMUS/LANCASTER)", nImus)
	}
	bul := byID["32b12afb46ed"]
	if bul.DateFrom != "2026-11-03" || bul.Beyond14d {
		t.Fatalf("bulacan = %+v", bul)
	}
	for _, src := range snap.Sources {
		if src.Name != "meralco-ms" {
			t.Fatalf("source = %q", src.Name)
		}
		if src.HTTPStatus != 200 {
			t.Fatalf("source status = %d", src.HTTPStatus)
		}
	}
	if ts := snap.SectionFetchedAt["power"]; ts == "" {
		t.Fatal("no power section timestamp")
	}
}

// msPowerServer serves the testdata files behind the listing URL
// layout; unfiltered pages past p5 return one expired item (stop).
func msPowerServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var requests []string
	oldOnly := `<html><body><div class="views-field views-field-title"><h3 class="field-content marginbottomremove"><a href="https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/september-1-2026-manila-tondo">September 1, 2026 - Manila (Tondo)</a></h3></div><div class="views-field views-field-field-service-maintenance-loc marginbottom10"><div class="field-content">Manila</div></div><div class="views-field views-field-body"><div class="field-content"><p><strong>BETWEEN 10:00AM AND 12:00NN – PORTION OF CIRCUIT TONDO 1AA</strong><br><br>Portion of Juan Luna St. in Bgy. Tondo.</p></div></div><div class="views-field views-field-field-reason"><div class="field-content"><p><strong>REASON</strong>: Line works.</p></div></div></div></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+"?"+r.URL.RawQuery)
		q := r.URL.Query()
		id, page := q.Get("field_service_maintenance_loc_target_id"), q.Get("page")
		name := "all_p" + page + ".html"
		if id != "" {
			name = "city" + id + "_p" + page + ".html"
		}
		if b, err := os.ReadFile("testdata/meralco_ms/" + name); err == nil {
			w.Header().Set("Content-Type", "text/html")
			w.Write(b)
			return
		}
		// Past the fixture pages: the unfiltered list ends with one
		// expired item (stops the walk); city filters 404.
		if id == "" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(oldOnly))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

// TestFetchPowerHTTPPlan checks the request plan (cap, order, UA) and
// the merged result over httptest.
func TestFetchPowerHTTPPlan(t *testing.T) {
	srv, requests := msPowerServer(t)
	var sleeps int
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC),
		MinDelay:      -1,
		UserAgent:     "PresyoBantay-Test/0.1",
		MeralcoMS:     true,
		MeralcoMSBase: srv.URL,
		Sections:      []string{"power"},
		Sleep:         func(time.Duration) { sleeps++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Errors) != 0 {
		t.Fatalf("errors = %v", snap.Errors)
	}
	if sleeps != 0 {
		t.Fatalf("sleeps = %d (MinDelay disabled in tests)", sleeps)
	}
	// (a) 4 city filters p0 + (b) Las Piñas p1 (Jul 22 older than 40d
	// stops the history walk) + (c) unfiltered p0..p6 (p6 all-old).
	if len(*requests) != 12 {
		t.Fatalf("requests = %d, want 12: %v", len(*requests), *requests)
	}
	if len(*requests) > 24 {
		t.Fatalf("request cap exceeded: %d", len(*requests))
	}
	seenIDs := map[string]bool{}
	for _, r := range *requests {
		if !strings.Contains(r, "page=") {
			t.Fatalf("request without page: %q", r)
		}
		if seenIDs[r] {
			t.Fatalf("duplicate request: %q", r)
		}
		seenIDs[r] = true
	}
	p := snap.Power
	if p == nil || p.LayoutAlarm != "" {
		t.Fatalf("power = %+v", p)
	}
	// History stops at the Jul 22 page (older than 40 days).
	if p.HistoryFrom != "2026-07-22" {
		t.Fatalf("history from = %q", p.HistoryFrom)
	}
	byID := map[string]PowerItem{}
	for _, it := range p.Items {
		byID[it.ID] = it
	}
	if _, ok := byID["e8535cbefb82"]; !ok {
		t.Fatal("Oct 7 Las Piñas missing")
	}
	if _, dup := byID["9364ce6ead1b"]; dup {
		t.Fatal("Oct 7 duplicate not deduped")
	}
	alerts := BuildPowerAlerts(p, time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC))
	oct := msAlertByID(alerts, "e8535cbefb82")
	if oct == nil {
		t.Fatal("Oct 7 alert missing")
	}
	if oct.NthThisMonth != 1 || oct.PrevMonthCount != 1 || !oct.HistoryComplete {
		t.Fatalf("oct trend = N=%d prev=%d complete=%v", oct.NthThisMonth, oct.PrevMonthCount, oct.HistoryComplete)
	}
}

// TestFetchPowerLayoutAlarm checks the alarm paths: a 200 response
// with zero rows on unfiltered page 0, or rows without any parsable
// date+window, sets LayoutAlarm, records Errors, keeps previous
// items on merge, and yields no alerts.
func TestFetchPowerLayoutAlarm(t *testing.T) {
	noDatePage := `<html><body><div class="views-field views-field-title"><h3 class="field-content marginbottomremove"><a href="https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/soon-las-pinas-city">Soon - Las Piñas City</a></h3></div><div class="views-field views-field-field-service-maintenance-loc marginbottom10"><div class="field-content">Las Pinas City</div></div><div class="views-field views-field-body"><div class="field-content"><p><strong>BETWEEN 10:00AM AND 12:00NN – PORTION OF CIRCUIT X 1AA</strong></p></div></div><div class="views-field views-field-field-reason"><div class="field-content"><p><strong>REASON</strong>: Line works.</p></div></div></div></body></html>`
	for _, tc := range []struct {
		name string
		body string
		// noDate serves the dateless page on every listing request
		// (the whole run then lacks a parsable date+window).
		noDate bool
	}{
		{"zero rows", string(mustRead(t, "meralco_ms/layout_changed.html")) + "\n", false},
		{"no parsable date", noDatePage, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.noDate || !strings.Contains(r.URL.RawQuery, "field_service_maintenance_loc_target_id") {
					if !tc.noDate && r.URL.Query().Get("page") != "0" {
						w.WriteHeader(404)
						return
					}
					w.Header().Set("Content-Type", "text/html")
					w.Write([]byte(tc.body))
					return
				}
				// City filters: only page 0 exists here.
				if r.URL.Query().Get("page") != "0" {
					w.WriteHeader(404)
					return
				}
				b, err := os.ReadFile(testdata(t, "meralco_ms/city215_p0.html"))
				if err != nil {
					t.Fatal(err)
				}
				w.Write(b)
			}))
			defer srv.Close()
			snap, err := Fetch(context.Background(), FetchOptions{
				Client:        srv.Client(),
				Now:           time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC),
				MinDelay:      -1,
				MeralcoMS:     true,
				MeralcoMSBase: srv.URL,
				Sections:      []string{"power"},
				Sleep:         func(time.Duration) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			if snap.Power == nil || snap.Power.LayoutAlarm == "" {
				t.Fatalf("no layout alarm: %+v", snap.Power)
			}
			if !strings.Contains(snap.Power.LayoutAlarm, "meralco-ms: layout changed") {
				t.Fatalf("alarm = %q", snap.Power.LayoutAlarm)
			}
			found := false
			for _, e := range snap.Errors {
				if strings.Contains(e, "layout changed") {
					found = true
				}
			}
			if !found {
				t.Fatalf("alarm not in errors: %v", snap.Errors)
			}
			if alerts := BuildPowerAlerts(snap.Power, time.Now()); len(alerts) != 0 {
				t.Fatalf("alarm yields %d alerts", len(alerts))
			}
			// Merge keeps the previous items alongside the alarm.
			base := &Snapshot{Power: &PowerStatus{
				CheckedAt:   "2026-10-28T12:00:00+08:00",
				Items:       msLasPinasPower(t).Items,
				HistoryFrom: "2026-06-06",
			}}
			merged := MergeSnapshots(base, snap, []string{"power"})
			if merged.Power == nil || merged.Power.LayoutAlarm == "" {
				t.Fatal("alarm lost on merge")
			}
			if len(merged.Power.Items) != 6 {
				t.Fatalf("previous items lost: %d", len(merged.Power.Items))
			}
			if merged.Power.HistoryFrom != "2026-06-06" {
				t.Fatalf("history from = %q", merged.Power.HistoryFrom)
			}
		})
	}
}

// TestFetchPowerNon200KeepsPrevious checks that non-200 responses are
// recorded in Errors and the previous section is kept on merge.
func TestFetchPowerNon200KeepsPrevious(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC),
		MinDelay:      -1,
		MeralcoMS:     true,
		MeralcoMSBase: srv.URL,
		Sections:      []string{"power"},
		Sleep:         func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Power != nil {
		t.Fatalf("power = %+v, want nil", snap.Power)
	}
	found := false
	for _, e := range snap.Errors {
		if strings.Contains(e, "meralco-ms") {
			found = true
		}
	}
	if !found {
		t.Fatalf("meralco-ms failure not recorded: %v", snap.Errors)
	}
	base := &Snapshot{Power: msLasPinasPower(t)}
	merged := MergeSnapshots(base, snap, []string{"power"})
	if merged.Power == nil || len(merged.Power.Items) != 6 {
		t.Fatalf("previous section lost: %+v", merged.Power)
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(testdata(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestFetchPowerRequestCap fuzzes the walk length: with endless
// current pages the run still stops (12 unfiltered + history pages
// within the 24-request cap).
func TestFetchPowerRequestCap(t *testing.T) {
	srv, requests := msPowerServer(t)
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC),
		MinDelay:      -1,
		UserAgent:     "PresyoBantay-Test/0.1",
		MeralcoMS:     true,
		MeralcoMSBase: srv.URL,
		Sections:      []string{"power"},
		Sleep:         func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*requests) > 24 {
		t.Fatalf("request cap exceeded: %d", len(*requests))
	}
	if snap.Power == nil {
		t.Fatal("no power section")
	}
}

// TestMsTitleAreas covers the title/slug parenthetical fallback used
// when an item has no barangay list in its area lines.
func TestMsTitleAreas(t *testing.T) {
	base := "https://company.meralco.com.ph/news-and-advisories/maintenance-schedule/"
	cases := []struct {
		name, title, url string
		want             []string
	}{
		{"title single", "July 22 - 23, 2026 - Las Piñas City (Talon I)", base + "x", []string{"Talon I"}},
		{"title roman and", "October 7, 2026 - Las Piñas City (Almanza I And Almanza Ii)", base + "x", []string{"Almanza I", "Almanza II"}},
		{"title semicolon and", "June 6, 2026 - Las Piñas City (Talon I; And Talon V)", base + "x", []string{"Talon I", "Talon V"}},
		{"title no parenthetical, slug", "July 22 - 23, 2026 - Las Piñas City", base + "july-22-23-2026-las-pinas-city-talon-i", []string{"Talon I"}},
		{"slug dup suffix", "X", base + "july-22-23-2026-las-pinas-city-talon-i-0", []string{"Talon I"}},
		{"slug two areas", "X", base + "june-6-2026-las-pinas-city-talon-i-and-talon-v", []string{"Talon I", "Talon V"}},
		{"slug other city only", "September 29, 2026 - Las Piñas City; And Muntinlupa City", base + "september-29-2026-las-pinas-city-and-muntinlupa-city", nil},
		{"nothing", "September 29, 2026 - Las Piñas City", base + "september-29-2026-las-pinas-city", nil},
		{"other city parenthetical ignored", "October 29, 2026 - Muntinlupa City (Poblacion)", base + "october-29-2026-muntinlupa-city-poblacion", nil},
	}
	for _, c := range cases {
		got := msTitleAreas(c.title, c.url)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestBuildPowerAlertsTitleAreaFallback: the Jul 22 fixture item has no
// barangay in its area lines; the alert takes "Talon I" from the title.
func TestBuildPowerAlertsTitleAreaFallback(t *testing.T) {
	var items []PowerItem
	for _, f := range []string{"city215_p0.html", "city215_p1.html", "city215_p2.html"} {
		b, err := os.ReadFile(filepath.Join("testdata", "meralco_ms", f))
		if err != nil {
			t.Fatal(err)
		}
		pg, err := ParseMeralcoMaintenance(b)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, pg.Items...)
	}
	alerts := BuildPowerAlerts(&PowerStatus{Items: items}, time.Date(2026, 10, 20, 12, 0, 0, 0, manilaLoc))
	var jul *PowerAlert
	for i := range alerts {
		if alerts[i].Start.Format("2006-01-02") == "2026-07-22" {
			jul = &alerts[i]
		}
	}
	if jul == nil {
		t.Fatal("Jul 22 alert missing")
	}
	if len(jul.Barangays) != 1 || jul.Barangays[0] != "Talon I" {
		t.Fatalf("jul barangays = %q", jul.Barangays)
	}
	// An item with neither list nor title area keeps the marker.
	for _, a := range alerts {
		if len(a.Barangays) == 0 {
			t.Fatalf("empty barangays for %s", a.ID)
		}
	}
}
