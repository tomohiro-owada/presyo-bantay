package utilities

import (
	"os"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(testdata(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseMeralcoListing(t *testing.T) {
	urls := ParseMeralcoListing(readFixture(t, "meralco_news_page.html"))
	if len(urls) != 3 {
		t.Fatalf("got %d urls: %v", len(urls), urls)
	}
	want := []string{
		"https://company.meralco.com.ph/news-and-advisories/higher-residential-rates-october-2026",
		"https://company.meralco.com.ph/news-and-advisories/lower-rates-september-2026",
		"https://company.meralco.com.ph/news-and-advisories/lower-rates-august-2026",
	}
	for i, w := range want {
		if urls[i] != w {
			t.Fatalf("urls[%d] = %q, want %q", i, urls[i], w)
		}
	}
}

func TestParseMeralcoRelease(t *testing.T) {
	oct, err := ParseMeralcoRelease(readFixture(t, "meralco_2026_10.html"))
	if err != nil {
		t.Fatal(err)
	}
	if oct.Month != "2026-10" || oct.Announced != "2026-10-13" {
		t.Fatalf("got %+v", oct)
	}
	if oct.Adjustment != 0.3191 || oct.Rate != 15.0615 || oct.PrevRate != 14.7424 || oct.Impact200 != 64 {
		t.Fatalf("got %+v", oct)
	}
	if !strings.Contains(oct.Title, "October 2026") {
		t.Fatalf("title = %q", oct.Title)
	}

	sep, err := ParseMeralcoRelease(readFixture(t, "meralco_2026_09.html"))
	if err != nil {
		t.Fatal(err)
	}
	if sep.Month != "2026-09" || sep.Announced != "2026-09-11" {
		t.Fatalf("got %+v", sep)
	}
	if sep.Adjustment != -0.0409 || sep.Rate != 14.7424 || sep.PrevRate != 14.7833 || sep.Impact200 != -8 {
		t.Fatalf("got %+v", sep)
	}
}

func TestParseInterruptions(t *testing.T) {
	empty, err := ParseInterruptions(readFixture(t, "maynilad_scheduled_laspinas_empty.json"), "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("got %d rows, want 0", len(empty))
	}

	imus, err := ParseInterruptions(readFixture(t, "maynilad_scheduled_imus.json"), "scheduled")
	if err != nil {
		t.Fatal(err)
	}
	if len(imus) < 1 {
		t.Fatal("no imus rows")
	}
	first := imus[0]
	if first.City != "Imus City" || first.Kind != "scheduled" {
		t.Fatalf("got %+v", first)
	}
	if first.Reason != "Network and Facility Limitations" {
		t.Fatalf("reason = %q", first.Reason)
	}
	if first.From != "2026-10-01" || first.To != "2026-10-31" {
		t.Fatalf("from/to = %q/%q raw %q/%q", first.From, first.To, first.FromRaw, first.ToRaw)
	}
	if first.Time != "5:00 AM to 11:00 PM" {
		t.Fatalf("time = %q", first.Time)
	}

	qc, err := ParseInterruptions(readFixture(t, "maynilad_emergency_qc.json"), "emergency")
	if err != nil {
		t.Fatal(err)
	}
	if len(qc) < 1 {
		t.Fatal("no qc rows")
	}
	if qc[0].City != "Quezon City" || qc[0].Barangays != "Brgys. Sauyo and Talipapa" {
		t.Fatalf("got %+v", qc[0])
	}
}

func TestParseMaintenancePosts(t *testing.T) {
	body := readFixture(t, "maynilad_maintenance_posts.json")

	lp, err := ParseMaintenancePosts(body, "Las Pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(lp) != 1 {
		t.Fatalf("got %d Las Pi rows: %+v", len(lp), lp)
	}
	row := lp[0]
	if !strings.Contains(row.PostDate, "2026-08-21") {
		t.Fatalf("post date = %q", row.PostDate)
	}
	if row.Barangays != "Almanza Uno, Pilar, Talon Uno, Talon Dos and Talon Singko" {
		t.Fatalf("barangays = %q", row.Barangays)
	}
	if row.Schedule != "10:00 p.m. of August 26, 2026 to 1:00 p.m. of August 27, 2026" {
		t.Fatalf("schedule = %q", row.Schedule)
	}
	if row.Activity != "Facility Maintenance" {
		t.Fatalf("activity = %q", row.Activity)
	}
	if row.Location != "Marcos Alvarez Pumping Station and Magdiwang Pumping Station and Reservoir" {
		t.Fatalf("location = %q", row.Location)
	}

	// Accent-insensitive prefix.
	lp2, err := ParseMaintenancePosts(body, "Las Piñas")
	if err != nil {
		t.Fatal(err)
	}
	if len(lp2) != 1 {
		t.Fatalf("accent prefix: got %d rows", len(lp2))
	}

	cal, err := ParseMaintenancePosts(body, "Caloocan")
	if err != nil {
		t.Fatal(err)
	}
	// 2 rows from the 2026-10-23 post (second inherits the city via
	// rowspan) plus 1 from the 2026-08-21 post.
	if len(cal) != 3 {
		t.Fatalf("got %d Caloocan rows: %+v", len(cal), cal)
	}
	if !strings.Contains(cal[1].PostDate, "2026-10-23") {
		t.Fatalf("second row post = %q", cal[1].PostDate)
	}
	if cal[1].City != "Caloocan" {
		t.Fatalf("inherited city = %q", cal[1].City)
	}
	if !strings.Contains(cal[1].Barangays, "Brgy. 49") {
		t.Fatalf("barangays = %q", cal[1].Barangays)
	}
}
