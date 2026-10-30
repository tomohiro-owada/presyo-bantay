package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
)

// utilEnv is an fbutil test sandbox: temp DB/home/site/state.
type utilEnv struct {
	db, home, site, out, state string
}

func setupUtil(t *testing.T, dates []string) utilEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRESYO_HOME", home)
	site := filepath.Join(home, "site")
	return utilEnv{
		db: autoTestDB(t, dates), home: home, site: site,
		out: filepath.Join(home, "out"), state: filepath.Join(home, "state.json"),
	}
}

func (e utilEnv) autoArgs(extra ...string) []string {
	return append([]string{
		"fbutil", "-db", e.db, "-auto", "-dry-run",
		"-site-public", e.site, "-out-dir", e.out, "-state", e.state,
	}, extra...)
}

// writeFrozenPost stages SITE/u/<kind>/<id>/{post.json,<kind>.png}.
func writeFrozenPost(t *testing.T, site, kind, id, genAt string, extra map[string]string) {
	t.Helper()
	dir := filepath.Join(site, "u", kind, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]string{
		"text": "post " + kind + " " + id, "kind": kind,
		"as_of": id, "generated_at": genAt, "check": "pass",
	}
	for k, v := range extra {
		doc[k] = v
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "post.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, kind+".png"), []byte("fakepng-"+kind), 0o644); err != nil {
		t.Fatal(err)
	}
}

func outFiles(t *testing.T, out string) []string {
	t.Helper()
	var names []string
	entries, err := os.ReadDir(out)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func regionEntries(t *testing.T, asOf, postedAt string, skip string) []fbPostEntry {
	t.Helper()
	var out []fbPostEntry
	for _, id := range regions.IDs() {
		if id == skip {
			continue
		}
		out = append(out, fbPostEntry{Region: id, AsOf: asOf, PostID: "p-" + id, PhotoID: "h-" + id, PostedAt: postedAt})
	}
	return out
}

func TestFBUtilAutoTable(t *testing.T) {
	oldGen := "2026-10-20T10:00:00+08:00"
	for _, tc := range []struct {
		name      string
		now       string
		dbDates   []string
		state     []fbPostEntry
		setupSite func(t *testing.T, e utilEnv)
		wantFiles []string // basenames expected in out dir (nil = none)
	}{
		{
			name: "outside window", now: "2026-10-20T05:00:00+08:00", dbDates: []string{"2026-10-19"},
		},
		{
			name: "sunday hold", now: "2026-10-25T17:00:00+08:00", dbDates: []string{"2026-10-19"},
		},
		{
			name: "disabled", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
		},
		{
			name: "gap", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-19", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T12:40:00+08:00"}},
		},
		{
			name: "da pending", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-19", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				// Age 7: fuel is not due, so the DA gate still skips.
				writeFrozenPost(t, e.site, "fuel", "2026-10-13", oldGen, nil)
			},
		},
		{
			name: "da done fuel", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
			},
			wantFiles: []string{"fuel.png", "fuel.txt"},
		},
		{
			// Fuel keeps the routine window 12:00-19:00: at 08:00
			// (inside the 05:30-20:00 water/power window) fuel is
			// skipped and the run falls through to "nothing due".
			name: "fuel outside routine window", now: "2026-10-20T08:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
			},
		},
		{
			name: "fuel already posted skips to nothing", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "util-fuel", AsOf: "2026-10-20", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"})
				writeAutoState(t, e.state, st)
			},
		},
		{
			name: "bills when fuel done", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
				writeFrozenPost(t, e.site, "bills", "2026-10", oldGen, nil)
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "util-fuel", AsOf: "2026-10-20", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"})
				writeAutoState(t, e.state, st)
			},
			wantFiles: []string{"bills.png", "bills.txt"},
		},
		{
			name: "page too fresh", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", "2026-10-20T12:56:00+08:00", nil)
			},
		},
		{
			name: "water exempt from da first", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-19", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", oldGen, map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
		{
			name: "one water per day", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "util-water", AsOf: "oldid", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T08:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", oldGen, map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
			},
		},
		{
			name: "emergency not yet ended", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid9", oldGen, map[string]string{
					"start": "2026-10-20T10:00:00+08:00", "end": "2026-10-20T18:00:00+08:00", "alert_kind": "emergency",
				})
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setupUtil(t, tc.dbDates)
			if tc.state != nil {
				writeAutoState(t, e.state, tc.state)
			}
			if tc.name == "disabled" {
				if err := os.MkdirAll(filepath.Join(e.home, ".config", "presyo"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(e.home, ".config", "presyo", "fbpost.disabled"), []byte{}, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.setupSite != nil {
				tc.setupSite(t, e)
			}
			if code := run(e.autoArgs("-now", tc.now)); code != 0 {
				t.Fatalf("exit = %d", code)
			}
			got := outFiles(t, e.out)
			if len(got) != len(tc.wantFiles) {
				t.Fatalf("out files = %v, want %v", got, tc.wantFiles)
			}
			for _, w := range tc.wantFiles {
				found := false
				for _, g := range got {
					if g == w {
						found = true
					}
				}
				if !found {
					t.Fatalf("out files = %v, want %v", got, tc.wantFiles)
				}
			}
		})
	}
}

// TestFBUtilAutoPosts ensures a selected post actually uploads once via
// httptest Graph and records util-* state.
func TestFBUtilAutoPosts(t *testing.T) {
	e := setupUtil(t, []string{"2026-10-19"})
	writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
	writeFrozenPost(t, e.site, "fuel", "2026-10-20", "2026-10-20T10:00:00+08:00", nil)
	t.Setenv("PRESYO_FB_PAGE_ID", "PAGE")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "SECRET")
	var hits int
	srv := graphOK(t, &hits)
	defer srv.Close()
	args := []string{"fbutil", "-db", e.db, "-auto", "-site-public", e.site,
		"-out-dir", e.out, "-state", e.state, "-graph-base", srv.URL,
		"-now", "2026-10-20T13:00:00+08:00"}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	st := readState(t, e.state)
	found := false
	for _, p := range st.Posts {
		if p.Region == "util-fuel" && p.AsOf == "2026-10-20" {
			found = true
		}
	}
	if !found {
		t.Fatalf("state = %+v", st.Posts)
	}
	// Second run: already posted (bills page missing → nothing due).
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (no repost)", hits)
	}
}

// TestFBUtilManualDryRun stages frozen copies without state or token.
func TestFBUtilManualDryRun(t *testing.T) {
	e := setupUtil(t, []string{"2026-10-19"})
	writeFrozenPost(t, e.site, "bills", "2026-10", "2026-10-20T10:00:00+08:00", nil)
	t.Setenv("PRESYO_FB_PAGE_ID", "")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "")
	args := []string{"fbutil", "-db", e.db, "-kind", "bills", "-dry-run",
		"-site-public", e.site, "-out-dir", e.out, "-state", e.state}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, name := range []string{"bills.txt", "bills.png"} {
		if _, err := os.Stat(filepath.Join(e.out, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(e.state); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write state")
	}
}

// TestFBPostUtilGap is the regression test: a util entry counts toward
// fbpost -auto min-gap but never fills a region slot.
func TestFBPostUtilGap(t *testing.T) {
	// Recent util entry blocks fbpost within 2h.
	e := setupAuto(t, []string{"2026-10-24"})
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "util-fuel", AsOf: "2026-10-20", PostID: "p", PhotoID: "h", PostedAt: "2026-10-25T06:40:00+08:00"},
	})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got := outFiles(t, e.out); len(got) != 0 {
		t.Fatalf("fbpost posted despite util gap: %v", got)
	}
	// Old util entry does not fill a region slot: the missing region
	// is still selected.
	e2 := setupAuto(t, []string{"2026-10-24"})
	st := regionEntries(t, "2026-10-24", "2026-10-24T05:00:00+08:00", "manila")
	st = append(st, fbPostEntry{Region: "util-fuel", AsOf: "2026-10-20", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T06:00:00+08:00"})
	writeAutoState(t, e2.state, st)
	if code := run(e2.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e2.out, "manila.txt")); err != nil {
		t.Fatalf("manila should be selected: %v", err)
	}
}

// TestFBUtilUsage checks flag handling without network.
func TestFBUtilUsage(t *testing.T) {
	if code := run([]string{"fbutil"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if code := run([]string{"fbutil", "-kind", "bogus", "-dry-run"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if code := run([]string{"fbutil", "-kind", "fuel", "-dry-run", "-site-public", t.TempDir()}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}
