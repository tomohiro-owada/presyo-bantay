package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The cross-source overlap guard: a water alert whose window overlaps
// (>= 50 % of the shorter window) an already-posted util-water entry
// is never posted, reading the posted ids' frozen post.json Start/End.
func TestFBUtilWaterOverlapGuard(t *testing.T) {
	oldGen := "2026-10-20T10:00:00+08:00"
	setup := func(t *testing.T) utilEnv {
		e := setupUtil(t, []string{"2026-10-19"})
		st := regionEntries(t, "2026-10-19", "2026-10-19T06:00:00+08:00", "")
		st = append(st, fbPostEntry{Region: "util-water", AsOf: "oldid", PostID: "p", PhotoID: "h", PostedAt: "2026-10-19T06:00:00+08:00"})
		writeAutoState(t, e.state, st)
		// Posted entry: Oct 21 14:00-17:00.
		writeFrozenPost(t, e.site, "water", "oldid", oldGen, map[string]string{
			"start": "2026-10-21T14:00:00+08:00", "end": "2026-10-21T17:00:00+08:00", "alert_kind": "scheduled",
		})
		return e
	}
	// Overlapping candidate (Oct 21 13:00-18:00, contains the posted
	// window) is skipped; the disjoint candidate (Oct 23 13:00-18:00)
	// is selected.
	e := setup(t)
	writeFrozenPost(t, e.site, "water", "wid1", oldGen, map[string]string{
		"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
	})
	writeFrozenPost(t, e.site, "water", "wid2", oldGen, map[string]string{
		"start": "2026-10-23T13:00:00+08:00", "end": "2026-10-23T18:00:00+08:00", "alert_kind": "scheduled",
	})
	if code := run(e.autoArgs("-now", "2026-10-20T13:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	txt, err := os.ReadFile(filepath.Join(e.out, "water.txt"))
	if err != nil {
		t.Fatalf("water.txt missing: %v", err)
	}
	if !strings.Contains(string(txt), "wid2") {
		t.Fatalf("overlap guard did not skip wid1: %q", txt)
	}
	// Only the overlapping candidate: nothing due.
	e2 := setup(t)
	writeFrozenPost(t, e2.site, "water", "wid1", oldGen, map[string]string{
		"start": "2026-10-21T13:00:00+08:00", "end": "2026-10-21T18:00:00+08:00", "alert_kind": "scheduled",
	})
	if code := run(e2.autoArgs("-now", "2026-10-20T13:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got := outFiles(t, e2.out); len(got) != 0 {
		t.Fatalf("overlapping alert posted: %v", got)
	}
}
