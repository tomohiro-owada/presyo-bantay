package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const gcToken = "grabcard-test-token-0123456789"

type gcRun map[string]any

func gcFare(min, max, std float64) map[string]any {
	return map[string]any{"currency": "PHP",
		"standard":     map[string]any{"price": std, "eta_min": 8},
		"metered_taxi": map[string]any{"min": min, "max": max, "eta_min": 8}}
}

func gcTraffic(summary string) map[string]any {
	return map[string]any{"level": "light", "summary": summary, "green_pct": 90, "yellow_pct": 8, "red_pct": 2, "segments": []any{}}
}

func gcRunOf(ts, cap string, min, max float64, traffic any, maps ...string) gcRun {
	if maps == nil {
		maps = []string{}
	}
	return gcRun{"ts": ts, "captured_at": cap, "analyzed_at": nil, "status": "complete",
		"fare": gcFare(min, max, min+1), "traffic": traffic, "map_images": maps}
}

// gcServer serves /v1/grab/recent and map images from the grabfare testdata
// (kind -> fixture file). Bearer is enforced.
func gcServer(t *testing.T, runs []gcRun, files map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gcToken {
			http.Error(w, "no", 401)
			return
		}
		switch {
		case r.URL.Path == "/v1/grab/recent":
			doc := map[string]any{"route": map[string]any{"from": "Zapote Bridge", "to": "Festival Supermall Alabang", "via": "Alabang-Zapote Road"},
				"currency": "PHP", "hours": 26, "count": len(runs), "runs": runs}
			json.NewEncoder(w).Encode(doc)
		case strings.HasPrefix(r.URL.Path, "/v1/grab/map/"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/grab/map/"), "/")
			kind := strings.TrimSuffix(parts[1], ".webp")
			f, ok := files[parts[0]+"/"+kind]
			if !ok {
				http.NotFound(w, r)
				return
			}
			data, err := os.ReadFile(filepath.Join("..", "..", "internal", "grabfare", "testdata", f))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/webp")
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func gcEnvFile(t *testing.T, url string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "grab.env")
	os.WriteFile(p, []byte(fmt.Sprintf("PRESYO_GRAB_API_URL=%s\nPRESYO_GRAB_API_TOKEN=%s\n", url, gcToken)), 0o600)
	return p
}

const gcNow = "2026-11-02T08:20:00+08:00"

func gcCap(h, m int) string {
	return time.Date(2026, 11, 2, h, m, 0, 0, time.FixedZone("m", 8*3600)).Format(time.RFC3339)
}

func gcRun2(t *testing.T, srv *httptest.Server, out string, extra ...string) (int, string) {
	t.Helper()
	args := append([]string{"-env-file", gcEnvFile(t, srv.URL), "-out-dir", out, "-now", gcNow}, extra...)
	var code int
	stderr := captureStderr(t, func() { code = cmdGrabcard(args) })
	return code, stderr
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	f()
	w.Close()
	os.Stderr = old
	b := make([]byte, 1<<16)
	n, _ := r.Read(b)
	return string(b[:n])
}

func readJSONFile(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGrabcardHappyPathDropsMarker(t *testing.T) {
	runs := []gcRun{
		gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, gcTraffic("Traffic is light along the whole route."), "map1", "map2", "map3", "map4"),
		gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, nil),
	}
	files := map[string]string{}
	for _, k := range []string{"map1", "map2", "map4"} {
		files["20261102-0800/"+k] = "clean.webp"
	}
	files["20261102-0800/map3"] = "marker.webp"
	srv := gcServer(t, runs, files)
	out := t.TempDir()
	code, stderr := gcRun2(t, srv, out)
	if code != 0 {
		t.Fatalf("exit %d (%s)", code, stderr)
	}
	meta := readJSONFile(t, filepath.Join(out, "grab-card.json"))
	if meta["run_ts"] != "20261102-0800" {
		t.Errorf("run_ts = %v", meta["run_ts"])
	}
	if got := fmt.Sprint(meta["maps"]); got != "[map1 map2 map4]" {
		t.Errorf("maps = %s", got)
	}
	if got := fmt.Sprint(meta["dropped_maps"]); !strings.Contains(got, "map3") || !strings.Contains(got, "device-marker") {
		t.Errorf("dropped = %s", got)
	}
	if tr := meta["traffic"].(map[string]any); tr["included"] != true {
		t.Errorf("traffic = %v", tr)
	}
	for _, f := range []string{"grab-card.png", "grab-card.txt", "grab-card.check.json"} {
		if st, err := os.Stat(filepath.Join(out, f)); err != nil || st.Size() == 0 {
			t.Errorf("missing %s", f)
		}
	}
	chk := readJSONFile(t, filepath.Join(out, "grab-card.check.json"))
	if chk["ok"] != true {
		t.Errorf("check: %v", chk)
	}
	// no secrets or server identifiers anywhere in outputs or stderr
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(out, e.Name()))
		for _, bad := range []string{gcToken, srv.URL, "127.0.0.1", "/home", "ssh"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s leaks %q", e.Name(), bad)
			}
		}
	}
	for _, bad := range []string{gcToken, srv.URL} {
		if strings.Contains(stderr, bad) {
			t.Errorf("stderr leaks %q", bad)
		}
	}
}

func TestGrabcardSkipsOverlayRunForOlderCleanRun(t *testing.T) {
	runs := []gcRun{
		gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, gcTraffic("The screenshot is a Grab food-order notification card overlaid on the map"), "map1", "map2"),
		gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, gcTraffic("Light traffic everywhere."), "map1", "map2"),
	}
	files := map[string]string{}
	for _, ts := range []string{"20261102-0800", "20261102-0700"} {
		files[ts+"/map1"], files[ts+"/map2"] = "clean.webp", "clean.webp"
	}
	srv := gcServer(t, runs, files)
	out := t.TempDir()
	if code, e := gcRun2(t, srv, out); code != 0 {
		t.Fatalf("exit %d %s", code, e)
	}
	if got := readJSONFile(t, filepath.Join(out, "grab-card.json"))["run_ts"]; got != "20261102-0700" {
		t.Errorf("run_ts = %v, want the clean older run", got)
	}
}

func TestGrabcardInsaneTrafficCardWithoutTraffic(t *testing.T) {
	tr := gcTraffic("Unable to determine the route colours.")
	runs := []gcRun{gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, tr, "map1", "map2"),
		gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, nil)}
	srv := gcServer(t, runs, map[string]string{"20261102-0800/map1": "clean.webp", "20261102-0800/map2": "clean.webp"})
	out := t.TempDir()
	if code, e := gcRun2(t, srv, out); code != 0 {
		t.Fatalf("exit %d %s", code, e)
	}
	meta := readJSONFile(t, filepath.Join(out, "grab-card.json"))
	if tr := meta["traffic"].(map[string]any); tr["included"] != false || tr["reason"] != "unusable" {
		t.Errorf("traffic = %v", tr)
	}
	b, _ := os.ReadFile(filepath.Join(out, "grab-card.txt"))
	if strings.Contains(string(b), "🚦") {
		t.Error("traffic line in caption")
	}
}

func TestGrabcardNoEligibleRun(t *testing.T) {
	// maps are all marker images -> fewer than 2 clean maps; and a run without maps
	runs := []gcRun{gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, nil, "map1", "map2"),
		gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, nil)}
	srv := gcServer(t, runs, map[string]string{"20261102-0800/map1": "marker.webp", "20261102-0800/map2": "marker.webp"})
	out := filepath.Join(t.TempDir(), "o")
	if code, _ := gcRun2(t, srv, out); code != 4 {
		t.Fatalf("exit %d, want 4", code)
	}
	if _, err := os.Stat(out); err == nil {
		entries, _ := os.ReadDir(out)
		if len(entries) != 0 {
			t.Error("wrote files without an eligible run")
		}
	}
	// -no-maps renders the fare-only card from the newest usable run
	out2 := t.TempDir()
	if code, e := gcRun2(t, srv, out2, "-no-maps"); code != 0 {
		t.Fatalf("no-maps exit %d %s", code, e)
	}
	if got := readJSONFile(t, filepath.Join(out2, "grab-card.json"))["run_ts"]; got != "20261102-0800" {
		t.Errorf("no-maps run_ts = %v", got)
	}
}

func TestGrabcardMissingConfigAndAuth(t *testing.T) {
	t.Setenv("PRESYO_GRAB_API_URL", "")
	t.Setenv("PRESYO_GRAB_API_TOKEN", "")
	out := t.TempDir()
	if code := cmdGrabcard([]string{"-out-dir", out}); code != 1 {
		t.Errorf("missing config exit %d", code)
	}
	runs := []gcRun{gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, nil, "map1")}
	srv := gcServer(t, runs, nil)
	p := filepath.Join(t.TempDir(), "bad.env")
	os.WriteFile(p, []byte("PRESYO_GRAB_API_URL="+srv.URL+"\nPRESYO_GRAB_API_TOKEN=wrong-token\n"), 0o600)
	var code int
	captureStderr(t, func() { code = cmdGrabcard([]string{"-env-file", p, "-out-dir", out, "-now", gcNow}) })
	if code != 1 {
		t.Errorf("bad token exit %d", code)
	}
}

func TestGrabcardInputFileMapsDir(t *testing.T) {
	dir := t.TempDir()
	runs := []gcRun{gcRunOf("20261102-0800", gcCap(8, 0), 320, 385, nil, "map1", "map2"),
		gcRunOf("20261102-0700", gcCap(7, 0), 308, 370, nil)}
	doc, _ := json.Marshal(map[string]any{"route": map[string]any{"from": "Zapote Bridge", "to": "Festival Supermall Alabang", "via": "Alabang-Zapote Road"}, "currency": "PHP", "runs": runs})
	in := filepath.Join(dir, "in.json")
	os.WriteFile(in, doc, 0o644)
	maps := filepath.Join(dir, "maps")
	os.MkdirAll(maps, 0o755)
	src, _ := os.ReadFile(filepath.Join("..", "..", "internal", "grabfare", "testdata", "clean.webp"))
	for _, k := range []string{"map1", "map2"} {
		os.WriteFile(filepath.Join(maps, "20261102-0800_"+k+".webp"), src, 0o644)
	}
	out := filepath.Join(dir, "out")
	var code int
	captureStderr(t, func() {
		code = cmdGrabcard([]string{"-input-file", in, "-maps-dir", maps, "-out-dir", out, "-now", gcNow, "-sample"})
	})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if meta := readJSONFile(t, filepath.Join(out, "grab-card.json")); meta["sample"] != true {
		t.Errorf("sample flag not recorded: %v", meta)
	}
}
