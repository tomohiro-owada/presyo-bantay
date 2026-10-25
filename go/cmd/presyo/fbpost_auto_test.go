package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// autoTestDB builds a DB with south-market prices on the given dates.
func autoTestDB(t *testing.T, dates []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auto.db")
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mk := map[string]int64{}
	for _, m := range []string{"New Las Piñas City Public Market", "Suki Market Moonwalk"} {
		res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		mk[m] = id
	}
	res, err := db.Exec("INSERT INTO commodities (name, category, unit) VALUES ('Tilapia', 'fish', 'per kg')")
	if err != nil {
		t.Fatal(err)
	}
	comm, _ := res.LastInsertId()
	for _, d := range dates {
		for _, id := range mk {
			if _, err := db.Exec("INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, 100)",
				d, id, comm); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

type autoEnv struct {
	db, home, out, state string
}

func setupAuto(t *testing.T, dates []string) autoEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRESYO_HOME", home)
	return autoEnv{
		db: autoTestDB(t, dates), home: home,
		out: filepath.Join(home, "out"), state: filepath.Join(home, "state.json"),
	}
}

func (e autoEnv) autoArgs(extra ...string) []string {
	return append([]string{
		"fbpost", "-db", e.db, "-auto", "-dry-run",
		"-out-dir", e.out, "-state", e.state,
	}, extra...)
}

func writeAutoState(t *testing.T, path string, posts []fbPostEntry) {
	t.Helper()
	data, err := json.Marshal(fbPostState{Posts: posts})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoOrder(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-24"})
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "south", AsOf: "2026-10-24", PostID: "p1", PhotoID: "h1", PostedAt: "2026-10-24T05:00:00+08:00"},
	})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.out, "manila.txt")); err != nil {
		t.Errorf("next region should be manila: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.out, "south.txt")); !os.IsNotExist(err) {
		t.Error("south must not be reposted")
	}
}

func TestAutoGap(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-24"})
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "south", AsOf: "2026-10-24", PostID: "p1", PhotoID: "h1", PostedAt: "2026-10-25T06:40:00+08:00"},
	})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if entries, _ := os.ReadDir(e.out); len(entries) != 0 {
		t.Errorf("gap not expired: nothing should render, got %v", entries)
	}
	// After the gap expires the next region is due.
	if code := run(e.autoArgs("-now", "2026-10-25T09:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.out, "manila.txt")); err != nil {
		t.Errorf("after gap, manila should render: %v", err)
	}
}

func TestAutoWindowEdges(t *testing.T) {
	for _, tc := range []struct {
		now  string
		want bool // true = renders south.txt
	}{
		{"2026-10-24T05:29:00+08:00", false},
		{"2026-10-24T05:30:00+08:00", true},
		{"2026-10-24T19:59:00+08:00", true},
		{"2026-10-24T20:00:00+08:00", false},
	} {
		e := setupAuto(t, []string{"2026-10-24"})
		if code := run(e.autoArgs("-now", tc.now)); code != 0 {
			t.Fatalf("%s: exit = %d", tc.now, code)
		}
		_, err := os.Stat(filepath.Join(e.out, "south.txt"))
		if tc.want && err != nil {
			t.Errorf("%s: south should render", tc.now)
		}
		if !tc.want && err == nil {
			t.Errorf("%s: nothing should render outside window", tc.now)
		}
	}
}

func TestAutoSwitchNewerDate(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-23", "2026-10-24"})
	// South was posted for the older date only; the newer date takes over.
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "south", AsOf: "2026-10-23", PostID: "p1", PhotoID: "h1", PostedAt: "2026-10-23T07:00:00+08:00"},
	})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(e.out, "south.signals.json"))
	if err != nil {
		t.Fatalf("south should render for the new date: %v", err)
	}
	var sig struct {
		AsOf string `json:"as_of"`
	}
	if err := json.Unmarshal(data, &sig); err != nil {
		t.Fatal(err)
	}
	if sig.AsOf != "2026-10-24" {
		t.Errorf("as_of = %q, want 2026-10-24", sig.AsOf)
	}
}

func TestAutoAllDone(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-24"})
	var posts []fbPostEntry
	for _, r := range []string{"south", "manila", "qc", "north", "east"} {
		posts = append(posts, fbPostEntry{Region: r, AsOf: "2026-10-24", PostID: r, PostedAt: "2026-10-24T06:00:00+08:00"})
	}
	writeAutoState(t, e.state, posts)
	if code := run(e.autoArgs("-now", "2026-10-25T12:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(e.out); !os.IsNotExist(err) {
		t.Error("all done: nothing should render")
	}
}

func TestAutoStale(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-20"})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(e.out); !os.IsNotExist(err) {
		t.Error("stale D: nothing should render")
	}
}

func TestAutoDryRunNeverTouchesNetworkOrState(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-24"})
	// No credentials: dry-run must not need them.
	t.Setenv("PRESYO_FB_PAGE_ID", "")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()
	before := []byte(`{"posts":[]}`)
	if err := os.WriteFile(e.state, before, 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"fbpost", "-db", e.db, "-auto", "-dry-run",
		"-out-dir", e.out, "-state", e.state,
		"-graph-base", srv.URL, "-now", "2026-10-24T07:00:00+08:00"}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 0 {
		t.Errorf("auto dry-run must make no HTTP request, hits = %d", hits)
	}
	after, err := os.ReadFile(e.state)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("state file changed by dry-run: before %q after %q", before, after)
	}
	if _, err := os.Stat(filepath.Join(e.out, "south.txt")); err != nil {
		t.Errorf("auto dry-run should render south.txt: %v", err)
	}
}

func TestAutoNoRegionFlagNeeded(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-24"})
	if code := run(e.autoArgs("-now", "2026-10-24T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.out, "south.txt")); err != nil {
		t.Errorf("first region should be south: %v", err)
	}
	_ = time.Now
}
