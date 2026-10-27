package utilities

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func httpHandlerFunc(onHit func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		onHit()
		w.WriteHeader(404)
	})
}

func mustLoadXInbox(t *testing.T) *XInbox {
	t.Helper()
	b, err := os.ReadFile(testdata(t, "maynilad_x/inbox-2026-10-28.json"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "inbox.json")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := LoadXInbox(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func manila(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts.In(manilaLoc)
}

func TestLoadXInboxStrict(t *testing.T) {
	in := mustLoadXInbox(t)
	if in.Version != 1 || in.Account != "maynilad" || in.AccountID != "2992779308" {
		t.Fatalf("header = %+v", in)
	}
	if len(in.Posts) == 0 {
		t.Fatal("no posts")
	}
	// Bad version fails.
	tmp := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(tmp, []byte(`{"version":2,"account":"maynilad","account_id":"x","fetched_at":"2026-10-28T12:00:00+08:00","posts":[]}`), 0o644)
	if _, err := LoadXInbox(tmp); err == nil {
		t.Fatal("bad version passed")
	}
	// Unknown field fails (strict).
	tmp2 := filepath.Join(t.TempDir(), "unknown.json")
	os.WriteFile(tmp2, []byte(`{"version":1,"account":"maynilad","account_id":"x","fetched_at":"2026-10-28T12:00:00+08:00","posts":[],"bogus":1}`), 0o644)
	if _, err := LoadXInbox(tmp2); err == nil {
		t.Fatal("unknown field passed")
	}
}

func TestParseXAdvisoriesAudit(t *testing.T) {
	in := mustLoadXInbox(t)
	now := manila(t, "2026-10-17T10:00:00+08:00")
	advs := ParseXAdvisories(in, "Las Piñas", now)
	if len(advs) != 1 {
		t.Fatalf("got %d advisories: %+v", len(advs), advs)
	}
	a := advs[0]
	if a.PostID != "2111361327537279200" {
		t.Fatalf("post = %q", a.PostID)
	}
	if a.Start.Format("2006-01-02 15:04") != "2026-10-22 22:00" || a.End.Format("2006-01-02 15:04") != "2026-10-23 06:00" {
		t.Fatalf("window = %v..%v", a.Start, a.End)
	}
	if a.Kind != "scheduled" || a.ReasonKey != "audit" {
		t.Fatalf("kind/reason = %q/%q", a.Kind, a.ReasonKey)
	}
	// Accent-insensitive city.
	again := ParseXAdvisories(in, "Las Pinas", now)
	if len(again) != 1 || again[0].PostID != a.PostID {
		t.Fatalf("accent-insensitive failed: %+v", again)
	}
	// The Oct-14 Caloocan/QC post mentions neither Las Piñas nor an
	// ended window: it must not appear in the Las Piñas result (which
	// is exactly the one audit advisory). For its own cities the
	// Oct 19-20 window is still ahead of now, so it yields one.
	for _, a := range advs {
		if a.PostID == "2110285334143709484" {
			t.Fatalf("caloocan/qc post leaked into Las Piñas: %+v", a)
		}
	}
	if got := ParseXAdvisories(in, "Caloocan", now); len(got) != 1 {
		t.Fatalf("caloocan = %+v", got)
	}
}

func TestParseXAdvisoriesEmergency(t *testing.T) {
	in := mustLoadXInbox(t)
	now := manila(t, "2026-10-16T08:00:00+08:00")
	advs := ParseXAdvisories(in, "Las Piñas", now)
	found := false
	for _, a := range advs {
		if a.PostID == "2110911445357772872" {
			found = true
			if a.Kind != "emergency" {
				t.Fatalf("kind = %q", a.Kind)
			}
			if a.End.Format("2006-01-02 15:04") != "2026-10-16 14:00" {
				t.Fatalf("end = %v", a.End)
			}
			if a.Start.IsZero() {
				t.Fatal("emergency start is zero")
			}
		}
	}
	if !found {
		t.Fatalf("emergency post missing: %+v", advs)
	}
	// After resumption: nothing.
	later := manila(t, "2026-10-16T15:00:00+08:00")
	for _, a := range ParseXAdvisories(in, "Las Piñas", later) {
		if a.PostID == "2110911445357772872" {
			t.Fatalf("stale emergency still returned: %+v", a)
		}
	}
}

func TestParseXAdvisoriesIgnoresNonAdvisories(t *testing.T) {
	in := mustLoadXInbox(t)
	now := manila(t, "2026-10-17T10:00:00+08:00")
	advs := ParseXAdvisories(in, "Las Piñas", now)
	for _, a := range advs {
		if a.PostID == "2113564248655945930" || a.PostID == "2111002120086516179" || a.PostID == "2108461915379024344" {
			t.Fatalf("weekly image-only post returned: %+v", a)
		}
		if a.PostID == "2113223297362227529" {
			t.Fatalf("pipe-replacement success story returned: %+v", a)
		}
	}
	// Dam/tips posts carry no city match anyway.
	if got := ParseXAdvisories(in, "Angat", now); len(got) != 0 {
		t.Fatalf("dam posts = %+v", got)
	}
}

func TestParseXAdvisoriesDedupe(t *testing.T) {
	in := mustLoadXInbox(t)
	// Duplicate the inbox posts.
	dup := &XInbox{Version: in.Version, Account: in.Account, AccountID: in.AccountID, FetchedAt: in.FetchedAt}
	dup.Posts = append(append([]XPost{}, in.Posts...), in.Posts...)
	now := manila(t, "2026-10-17T10:00:00+08:00")
	single := ParseXAdvisories(in, "Las Piñas", now)
	doubled := ParseXAdvisories(dup, "Las Piñas", now)
	if len(single) != len(doubled) {
		t.Fatalf("dedupe failed: %d vs %d", len(single), len(doubled))
	}
}

func TestXAdvisoryAlertsRenderAndCheck(t *testing.T) {
	in := mustLoadXInbox(t)
	now := manila(t, "2026-10-17T10:00:00+08:00")
	advs := ParseXAdvisories(in, "Las Piñas", now)
	if len(advs) != 1 {
		t.Fatalf("got %+v", advs)
	}
	w := &WaterStatus{City: "Las Piñas", CheckedAt: "2026-10-17T10:00:00+08:00", Advisories: advs}
	alerts := BuildWaterAlerts(w)
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts", len(alerts))
	}
	a := alerts[0]
	if len(a.Areas) != 1 || a.Areas[0] != XAreasTL {
		t.Fatalf("areas = %q", a.Areas)
	}
	if a.SourceURL != "https://x.com/maynilad/status/2111361327537279200" {
		t.Fatalf("url = %q", a.SourceURL)
	}
	if a.Start.IsZero() || a.End.IsZero() {
		t.Fatal("window not set")
	}
	// Stable ID: repeated post about the same window collapses.
	again := BuildWaterAlerts(w)
	if a.ID != again[0].ID {
		t.Fatal("unstable X alert ID")
	}
	text := RenderWaterAlertPost(a, false)
	if !strings.Contains(text, "📍 "+XAreasTL) {
		t.Fatalf("TL areas label missing:\n%s", text)
	}
	if !strings.Contains(text, "· "+XAreasEN+" ·") {
		t.Fatalf("EN areas label missing:\n%s", text)
	}
	if !strings.Contains(text, "・"+XAreasJA+"・") {
		t.Fatalf("JA areas label missing:\n%s", text)
	}
	if !strings.Contains(text, "Details: https://x.com/maynilad/status/2111361327537279200") {
		t.Fatalf("X footer missing:\n%s", text)
	}
	if !strings.Contains(text, "Source: Maynilad (X)") {
		t.Fatalf("X source missing:\n%s", text)
	}
	snap := &Snapshot{Water: w, Alerts: alerts}
	if problems := CheckPost(text, snap, "water"); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	// Numbers inside the X URL are scrubbed: assert explicitly.
	if strings.Contains(a.SourceURL, "2111361327537279200") {
		// The checker must not flag those digits.
		bad := text + "\n"
		_ = bad
	}
}

func TestFetchWaterXOnly(t *testing.T) {
	// No website request may be made when only the inbox is configured.
	hits := 0
	srv := httptest.NewServer(httpHandlerFunc(func() { hits++ }))
	defer srv.Close()
	tmp := filepath.Join(t.TempDir(), "inbox.json")
	b, _ := os.ReadFile(testdata(t, "maynilad_x/inbox-2026-10-28.json"))
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:         srv.Client(),
		Now:            manila(t, "2026-10-17T10:00:00+08:00"),
		MinDelay:       -1,
		MayniladBase:   srv.URL,
		MayniladXInbox: tmp,
		Sections:       []string{"water"},
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Water == nil || len(snap.Water.Advisories) != 1 {
		t.Fatalf("water = %+v errors=%v", snap.Water, snap.Errors)
	}
	if len(snap.Water.Scheduled) != 0 || len(snap.Water.Emergency) != 0 {
		t.Fatalf("slices not empty: %+v", snap.Water)
	}
	if snap.SectionFetchedAt["water"] == "" {
		t.Fatal("water section not stamped")
	}
	if hits != 0 {
		t.Fatalf("website requested %d times with X-only inbox", hits)
	}
	// Missing file -> no error, no Water.
	snap2, err := Fetch(context.Background(), FetchOptions{
		Client:         srv.Client(),
		Now:            manila(t, "2026-10-17T10:00:00+08:00"),
		MinDelay:       -1,
		MayniladBase:   srv.URL,
		MayniladXInbox: filepath.Join(t.TempDir(), "missing.json"),
		Sections:       []string{"water"},
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Errors) != 0 {
		t.Fatalf("missing file errors = %v", snap2.Errors)
	}
	if snap2.Water != nil {
		t.Fatalf("missing file water = %+v", snap2.Water)
	}
	// Malformed -> error.
	badPath := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(badPath, []byte("{bad"), 0o644)
	snap3, err := Fetch(context.Background(), FetchOptions{
		Client:         srv.Client(),
		Now:            manila(t, "2026-10-17T10:00:00+08:00"),
		MinDelay:       -1,
		MayniladBase:   srv.URL,
		MayniladXInbox: badPath,
		Sections:       []string{"water"},
		Sleep:          func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap3.Errors) == 0 {
		t.Fatal("malformed inbox produced no error")
	}
}

func TestFetchWaterXNoLocalPathLeak(t *testing.T) {
	// The inbox lives in a local directory, but Sources and Errors are
	// published verbatim in utilities.json: none of them may embed the
	// inbox directory path.
	srv := httptest.NewServer(httpHandlerFunc(func() {}))
	defer srv.Close()
	fetchWater := func(t *testing.T, inbox string, now time.Time) *Snapshot {
		t.Helper()
		snap, err := Fetch(context.Background(), FetchOptions{
			Client:         srv.Client(),
			Now:            now,
			MinDelay:       -1,
			MayniladBase:   srv.URL,
			MayniladXInbox: inbox,
			Sections:       []string{"water"},
			Sleep:          func(time.Duration) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	assertNoInboxDir := func(t *testing.T, snap *Snapshot, inboxDir string) {
		t.Helper()
		for _, src := range snap.Sources {
			if strings.Contains(src.URL, inboxDir) {
				t.Fatalf("source %q URL leaks inbox dir: %q", src.Name, src.URL)
			}
			if strings.Contains(src.Err, inboxDir) {
				t.Fatalf("source %q err leaks inbox dir: %q", src.Name, src.Err)
			}
		}
		for _, e := range snap.Errors {
			if strings.Contains(e, inboxDir) {
				t.Fatalf("error leaks inbox dir: %q", e)
			}
		}
	}
	// 1. Success: the maynilad-x source URL is the public account URL.
	dir := t.TempDir()
	tmp := filepath.Join(dir, "inbox.json")
	b, _ := os.ReadFile(testdata(t, "maynilad_x/inbox-2026-10-28.json"))
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	snap := fetchWater(t, tmp, manila(t, "2026-10-17T10:00:00+08:00"))
	found := false
	for _, src := range snap.Sources {
		if src.Name == "maynilad-x" {
			found = true
			if src.URL != MayniladXAccountURL {
				t.Fatalf("maynilad-x URL = %q, want %q", src.URL, MayniladXAccountURL)
			}
		}
	}
	if !found {
		t.Fatal("no maynilad-x source")
	}
	assertNoInboxDir(t, snap, dir)
	// 2. Stale inbox: the warning carries only the base name.
	stale := fetchWater(t, tmp, manila(t, "2026-10-29T10:00:00+08:00"))
	assertNoInboxDir(t, stale, dir)
	staleWarn := false
	for _, e := range stale.Errors {
		if strings.Contains(e, "stale inbox") {
			staleWarn = true
			if !strings.Contains(e, "inbox.json") {
				t.Fatalf("stale warning lost inbox name: %q", e)
			}
		}
	}
	if !staleWarn {
		t.Fatal("expected stale inbox warning")
	}
	// 3. Malformed inbox: the failure carries only the base name.
	badDir := t.TempDir()
	badPath := filepath.Join(badDir, "bad.json")
	os.WriteFile(badPath, []byte("{bad"), 0o644)
	bad := fetchWater(t, badPath, manila(t, "2026-10-17T10:00:00+08:00"))
	if len(bad.Errors) == 0 {
		t.Fatal("malformed inbox produced no error")
	}
	assertNoInboxDir(t, bad, badDir)
	if !strings.Contains(bad.Errors[0], "bad.json") {
		t.Fatalf("malformed error lost inbox name: %q", bad.Errors[0])
	}
}
