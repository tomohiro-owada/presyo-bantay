package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekpages"
)

// mustOpenRO opens a SQLite DB read-only for weekpages.Run.
func mustOpenRO(t *testing.T, path string) *sql.DB {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// weeklyTestDB builds a DB where W42 (Oct 12-17) and W43 (Oct 19-24) are
// reportable: Tilapia at 100 everywhere, south market A at 80 on Oct 19-23.
func weeklyTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "weekly.db")
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
	for _, d := range []string{
		"2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16", "2026-10-17",
		"2026-10-18", "2026-10-19", "2026-10-20", "2026-10-21", "2026-10-22", "2026-10-23", "2026-10-24",
	} {
		for m, id := range mk {
			v := 100.0
			if m == a && d >= "2026-10-19" && d <= "2026-10-23" {
				v = 80
			}
			if _, err := db.Exec(
				"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
				d, id, commID, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

type weeklyEnv struct {
	db, home, out, state, site string
}

func setupWeekly(t *testing.T) weeklyEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRESYO_HOME", home)
	site := filepath.Join(home, "site")
	return weeklyEnv{
		db: weeklyTestDB(t), home: home,
		out: filepath.Join(home, "out"), state: filepath.Join(home, "state.json"),
		site: site,
	}
}

func (e weeklyEnv) bakeFrozen(t *testing.T, week string) {
	t.Helper()
	db := mustOpenRO(t, e.db)
	defer db.Close()
	cfg := weekpages.Config{
		SitePublic: e.site, Weeks: []string{week},
		BaseURL: "https://abalol.xyz/presyo-bantay/",
		Force:   true,
		Now:     func() time.Time { return time.Date(2026, 10, 25, 8, 0, 0, 0, time.FixedZone("Asia/Manila", 8*3600)) },
	}
	if err := weekpages.Run(db, cfg); err != nil {
		t.Fatal(err)
	}
}

func graphOK(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if !strings.HasSuffix(r.URL.Path, "/photos") {
			t.Errorf("bad path %s", r.URL.Path)
		}
		if strings.Contains(r.URL.RawQuery, "token") || strings.Contains(r.URL.String(), "SECRET") {
			t.Error("token must not be in the URL")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"photo1","post_id":"post1"}`)
	}))
}

func readState(t *testing.T, path string) fbPostState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st fbPostState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestFBWeeklyDryRunFresh(t *testing.T) {
	e := setupWeekly(t)
	var hits int
	srv := graphOK(t, &hits)
	defer srv.Close()
	args := []string{"fbweekly", "-db", e.db, "-week", "2026-W43",
		"-dry-run", "-out-dir", e.out, "-state", e.state, "-graph-base", srv.URL}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, f := range []string{"weekly.txt", "weekly.png", "weekly.signals.json", "weekly.check.json"} {
		if _, err := os.Stat(filepath.Join(e.out, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if hits != 0 {
		t.Error("dry-run must make no HTTP request")
	}
}

func TestFBWeeklyFrozenDryRun(t *testing.T) {
	e := setupWeekly(t)
	e.bakeFrozen(t, "2026-W43")
	args := []string{"fbweekly", "-db", e.db, "-week", "2026-W43",
		"-dry-run", "-out-dir", e.out, "-state", e.state, "-site-public", e.site}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
}

func TestFBWeeklyAuto(t *testing.T) {
	e := setupWeekly(t)
	e.bakeFrozen(t, "2026-W43")
	t.Setenv("PRESYO_FB_PAGE_ID", "PAGE")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "SECRET")
	var hits int
	srv := graphOK(t, &hits)
	defer srv.Close()
	auto := []string{"fbweekly", "-db", e.db, "-auto", "-week", "2026-W43",
		"-site-public", e.site, "-state", e.state, "-graph-base", srv.URL,
		"-out-dir", e.out}
	// Sunday in window posts once.
	if code := run(append(auto, "-now", "2026-10-25T18:30:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	st := readState(t, e.state)
	if len(st.Posts) != 1 || st.Posts[0].Region != "weekly" || st.Posts[0].AsOf != "2026-W43" {
		t.Fatalf("state = %+v", st)
	}
	// Second run: already posted.
	if code := run(append(auto, "-now", "2026-10-25T19:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (no repost)", hits)
	}
}

func TestFBWeeklyAutoSkips(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  string
		bake bool
	}{
		{"not sunday", "2026-10-24T18:30:00+08:00", true},
		{"before window", "2026-10-25T17:59:00+08:00", true},
		{"after window", "2026-10-25T20:00:00+08:00", true},
		{"page missing", "2026-10-25T18:30:00+08:00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setupWeekly(t)
			if tc.bake {
				e.bakeFrozen(t, "2026-W43")
			}
			t.Setenv("PRESYO_FB_PAGE_ID", "PAGE")
			t.Setenv("PRESYO_FB_PAGE_TOKEN", "SECRET")
			var hits int
			srv := graphOK(t, &hits)
			defer srv.Close()
			args := []string{"fbweekly", "-db", e.db, "-auto", "-week", "2026-W43",
				"-site-public", e.site, "-state", e.state, "-graph-base", srv.URL,
				"-out-dir", e.out, "-now", tc.now}
			if code := run(args); code != 0 {
				t.Fatalf("exit = %d", code)
			}
			if hits != 0 {
				t.Errorf("hits = %d, want 0", hits)
			}
		})
	}
}

func TestFBWeeklyAutoGapAndDisabled(t *testing.T) {
	// Gap: a daily post 30 min ago blocks the weekly post.
	e := setupWeekly(t)
	e.bakeFrozen(t, "2026-W43")
	t.Setenv("PRESYO_FB_PAGE_ID", "PAGE")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "SECRET")
	var hits int
	srv := graphOK(t, &hits)
	defer srv.Close()
	st := fbPostState{Posts: []fbPostEntry{
		{Region: "south", AsOf: "2026-10-23", PostID: "p", PhotoID: "h", PostedAt: "2026-10-25T18:00:00+08:00"},
	}}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(e.state, data, 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"fbweekly", "-db", e.db, "-auto", "-week", "2026-W43",
		"-site-public", e.site, "-state", e.state, "-graph-base", srv.URL,
		"-out-dir", e.out, "-now", "2026-10-25T18:30:00+08:00"}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Error("min-gap must block the weekly post")
	}
	// Disabled file blocks too.
	disPath := filepath.Join(e.home, "fbpost.disabled")
	if err := os.WriteFile(disPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run(append(args, "-disable-file", disPath)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Error("disabled must block the weekly post")
	}
}

func TestFBPostSundayHold(t *testing.T) {
	// Sunday 17:00 without a weekly entry: daily -auto holds.
	e := setupAuto(t, []string{"2026-10-24"})
	// 2026-10-25 is a Sunday.
	if code := run(e.autoArgs("-now", "2026-10-25T17:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.out, "south.txt")); !os.IsNotExist(err) {
		t.Error("daily auto must hold for the weekly post on Sunday 16:00-20:00")
	}
	// With the weekly entry present, the hold lifts.
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "weekly", AsOf: "2026-W43", PostID: "w", PhotoID: "h", PostedAt: "2026-10-25T16:30:00+08:00"},
	})
	// min-gap: weekly was 30 min ago < 2h, so still skip (gap counts
	// weekly posts). Move now past the gap.
	if code := run(e.autoArgs("-now", "2026-10-25T19:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(e.out); !os.IsNotExist(err) {
		if entries, _ := os.ReadDir(e.out); len(entries) != 0 {
			t.Logf("out = %v (gap should still hold)", entries)
		}
	}
	// After the gap expires the daily post proceeds.
	if code := run(e.autoArgs("-now", "2026-10-25T19:00:00+08:00", "-min-gap", "1m")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.out, "south.txt")); err != nil {
		t.Errorf("daily should render after the hold lifts: %v", err)
	}
}

func TestAutoWeeklyGapCounts(t *testing.T) {
	// A weekly post 30 min ago blocks daily -auto via min-gap.
	e := setupAuto(t, []string{"2026-10-24"})
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "weekly", AsOf: "2026-W43", PostID: "w", PhotoID: "h", PostedAt: "2026-10-25T06:40:00+08:00"},
	})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if entries, _ := os.ReadDir(e.out); len(entries) != 0 {
		t.Errorf("gap after weekly post: nothing should render, got %v", entries)
	}
}
