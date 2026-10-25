package weekpages

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// fixtureDB builds a DB with daily history for two south markets:
// base price 100, market A at 80 on Oct 19-23.
func fixtureDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "week.db")
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, err := db.Exec("INSERT INTO commodities (name, category, unit) VALUES ('Tilapia', 'fish', 'per kg')")
	if err != nil {
		t.Fatal(err)
	}
	commID, _ := res.LastInsertId()
	mk := map[string]int64{}
	for _, m := range []string{
		regions.Regions[0].Markets[0].Name,
		regions.Regions[0].Markets[1].Name,
	} {
		res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		mk[m] = id
	}
	a := regions.Regions[0].Markets[0].Name
	start, _ := time.Parse("2006-01-02", "2026-09-14")
	end, _ := time.Parse("2006-01-02", "2026-10-24")
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		ds := d.Format("2006-01-02")
		for m, id := range mk {
			v := 100.0
			if m == a && ds >= "2026-10-19" && ds <= "2026-10-23" {
				v = 80
			}
			if _, err := db.Exec(
				"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
				ds, id, commID, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

func openRO(t *testing.T, path string) *sql.DB {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testConfig(site string, weeks ...string) Config {
	return Config{
		SitePublic: site,
		Weeks:      weeks,
		BaseURL:    "https://abalol.xyz/presyo-bantay/",
		Now:        func() time.Time { return time.Date(2026, 10, 25, 8, 0, 0, 0, time.FixedZone("Asia/Manila", 8*3600)) },
	}
}

func readWeek(t *testing.T, site, week string) WeekDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(site, "w", week, "week.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc WeekDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestWeeksInRange(t *testing.T) {
	weeks, err := WeeksInRange("2026-W41", "2026-W43")
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 3 || weeks[0] != "2026-W41" || weeks[2] != "2026-W43" {
		t.Errorf("range = %v", weeks)
	}
	if _, err := WeeksInRange("2026-W43", "2026-W41"); err == nil {
		t.Error("reversed range must fail")
	}
}

func TestLatestReportableWeek(t *testing.T) {
	db := openRO(t, fixtureDB(t))
	sunday := time.Date(2026, 10, 25, 8, 0, 0, 0, time.FixedZone("Asia/Manila", 8*3600))
	got, err := LatestReportableWeek(db, sunday)
	if err != nil || got != "2026-W43" {
		t.Errorf("latest = %q %v", got, err)
	}
	// On Saturday the current week has not ended yet: W42.
	sat := time.Date(2026, 10, 24, 8, 0, 0, 0, time.FixedZone("Asia/Manila", 8*3600))
	got, err = LatestReportableWeek(db, sat)
	if err != nil || got != "2026-W42" {
		t.Errorf("saturday latest = %q %v", got, err)
	}
}

func TestRenderWeekJSON(t *testing.T) {
	db := openRO(t, fixtureDB(t))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-W43")); err != nil {
		t.Fatal(err)
	}
	doc := readWeek(t, site, "2026-W43")
	if doc.Week != "2026-W43" || !doc.Reportable || doc.Check != "pass" {
		t.Errorf("doc = %+v", doc)
	}
	if len(doc.Covered) < 3 {
		t.Errorf("covered = %v", doc.Covered)
	}
	for _, f := range []string{"week.png", "week.json", "index.html"} {
		if _, err := os.Stat(filepath.Join(site, "w", "2026-W43", f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if doc.Text == "" || !strings.Contains(doc.Text, "Lingguhang ulat") {
		t.Error("text missing")
	}
	if !strings.Contains(doc.Text, "https://abalol.xyz/presyo-bantay/w/2026-W43/") {
		t.Error("text missing week link")
	}
	if doc.Blocks.TL == "" || doc.Blocks.EN == "" || doc.Blocks.JA == "" {
		t.Error("empty blocks")
	}
	if doc.Label.TL == "" || doc.Label.EN == "" || doc.Label.JA == "" {
		t.Error("empty label")
	}
	// weeks.json newest-first.
	data, err := os.ReadFile(filepath.Join(site, "data", "weeks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wd WeeksDoc
	if err := json.Unmarshal(data, &wd); err != nil {
		t.Fatal(err)
	}
	if wd.Latest != "2026-W43" || len(wd.Weeks) != 1 {
		t.Errorf("weeks = %+v", wd)
	}
	if wd.Weeks[0].Path != "w/2026-W43/" || wd.Weeks[0].ID != "2026-W43" {
		t.Errorf("entry = %+v", wd.Weeks[0])
	}
	if _, err := os.Stat(filepath.Join(site, "w", "index.html")); err != nil {
		t.Errorf("missing w/index.html: %v", err)
	}
}

func TestFreeze(t *testing.T) {
	db := openRO(t, fixtureDB(t))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-W43")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(site, "w", "2026-W43", "week.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Re-run without -force: frozen, byte-identical.
	cfg := testConfig(site, "2026-W43")
	cfg.Now = func() time.Time { return time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC) }
	if err := Run(db, cfg); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(site, "w", "2026-W43", "week.json"))
	if string(after) != string(before) {
		t.Error("frozen week.json changed without -force")
	}
	// -force re-renders with the new timestamp.
	cfg.Force = true
	if err := Run(db, cfg); err != nil {
		t.Fatal(err)
	}
	forced, _ := os.ReadFile(filepath.Join(site, "w", "2026-W43", "week.json"))
	if string(forced) == string(before) {
		t.Error("forced re-render must update generated_at")
	}
}

func TestSkipUnreportable(t *testing.T) {
	db := openRO(t, fixtureDB(t))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-W30")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(site, "w", "2026-W30", "week.json")); !os.IsNotExist(err) {
		t.Error("unreportable week must be skipped")
	}
}

// TestWeekHTML checks OG/GA4/privacy/prev-next/day-links and determinism.
func TestWeekHTML(t *testing.T) {
	db := openRO(t, fixtureDB(t))
	site := t.TempDir()
	// Two weeks for prev/next testing: W42 and W43 both reportable in
	// the fixture (W41 has data too, so W42's prev week is covered).
	if err := Run(db, testConfig(site, "2026-W42", "2026-W43")); err != nil {
		t.Fatal(err)
	}
	// Fake one existing day page of W43 for the day-links test.
	for _, d := range []string{"2026-10-19", "2026-10-23"} {
		dir := filepath.Join(site, "d", d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "day.json"), []byte(`{"date":"`+d+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := testConfig(site)
	cfg.HTMLOnly = true
	if err := Run(db, cfg); err != nil {
		t.Fatal(err)
	}
	mid, err := os.ReadFile(filepath.Join(site, "w", "2026-W43", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(mid)
	base := "https://abalol.xyz/presyo-bantay/w/2026-W43/"
	for _, want := range []string{
		`og:type" content="article"`,
		`og:url" content="` + base + `"`,
		`og:image" content="https://abalol.xyz/presyo-bantay/w/2026-W43/week.png"`,
		`og:image:width" content="1080"`,
		`og:image:height" content="1350"`,
		`og:locale" content="fil_PH"`,
		`twitter:card" content="summary_large_image"`,
		`<link rel="canonical" href="` + base + `"`,
		`article:published_time" content="2026-10-25"`,
		`G-VL047YCMK8`,
		`googletagmanager.com/gtag/js?id=G-VL047YCMK8`,
		`<html lang="fil">`,
		`data-lang="tl"`,
		`data-lang="en"`,
		`data-lang="ja"`,
		`pb-lang`,
		`href="../2026-W42/"`, // older
		`da.gov.ph/price-monitoring`,
		`width="1080" height="1350"`,
		`Lingguhang ulat`,
		`Weekly report`,
		`週間レポート`,
		`aria-label="Wika / Language / 言語"`,
		`max-width:519px`,
		`../../d/2026-10-19/`,
		`../../d/2026-10-23/`,
		`Gumagamit ang site na ito ng Google Analytics para sa bilang ng bisita.`,
		`This site uses Google Analytics to count visits.`,
		`アクセス解析にGoogleアナリティクスを使っています。`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("week html missing %q", want)
		}
	}
	// Last week has no newer link; first has no older.
	if strings.Contains(html, "../2026-W44/") {
		t.Error("latest week must not link a newer week")
	}
	first, _ := os.ReadFile(filepath.Join(site, "w", "2026-W42", "index.html"))
	if strings.Contains(string(first), "../2026-W41/") {
		t.Error("first week must not link an older week")
	}
	if !strings.Contains(string(first), "../2026-W43/") {
		t.Error("first week must link the newer week")
	}
	// Archive newest-first with GA4 + canonical.
	arch, err := os.ReadFile(filepath.Join(site, "w", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	a := string(arch)
	if !strings.Contains(a, "G-VL047YCMK8") || !strings.Contains(a, `href="https://abalol.xyz/presyo-bantay/w/"`) {
		t.Error("archive head wrong")
	}
	i43 := strings.Index(a, "2026-W43/")
	i42 := strings.Index(a, "2026-W42/")
	if i43 < 0 || !(i43 < i42) {
		t.Error("archive not newest-first")
	}
	// Determinism: rebuild from disk only.
	snap, _ := os.ReadFile(filepath.Join(site, "w", "2026-W43", "index.html"))
	if err := Run(db, cfg); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(site, "w", "2026-W43", "index.html"))
	if string(again) != string(snap) {
		t.Error("week html not deterministic")
	}
}
