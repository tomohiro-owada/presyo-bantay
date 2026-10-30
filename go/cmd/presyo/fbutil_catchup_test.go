package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeOpenEndedWater stages SITE/u/water/<id>/{post.json,water.png}
// for an emergency alert with no known end time (open_ended bool).
func writeOpenEndedWater(t *testing.T, site, id, genAt, start string) {
	t.Helper()
	writeFrozenPost(t, site, "water", id, genAt, map[string]string{
		"start": start, "alert_kind": "emergency",
	})
	p := filepath.Join(site, "u", "water", id, "post.json")
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

func TestFBUtilCatchup(t *testing.T) {
	oldGen := "2026-10-20T10:00:00+08:00"
	for _, tc := range []struct {
		name      string
		now       string
		dbDates   []string
		state     []fbPostEntry
		setupSite func(t *testing.T, e utilEnv)
		extra     []string
		wantFiles []string
	}{
		{
			// Thursday: fuel posts any weekday now (old Tue/Wed
			// restriction is gone).
			name: "fuel thursday age 2", now: "2026-10-22T13:00:00+08:00", dbDates: []string{"2026-10-21"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-21", "2026-10-21T06:00:00+08:00", ""))
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
			wantFiles: []string{"fuel.png", "fuel.txt"},
		},
		{
			// Monday, age 6: still due.
			name: "fuel monday age 6", now: "2026-10-26T13:00:00+08:00", dbDates: []string{"2026-10-25"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-25", "2026-10-25T06:00:00+08:00", ""))
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
			wantFiles: []string{"fuel.png", "fuel.txt"},
		},
		{
			name: "fuel age 7 not due", now: "2026-10-27T13:00:00+08:00", dbDates: []string{"2026-10-26"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-26", "2026-10-26T06:00:00+08:00", ""))
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
		},
		{
			name: "fuel posted", now: "2026-10-22T13:00:00+08:00", dbDates: []string{"2026-10-21"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-21", "2026-10-21T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "util-fuel", AsOf: "2026-10-20", PostID: "p", PhotoID: "h", PostedAt: "2026-10-21T06:00:00+08:00"})
				writeAutoState(t, e.state, st)
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
		},
		{
			// Fuel is evaluated before the DA-first gate.
			name: "fuel due while da pending", now: "2026-10-22T13:00:00+08:00", dbDates: []string{"2026-10-21"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-21", PostID: "p", PhotoID: "h", PostedAt: "2026-10-21T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
			wantFiles: []string{"fuel.png", "fuel.txt"},
		},
		{
			// Young bills wait for the DA queue.
			name: "bills wait when young", now: "2026-10-22T13:00:00+08:00", dbDates: []string{"2026-10-21"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-21", PostID: "p", PhotoID: "h", PostedAt: "2026-10-21T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "bills", "2026-10", "2026-10-22T12:00:00+08:00", nil)
			},
		},
		{
			// A stale bills page bypasses the DA gate.
			name: "bills bypass when old", now: "2026-10-22T13:00:00+08:00", dbDates: []string{"2026-10-21"},
			state: []fbPostEntry{{Region: "south", AsOf: "2026-10-21", PostID: "p", PhotoID: "h", PostedAt: "2026-10-21T06:00:00+08:00"}},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "bills", "2026-10", "2026-10-21T11:00:00+08:00", nil)
			},
			wantFiles: []string{"bills.png", "bills.txt"},
		},
		{
			// Short notice (starts in 1h) is due: the old 3h lead
			// is gone for water.
			name: "water short notice due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", oldGen, map[string]string{
					"start": "2026-10-20T14:00:00+08:00", "end": "2026-10-20T18:00:00+08:00", "alert_kind": "scheduled",
				})
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
		{
			name: "water ended not due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", oldGen, map[string]string{
					"start": "2026-10-20T08:00:00+08:00", "end": "2026-10-20T10:00:00+08:00", "alert_kind": "scheduled",
				})
			},
		},
		{
			name: "power short notice due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-20T14:00:00+08:00", "2026-10-20T18:00:00+08:00")
			},
			wantFiles: []string{"power.png", "power.txt"},
		},
		{
			name: "power ended not due", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writePowerFrozen(t, e.site, "pow1", oldGen, "2026-10-20T08:00:00+08:00", "2026-10-20T10:00:00+08:00")
			},
		},
		{
			// Util kinds post inside the new 05:30-20:00 window
			// (06:00 was outside the old 12:00-19:00 default).
			name: "water posts at 06:00", now: "2026-10-20T06:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", "2026-10-20T05:00:00+08:00", map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
		{
			name: "water holds at 05:00", now: "2026-10-20T05:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", "2026-10-19T10:00:00+08:00", map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
			},
		},
		{
			// Fuel/bills use the routine window 12:00-19:00: fuel
			// due at 14:00 ...
			name: "fuel due at 14:00", now: "2026-10-20T14:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
			wantFiles: []string{"fuel.png", "fuel.txt"},
		},
		{
			// ... but not at 08:00 (inside the 05:30-20:00 main
			// window, outside the routine window): skipped,
			// falls through to "nothing due".
			name: "fuel not due at 08:00", now: "2026-10-20T08:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
		},
		{
			// Water uses the main window 05:30-20:00, so it is
			// still due at 08:00.
			name: "water due at 08:00", now: "2026-10-20T08:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeFrozenPost(t, e.site, "water", "wid1", "2026-10-20T05:00:00+08:00", map[string]string{
					"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
				})
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
		{
			// Open-ended emergency: posted 1h ago, due.
			name: "open ended due within 24h", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeOpenEndedWater(t, e.site, "woe1", "2026-10-20T12:00:00+08:00", "2026-10-20T12:00:00+08:00")
			},
			wantFiles: []string{"water.png", "water.txt"},
		},
		{
			// Open-ended emergency: posted 26h ago, no longer due.
			name: "open ended not after 24h", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				writeAutoState(t, e.state, regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", ""))
				writeOpenEndedWater(t, e.site, "woe1", "2026-10-19T11:00:00+08:00", "2026-10-19T11:00:00+08:00")
			},
		},
		{
			// Open-ended emergency posts once (posted-id dedupe).
			name: "open ended posted once", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "util-water", AsOf: "woe1", PostID: "p", PhotoID: "h", PostedAt: "2026-10-20T12:05:00+08:00"})
				writeAutoState(t, e.state, st)
				writeOpenEndedWater(t, e.site, "woe1", "2026-10-20T12:00:00+08:00", "2026-10-20T12:00:00+08:00")
			},
		},
		{
			// Gap grace applies to fbutil too: 1h59m39s passes.
			name: "gap grace allows 1h59m39s", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T11:00:21+08:00"})
				writeAutoState(t, e.state, st)
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
			wantFiles: []string{"fuel.png", "fuel.txt"},
		},
		{
			name: "gap 1h50m skips", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T11:10:00+08:00"})
				writeAutoState(t, e.state, st)
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
			},
		},
		{
			name: "gap-grace 0 skips 1h59m39s", now: "2026-10-20T13:00:00+08:00", dbDates: []string{"2026-10-19"},
			extra: []string{"-gap-grace", "0s"},
			setupSite: func(t *testing.T, e utilEnv) {
				st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
				st = append(st, fbPostEntry{Region: "south", AsOf: "2026-10-20", PostID: "q", PhotoID: "h", PostedAt: "2026-10-20T11:00:21+08:00"})
				writeAutoState(t, e.state, st)
				writeFrozenPost(t, e.site, "fuel", "2026-10-20", oldGen, nil)
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
		})
	}
}
