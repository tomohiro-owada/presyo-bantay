package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writePowerFrozen stages SITE/u/power/<id>/{post.json,power.png}.
func writePowerFrozen(t *testing.T, site, id, genAt, start, end string) {
	t.Helper()
	writeFrozenPost(t, site, "power", id, genAt, map[string]string{
		"start": start, "end": end, "alert_kind": "power",
	})
}

func TestFBUtilPowerAutoTable(t *testing.T) {
	oldGen := "2026-10-20T10:00:00+08:00"
	for _, tc := range []struct {
		name      string
		now       string
		dbDates   []string
		state     []fbPostEntry
		setupSite func(t *testing.T, e utilEnv)
		wantFiles []string
	}{
		{
			name: "power due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "already posted same id", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "util-power", AsOf: "pow1", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
		},
		{
			name: "one power per day", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "util-power", AsOf: "oldid", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T08:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
		},
		{
			// Short notice (starts in 1h) is due: the old 3h lead
			// is gone, only not-yet-ended and within 72h matter.
			name: "short notice due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-20T14:00:00+08:00", "2026-10-20T18:00:00+08:00")
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "too far over 72h", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-24T13:00:00+08:00", "2026-10-24T18:00:00+08:00")
			},
		},
		{
			name: "gap blocks power", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-19", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T12:40:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
		},
		{
			// Cross-source overlap with a posted water window is NOT
			// relevant for power: the power alert is still due.
			name: "water overlap ignored", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "util-water", AsOf: "wid0", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid0", oldGen, map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "water beats power", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", oldGen, map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
		{
			name: "power beats fuel", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				writeAutoState(t, e.state, st)
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "page too fresh", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", "2026-10-20T12:56:00+08:00", "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setupUtil(t, tc.dbDates)
			if tc.state != nil {
				writeAutoState(t, e.state, tc.state)
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

// TestFBUtilPowerAutoPosts ensures a selected power post uploads once
// via httptest Graph and records util-power state.
func TestFBUtilPowerAutoPosts(t *testing.T) {
	e := setupUtil(t, []string{"2026-10-19"})
	writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
	writePowerFrozen(t, e.site, "pow1", "2026-10-20T10:00:00+08:00", "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
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
		if p.Region == "util-power" && p.AsOf == "pow1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("state = %+v", st.Posts)
	}
}

// TestFBUtilPowerManualDryRun stages frozen power copies without state.
func TestFBUtilPowerManualDryRun(t *testing.T) {
	e := setupUtil(t, []string{"2026-10-19"})
	writePowerFrozen(t, e.site, "pow9", "2026-10-20T10:00:00+08:00", "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
	args := []string{"fbutil", "-db", e.db, "-kind", "power", "-dry-run",
		"-site-public", e.site, "-out-dir", e.out, "-state", e.state}
	if code := run(args); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, name := range []string{"power.txt", "power.png"} {
		if _, err := os.Stat(filepath.Join(e.out, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
}
