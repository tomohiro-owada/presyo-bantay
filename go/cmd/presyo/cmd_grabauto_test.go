package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const gaFBToken = "fb-page-token-should-never-print-9876"

type gaGraph struct {
	srv  *httptest.Server
	mu   sync.Mutex
	msgs []string
}

func gaFakeGraph(t *testing.T) *gaGraph {
	t.Helper()
	g := &gaGraph{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/photos") {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "bad", 400)
			return
		}
		if r.FormValue("access_token") != gaFBToken {
			http.Error(w, `{"error":{"message":"bad token","code":190}}`, 400)
			return
		}
		g.mu.Lock()
		g.msgs = append(g.msgs, r.FormValue("message"))
		n := len(g.msgs)
		g.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"id": "photo" + string(rune('0'+n)), "post_id": "page_" + string(rune('0'+n))})
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *gaGraph) count() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.msgs) }

type gaEnv struct {
	t      *testing.T
	api    *httptest.Server
	graph  *gaGraph
	home   string
	state  string
	envF   string
	fbEnv  string
	stdout string
	stderr string
}

func gaSetup(t *testing.T, runs []gcRun, files map[string]string) *gaEnv {
	t.Helper()
	t.Setenv("PRESYO_GRAB_FB", "1")
	t.Setenv("PRESYO_GRAB_POST_WINDOW", "")
	t.Setenv("PRESYO_FB_PAGE_ID", "")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "")
	e := &gaEnv{t: t, graph: gaFakeGraph(t), home: t.TempDir()}
	e.api = gcServer(t, runs, files)
	e.state = filepath.Join(e.home, "state", "fbpost.json")
	e.envF = gcEnvFile(t, e.api.URL)
	e.fbEnv = filepath.Join(e.home, "fb.env")
	os.WriteFile(e.fbEnv, []byte("PRESYO_FB_PAGE_ID=123\nPRESYO_FB_PAGE_TOKEN="+gaFBToken+"\n"), 0o600)
	return e
}

func (e *gaEnv) tick(hhmm string, extra ...string) int {
	e.t.Helper()
	now := "2026-11-02T" + hhmm + ":00+08:00"
	args := []string{"-auto", "-env-file", e.envF, "-fb-env-file", e.fbEnv, "-state", e.state,
		"-graph-base", e.graph.srv.URL, "-out-dir", filepath.Join(e.home, "out", strings.ReplaceAll(hhmm, ":", "")),
		"-now", now, "-disable-file", filepath.Join(e.home, "disabled")}
	args = append(args, extra...)
	var code int
	oldOut := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	e.stderr = captureStderr(e.t, func() { code = cmdGrabcard(args) })
	w.Close()
	os.Stdout = oldOut
	b := make([]byte, 1<<16)
	n, _ := r.Read(b)
	e.stdout = string(b[:n])
	for _, bad := range []string{gaFBToken, gcToken, e.api.URL, e.graph.srv.URL} {
		if strings.Contains(e.stdout, bad) || strings.Contains(e.stderr, bad) {
			e.t.Errorf("tick %s leaks a secret/URL", hhmm)
		}
	}
	return code
}

func (e *gaEnv) posts() []fbPostEntry {
	st, err := loadFBState(e.state)
	if err != nil {
		e.t.Fatal(err)
	}
	return st.Posts
}

func (e *gaEnv) seed(posts ...fbPostEntry) {
	e.t.Helper()
	if err := saveFBState(e.state, &fbPostState{Posts: posts}); err != nil {
		e.t.Fatal(err)
	}
}

// 08:xx runs: ts, fare, optional maps (clean fixtures).
func gaRuns08(withMaps bool) ([]gcRun, map[string]string) {
	var maps []string
	files := map[string]string{}
	if withMaps {
		maps = []string{"map1", "map2", "map4"}
		for _, k := range maps {
			files["20261102-0800/"+k] = "clean.webp"
		}
	}
	return []gcRun{
		gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, nil, maps...),
		gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, nil, "map1", "map2"),
	}, files
}

func TestGrabAutoSequenceWithMapsPostsOncePerHour(t *testing.T) {
	runs, files := gaRuns08(true)
	e := gaSetup(t, runs, files)
	if code := e.tick("08:10"); code != 0 {
		t.Fatalf("08:10 exit %d: %s", code, e.stderr)
	}
	if e.graph.count() != 1 {
		t.Fatalf("posts = %d", e.graph.count())
	}
	if !strings.Contains(e.graph.msgs[0], "Hindi opisyal na pamasahe") || !strings.Contains(e.graph.msgs[0], "kumpara sa") {
		t.Errorf("caption missing disclaimer/trend: %q", e.graph.msgs[0])
	}
	ps := e.posts()
	if len(ps) != 1 || ps[0].Region != "grab" || ps[0].AsOf != "2026-11-02T08" || ps[0].PostID == "" {
		t.Fatalf("state = %+v", ps)
	}
	for _, hm := range []string{"08:15", "08:20"} {
		if code := e.tick(hm); code != 0 || !strings.Contains(e.stderr, "already posted") {
			t.Errorf("%s: exit %d stderr=%s", hm, code, e.stderr)
		}
	}
	if e.graph.count() != 1 || len(e.posts()) != 1 {
		t.Errorf("later ticks must no-op: posts=%d state=%d", e.graph.count(), len(e.posts()))
	}
}

func TestGrabAutoRetryThenFareOnlyFallback(t *testing.T) {
	runs, files := gaRuns08(false) // 08:00 fare landed, maps not ready
	e := gaSetup(t, runs, files)
	for _, hm := range []string{"08:10", "08:15"} {
		if code := e.tick(hm); code != 4 {
			t.Fatalf("%s: exit %d, want 4 (no fresh maps); %s", hm, code, e.stderr)
		}
		if e.graph.count() != 0 {
			t.Fatalf("%s posted without maps", hm)
		}
	}
	if code := e.tick("08:20"); code != 0 {
		t.Fatalf("08:20 exit %d: %s", code, e.stderr)
	}
	if e.graph.count() != 1 || !strings.Contains(e.stdout, "fare_only=true") {
		t.Errorf("fare-only fallback missing: n=%d out=%s err=%s", e.graph.count(), e.stdout, e.stderr)
	}
	if !strings.Contains(e.stderr, "falling back to the fare-only card") {
		t.Errorf("stderr = %s", e.stderr)
	}
}

func TestGrabAutoMapsAppearAtTwentyUsesMaps(t *testing.T) {
	runs, files := gaRuns08(true)
	e := gaSetup(t, runs, files)
	if code := e.tick("08:20"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(e.stderr, "fare-only") {
		t.Errorf("used fare-only although maps exist: %s", e.stderr)
	}
}

func TestGrabAutoNeverPostsStaleOrMissingFare(t *testing.T) {
	// Only the 07:00 hour is available at 08:xx: never posted as 08:00.
	runs := []gcRun{gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, nil, "map1", "map2")}
	e := gaSetup(t, runs, map[string]string{"20261102-0700/map1": "clean.webp", "20261102-0700/map2": "clean.webp"})
	for _, hm := range []string{"08:10", "08:15", "08:20", "08:25"} {
		code := e.tick(hm)
		want := 4
		if code != want {
			t.Errorf("%s: exit %d want %d", hm, code, want)
		}
	}
	if e.graph.count() != 0 {
		t.Fatal("posted stale data")
	}
	// Pending fare in the current hour: skip.
	pending := gcRun{"ts": "20261102-0800", "captured_at": gcCap(8, 0), "status": "pending", "fare": nil, "traffic": nil, "map_images": []string{"map1", "map2"}}
	e2 := gaSetup(t, []gcRun{pending}, map[string]string{"20261102-0800/map1": "clean.webp", "20261102-0800/map2": "clean.webp"})
	if code := e2.tick("08:20"); code != 4 || e2.graph.count() != 0 {
		t.Errorf("pending fare: exit %d posts %d", code, e2.graph.count())
	}
}

func TestGrabAutoGatesWindowMinuteAndKillSwitch(t *testing.T) {
	runs, files := gaRuns08(true)
	e := gaSetup(t, runs, files)
	t.Setenv("PRESYO_GRAB_FB", "")
	if code := e.tick("08:10"); code != 0 || !strings.Contains(e.stderr, "PRESYO_GRAB_FB") || e.graph.count() != 0 {
		t.Errorf("gate off: exit %d err=%s posts=%d", code, e.stderr, e.graph.count())
	}
	t.Setenv("PRESYO_GRAB_FB", "1")
	for _, hm := range []string{"08:05", "08:09", "08:30", "08:45"} {
		if code := e.tick(hm); code != 0 || !strings.Contains(e.stderr, "tick minutes") {
			t.Errorf("%s should be outside tick minutes: exit %d err=%s", hm, code, e.stderr)
		}
	}
	for _, hm := range []string{"04:10", "22:10", "23:10", "00:10"} {
		if code := e.tick(hm); code != 0 || !strings.Contains(e.stderr, "outside posting window") {
			t.Errorf("%s should be outside window: exit %d err=%s", hm, code, e.stderr)
		}
	}
	if e.graph.count() != 0 {
		t.Fatal("posted outside gates")
	}
	// configurable window via env
	t.Setenv("PRESYO_GRAB_POST_WINDOW", "08:00-09:00")
	if code := e.tick("07:10"); !strings.Contains(e.stderr, "outside posting window 08:00-09:00") || code != 0 {
		t.Errorf("env window ignored: %s", e.stderr)
	}
	// kill switch
	os.WriteFile(filepath.Join(e.home, "disabled"), nil, 0o644)
	if code := e.tick("08:10"); code != 0 || !strings.Contains(e.stderr, "disabled") || e.graph.count() != 0 {
		t.Errorf("kill switch: exit %d err=%s", code, e.stderr)
	}
	os.Remove(filepath.Join(e.home, "disabled"))
	if code := e.tick("08:10"); code != 0 || e.graph.count() != 1 {
		t.Errorf("post after clearing gates: exit %d posts %d err=%s", code, e.graph.count(), e.stderr)
	}
}

func TestGrabAutoGapRules(t *testing.T) {
	cases := []struct {
		name string
		seed []fbPostEntry
		at   string
		post bool
		why  string
	}{
		{"previous grab 25 min ago", []fbPostEntry{{Region: "grab", AsOf: "2026-11-02T07", PostID: "g", PostedAt: gcCap(7, 45)}}, "08:10", false, "last Grab post"},
		{"previous grab 40 min ago", []fbPostEntry{{Region: "grab", AsOf: "2026-11-02T07", PostID: "g", PostedAt: gcCap(7, 30)}}, "08:10", false, "last Grab post"},
		{"previous grab 45 min ago (grace)", []fbPostEntry{{Region: "grab", AsOf: "2026-11-02T07", PostID: "g", PostedAt: gcCap(7, 25)}}, "08:10", true, ""},
		{"previous grab 60 min ago", []fbPostEntry{{Region: "grab", AsOf: "2026-11-02T07", PostID: "g", PostedAt: gcCap(7, 10)}}, "08:10", true, ""},
		{"other post 8 min ago", []fbPostEntry{{Region: "da-digest", AsOf: "2026-11-02", PostID: "d", PostedAt: gcCap(8, 2)}}, "08:10", false, "another post"},
		{"other post 14 min ago", []fbPostEntry{{Region: "util-fuel", AsOf: "x", PostID: "u", PostedAt: gcCap(7, 56)}}, "08:10", false, "another post"},
		{"other post 16 min ago", []fbPostEntry{{Region: "util-water", AsOf: "x", PostID: "u", PostedAt: gcCap(7, 54)}}, "08:10", true, ""},
		{"other post 18 min ago at :20", []fbPostEntry{{Region: "da-digest", AsOf: "2026-11-02", PostID: "d", PostedAt: gcCap(8, 2)}}, "08:20", true, ""},
		{"other post 70 min ago", []fbPostEntry{{Region: "weekly", AsOf: "w", PostID: "w", PostedAt: gcCap(7, 0)}}, "08:10", true, ""},
	}
	for _, c := range cases {
		runs, files := gaRuns08(true)
		e := gaSetup(t, runs, files)
		e.seed(c.seed...)
		code := e.tick(c.at)
		if code != 0 {
			t.Errorf("%s: exit %d %s", c.name, code, e.stderr)
			continue
		}
		if got := e.graph.count() == 1; got != c.post {
			t.Errorf("%s: posted=%v want %v (%s)", c.name, got, c.post, e.stderr)
		}
		if !c.post && !strings.Contains(e.stderr, c.why) {
			t.Errorf("%s: stderr %q lacks %q", c.name, e.stderr, c.why)
		}
	}
}

func TestLastNonGrabPostIgnoresGrab(t *testing.T) {
	st := &fbPostState{Posts: []fbPostEntry{
		{Region: "da-digest", PostedAt: gcCap(6, 0)},
		{Region: "grab", PostedAt: gcCap(8, 10)},
		{Region: "grab", PostedAt: gcCap(9, 10)},
	}}
	got := lastNonGrabPost(st)
	want, _ := time.Parse(time.RFC3339, gcCap(6, 0))
	if !got.Equal(want) {
		t.Errorf("lastNonGrabPost = %v want %v", got, want)
	}
	if !lastNonGrabPost(&fbPostState{Posts: []fbPostEntry{{Region: "grab", PostedAt: gcCap(9, 10)}}}).IsZero() {
		t.Error("only grab posts must give zero time")
	}
}

// The 75-min digest rule and the utilities/weekly gaps must not be blocked by
// hourly Grab posts.
func TestGrabPostsDoNotBlockOtherGaps(t *testing.T) {
	st := &fbPostState{Posts: []fbPostEntry{{Region: "grab", PostedAt: gcCap(8, 10)}}}
	if !lastNonGrabPost(st).IsZero() {
		t.Fatal("grab counted as last post")
	}
}

func TestGrabAutoDryRunNeverPostsOrWritesState(t *testing.T) {
	runs, files := gaRuns08(true)
	e := gaSetup(t, runs, files)
	os.Remove(e.fbEnv) // dry-run must not need FB credentials
	if code := e.tick("08:10", "-dry-run"); code != 0 {
		t.Fatalf("exit %d %s", code, e.stderr)
	}
	if !strings.Contains(e.stdout, "dry-run: would post grab 2026-11-02T08") || e.graph.count() != 0 {
		t.Errorf("out=%s posts=%d", e.stdout, e.graph.count())
	}
	if _, err := os.Stat(e.state); err == nil {
		t.Error("dry-run wrote the state file")
	}
	// gate is not needed for a dry run
	t.Setenv("PRESYO_GRAB_FB", "")
	if code := e.tick("08:10", "-dry-run"); code != 0 || !strings.Contains(e.stdout, "would post") {
		t.Errorf("dry-run without gate: %d %s", code, e.stderr)
	}
	// -sim-record: simulates idempotency with a scratch state
	if code := e.tick("08:10", "-dry-run", "-sim-record"); code != 0 {
		t.Fatal(code)
	}
	if ps := e.posts(); len(ps) != 1 || ps[0].PostID != "dry-run" {
		t.Errorf("sim state = %+v", ps)
	}
	if code := e.tick("08:15", "-dry-run", "-sim-record"); code != 0 || !strings.Contains(e.stderr, "already posted") {
		t.Errorf("sim tick 2: %d %s", code, e.stderr)
	}
}

func TestGrabAutoErrors(t *testing.T) {
	runs, files := gaRuns08(true)
	e := gaSetup(t, runs, files)
	if code := e.tick("08:10", "-sample"); code != 1 || e.graph.count() != 0 {
		t.Errorf("-sample while posting: exit %d", code)
	}
	os.Remove(e.fbEnv)
	if code := e.tick("08:10"); code != 1 || e.graph.count() != 0 {
		t.Errorf("missing FB creds: exit %d", code)
	}
	if ps, _ := loadFBState(e.state); len(ps.Posts) != 0 {
		t.Error("state written after failure")
	}
	// bad token -> graph error, nothing recorded, token scrubbed
	os.WriteFile(e.fbEnv, []byte("PRESYO_FB_PAGE_ID=1\nPRESYO_FB_PAGE_TOKEN=wrong-token\n"), 0o600)
	if code := e.tick("08:10"); code != 1 {
		t.Errorf("graph error exit %d", code)
	}
	if ps, _ := loadFBState(e.state); len(ps.Posts) != 0 {
		t.Error("state written after a failed post")
	}
	if code := e.tick("08:10", "-window", "bogus"); code != 1 {
		t.Errorf("bad window exit %d", code)
	}
}

// An hourly Grab post 10 minutes ago must not trigger the DA digest's 75-min
// min-gap (digest "blocked60m" case with a Grab entry instead of util-fuel),
// and a grab entry must not satisfy any region slot.
func TestFBDigestIgnoresGrabPostsForGap(t *testing.T) {
	e := setupFBDigest(t)
	var hits int
	srv := digestGraph(t, &hits)
	defer srv.Close()
	writeDigestState(t, e.state, []fbPostEntry{
		{Region: "grab", AsOf: "2026-10-30T06", PostID: "g", PostedAt: "2026-10-30T06:50:00+08:00"},
	})
	if code := run(e.args("-auto", "-min-gap", "75m", "-now", "2026-10-30T07:00:00+08:00",
		"-out-dir", filepath.Join(e.home, "out"), "-graph-base", srv.URL)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Errorf("digest hits = %d, want 1 (a Grab post must not block it)", hits)
	}
}

func TestFBPostAutoIgnoresGrabPostsForGap(t *testing.T) {
	e := setupAuto(t, []string{"2026-10-24"})
	writeAutoState(t, e.state, []fbPostEntry{
		{Region: "grab", AsOf: "2026-10-25T06", PostID: "g", PostedAt: "2026-10-25T06:50:00+08:00"},
	})
	if code := run(e.autoArgs("-now", "2026-10-25T07:00:00+08:00")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.out, "south.txt")); err != nil {
		t.Errorf("a Grab post blocked the DA region post: %v", err)
	}
}
