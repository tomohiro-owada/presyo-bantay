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

// fixtureServer serves the saved fixtures behind the Fetch URL layout.
// Page paths are literal real paths (not the production constants) so
// a wrong path in Fetch 404s and fails the test.
type fixtureServer struct {
	t        *testing.T
	pdfs     map[string]string // URL file key -> fixture name
	requests []string
	uas      []string
	// pages overrides literal page paths: fixture name, "__notfound__"
	// (HTTP 200 "Page Not Found"), "__empty__" (HTTP 200, no links),
	// or "__404__" (HTTP 404, releases only).
	pages map[string]string
}

const (
	notFoundHTML = `<html><head><title>Page Not Found</title></head><body><h1>Page Not Found</h1></body></html>`
	emptyHTML    = `<html><head><title>Empty</title></head><body><p>no attachments</p></body></html>`
)

func (s *fixtureServer) servePage(w http.ResponseWriter, path, defFixture string) {
	if ov, ok := s.pages[path]; ok {
		switch ov {
		case "__notfound__":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(notFoundHTML))
			return
		case "__empty__":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(emptyHTML))
			return
		default:
			defFixture = ov
		}
	}
	b, err := os.ReadFile(testdata(s.t, defFixture))
	if err != nil {
		s.t.Fatalf("fixture %s: %v", defFixture, err)
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(b)
}

func (s *fixtureServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r.URL.Path+"?"+r.URL.RawQuery)
		s.uas = append(s.uas, r.Header.Get("User-Agent"))
		p := r.URL.Path
		serve := func(name, ctype string) {
			b, err := os.ReadFile(testdata(s.t, name))
			if err != nil {
				s.t.Fatalf("fixture %s: %v", name, err)
			}
			w.Header().Set("Content-Type", ctype)
			w.Write(b)
		}
		switch {
		case p == "/data-and-prices/liquid-fuels/oil-monitor":
			s.servePage(w, p, "doe_oil_monitor_page.html")
		case p == "/data-and-prices/liquid-fuels/retail-pump-prices/ncr-pump-prices":
			s.servePage(w, p, "doe_ncr_pump_page.html")
		case p == "/data-and-prices/lpg-monitor/ncr-lpg-prices":
			s.servePage(w, p, "doe_ncr_lpg_page.html")
		case p == "/news-and-advisories/latest-news-and-press-releases":
			s.servePage(w, p, "meralco_news_page.html")
		case strings.HasPrefix(p, "/news-and-advisories/"):
			if ov, ok := s.pages[p]; ok && ov == "__404__" {
				w.WriteHeader(404)
				return
			}
			slug := strings.TrimPrefix(p, "/news-and-advisories/")
			switch {
			case strings.Contains(slug, "october-2026"):
				serve("meralco_2026_10.html", "text/html")
			case strings.Contains(slug, "september-2026"):
				serve("meralco_2026_09.html", "text/html")
			default:
				w.WriteHeader(404)
			}
		case strings.HasPrefix(p, "/api/media/file/"):
			key := strings.TrimPrefix(p, "/api/media/file/")
			if fix, ok := s.pdfs[key]; ok {
				serve(fix, "application/pdf")
			} else {
				w.WriteHeader(404)
			}
		case strings.HasPrefix(p, "/wp-json/interruptions/"):
			serve("maynilad_scheduled_laspinas_empty.json", "application/json")
		case strings.HasPrefix(p, "/wp-json/wp/v2/posts"):
			serve("maynilad_maintenance_posts.json", "application/json")
		default:
			w.WriteHeader(404)
		}
	})
}

func newFixtureServer(t *testing.T) (*httptest.Server, *fixtureServer) {
	t.Helper()
	fs := &fixtureServer{t: t, pdfs: map[string]string{
		"Oil Monitor 20 October.pdf":                  "oilmon_20oct.pdf",
		"Oil Monitor 13 October.pdf":                  "oilmon_13oct.pdf",
		"Oil Monitor 06 October.pdf":                  "oilmon_06oct.pdf",
		"Oil Monitor 29 September.pdf":                "oilmon_29sep.pdf",
		"Oil Monitor 22 September.pdf":                "oilmon_22sep.pdf",
		"Oil Monitor As of 15 September 2026.pdf":     "oilmon_15sep.pdf",
		"NCR Price Monitoring 10132026.pdf":           "ncr_pump_1013.pdf",
		"NCR Price Monitoring 10062026.pdf":           "ncr_pump_1006.pdf",
		"NCR October 2026 LPG Price Monitoring.pdf":   "ncr_lpg_oct.pdf",
		"NCR September 2026 LPG Price Monitoring.pdf": "ncr_lpg_sep.pdf",
	}}
	srv := httptest.NewServer(fs.handler())
	t.Cleanup(srv.Close)
	return srv, fs
}

func fetchOpts(srv *httptest.Server, fs *fixtureServer, cache string) FetchOptions {
	return FetchOptions{
		Client:       srv.Client(),
		CacheDir:     cache,
		Now:          time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
		MinDelay:     -1,
		UserAgent:    "PresyoBantay-Test/0.1",
		DOEBase:      srv.URL,
		DOEMediaBase: srv.URL,
		MeralcoBase:  srv.URL,
		MayniladBase: srv.URL,
		Sleep:        func(time.Duration) {},
	}
}

func pdfKeys(fs *fixtureServer, onlySecond bool) []string {
	var out []string
	for _, r := range fs.requests {
		if !strings.HasPrefix(r, "/api/media/file/") {
			continue
		}
		key := strings.SplitN(strings.TrimPrefix(r, "/api/media/file/"), "?", 2)[0]
		out = append(out, key)
	}
	return out
}

func TestFetchBuildsSnapshot(t *testing.T) {
	srv, fs := newFixtureServer(t)
	snap, err := Fetch(context.Background(), fetchOpts(srv, fs, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if snap.FetchedAt != "2026-10-26T20:00:00+08:00" {
		t.Fatalf("fetched_at = %q", snap.FetchedAt)
	}
	if len(snap.Fuel.Adjustments) != 6 {
		t.Fatalf("adjustments = %d, errors = %v", len(snap.Fuel.Adjustments), snap.Errors)
	}
	a0 := snap.Fuel.Adjustments[0]
	if a0.EffectiveFrom != "2026-10-20" || a0.Gasoline == nil || *a0.Gasoline != (Range{-4.30, -4.30}) {
		t.Fatalf("newest adjustment = %+v", a0)
	}
	if len(snap.Fuel.NCRPump) != 2 || snap.Fuel.NCRPump[0].WeekFrom != "2026-10-13" {
		t.Fatalf("pump = %+v errors = %v", snap.Fuel.NCRPump, snap.Errors)
	}
	if len(snap.LPG) != 2 || snap.LPG[0].PeriodFrom != "2026-10-01" {
		t.Fatalf("lpg = %+v", snap.LPG)
	}
	if len(snap.Meralco) != 2 || snap.Meralco[0].Month != "2026-10" {
		t.Fatalf("meralco = %+v errors = %v", snap.Meralco, snap.Errors)
	}
	if snap.Water == nil || snap.Water.City != "Las Piñas" {
		t.Fatalf("water = %+v", snap.Water)
	}
	if len(snap.Water.Maintenance) != 1 {
		t.Fatalf("las pinas maintenance = %+v", snap.Water.Maintenance)
	}
	if len(snap.Errors) == 0 {
		t.Fatal("expected partial-source errors (missing pump PDFs)")
	}
	if len(snap.Sources) == 0 {
		t.Fatal("no sources recorded")
	}
	for _, u := range fs.uas {
		if u != "PresyoBantay-Test/0.1" {
			t.Fatalf("UA = %q", u)
		}
	}
	if snap.Empty() {
		t.Fatal("snapshot reports empty")
	}
}

func TestFetchCacheSkipsRedownload(t *testing.T) {
	srv, fs := newFixtureServer(t)
	cache := t.TempDir()
	if _, err := Fetch(context.Background(), fetchOpts(srv, fs, cache)); err != nil {
		t.Fatal(err)
	}
	first := pdfKeys(fs, false)
	if len(first) == 0 {
		t.Fatal("no PDF requests on first fetch")
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) == 0 {
		t.Fatalf("cache empty: %v", entries)
	}
	// Only failed (uncached) downloads may repeat.
	var missing []string
	for _, k := range first {
		if _, ok := fs.pdfs[k]; !ok {
			missing = append(missing, k)
		}
	}
	fs.requests = nil
	if _, err := Fetch(context.Background(), fetchOpts(srv, fs, cache)); err != nil {
		t.Fatal(err)
	}
	second := pdfKeys(fs, true)
	if len(second) != len(missing) {
		t.Fatalf("second fetch requested %v, want only missing %v", second, missing)
	}
	for _, k := range second {
		if _, ok := fs.pdfs[k]; ok {
			t.Fatalf("cached PDF re-downloaded: %q", k)
		}
	}
}

func TestFetchMinDelay(t *testing.T) {
	srv, fs := newFixtureServer(t)
	var sleeps []time.Duration
	opts := fetchOpts(srv, fs, t.TempDir())
	opts.MinDelay = 2 * time.Second
	opts.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	if _, err := Fetch(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(sleeps) == 0 {
		t.Fatal("no politeness sleeps")
	}
	for _, s := range sleeps {
		if s <= 0 || s > 2*time.Second {
			t.Fatalf("sleep = %v", s)
		}
	}
}

func TestFetchMaxRequests(t *testing.T) {
	srv, fs := newFixtureServer(t)
	opts := fetchOpts(srv, fs, t.TempDir())
	opts.MaxRequests = 3
	snap, err := Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.requests) != 3 {
		t.Fatalf("requests = %d: %v", len(fs.requests), fs.requests)
	}
	if len(snap.Errors) == 0 {
		t.Fatal("expected max-requests errors")
	}
}

func TestFetchPartialFailureContinues(t *testing.T) {
	// A server that 404s everything except the Meralco listing chain.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		serve := func(name string) {
			b, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				w.WriteHeader(404)
				return
			}
			w.Write(b)
		}
		switch {
		case p == "/news-and-advisories/latest-news-and-press-releases":
			serve("meralco_news_page.html")
		case strings.HasPrefix(p, "/news-and-advisories/"):
			if strings.Contains(p, "october-2026") {
				serve("meralco_2026_10.html")
			} else {
				w.WriteHeader(404)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	opts := fetchOpts(srv, nil, t.TempDir())
	if _, err := Fetch(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
}

func TestFetchNotFoundPageRecorded(t *testing.T) {
	// The DOE "Page Not Found" page is served with HTTP 200: it must be
	// recorded in Errors, never silently empty.
	srv, fs := newFixtureServer(t)
	fs.pages = map[string]string{
		"/data-and-prices/liquid-fuels/oil-monitor": "__notfound__",
	}
	snap, err := Fetch(context.Background(), fetchOpts(srv, fs, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Fuel.Adjustments) != 0 {
		t.Fatalf("adjustments = %+v", snap.Fuel.Adjustments)
	}
	found := false
	for _, e := range snap.Errors {
		if strings.Contains(e, "doe-oil-monitor") && strings.Contains(strings.ToLower(e), "page not found") {
			found = true
		}
	}
	if !found {
		t.Fatalf("not-found not recorded: %v", snap.Errors)
	}
	// Other sections still parse.
	if len(snap.Fuel.NCRPump) != 2 || len(snap.LPG) != 2 || len(snap.Meralco) != 2 {
		t.Fatalf("other sections missing: %+v", snap)
	}
}

func TestFetchEmptyListingRecorded(t *testing.T) {
	// HTTP 200 with zero links must be recorded, not silently empty.
	srv, fs := newFixtureServer(t)
	fs.pages = map[string]string{
		"/data-and-prices/lpg-monitor/ncr-lpg-prices": "__empty__",
	}
	snap, err := Fetch(context.Background(), fetchOpts(srv, fs, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.LPG) != 0 {
		t.Fatalf("lpg = %+v", snap.LPG)
	}
	found := false
	for _, e := range snap.Errors {
		if strings.Contains(e, "doe-ncr-lpg") && strings.Contains(e, "no links") {
			found = true
		}
	}
	if !found {
		t.Fatalf("empty listing not recorded: %v", snap.Errors)
	}
}

func TestFetchMeralcoSecondReleaseFails(t *testing.T) {
	// A broken second release must not halve the dataset: the third
	// listing entry is attempted.
	srv, fs := newFixtureServer(t)
	fs.pages = map[string]string{
		"/news-and-advisories/lower-rates-september-2026": "__404__",
		"/news-and-advisories/lower-rates-august-2026":    "__404__",
	}
	snap, err := Fetch(context.Background(), fetchOpts(srv, fs, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Meralco) != 1 || snap.Meralco[0].Month != "2026-10" {
		t.Fatalf("meralco = %+v", snap.Meralco)
	}
	releases := 0
	for _, r := range fs.requests {
		if strings.HasPrefix(r, "/news-and-advisories/lower-rates") || strings.Contains(r, "october-2026") && strings.HasPrefix(r, "/news-and-advisories/") {
			releases++
		}
	}
	if releases != 3 {
		t.Fatalf("release requests = %d, want 3: %v", releases, fs.requests)
	}
	if len(snap.Errors) == 0 {
		t.Fatal("expected release errors")
	}
}

func TestFetchSourceTimesAreRequestTimes(t *testing.T) {
	// Source.FetchedAt is the actual time of each request; Now only
	// sets Snapshot.FetchedAt (pinned far from the real clock here so
	// the two cannot coincide).
	srv, fs := newFixtureServer(t)
	opts := fetchOpts(srv, fs, t.TempDir())
	opts.Now = time.Date(2030, 5, 6, 7, 8, 9, 0, time.UTC)
	snap, err := Fetch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sources) == 0 {
		t.Fatal("no sources")
	}
	for _, src := range snap.Sources {
		if _, err := time.Parse(time.RFC3339, src.FetchedAt); err != nil {
			t.Fatalf("source %q FetchedAt %q: %v", src.Name, src.FetchedAt, err)
		}
	}
	if snap.Sources[0].FetchedAt == snap.FetchedAt {
		t.Fatalf("source time %q equals pinned snapshot time", snap.Sources[0].FetchedAt)
	}
}
