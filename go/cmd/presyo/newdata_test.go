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

	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// runCapture runs argv with stdout captured.
func runCapture(t *testing.T, argv []string) (int, string) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := run(argv)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	return code, buf.String()
}

// newdataTestDB builds a DB whose MAX(prices.date) is maxDate.
func newdataTestDB(t *testing.T, maxDate string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nd.db")
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	res, err := db.Exec("INSERT INTO markets (name) VALUES ('M')")
	if err != nil {
		t.Fatal(err)
	}
	mkt, _ := res.LastInsertId()
	res, err = db.Exec("INSERT INTO commodities (name, category, unit) VALUES ('Tilapia', 'fish', 'per kg')")
	if err != nil {
		t.Fatal(err)
	}
	comm, _ := res.LastInsertId()
	if _, err := db.Exec("INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, 100)",
		maxDate, mkt, comm); err != nil {
		t.Fatal(err)
	}
	return path
}

func listingHTML(dates ...string) string {
	var b strings.Builder
	b.WriteString("<html><body>\n")
	for _, d := range dates {
		// d is YYYY-MM-DD; filename wants Month-D-YYYY.
		var y, m, day string
		fmt.Sscanf(d, "%4s-%2s-%2s", &y, &m, &day)
		months := map[string]string{"10": "October", "09": "September", "11": "November"}
		mn := months[m]
		day = strings.TrimLeft(day, "0")
		fmt.Fprintf(&b, `<a href="https://www.da.gov.ph/wp-content/uploads/2026/10/Price-Monitoring-%s-%s-%s.pdf">x</a>`+"\n",
			mn, day, y)
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

func TestNewdataNewDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("unexpected path %q (must only GET the listing page)", r.URL.Path)
		}
		fmt.Fprint(w, listingHTML("2026-10-24", "2026-10-23"))
	}))
	defer srv.Close()
	db := newdataTestDB(t, "2026-10-23")
	st := filepath.Join(t.TempDir(), "newdata.json")
	code, out := runCapture(t, []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (out=%q)", code, out)
	}
	if strings.TrimSpace(out) != "2026-10-24" {
		t.Errorf("output = %q, want 2026-10-24", out)
	}
}

func TestNewdataNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, listingHTML("2026-10-23"))
	}))
	defer srv.Close()
	db := newdataTestDB(t, "2026-10-23")
	st := filepath.Join(t.TempDir(), "newdata.json")
	if code, _ := runCapture(t, []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL}); code != 10 {
		t.Fatalf("exit = %d, want 10", code)
	}
}

func TestNewdata304(t *testing.T) {
	var sawValidator bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"abc"` {
			sawValidator = true
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		fmt.Fprint(w, listingHTML("2026-10-24"))
	}))
	defer srv.Close()
	db := newdataTestDB(t, "2026-10-24")
	st := filepath.Join(t.TempDir(), "newdata.json")
	if code, _ := runCapture(t, []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL}); code != 10 {
		t.Fatalf("first run exit = %d, want 10", code)
	}
	if code, _ := runCapture(t, []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL}); code != 10 {
		t.Fatalf("304 run exit = %d, want 10", code)
	}
	if !sawValidator {
		t.Error("second request did not send If-None-Match")
	}
}

func TestNewdataError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	db := newdataTestDB(t, "2026-10-23")
	st := filepath.Join(t.TempDir(), "newdata.json")
	if code, _ := runCapture(t, []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

// A 200 that sees a new date stores the listed dates; if the DB still
// lacks that date on the next run (daily.sh failed to ingest), a 304
// must keep reporting it instead of going quiet.
func TestNewdata304RetryAfterFailedIngest(t *testing.T) {
	mode := "full"
	var sawValidator bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			sawValidator = true
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if mode == "full" {
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, listingHTML("2026-10-24", "2026-10-23"))
			return
		}
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, listingHTML("2026-10-23"))
	}))
	defer srv.Close()
	db := newdataTestDB(t, "2026-10-23")
	st := filepath.Join(t.TempDir(), "newdata.json")
	args := []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL}

	code, out := runCapture(t, args)
	if code != 0 || strings.TrimSpace(out) != "2026-10-24" {
		t.Fatalf("first run exit = %d out = %q, want 0 / 2026-10-24", code, out)
	}
	// The listed dates must be stored in the state file.
	var saved struct {
		Dates []string `json:"dates"`
	}
	raw, err := os.ReadFile(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Dates) == 0 || saved.Dates[0] != "2026-10-24" {
		t.Fatalf("state dates = %v, want newest first 2026-10-24", saved.Dates)
	}

	// DB still at 2026-10-23 (ingest failed); listing goes quiet -> 304.
	code, out = runCapture(t, args)
	if code != 0 || strings.TrimSpace(out) != "2026-10-24" {
		t.Fatalf("304 run exit = %d out = %q, want 0 / 2026-10-24", code, out)
	}
	if !sawValidator {
		t.Error("304 run did not send If-None-Match")
	}
}

// A 304 with the DB caught up to the cached listed dates means no data.
func TestNewdata304CaughtUp(t *testing.T) {
	var sawValidator bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v9"` {
			sawValidator = true
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v9"`)
		fmt.Fprint(w, listingHTML("2026-10-24"))
	}))
	defer srv.Close()
	db := newdataTestDB(t, "2026-10-24")
	st := filepath.Join(t.TempDir(), "newdata.json")
	primed, err := json.Marshal(map[string]any{"etag": `"v9"`, "dates": []string{"2026-10-24", "2026-10-23"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st, primed, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := runCapture(t, []string{"newdata", "-db", db, "-state", st, "-listing-url", srv.URL}); code != 10 {
		t.Fatalf("exit = %d, want 10", code)
	}
	if !sawValidator {
		t.Error("request did not send If-None-Match")
	}
}
