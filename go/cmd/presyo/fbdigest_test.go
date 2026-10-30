package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/digest"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

const fbdigestTestD = "2026-10-30"

var fbdigestTestRaws = []string{
	"Well-milled Rice (Local)",
	"Fresh Pork Liempo (Local)",
	"Fresh Whole Chicken",
	"Egg (Medium)",
	"Galunggong",
	"Tomato",
	"Red Onion (Local)",
	"Sugar (Refined)",
}

var fbdigestTestBase = map[string]float64{
	"Well-milled Rice (Local)":  60,
	"Fresh Pork Liempo (Local)": 340,
	"Fresh Whole Chicken":       185,
	"Egg (Medium)":              7.5,
	"Galunggong":                220,
	"Tomato":                    95,
	"Red Onion (Local)":         160,
	"Sugar (Refined)":           92,
}

// fbdigestTestDB builds a temp DB with the first 3 markets of all 5
// regions, all 8 key items priced flat for 12 days up to fbdigestTestD.
func fbdigestTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "digest.db")
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mkIDs := map[string]int64{}
	for _, r := range regions.Regions {
		for _, m := range r.Markets[:3] {
			res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m.Name)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := res.LastInsertId()
			mkIDs[m.Name] = id
		}
	}
	commIDs := map[string]int64{}
	for _, raw := range fbdigestTestRaws {
		unit := "per kg"
		if raw == "Egg (Medium)" {
			unit = "per piece"
		}
		res, err := db.Exec("INSERT INTO commodities (name, category, unit) VALUES (?, 'misc', ?)", raw, unit)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		commIDs[raw] = id
	}
	d, _ := time.Parse("2006-01-02", fbdigestTestD)
	for ago := 11; ago >= 0; ago-- {
		ds := d.AddDate(0, 0, -ago).Format("2006-01-02")
		for _, mid := range mkIDs {
			for raw, cid := range commIDs {
				if _, err := db.Exec(
					"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
					ds, mid, cid, fbdigestTestBase[raw]); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return path
}

type fbdigestEnv struct {
	db, home, state string
}

func setupFBDigest(t *testing.T) fbdigestEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRESYO_HOME", home)
	t.Setenv("PRESYO_FB_PAGE_ID", "12345")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "test-token-abc")
	t.Cleanup(func() { fbdigestForceFail = nil })
	return fbdigestEnv{db: fbdigestTestDB(t), home: home, state: filepath.Join(home, "state.json")}
}

func (e fbdigestEnv) args(extra ...string) []string {
	return append([]string{"fbdigest", "-db", e.db, "-state", e.state}, extra...)
}

func digestGraph(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"photo1","post_id":"post1"}`)
	}))
}

func writeDigestState(t *testing.T, path string, posts []fbPostEntry) {
	t.Helper()
	data, err := json.Marshal(fbPostState{Posts: posts})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readDigestState(t *testing.T, path string) fbPostState {
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

func digestEntry(t *testing.T, st fbPostState) fbPostEntry {
	t.Helper()
	for _, p := range st.Posts {
		if p.Region == digestRegion {
			return p
		}
	}
	t.Fatalf("no %q entry in %+v", digestRegion, st.Posts)
	return fbPostEntry{}
}

func TestFBDigestDryRun(t *testing.T) {
	e := setupFBDigest(t)
	t.Setenv("PRESYO_FB_PAGE_ID", "")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "")
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	out := filepath.Join(e.home, "out")
	if code := run(e.args("-date", fbdigestTestD, "-dry-run", "-out-dir", out, "-graph-base", srv.URL)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Errorf("dry-run must make no HTTP request, hits = %d", hits)
	}
	for _, f := range []string{"digest.png", "digest.txt", "digest.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing artifact %s: %v", f, err)
		}
	}
	if _, err := os.Stat(e.state); !os.IsNotExist(err) {
		t.Error("dry-run must not touch the state file")
	}
}

func TestFBDigestAutoWindow(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	base := []string{"-auto", "-graph-base", srv.URL}
	if code := run(e.args(append(base, "-now", "2026-10-30T06:59:00+08:00", "-out-dir", filepath.Join(e.home, "o1"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if code := run(e.args(append(base, "-now", "2026-10-30T19:00:00+08:00", "-out-dir", filepath.Join(e.home, "o2"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Fatalf("outside window must not post, hits = %d", hits)
	}
	if _, err := os.Stat(filepath.Join(e.home, "o1")); !os.IsNotExist(err) {
		t.Error("outside window must render nothing")
	}
	if code := run(e.args(append(base, "-now", "2026-10-30T07:00:00+08:00", "-out-dir", filepath.Join(e.home, "o3"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("inside window must post once, hits = %d", hits)
	}
	st := readDigestState(t, e.state)
	p := digestEntry(t, st)
	if p.AsOf != fbdigestTestD || len(p.Regions) != 5 {
		t.Errorf("state = %+v, want as_of=%s with 5 regions", p, fbdigestTestD)
	}
	// Second run in the same minute: nothing new, no second post.
	if code := run(e.args(append(base, "-now", "2026-10-30T07:00:00+08:00", "-out-dir", filepath.Join(e.home, "o4"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Errorf("second run must not post again, hits = %d", hits)
	}
	if got := len(readDigestState(t, e.state).Posts); got != 1 {
		t.Errorf("state posts = %d, want 1", got)
	}
}

func TestFBDigestWeekdayWindowStart(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	base := []string{"-auto", "-graph-base", srv.URL, "-weekday-window-start", "09:00"}
	// Monday 08:00 is before the weekday override: skip.
	if code := run(e.args(append(base, "-date", "2026-10-26", "-now", "2026-10-26T08:00:00+08:00",
		"-out-dir", filepath.Join(e.home, "mon"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	// Friday 08:00 is delayed too.
	if code := run(e.args(append(base, "-now", "2026-10-30T08:00:00+08:00",
		"-out-dir", filepath.Join(e.home, "fri"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Fatalf("weekday override must delay, hits = %d", hits)
	}
	// Saturday uses -window-start (07:00): 08:00 posts.
	if code := run(e.args(append(base, "-now", "2026-10-31T08:00:00+08:00",
		"-out-dir", filepath.Join(e.home, "sat"))...)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Errorf("saturday must use window-start, hits = %d", hits)
	}
}

func TestFBDigestMinGap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		posted  string
		wantHit bool
	}{
		{"blocked60m", "2026-10-30T06:00:00+08:00", false},
		{"allowed80m", "2026-10-30T05:40:00+08:00", true},
		{"grace74m", "2026-10-30T05:46:00+08:00", true}, // 74m < 75m but within 5m grace
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setupFBDigest(t)
			var hits int
			srv := digestGraph(t, &hits)
			defer srv.Close()
			writeDigestState(t, e.state, []fbPostEntry{
				{Region: "util-fuel", AsOf: "2026-10-30", PostID: "u", PhotoID: "u", PostedAt: tc.posted},
			})
			code := run(e.args("-auto", "-min-gap", "75m", "-now", "2026-10-30T07:00:00+08:00",
				"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL))
			if code != 0 {
				t.Fatalf("exit = %d", code)
			}
			want := 0
			if tc.wantHit {
				want = 1
			}
			if hits != want {
				t.Errorf("hits = %d, want %d", hits, want)
			}
		})
	}
}

func TestFBDigestCapAndDigestGap(t *testing.T) {
	t.Run("dailyCap", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		writeDigestState(t, e.state, []fbPostEntry{
			{Region: digestRegion, AsOf: fbdigestTestD, PostID: "a", PhotoID: "a", PostedAt: "2026-10-30T04:00:00+08:00", Regions: []string{"south"}},
			{Region: digestRegion, AsOf: fbdigestTestD, PostID: "b", PhotoID: "b", PostedAt: "2026-10-30T05:00:00+08:00", Regions: []string{"manila"}},
		})
		code := run(e.args("-auto", "-max-per-day", "2", "-digest-gap", "30m",
			"-now", "2026-10-30T07:00:00+08:00",
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL))
		if code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 0 {
			t.Errorf("daily cap must block, hits = %d", hits)
		}
	})
	t.Run("digestGap", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		writeDigestState(t, e.state, []fbPostEntry{
			{Region: digestRegion, AsOf: fbdigestTestD, PostID: "a", PhotoID: "a", PostedAt: "2026-10-30T05:00:00+08:00", Regions: []string{"south"}},
		})
		code := run(e.args("-auto", "-digest-gap", "3h",
			"-now", "2026-10-30T07:00:00+08:00",
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL))
		if code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 0 {
			t.Errorf("digest gap must block, hits = %d", hits)
		}
	})
	t.Run("gapExpired", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		writeDigestState(t, e.state, []fbPostEntry{
			{Region: digestRegion, AsOf: fbdigestTestD, PostID: "a", PhotoID: "a", PostedAt: "2026-10-30T03:00:00+08:00", Regions: []string{"south"}},
		})
		code := run(e.args("-auto", "-digest-gap", "3h",
			"-now", "2026-10-30T07:00:00+08:00",
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL))
		if code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 1 {
			t.Errorf("expired gap must allow a post, hits = %d", hits)
		}
	})
}

func TestFBDigestAfternoonUpdate(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	base := []string{"-auto", "-graph-base", srv.URL}
	fbdigestForceFail = func(id string) bool { return id == "east" }
	if code := run(e.args(append(base, "-now", "2026-10-30T07:00:00+08:00", "-out-dir", filepath.Join(e.home, "am"))...)); code != 0 {
		t.Fatalf("morning exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("morning must post, hits = %d", hits)
	}
	first := digestEntry(t, readDigestState(t, e.state))
	if len(first.Regions) != 4 {
		t.Fatalf("morning regions = %v, want 4", first.Regions)
	}
	for _, r := range first.Regions {
		if r == "east" {
			t.Fatalf("morning must not cover east: %v", first.Regions)
		}
	}
	fbdigestForceFail = nil
	if code := run(e.args(append(base, "-now", "2026-10-30T13:00:00+08:00", "-out-dir", filepath.Join(e.home, "pm"))...)); code != 0 {
		t.Fatalf("afternoon exit = %d", code)
	}
	if hits != 2 {
		t.Fatalf("afternoon must post the new region, hits = %d", hits)
	}
	st := readDigestState(t, e.state)
	if len(st.Posts) != 2 || len(st.Posts[1].Regions) != 5 {
		t.Fatalf("state = %+v, want a second post covering 5", st.Posts)
	}
	// All five covered: never posts again for D even with tiny gaps.
	if code := run(e.args(append(base, "-min-gap", "1m", "-digest-gap", "1m", "-max-per-day", "3",
		"-now", "2026-10-30T15:00:00+08:00", "-out-dir", filepath.Join(e.home, "late"))...)); code != 0 {
		t.Fatalf("late exit = %d", code)
	}
	if hits != 2 {
		t.Errorf("fully covered D must never repost, hits = %d", hits)
	}
}

func TestFBDigestLegacyCoverage(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	writeDigestState(t, e.state, []fbPostEntry{
		{Region: "south", AsOf: fbdigestTestD, PostID: "s", PhotoID: "s", PostedAt: "2026-10-30T05:00:00+08:00"},
		{Region: "manila", AsOf: fbdigestTestD, PostID: "m", PhotoID: "m", PostedAt: "2026-10-30T05:01:00+08:00"},
	})
	out := filepath.Join(e.home, "out")
	if code := run(e.args("-auto", "-now", "2026-10-30T07:00:00+08:00",
		"-out-dir", out, "-graph-base", srv.URL)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("legacy coverage leaves fresh regions, hits = %d", hits)
	}
	p := digestEntry(t, readDigestState(t, e.state))
	if len(p.Regions) != 5 {
		t.Errorf("posted regions = %v, want all 5", p.Regions)
	}
	data, err := os.ReadFile(filepath.Join(out, "digest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d digest.Digest
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	if got := d.Included(); len(got) != 5 {
		t.Errorf("image digest covers %v, want all 5", got)
	}
}

func TestFBDigestDisableFile(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	dis := filepath.Join(e.home, "fbpost.disabled")
	if err := os.WriteFile(dis, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run(e.args("-auto", "-disable-file", dis, "-now", "2026-10-30T07:00:00+08:00",
		"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Errorf("kill switch must skip, hits = %d", hits)
	}
}

func TestFBDigestStale(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	if code := run(e.args("-auto", "-now", "2026-11-03T07:00:00+08:00",
		"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Errorf("stale data must skip, hits = %d", hits)
	}
}

func TestFBDigestForceFail(t *testing.T) {
	t.Run("droppedButPosts", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		fbdigestForceFail = func(id string) bool { return id == "east" }
		if code := run(e.args("-auto", "-now", "2026-10-30T07:00:00+08:00",
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 1 {
			t.Fatalf("4 included regions must still post, hits = %d", hits)
		}
		if got := digestEntry(t, readDigestState(t, e.state)).Regions; len(got) != 4 {
			t.Errorf("posted regions = %v, want 4", got)
		}
	})
	t.Run("tooFewIncluded", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		fbdigestForceFail = func(id string) bool { return id == "south" || id == "manila" || id == "qc" }
		if code := run(e.args("-auto", "-now", "2026-10-30T07:00:00+08:00",
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 0 {
			t.Errorf("<3 included must skip, hits = %d", hits)
		}
	})
}

func TestFBDigestSundayHold(t *testing.T) {
	now := "2026-11-01T17:00:00+08:00" // Sunday 17:00 Manila
	t.Run("holdsWithoutWeekly", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		if code := run(e.args("-auto", "-now", now,
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 0 {
			t.Errorf("sunday hold must skip, hits = %d", hits)
		}
	})
	t.Run("postsWithWeekly", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		tn, _ := time.Parse(time.RFC3339, now)
		writeDigestState(t, e.state, []fbPostEntry{
			{Region: "weekly", AsOf: weekly.WeekIDOf(tn), PostID: "w", PhotoID: "w", PostedAt: "2026-11-01T10:00:00+08:00"},
		})
		if code := run(e.args("-auto", "-now", now,
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 1 {
			t.Errorf("weekly done must allow the digest, hits = %d", hits)
		}
	})
}

func TestFBDigestSitePublic(t *testing.T) {
	now := []string{"-auto", "-now", "2026-10-30T07:00:00+08:00"}
	t.Run("missing", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		args := append(e.args(), now...)
		args = append(args, "-site-public", filepath.Join(e.home, "site"),
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)
		if code := run(args); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 0 {
			t.Errorf("missing day.json must skip, hits = %d", hits)
		}
	})
	t.Run("tooFresh", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		site := filepath.Join(e.home, "site")
		if err := os.MkdirAll(filepath.Join(site, "d", fbdigestTestD), 0o755); err != nil {
			t.Fatal(err)
		}
		doc := `{"generated_at":"2026-10-30T06:58:00+08:00"}` + "\n"
		if err := os.WriteFile(filepath.Join(site, "d", fbdigestTestD, "day.json"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		args := append(e.args(), now...)
		args = append(args, "-site-public", site,
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)
		if code := run(args); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 0 {
			t.Errorf("2-minute-old day page must skip, hits = %d", hits)
		}
	})
	t.Run("ready", func(t *testing.T) {
		e := setupFBDigest(t)
		var hits int
		srv := digestGraph(t, &hits)
		defer srv.Close()
		site := filepath.Join(e.home, "site")
		if err := os.MkdirAll(filepath.Join(site, "d", fbdigestTestD), 0o755); err != nil {
			t.Fatal(err)
		}
		doc := `{"generated_at":"2026-10-30T06:50:00+08:00"}` + "\n"
		if err := os.WriteFile(filepath.Join(site, "d", fbdigestTestD, "day.json"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		args := append(e.args(), now...)
		args = append(args, "-site-public", site,
			"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)
		if code := run(args); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if hits != 1 {
			t.Errorf("10-minute-old day page must allow the post, hits = %d", hits)
		}
	})
}
