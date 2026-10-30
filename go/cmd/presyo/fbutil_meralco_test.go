package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writePowerEmergency stages SITE/u/power/<id>/{post.json,power.png}
// for an emergency alert (AlertKind "emergency"). When openEnded is
// true no end is written and open_ended is set; otherwise end is
// written.
func writePowerEmergency(t *testing.T, site, id, genAt, start, end string, openEnded bool) {
	t.Helper()
	extra := map[string]string{
		"start": start, "alert_kind": "emergency",
	}
	if !openEnded {
		extra["end"] = end
	}
	writeFrozenPost(t, site, "power", id, genAt, extra)
	if openEnded {
		p := filepath.Join(site, "u", "power", id, "post.json")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		doc["open_ended"] = true
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFBUtilMeralcoEmergency(t *testing.T) {
	oldGen := "2026-10-20T10:00:00+08:00"
	for _, tc := range []struct {
		name      string
		now       string
		dbDates   []string
		state     []fbPostEntry
		extra     []string
		setupSite func(t *testing.T, e utilEnv)
		wantFiles []string
	}{
		{
			name: "emergency due now", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-20T10:00:00+08:00", "2026-10-20T18:00:00+08:00", false)
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "ended emergency not due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-20T08:00:00+08:00", "2026-10-20T10:00:00+08:00", false)
			},
		},
		{
			name: "open ended due within 24h", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerEmergency(t, e.site, "x-2", "2026-10-20T12:00:00+08:00", "2026-10-20T12:00:00+08:00", "", true)
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "open ended not after 24h", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
				writePowerEmergency(t, e.site, "x-2", "2026-10-19T11:00:00+08:00", "2026-10-19T11:00:00+08:00", "", true)
			},
		},
		{
			name: "planned same day does not block emergency", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "util-power", AsOf: "pow-old", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T08:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-20T10:00:00+08:00", "2026-10-20T18:00:00+08:00", false)
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "emergency same day does not block planned", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			state: []fbPostEntry{{Region: "util-power", AsOf: "x-old", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T08:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerEmergency(t, e.site, "x-old", oldGen, "2026-10-20T06:00:00+08:00", "2026-10-20T07:00:00+08:00", false)
				writePowerFrozen(t, e.site, "pow-new", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "4th emergency blocked", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st,
					fbPostEntry{Region: "util-power", AsOf: "x-a", PostID: "p1", PhotoID: "h", PostedAt: "2026-10-20T08:00:00+08:00"},
					fbPostEntry{Region: "util-power", AsOf: "x-b", PostID: "p2", PhotoID: "h", PostedAt: "2026-10-20T09:00:00+08:00"},
					fbPostEntry{Region: "util-power", AsOf: "x-c", PostID: "p3", PhotoID: "h", PostedAt: "2026-10-20T10:00:00+08:00"},
				)
				writeAutoState(t, e.state, st)
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-20T10:00:00+08:00", "2026-10-20T18:00:00+08:00", false)
			},
		},
		{
			name: "emergency overlapping posted planned skipped", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "util-power", AsOf: "pow-old", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"})
				writeAutoState(t, e.state, st)
				writePowerFrozen(t, e.site, "pow-old", oldGen, "2026-10-21T14:00:00+08:00", "2026-10-21T17:00:00+08:00")
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00", false)
			},
		},
		{
			name: "planned overlapping posted emergency skipped", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "util-power", AsOf: "x-old", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"})
				writeAutoState(t, e.state, st)
				writePowerEmergency(t, e.site, "x-old", oldGen, "2026-10-21T14:00:00+08:00", "2026-10-21T17:00:00+08:00", false)
				writePowerFrozen(t, e.site, "pow-new", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
		},
		{
			name: "gap 40m emergency posts", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T12:20:00+08:00"})
				writeAutoState(t, e.state, st)
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-20T10:00:00+08:00", "2026-10-20T18:00:00+08:00", false)
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "gap 40m planned skips", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T12:20:00+08:00"})
				writeAutoState(t, e.state, st)
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
			},
		},
		{
			name: "gap 40m fuel skips", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T12:20:00+08:00"})
				writeAutoState(t, e.state, st)
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
		},
		{
			name: "gap 20m emergency skips", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T12:40:00+08:00"})
				writeAutoState(t, e.state, st)
				writePowerEmergency(t, e.site, "x-1", oldGen, "2026-10-20T10:00:00+08:00", "2026-10-20T18:00:00+08:00", false)
			},
		},
		{
			name: "sunday hold emergency posts", now: "2026-10-25T17:00:00+08:00", dbDates: []string{"2026-10-24"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerEmergency(t, e.site, "x-1", "2026-10-25T10:00:00+08:00", "2026-10-25T10:00:00+08:00", "2026-10-25T20:00:00+08:00", false)
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "sunday hold fuel skips", now: "2026-10-25T17:00:00+08:00", dbDates: []string{"2026-10-24"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-24", "2026-10-24T06:00:00+08:00", ""))
				writeFrozenPost(t, e.site, "fuel", "2026-10-24", "2026-10-25T10:00:00+08:00", nil)
			},
		},
		{
			name: "outside window skips", now: "2026-10-20T05:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerEmergency(t, e.site, "x-1", "2026-10-19T10:00:00+08:00", "2026-10-20T04:00:00+08:00", "2026-10-20T10:00:00+08:00", false)
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
			args := append(e.autoArgs("-now", tc.now), tc.extra...)
			if code := run(args); code != 0 {
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
			// Dry-run never writes state.
			if _, err := os.Stat(e.state); !os.IsNotExist(err) && len(tc.wantFiles) > 0 {
				// State may exist from setup; dry-run must not add entries.
				st := readState(t, e.state)
				for _, p := range st.Posts {
					if p.Region == "util-power" && (p.AsOf == "x-1" || p.AsOf == "x-2") {
						t.Fatalf("dry-run wrote state: %+v", p)
					}
				}
			}
		})
	}
}

func TestFBUtilMeralcoEmergencyBeatsPlanned(t *testing.T) {
	e := setupUtil(t, []string{"2026-10-19"})
	writePowerFrozen(t, e.site, "pow-plan", "2026-10-20T10:00:00+08:00", "2026-10-21T13:00:00+08:00", "2026-10-21T18:00:00+08:00")
	writePowerEmergency(t, e.site, "x-9", "2026-10-20T10:00:00+08:00", "2026-10-20T10:00:00+08:00", "2026-10-20T18:00:00+08:00", false)
	if code := run(e.autoArgs("-now", "2026-10-20T13:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(e.out, "power.txt"))
	if err != nil {
		t.Fatalf("power.txt missing: %v", err)
	}
	if string(data) != "post power x-9\n" {
		t.Fatalf("emergency should win, got %q", string(data))
	}
}
