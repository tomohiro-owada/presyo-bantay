package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The -auto fairness order: candidates are regions with no state entry
// for D, most-overdue first (no state entry sorts first as ""), ties
// keeping regions.IDs() order.
func TestAutoCandidateOrder(t *testing.T) {
	// D = 2026-10-27; south, manila, qc were posted for 10-26 only.
	e := setupAuto(t, []string{"2026-10-27"})
	posted := []fbPostEntry{
		{Region: "south", AsOf: "2026-10-26", PostID: "p1", PhotoID: "h1", PostedAt: "2026-10-26T06:00:00+08:00"},
		{Region: "manila", AsOf: "2026-10-26", PostID: "p2", PhotoID: "h2", PostedAt: "2026-10-26T06:00:00+08:00"},
		{Region: "qc", AsOf: "2026-10-26", PostID: "p3", PhotoID: "h3", PostedAt: "2026-10-26T06:00:00+08:00"},
	}
	writeAutoState(t, e.state, posted)
	// Most-overdue first: north, east (no state at all), then south
	// (ties keep regions.IDs() order).
	for i, want := range []string{"north", "east", "south"} {
		if code := run(e.autoArgs("-now", "2026-10-27T07:00:00+08:00")); code != 0 {
			t.Fatalf("run %d: exit = %d", i, code)
		}
		if _, err := os.Stat(filepath.Join(e.out, want+".txt")); err != nil {
			t.Fatalf("run %d: %s should render first: %v", i, want, err)
		}
		posted = append(posted, fbPostEntry{Region: want, AsOf: "2026-10-27", PostID: "q" + want, PostedAt: "2026-10-26T06:00:00+08:00"})
		writeAutoState(t, e.state, posted)
	}
	// manila and qc (posted for 10-26) were never due in these runs.
	for _, r := range []string{"manila", "qc"} {
		if _, err := os.Stat(filepath.Join(e.out, r+".txt")); !os.IsNotExist(err) {
			t.Errorf("%s must not render while more-overdue candidates remain", r)
		}
	}
}

// Head-of-line blocking: when the first candidate fails the checker,
// the next candidate is rendered in the same run.
func TestAutoHeadOfLine(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-27"})
	old := fbpostForceCheckFail
	fbpostForceCheckFail = func(regionID string) bool { return regionID == "south" }
	defer func() { fbpostForceCheckFail = old }()
	if code := run(e.autoArgs("-now", "2026-10-27T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	// South fails the check (files still staged), manila renders next.
	if _, err := os.Stat(filepath.Join(e.out, "manila.txt")); err != nil {
		t.Errorf("manila should render after south fails the check: %v", err)
	}
}

// Min-gap grace: posted_at is written after the upload, so a cron run
// at exactly +2h sees since=1h59m39s. The default 5m grace allows it;
// 1h50m still skips; -gap-grace 0 restores the strict 2h.
func TestAutoGapGrace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		postedAt string
		extra    []string
		wantRend bool
	}{
		{"grace allows 1h59m39s", "2026-10-25T05:00:21+08:00", nil, true},
		{"1h50m still skips", "2026-10-25T05:10:00+08:00", nil, false},
		{"gap-grace 0 skips 1h59m39s", "2026-10-25T05:00:21+08:00", []string{"-gap-grace", "0s"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setupAuto(t, []string{"2026-10-24"})
			writeAutoState(t, e.state, []fbPostEntry{
				{Region: "south", AsOf: "2026-10-24", PostID: "p1", PhotoID: "h1", PostedAt: tc.postedAt},
			})
			args := append(e.autoArgs("-now", "2026-10-25T07:00:00+08:00"), tc.extra...)
			if code := run(args); code != 0 {
				t.Fatalf("exit = %d", code)
			}
			_, err := os.Stat(filepath.Join(e.out, "manila.txt"))
			if tc.wantRend && err != nil {
				t.Errorf("manila should render: %v", err)
			}
			if !tc.wantRend && err == nil {
				t.Error("nothing should render while the gap holds")
			}
		})
	}
}
