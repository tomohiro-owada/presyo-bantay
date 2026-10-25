package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// fbpostTestDB builds a small DB with two south markets and one deal.
func fbpostTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fb.db")
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mk := map[string]int64{}
	for _, m := range []string{"New Las Piñas City Public Market", "Suki Market Moonwalk"} {
		res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		mk[m] = id
	}
	res, err := db.Exec("INSERT INTO commodities (name, category, unit) VALUES ('Tilapia', 'fish', 'per kg')")
	if err != nil {
		t.Fatal(err)
	}
	commID, _ := res.LastInsertId()
	asOf, _ := time.Parse("2006-01-02", "2026-10-23")
	for ago := 29; ago >= 0; ago-- {
		d := asOf.AddDate(0, 0, -ago).Format("2006-01-02")
		v := 100.0
		if ago == 0 {
			v = 85 // -15% deal in both markets
		}
		for _, id := range mk {
			if _, err := db.Exec("INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
				d, id, commID, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

type fbpostEnv struct {
	db, home, out string
}

func (e fbpostEnv) args(extra ...string) []string {
	return append([]string{
		"fbpost", "-db", e.db, "-region", "south",
		"-out-dir", e.out, "-max-age-days", "9999",
	}, extra...)
}

func setupFBPost(t *testing.T) fbpostEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PRESYO_HOME", home)
	t.Setenv("PRESYO_FB_PAGE_ID", "12345")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "test-token-abc")
	return fbpostEnv{db: fbpostTestDB(t), home: home, out: filepath.Join(home, "out")}
}

func TestFBPostSuccessWritesState(t *testing.T) {
	e := setupFBPost(t)
	var hits int
	var gotURL, gotCT string
	var fields map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotURL = r.URL.String()
		gotCT = r.Header.Get("Content-Type")
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart: %v", err)
		}
		fields = map[string]string{}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("part: %v", err)
				break
			}
			slurp, _ := io.ReadAll(p)
			if p.FormName() == "source" {
				fields["source_len"] = fmt.Sprint(len(slurp))
				fields["source_file"] = p.FileName()
			} else {
				fields[p.FormName()] = string(slurp)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"photo1","post_id":"post1"}`)
	}))
	defer srv.Close()

	if code := run(append(e.args(), "-graph-base", srv.URL, "-state", filepath.Join(e.home, "state.json"))); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
	if !strings.HasPrefix(gotURL, "/12345/photos") {
		t.Errorf("url = %q", gotURL)
	}
	if strings.Contains(gotURL, "test-token-abc") {
		t.Error("token must not be in the URL")
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Errorf("content-type = %q", gotCT)
	}
	for _, k := range []string{"message", "access_token", "published"} {
		if _, ok := fields[k]; !ok {
			t.Errorf("missing multipart field %q: %v", k, fields)
		}
	}
	if fields["access_token"] != "test-token-abc" {
		t.Error("access_token field wrong")
	}
	if fields["published"] != "true" {
		t.Error("published field wrong")
	}
	if !strings.Contains(fields["message"], "Sulit sa South Metro") {
		t.Error("message missing title")
	}
	if fields["source_len"] == "" || fields["source_len"] == "0" {
		t.Errorf("source file missing: %v", fields)
	}
	// State written.
	stData, err := os.ReadFile(filepath.Join(e.home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st fbPostState
	if err := json.Unmarshal(stData, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Posts) != 1 || st.Posts[0].Region != "south" || st.Posts[0].AsOf != "2026-10-23" ||
		st.Posts[0].PostID != "post1" || st.Posts[0].PhotoID != "photo1" || st.Posts[0].PostedAt == "" {
		t.Errorf("state = %+v", st)
	}
	// Artifact files exist.
	for _, f := range []string{"south.txt", "south.png", "south.signals.json", "south.check.json"} {
		if _, err := os.Stat(filepath.Join(e.out, f)); err != nil {
			t.Errorf("missing artifact %s: %v", f, err)
		}
	}

	// Second run skips (already posted), no new request.
	if code := run(append(e.args(), "-graph-base", srv.URL, "-state", filepath.Join(e.home, "state.json"))); code != 0 {
		t.Fatalf("second run exit = %d", code)
	}
	if hits != 1 {
		t.Errorf("second run must not post again, hits = %d", hits)
	}
}

func TestFBPostDisabledSkips(t *testing.T) {
	e := setupFBPost(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not post when disabled")
	}))
	defer srv.Close()
	disPath := filepath.Join(e.home, "fbpost.disabled")
	if err := os.WriteFile(disPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run(append(e.args(), "-graph-base", srv.URL, "-disable-file", disPath)); code != 0 {
		t.Fatalf("exit = %d", code)
	}
}

func TestFBPostStaleSkips(t *testing.T) {
	e := setupFBPost(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not post stale data")
	}))
	defer srv.Close()
	if code := run(append(e.args(), "-graph-base", srv.URL, "-date", "2020-01-01", "-max-age-days", "2")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
}

func TestFBPostGraphError(t *testing.T) {
	e := setupFBPost(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintln(w, `{"error":{"message":"Invalid token","code":190}}`)
	}))
	defer srv.Close()
	stPath := filepath.Join(e.home, "state.json")
	if code := run(append(e.args(), "-graph-base", srv.URL, "-state", stPath)); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if _, err := os.Stat(stPath); !os.IsNotExist(err) {
		t.Error("state must not be written on graph error")
	}
}

func TestFBPostDryRunNoHTTP(t *testing.T) {
	e := setupFBPost(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("dry-run must make no HTTP request")
	}))
	defer srv.Close()
	// Unset credentials: dry-run must not need the token.
	t.Setenv("PRESYO_FB_PAGE_ID", "")
	t.Setenv("PRESYO_FB_PAGE_TOKEN", "")
	if code := run(append(e.args(), "-graph-base", srv.URL, "-dry-run")); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, f := range []string{"south.txt", "south.png", "south.signals.json", "south.check.json"} {
		if _, err := os.Stat(filepath.Join(e.out, f)); err != nil {
			t.Errorf("missing artifact %s: %v", f, err)
		}
	}
}

func TestFBPostMultipartShape(t *testing.T) {
	// Verify the request is well-formed multipart with a PNG part.
	e := setupFBPost(t)
	var fn, ct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			// Fall back to manual reader for large file parts.
			mr, merr := r.MultipartReader()
			if merr != nil {
				t.Errorf("multipart: %v", err)
				return
			}
			_ = mr
		}
		f, hdr, err := r.FormFile("source")
		if err != nil {
			t.Errorf("source part: %v", err)
			return
		}
		defer f.Close()
		fn, ct = hdr.Filename, hdr.Header.Get("Content-Type")
		head := make([]byte, 8)
		if _, err := io.ReadFull(f, head); err != nil {
			t.Errorf("read: %v", err)
		}
		if string(head[1:4]) != "PNG" {
			t.Errorf("source is not a PNG")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":"p","post_id":"q"}`)
	}))
	defer srv.Close()
	if code := run(append(e.args(), "-graph-base", srv.URL, "-state", filepath.Join(e.home, "s2.json"))); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if fn == "" || ct != "image/png" {
		t.Errorf("source part filename=%q content-type=%q", fn, ct)
	}
}
