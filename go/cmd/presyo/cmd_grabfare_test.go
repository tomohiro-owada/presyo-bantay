package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func grabPayload() string {
	return `{"route":{"from":"Zapote Bridge","to":"Festival Mall","via":"Alabang-Zapote Rd","label":"x"},"source":"grab","currency":"PHP","server_time":"2026-11-02T08:00:00+08:00","hours":26,"count":2,"runs":[{"ts":"20261102-0800","captured_at":"2026-11-02T08:00:00+08:00","analyzed_at":"2026-11-02T08:01:00+08:00","status":"complete","fare":{"currency":"PHP","standard":{"price":300,"eta_min":5},"metered_taxi":{"min":282,"max":338,"eta_min":7}},"traffic":{"level":"heavy","summary":"Heavy traffic","green_pct":40,"yellow_pct":25,"red_pct":35,"segments":[{"level":"heavy","red_sections":["Alabang-Zapote Rd"],"yellow_sections":["Madrigal Ave"]}]}},{"ts":"20261102-0700","captured_at":"2026-11-02T07:00:00+08:00","analyzed_at":"2026-11-02T07:01:00+08:00","status":"complete","fare":{"currency":"PHP","standard":{"price":290,"eta_min":5},"metered_taxi":{"min":270,"max":326,"eta_min":7}},"traffic":null}]}`
}

func captureGrabfare(t *testing.T, argv []string) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	var outB, errB bytes.Buffer
	doneO := make(chan struct{})
	doneE := make(chan struct{})
	go func() { io.Copy(&outB, ro); close(doneO) }()
	go func() { io.Copy(&errB, re); close(doneE) }()
	code := cmdGrabfare(argv)
	wo.Close()
	we.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	<-doneO
	<-doneE
	return code, outB.String(), errB.String()
}

func TestGrabfareInputFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	if err := os.WriteFile(in, []byte(grabPayload()), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	code, _, _ := captureGrabfare(t, []string{"-out-dir", outDir, "-input-file", in, "-now", "2026-11-02T08:30:00+08:00"})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, n := range []string{"grab-fare.json", "grab-fare.html", "grab-fare-preview.html"} {
		if _, err := os.Stat(filepath.Join(outDir, n)); err != nil {
			t.Fatalf("%s missing: %v", n, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(outDir, "grab-fare.json"))
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["visible"] != true || doc["state"] != "fresh" {
		t.Fatalf("doc=%v", doc)
	}
}

func TestGrabfareFetchBearer(t *testing.T) {
	var gotAuth, gotHours, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHours = r.URL.Query().Get("hours")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, grabPayload())
	}))
	defer srv.Close()
	t.Setenv("PRESYO_GRAB_API_URL", srv.URL)
	t.Setenv("PRESYO_GRAB_API_TOKEN", "tok123")
	dir := t.TempDir()
	code, out, errOut := captureGrabfare(t, []string{"-out-dir", dir, "-now", "2026-11-02T08:30:00+08:00"})
	if code != 0 {
		t.Fatalf("exit %d out=%q err=%q", code, out, errOut)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotPath != "/v1/grab/recent" || gotHours != "50" {
		t.Fatalf("path=%q hours=%q", gotPath, gotHours)
	}
	combined := out + errOut
	if strings.Contains(combined, "tok123") || strings.Contains(combined, srv.URL) {
		t.Fatalf("token/URL leaked: %q", combined)
	}
	for _, n := range []string{"grab-fare.json", "grab-fare.html", "grab-fare-preview.html"} {
		data, _ := os.ReadFile(filepath.Join(dir, n))
		if strings.Contains(string(data), "tok123") || strings.Contains(string(data), srv.URL) {
			t.Fatalf("%s leaks secret", n)
		}
	}
}

func TestGrabfareMissingConfig(t *testing.T) {
	dir := t.TempDir()
	// Ensure env is clean.
	t.Setenv("PRESYO_GRAB_API_URL", "")
	t.Setenv("PRESYO_GRAB_API_TOKEN", "")
	os.Unsetenv("PRESYO_GRAB_API_URL")
	os.Unsetenv("PRESYO_GRAB_API_TOKEN")
	code, out, errOut := captureGrabfare(t, []string{"-out-dir", dir, "-now", "2026-11-02T08:30:00+08:00"})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	_ = out
	if !strings.Contains(errOut, "missing configuration") {
		t.Fatalf("stderr=%q", errOut)
	}
}

func TestGrabfareFailureExpiredRewritten(t *testing.T) {
	dir := t.TempDir()
	// Seed an expired card.
	if err := os.WriteFile(filepath.Join(dir, "grab-fare.json"),
		[]byte(`{"generated_at":"2026-11-02T05:00:00+08:00","visible":true,"state":"fresh","reason":"fresh","valid_until":"2026-11-02T07:00:00+08:00","route":{"from":"a","to":"b","via":"c"},"currency":"PHP"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "grab-fare.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	t.Setenv("PRESYO_GRAB_API_URL", srv.URL)
	t.Setenv("PRESYO_GRAB_API_TOKEN", "tok123")
	code, _, _ := captureGrabfare(t, []string{"-out-dir", dir, "-now", "2026-11-02T08:30:00+08:00"})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "grab-fare.json"))
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["visible"] != false {
		t.Fatalf("expired card must be hidden: %s", data)
	}
	snip, _ := os.ReadFile(filepath.Join(dir, "grab-fare.html"))
	if string(snip) != "<!-- grab-fare: hidden (stale) -->\n" {
		t.Fatalf("snippet=%q", snip)
	}
}

func TestGrabfareFailureValidLeftAlone(t *testing.T) {
	dir := t.TempDir()
	origJSON := `{"generated_at":"2026-11-02T08:00:00+08:00","visible":true,"state":"fresh","reason":"fresh","valid_until":"2026-11-02T10:00:00+08:00","route":{"from":"a","to":"b","via":"c"},"currency":"PHP"}`
	if err := os.WriteFile(filepath.Join(dir, "grab-fare.json"), []byte(origJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "grab-fare.html"), []byte("keepme"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	t.Setenv("PRESYO_GRAB_API_URL", srv.URL)
	t.Setenv("PRESYO_GRAB_API_TOKEN", "tok123")
	code, _, _ := captureGrabfare(t, []string{"-out-dir", dir, "-now", "2026-11-02T08:30:00+08:00"})
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "grab-fare.json"))
	if string(data) != origJSON {
		t.Fatalf("valid card must be untouched: %q", data)
	}
	snip, _ := os.ReadFile(filepath.Join(dir, "grab-fare.html"))
	if string(snip) != "keepme" {
		t.Fatalf("snippet must be untouched: %q", snip)
	}
}

func TestGrabfareScanOutputs(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	os.WriteFile(in, []byte(grabPayload()), 0o644)
	outDir := filepath.Join(dir, "out")
	if code, _, _ := captureGrabfare(t, []string{"-out-dir", outDir, "-input-file", in, "-now", "2026-11-02T08:30:00+08:00"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, n := range []string{"grab-fare.json", "grab-fare.html", "grab-fare-preview.html"} {
		data, _ := os.ReadFile(filepath.Join(outDir, n))
		s := string(data)
		for _, bad := range []string{"http", "ssh", "/home", "@"} {
			if strings.Contains(s, bad) {
				t.Fatalf("%s contains %q", n, bad)
			}
		}
	}
}

func TestGrabfareEnvFile(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, grabPayload())
	}))
	defer srv.Close()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "grab.env")
	os.WriteFile(envPath, []byte("# comment\nPRESYO_GRAB_API_URL="+srv.URL+"\nPRESYO_GRAB_API_TOKEN=envtok\n"), 0o644)
	os.Unsetenv("PRESYO_GRAB_API_URL")
	os.Unsetenv("PRESYO_GRAB_API_TOKEN")
	outDir := filepath.Join(dir, "out")
	code, _, _ := captureGrabfare(t, []string{"-out-dir", outDir, "-env-file", envPath, "-now", "2026-11-02T08:30:00+08:00"})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if gotAuth != "Bearer envtok" {
		t.Fatalf("auth=%q", gotAuth)
	}
}

func TestGrabfarePickupEndToEnd(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	doc := map[string]any{
		"route":  map[string]any{"from": "Zapote Bridge", "to": "Festival Mall", "via": "Alabang-Zapote Rd", "label": "x"},
		"pickup": map[string]any{"current": "Old Zapote Public Market", "since_ts": "20261102-1220", "since": "2026-11-02T12:20:00+08:00", "history": []map[string]any{{"since_ts": "19700101-0000", "pickup": "Zapote Bridge"}, {"since_ts": "20261102-1220", "pickup": "Old Zapote Public Market"}}},
		"source": "grab", "currency": "PHP", "server_time": "2026-11-02T13:00:00+08:00", "hours": 26, "count": 3,
		"runs": []map[string]any{
			{"ts": "20261102-1300", "captured_at": "2026-11-02T13:00:00+08:00", "status": "complete", "fare": map[string]any{"currency": "PHP", "standard": map[string]any{"price": 320}, "metered_taxi": map[string]any{"min": 300, "max": 360}}, "traffic": nil},
			{"ts": "20261102-1220", "captured_at": "2026-11-02T12:20:00+08:00", "status": "complete", "fare": map[string]any{"currency": "PHP", "standard": map[string]any{"price": 310}, "metered_taxi": map[string]any{"min": 290, "max": 350}}, "traffic": nil},
			{"ts": "20261102-1200", "captured_at": "2026-11-02T12:00:00+08:00", "status": "complete", "fare": map[string]any{"currency": "PHP", "standard": map[string]any{"price": 300}, "metered_taxi": map[string]any{"min": 280, "max": 340}}, "traffic": nil},
		},
	}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(in, b, 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	code, _, _ := captureGrabfare(t, []string{"-out-dir", outDir, "-input-file", in, "-now", "2026-11-02T13:10:00+08:00"})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(outDir, "grab-fare.json"))
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if route, _ := got["route"].(map[string]any); route["from"] != "Old Zapote Public Market" {
		t.Fatalf("route.from=%v", route["from"])
	}
	if pickup, _ := got["pickup"].(map[string]any); pickup["name"] != "Old Zapote Public Market" {
		t.Fatalf("pickup=%v", got["pickup"])
	}
	if trend, _ := got["trend"].(map[string]any); trend["previous_hhmm"] != "12:20" {
		t.Fatalf("trend=%v", got["trend"])
	}
	hourly, _ := got["hourly"].(map[string]any)
	today, _ := hourly["today"].([]any)
	if len(today) == 0 {
		t.Fatal("hourly.today empty")
	}
	text, _ := got["text"].(map[string]any)
	en, _ := text["en"].(map[string]any)
	if pn, _ := en["pickup_note"].(string); pn == "" {
		t.Fatal("pickup_note empty")
	}
}
