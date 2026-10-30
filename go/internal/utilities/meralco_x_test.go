package utilities

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustLoadMeralcoXInbox(t *testing.T) *XInbox {
	t.Helper()
	b, err := os.ReadFile(testdata(t, "meralco_x/inbox-2026-10-30.json"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "meralco_inbox.json")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := LoadXInbox(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func meralcoXPost(id, createdAt, text string) XPost {
	ts, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		panic(err)
	}
	return XPost{
		ID:        id,
		CreatedAt: ts,
		Text:      text,
		URL:       "https://x.com/meralco/status/" + id,
	}
}

func synthMeralcoInbox(posts ...XPost) *XInbox {
	return &XInbox{
		Version:   1,
		Account:   "meralco",
		AccountID: "15371662",
		FetchedAt: mustParseTime("2026-10-19T10:00:00+08:00"),
		Posts:     posts,
	}
}

func mustParseTime(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return ts
}

func TestParseMeralcoXFixtureLasPinas(t *testing.T) {
	in := mustLoadMeralcoXInbox(t)
	now := manila(t, "2026-10-19T10:00:00+08:00")
	evs := ParseMeralcoXEvents(in, now)
	if len(evs) != 1 {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.PostID != "2111996138043957361" {
		t.Fatalf("post = %q", ev.PostID)
	}
	if ev.Start.Format("2006-01-02 15:04") != "2026-10-19 08:30" {
		t.Fatalf("start = %v", ev.Start)
	}
	if ev.End.Format("2006-01-02 15:04") != "2026-10-19 11:30" {
		t.Fatalf("end = %v", ev.End)
	}
	if ev.ETRHours != 3 {
		t.Fatalf("etr = %v", ev.ETRHours)
	}
	if ev.OpenEnded {
		t.Fatal("open-ended set")
	}
	if ev.City != "Las Piñas" {
		t.Fatalf("city = %q", ev.City)
	}
	if ev.OccurredText != "8:30 AM" {
		t.Fatalf("occurred = %q", ev.OccurredText)
	}
	if !strings.Contains(ev.PostURL, "2111996138043957361") {
		t.Fatalf("url = %q", ev.PostURL)
	}
}

func TestParseMeralcoXFixtureTooOld(t *testing.T) {
	in := mustLoadMeralcoXInbox(t)
	now := manila(t, "2026-10-30T20:00:00+08:00")
	if evs := ParseMeralcoXEvents(in, now); len(evs) != 0 {
		t.Fatalf("got %+v", evs)
	}
}

func TestParseMeralcoXFixtureScheduledAndNonLP(t *testing.T) {
	in := mustLoadMeralcoXInbox(t)
	// Far-future now keeps every recent post in range; only the
	// Las Pinas Oct-19 emergency qualifies, and it is >48h old, so
	// with now Oct-21 nothing at all must remain: scheduled
	// advisories and non-Las Pinas emergencies yield nothing.
	now := manila(t, "2026-10-21T10:00:00+08:00")
	if evs := ParseMeralcoXEvents(in, now); len(evs) != 0 {
		t.Fatalf("got %+v", evs)
	}
	// Direct synthetic checks: scheduled text with Las Pinas and
	// non-LP emergency texts yield nothing.
	now2 := manila(t, "2026-10-19T10:00:00+08:00")
	synth := synthMeralcoInbox(
		meralcoXPost("s1", "2026-10-19T09:00:00+08:00", "ADVISORY: May scheduled power interruptions sa ilang areas sa Muntinlupa City and Las Piñas City sa October 7, 2026. More: https://t.co/x"),
		meralcoXPost("s2", "2026-10-19T09:00:00+08:00", "Your area in Muntinlupa may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x"),
		meralcoXPost("s3", "2026-10-19T09:00:00+08:00", "Your area in Quezon City, may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x"),
	)
	if evs := ParseMeralcoXEvents(synth, now2); len(evs) != 0 {
		t.Fatalf("scheduled/non-LP yielded %+v", evs)
	}
}

func TestParseMeralcoXVariants(t *testing.T) {
	now := manila(t, "2026-10-19T10:00:00+08:00")
	cases := []struct {
		name   string
		posted string
		text   string
		start  string
		end    string
		etr    float64
		open   bool
	}{
		{"that", "2026-10-19T09:41:32+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble that occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x", "2026-10-19 08:30", "2026-10-19 11:30", 3, false},
		{"double-space", "2026-10-19T09:41:32+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at  8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x", "2026-10-19 08:30", "2026-10-19 11:30", 3, false},
		{"pm", "2026-10-19T15:00:00+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 02:46 PM with an estimated restoration time of 2 hrs. https://t.co/x", "2026-10-19 14:46", "2026-10-19 16:46", 2, false},
		{"multi-city", "2026-10-19T09:41:32+08:00", "Your area in Mandaluyong and Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x", "2026-10-19 08:30", "2026-10-19 11:30", 3, false},
		{"no-etr", "2026-10-19T09:41:32+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM with no estimated restoration time. https://t.co/x", "2026-10-19 08:30", "", 0, true},
		{"missing-etr", "2026-10-19T09:41:32+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM. https://t.co/x", "2026-10-19 08:30", "", 0, true},
	}
	for _, c := range cases {
		in := synthMeralcoInbox(meralcoXPost("p1", c.posted, c.text))
		evs := ParseMeralcoXEvents(in, now)
		if len(evs) != 1 {
			t.Fatalf("%s: got %+v", c.name, evs)
		}
		ev := evs[0]
		if ev.Start.Format("2006-01-02 15:04") != c.start {
			t.Fatalf("%s: start = %v", c.name, ev.Start)
		}
		if c.open {
			if !ev.OpenEnded || !ev.End.IsZero() {
				t.Fatalf("%s: want open-ended, got %+v", c.name, ev)
			}
		} else {
			if ev.End.Format("2006-01-02 15:04") != c.end {
				t.Fatalf("%s: end = %v", c.name, ev.End)
			}
			if ev.ETRHours != c.etr {
				t.Fatalf("%s: etr = %v", c.name, ev.ETRHours)
			}
		}
	}
}

func TestParseMeralcoXPreviousDay(t *testing.T) {
	now := manila(t, "2026-10-19T01:00:00+08:00")
	in := synthMeralcoInbox(meralcoXPost("p1", "2026-10-19T00:10:00+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 11:50 PM with an estimated restoration time of 3 hrs. https://t.co/x"))
	evs := ParseMeralcoXEvents(in, now)
	if len(evs) != 1 {
		t.Fatalf("got %+v", evs)
	}
	if evs[0].Start.Format("2006-01-02 15:04") != "2026-10-18 23:50" {
		t.Fatalf("start = %v", evs[0].Start)
	}
}

func TestParseMeralcoXRestorationGuard(t *testing.T) {
	now := manila(t, "2026-10-19T10:00:00+08:00")
	// (a) pattern plus a resolution marker: dropped.
	for _, extra := range []string{" Power has been restored.", " naibalik na ang serbisyo.", " back to normal.", " lifted."} {
		text := "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs." + extra
		in := synthMeralcoInbox(meralcoXPost("p1", "2026-10-19T09:41:32+08:00", text))
		if evs := ParseMeralcoXEvents(in, now); len(evs) != 0 {
			t.Fatalf("restoration %q yielded %+v", extra, evs)
		}
	}
	// Non-(a) posts with restoration words: never an alert.
	in := synthMeralcoInbox(meralcoXPost("p2", "2026-10-19T09:41:32+08:00", "Power in Las Piñas City has been restored. Thank you!"))
	if evs := ParseMeralcoXEvents(in, now); len(evs) != 0 {
		t.Fatalf("non-(a) restoration yielded %+v", evs)
	}
}

func TestParseMeralcoXWrongAccount(t *testing.T) {
	now := manila(t, "2026-10-19T10:00:00+08:00")
	in := synthMeralcoInbox(meralcoXPost("p1", "2026-10-19T09:41:32+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x"))
	in.Account = "maynilad"
	if evs := ParseMeralcoXEvents(in, now); evs != nil {
		t.Fatalf("wrong account yielded %+v", evs)
	}
	if err := CheckMeralcoXInbox(in); err == nil {
		t.Fatal("wrong account passed check")
	}
	ok := synthMeralcoInbox(meralcoXPost("p1", "2026-10-19T09:41:32+08:00", "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x"))
	if err := CheckMeralcoXInbox(ok); err != nil {
		t.Fatalf("good account failed: %v", err)
	}
}

func TestParseMeralcoXDedupe(t *testing.T) {
	now := manila(t, "2026-10-19T10:00:00+08:00")
	text := "Your area in Las Piñas City may have been affected by an emergency line trouble which occurred at 8:30 AM with an estimated restoration time of 3 hrs. https://t.co/x"
	in := synthMeralcoInbox(
		meralcoXPost("dup2", "2026-10-19T09:45:00+08:00", text),
		meralcoXPost("dup1", "2026-10-19T09:41:32+08:00", text),
	)
	evs := ParseMeralcoXEvents(in, now)
	if len(evs) != 1 {
		t.Fatalf("got %+v", evs)
	}
	if evs[0].PostID != "dup1" {
		t.Fatalf("kept %q, want earliest dup1", evs[0].PostID)
	}
}

func TestBuildMeralcoXAlerts(t *testing.T) {
	start := manila(t, "2026-10-19T08:30:00+08:00")
	end := manila(t, "2026-10-19T11:30:00+08:00")
	st := &MeralcoXStatus{
		CheckedAt: "2026-10-19T10:00:00+08:00",
		Events: []MeralcoXEvent{{
			PostID:       "2111996138043957361",
			PostURL:      "https://x.com/meralco/status/2111996138043957361",
			PostedAt:     manila(t, "2026-10-19T09:41:32+08:00"),
			Start:        start,
			End:          end,
			City:         "Las Piñas",
			AreaText:     "Las Piñas City",
			OccurredText: "8:30 AM",
			ETRHours:     3,
		}},
	}
	now := manila(t, "2026-10-19T10:00:00+08:00")
	alerts := BuildMeralcoXAlerts(st, nil, now)
	if len(alerts) != 1 {
		t.Fatalf("got %+v", alerts)
	}
	a := alerts[0]
	if a.ID != "x-2111996138043957361" {
		t.Fatalf("id = %q", a.ID)
	}
	if a.Kind != "emergency" {
		t.Fatalf("kind = %q", a.Kind)
	}
	if a.City != "Las Piñas" {
		t.Fatalf("city = %q", a.City)
	}
	if !a.Start.Equal(start) || !a.End.Equal(end) {
		t.Fatalf("window = %v..%v", a.Start, a.End)
	}
	if len(a.Windows) != 1 || !a.Windows[0].Start.Equal(start) || !a.Windows[0].End.Equal(end) {
		t.Fatalf("windows = %+v", a.Windows)
	}
	if a.OpenEnded || !a.EstimatedEnd {
		t.Fatalf("flags = %+v", a)
	}
	if len(a.Barangays) != 1 || a.Barangays[0] != PowerAreasTL {
		t.Fatalf("barangays = %q", a.Barangays)
	}
	if a.ReasonKey != PowerReasonEmergency {
		t.Fatalf("reason = %q", a.ReasonKey)
	}
	if a.SourceURL != "https://x.com/meralco/status/2111996138043957361" {
		t.Fatalf("url = %q", a.SourceURL)
	}
	if got := PowerReason(PowerReasonEmergency, 0); got != "biglaang aberya sa linya" {
		t.Fatalf("tl = %q", got)
	}
	if got := PowerReason(PowerReasonEmergency, 1); got != "emergency line trouble" {
		t.Fatalf("en = %q", got)
	}
	// Covering planned alert suppresses the event.
	covering := []PowerAlert{{
		ID:      "planned",
		City:    "Las Piñas",
		Start:   manila(t, "2026-10-19T08:00:00+08:00"),
		End:     manila(t, "2026-10-19T12:00:00+08:00"),
		Windows: []TimeWindow{{Start: manila(t, "2026-10-19T08:00:00+08:00"), End: manila(t, "2026-10-19T12:00:00+08:00")}},
	}}
	if got := BuildMeralcoXAlerts(st, covering, now); len(got) != 0 {
		t.Fatalf("covering planned did not suppress: %+v", got)
	}
	// Non-overlapping planned alert does not suppress.
	other := []PowerAlert{{
		ID:      "planned2",
		City:    "Las Piñas",
		Start:   manila(t, "2026-10-19T14:00:00+08:00"),
		End:     manila(t, "2026-10-19T16:00:00+08:00"),
		Windows: []TimeWindow{{Start: manila(t, "2026-10-19T14:00:00+08:00"), End: manila(t, "2026-10-19T16:00:00+08:00")}},
	}}
	if got := BuildMeralcoXAlerts(st, other, now); len(got) != 1 {
		t.Fatalf("non-overlapping suppressed: %+v", got)
	}
	// Ended >24h ago: skipped. Open-ended posted >24h ago: skipped.
	oldNow := manila(t, "2026-10-21T12:00:00+08:00")
	if got := BuildMeralcoXAlerts(st, nil, oldNow); len(got) != 0 {
		t.Fatalf("stale ended event kept: %+v", got)
	}
	openSt := &MeralcoXStatus{Events: []MeralcoXEvent{{
		PostID:    "o1",
		PostURL:   "https://x.com/meralco/status/o1",
		PostedAt:  manila(t, "2026-10-19T09:41:32+08:00"),
		Start:     start,
		City:      "Las Piñas",
		OpenEnded: true,
	}}}
	if got := BuildMeralcoXAlerts(openSt, nil, oldNow); len(got) != 0 {
		t.Fatalf("stale open-ended kept: %+v", got)
	}
	if got := BuildMeralcoXAlerts(openSt, nil, now); len(got) != 1 || !got[0].OpenEnded || got[0].EstimatedEnd {
		t.Fatalf("open-ended alert = %+v", got)
	}
}

func TestFetchMeralcoXOnly(t *testing.T) {
	srv := httptest.NewServer(httpHandlerFunc(func() {}))
	defer srv.Close()
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           manila(t, "2026-10-19T10:00:00+08:00"),
		MinDelay:      -1,
		MeralcoXInbox: testdata(t, "meralco_x/inbox-2026-10-30.json"),
		Sections:      []string{"meralcox"},
		Sleep:         func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.MeralcoX == nil || len(snap.MeralcoX.Events) != 1 {
		t.Fatalf("meralcox = %+v errors=%v", snap.MeralcoX, snap.Errors)
	}
	if snap.SectionFetchedAt["meralcox"] == "" {
		t.Fatal("meralcox section not stamped")
	}
	found := false
	for _, src := range snap.Sources {
		if src.Name == "meralcox" {
			found = true
			if src.URL != MeralcoXAccountURL {
				t.Fatalf("meralcox URL = %q", src.URL)
			}
		}
	}
	if !found {
		t.Fatal("no meralcox source")
	}
	// Missing file: silent.
	snap2, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           manila(t, "2026-10-19T10:00:00+08:00"),
		MinDelay:      -1,
		MeralcoXInbox: filepath.Join(t.TempDir(), "missing.json"),
		Sections:      []string{"meralcox"},
		Sleep:         func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Errors) != 0 {
		t.Fatalf("missing file errors = %v", snap2.Errors)
	}
	if snap2.MeralcoX != nil {
		t.Fatalf("missing file meralcox = %+v", snap2.MeralcoX)
	}
	_ = os.Getenv("")
}

func TestFetchMeralcoXNoLocalPathLeak(t *testing.T) {
	srv := httptest.NewServer(httpHandlerFunc(func() {}))
	defer srv.Close()
	dir := t.TempDir()
	tmp := filepath.Join(dir, "meralco_inbox.json")
	b, _ := os.ReadFile(testdata(t, "meralco_x/inbox-2026-10-30.json"))
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           manila(t, "2026-10-19T10:00:00+08:00"),
		MinDelay:      -1,
		MeralcoXInbox: tmp,
		Sections:      []string{"meralcox"},
		Sleep:         func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range snap.Sources {
		if strings.Contains(src.URL, dir) {
			t.Fatalf("source %q URL leaks inbox dir", src.Name)
		}
		if strings.Contains(src.Err, dir) {
			t.Fatalf("source %q err leaks inbox dir", src.Name)
		}
	}
	for _, e := range snap.Errors {
		if strings.Contains(e, dir) {
			t.Fatalf("error leaks inbox dir: %q", e)
		}
	}
	// Malformed inbox: failure carries only the base name.
	badDir := t.TempDir()
	badPath := filepath.Join(badDir, "meralco_bad.json")
	os.WriteFile(badPath, []byte("{bad"), 0o644)
	bad, err := Fetch(context.Background(), FetchOptions{
		Client:        srv.Client(),
		Now:           manila(t, "2026-10-19T10:00:00+08:00"),
		MinDelay:      -1,
		MeralcoXInbox: badPath,
		Sections:      []string{"meralcox"},
		Sleep:         func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad.Errors) == 0 {
		t.Fatal("malformed inbox produced no error")
	}
	for _, e := range bad.Errors {
		if strings.Contains(e, badDir) {
			t.Fatalf("error leaks inbox dir: %q", e)
		}
	}
	if !strings.Contains(bad.Errors[0], "meralco_bad.json") {
		t.Fatalf("malformed error lost inbox name: %q", bad.Errors[0])
	}
}

func TestMergeSnapshotsMeralcoX(t *testing.T) {
	base := &Snapshot{
		FetchedAt:        "2026-10-20T00:00:00+08:00",
		SectionFetchedAt: map[string]string{"meralcox": "old"},
		Sources:          []Source{{Name: "meralcox", URL: "old"}},
	}
	fresh := &Snapshot{
		FetchedAt: "2026-10-26T00:00:00+08:00",
		MeralcoX: &MeralcoXStatus{
			CheckedAt: "2026-10-26T00:00:00+08:00",
			Events:    []MeralcoXEvent{{PostID: "p1"}},
		},
		SectionFetchedAt: map[string]string{"meralcox": "new"},
		Sources:          []Source{{Name: "meralcox", URL: "new"}},
	}
	merged := MergeSnapshots(base, fresh, []string{"meralcox"})
	if merged.MeralcoX == nil || len(merged.MeralcoX.Events) != 1 {
		t.Fatalf("merged = %+v", merged.MeralcoX)
	}
	if merged.SectionFetchedAt["meralcox"] != "new" {
		t.Fatalf("timestamps = %v", merged.SectionFetchedAt)
	}
	n := 0
	for _, s := range merged.Sources {
		if s.Name == "meralcox" {
			n++
			if s.URL != "new" {
				t.Fatalf("stale source kept: %+v", s)
			}
		}
	}
	if n != 1 {
		t.Fatalf("sources = %+v", merged.Sources)
	}
	// Failed section does not overwrite.
	merged2 := MergeSnapshots(base, &Snapshot{FetchedAt: "x"}, []string{"meralcox"})
	if merged2.MeralcoX != nil {
		t.Fatalf("failed fetch overwrote meralcox: %+v", merged2.MeralcoX)
	}
	// Empty ignores MeralcoX: a snapshot with only MeralcoX is Empty.
	only := &Snapshot{MeralcoX: fresh.MeralcoX}
	if !only.Empty() {
		t.Fatal("Empty() should ignore MeralcoX")
	}
}
