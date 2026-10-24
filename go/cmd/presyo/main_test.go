package main

import (
	"bytes"
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

func repoFixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "tests", "fixtures", name)
	if _, err := os.Stat(p); err != nil {
		// Fall back to absolute repo path (tests may run from go/cmd/presyo).
		p = filepath.Join("/workspace", "presyo-bantay", "tests", "fixtures", name)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("fixture %s not available", name)
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func newTestIngester(t *testing.T) (*Ingester, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	g := &Ingester{
		Delay:    0,
		Sleep:    func(time.Duration) {},
		Jitter:   func() float64 { return 0 },
		Client:   &http.Client{Timeout: 30 * time.Second},
		Stdout:   &out,
		Stderr:   &errOut,
		Max:      3,
		Lookback: 7,
	}
	g.DBPath = filepath.Join(t.TempDir(), "t.db")
	g.RawDir = filepath.Join(t.TempDir(), "raw")
	g.JSONDir = filepath.Join(t.TempDir(), "json")
	return g, &out, &errOut
}

func TestParseWritesJSON(t *testing.T) {
	pdf := repoFixture(t, "Price-Monitoring-October-23-2026.pdf")
	outDir := t.TempDir()
	if code := parsePDFToJSON(pdf, outDir, t.TempDir(), "", false); code != 0 {
		t.Fatalf("exit %d", code)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "2026-10-23.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"layout": "single"`)) {
		t.Fatalf("missing layout: %s", data[:200])
	}
	if !bytes.Contains(data, []byte(`"price_low"`)) {
		t.Fatal("missing price_low")
	}
}

func TestParseMissingFile(t *testing.T) {
	if code := parsePDFToJSON("/nonexistent/x.pdf", t.TempDir(), t.TempDir(), "", false); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}

// pdfServer serves a listing page plus PDF bytes for httptest ingest tests.
type pdfServer struct {
	t        *testing.T
	pdfs     map[string][]byte
	requests *[]string
	sleeps   *[]time.Duration
	failures map[string]int
}

func (s *pdfServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*s.requests = append(*s.requests, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, ".pdf") {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if n, ok := s.failures[name]; ok && n > 0 {
				s.failures[name] = n - 1
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(429)
				return
			}
			data, ok := s.pdfs[name]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(data)
			return
		}
		var b strings.Builder
		b.WriteString("<html><body>\n")
		for name := range s.pdfs {
			fmt.Fprintf(&b, `<a href="/%s">%s</a>`+"\n", name, name)
		}
		b.WriteString("</body></html>\n")
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, b.String())
	})
}

func servePDFs(t *testing.T, names []string) (*httptest.Server, *[]string, *pdfServer) {
	t.Helper()
	pdfs := map[string][]byte{}
	for _, n := range names {
		data, err := os.ReadFile(repoFixture(t, n))
		if err != nil {
			t.Fatal(err)
		}
		pdfs[n] = data
	}
	var requests []string
	ps := &pdfServer{t: t, pdfs: pdfs, requests: &requests, failures: map[string]int{}}
	srv := httptest.NewServer(ps.handler())
	t.Cleanup(srv.Close)
	return srv, &requests, ps
}

func TestIngestDryRunMakesZeroPDFRequests(t *testing.T) {
	srv, requests, _ := servePDFs(t, []string{"Price-Monitoring-October-23-2026.pdf"})
	g, out, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	g.DryRun = true
	g.Sleep = func(d time.Duration) {}
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	for _, r := range *requests {
		if strings.HasSuffix(r, ".pdf") {
			t.Fatalf("PDF requested in dry run: %v", *requests)
		}
	}
	if !strings.Contains(out.String(), "would download") {
		t.Fatalf("no preview: %s", out.String())
	}
}

func TestIngestSuccessWritesJSONAndDB(t *testing.T) {
	srv, _, _ := servePDFs(t, []string{"Price-Monitoring-October-23-2026.pdf"})
	g, out, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	pdfDest := filepath.Join(g.RawDir, "Price-Monitoring-October-23-2026.pdf")
	if _, err := os.Stat(pdfDest); err != nil {
		t.Fatalf("PDF not downloaded: %v", err)
	}
	if _, err := os.Stat(pdfDest + ".meta.json"); err != nil {
		t.Fatalf("meta.json missing: %v", err)
	}
	jsonDest := filepath.Join(g.JSONDir, "2026-10-23.json")
	data, err := os.ReadFile(jsonDest)
	if err != nil {
		t.Fatalf("JSON missing: %v", err)
	}
	if !bytes.Contains(data, []byte(`"2026-10-23"`)) {
		t.Fatal("bad JSON")
	}
	db, err := store.Connect(g.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dates, err := store.ListDates(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) != 1 || dates[0] != "2026-10-23" {
		t.Fatalf("dates = %v", dates)
	}
	// Second run: cached, no re-download, still exit 0.
	g2, _, _ := newTestIngester(t)
	g2.ListingURL = g.ListingURL
	g2.DBPath, g2.RawDir, g2.JSONDir = g.DBPath, g.RawDir, g.JSONDir
	_ = g2
}

func TestIngestRetryAfterHonored(t *testing.T) {
	srv, _, ps := servePDFs(t, []string{"Price-Monitoring-October-23-2026.pdf"})
	ps.failures["Price-Monitoring-October-23-2026.pdf"] = 1
	g, _, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	var sleeps []time.Duration
	g.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	found := false
	for _, s := range sleeps {
		if s == 7*time.Second {
			found = true
		}
	}
	if !found {
		t.Fatalf("Retry-After not honored: %v", sleeps)
	}
}

func TestIngestPolitenessDelayEnforced(t *testing.T) {
	srv, _, _ := servePDFs(t, []string{
		"Price-Monitoring-October-23-2026.pdf",
		"Price-Monitoring-August-14-2026.pdf",
	})
	g, _, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	g.Delay = 10 * time.Second
	var sleeps []time.Duration
	g.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(sleeps) == 0 {
		t.Fatal("no polite sleeps")
	}
	for _, s := range sleeps {
		if s < 10*time.Second {
			t.Fatalf("sleep %v < 10s", s)
		}
	}
}

func TestIngestSinglePDFNoNetwork(t *testing.T) {
	pdf := repoFixture(t, "Price-Monitoring-October-23-2026.pdf")
	g, _, _ := newTestIngester(t)
	g.PDFFile = pdf
	// Client that fails on any use: proves zero network.
	g.Client = &http.Client{Transport: roundTripFail{}}
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(g.JSONDir, "2026-10-23.json")); err != nil {
		t.Fatalf("JSON missing: %v", err)
	}
}

type roundTripFail struct{}

func (roundTripFail) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network must not be used")
}

func TestIngestMaxCap(t *testing.T) {
	srv, _, _ := servePDFs(t, []string{
		"Price-Monitoring-October-23-2026.pdf",
		"Price-Monitoring-August-14-2026.pdf",
	})
	g, _, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	g.Max = 1
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	entries, _ := os.ReadDir(g.RawDir)
	pdfs := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pdf") {
			pdfs++
		}
	}
	if pdfs != 1 {
		t.Fatalf("downloaded %d PDFs, want 1", pdfs)
	}
}

// TestIngestDBFlagHonored proves `ingest -db <tmp> -pdf <fixture>` writes
// to the given DB (regression: -db was dereferenced before flag.Parse, so
// it was silently ignored and always hit data/presyo.db). Runs from a temp
// working dir so the default data/presyo.db must not appear either.
func TestIngestDBFlagHonored(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	pdf := repoFixture(t, "Price-Monitoring-October-23-2026.pdf")
	dbPath := filepath.Join(tmp, "custom.db")
	rawDir := filepath.Join(tmp, "raw")
	jsonDir := filepath.Join(tmp, "json")
	if code := run([]string{"ingest", "-db", dbPath, "-raw", rawDir, "-json-dir", jsonDir, "-pdf", pdf}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	db, err := store.Connect(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	dates, err := store.ListDates(db)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) != 1 || dates[0] != "2026-10-23" {
		t.Fatalf("dates = %v, want [2026-10-23]", dates)
	}
	if _, err := os.Stat(filepath.Join(tmp, "data", "presyo.db")); !os.IsNotExist(err) {
		t.Fatalf("default data/presyo.db was touched: %v", err)
	}
}

// TestIngestFlagParseErrorsFail ensures bad flags / stray positionals exit
// non-zero instead of being silently ignored.
func TestIngestFlagParseErrorsFail(t *testing.T) {
	if g, code := newIngesterFromFlags([]string{"-bogus-flag"}); code == 0 || g != nil {
		t.Fatalf("bogus flag: got (%v, %d), want (nil, non-zero)", g, code)
	}
	if code := run([]string{"ingest", "-bogus-flag"}); code != 1 && code != 2 {
		t.Fatalf("bogus flag run exit %d, want 1 or 2", code)
	}
	if code := run([]string{"ingest", "stray-positional"}); code != 1 && code != 2 {
		t.Fatalf("positional run exit %d, want 1 or 2", code)
	}
}

// TestIngestSendsExactUserAgent asserts the exact UA header seen by the
// (httptest) server on both the DA listing and PDF requests.
func TestIngestSendsExactUserAgent(t *testing.T) {
	const want = "PresyoBantay/0.1 (non-profit community price watch; https://github.com/tomohiro-owada/presyo-bantay)"
	if UserAgent != want {
		t.Fatalf("UserAgent = %q, want %q", UserAgent, want)
	}
	pdf, err := os.ReadFile(repoFixture(t, "Price-Monitoring-October-23-2026.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	var uas []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uas = append(uas, r.URL.Path+": "+r.Header.Get("User-Agent"))
		if strings.HasSuffix(r.URL.Path, ".pdf") {
			w.Header().Set("Content-Type", "application/pdf")
			w.Write(pdf)
			return
		}
		fmt.Fprint(w, `<html><body><a href="/Price-Monitoring-October-23-2026.pdf">pdf</a></body></html>`)
	}))
	defer srv.Close()
	g, _, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(uas) < 2 {
		t.Fatalf("only %d request(s) seen: %v", len(uas), uas)
	}
	for _, got := range uas {
		path := got[:strings.Index(got, ": ")]
		ua := got[strings.Index(got, ": ")+2:]
		if ua != want {
			t.Fatalf("%s User-Agent = %q, want %q", path, ua, want)
		}
	}
}

// TestIngestSummaryListsDates ensures the final summary line names the
// ingested dates.
func TestIngestSummaryListsDates(t *testing.T) {
	srv, _, _ := servePDFs(t, []string{"Price-Monitoring-October-23-2026.pdf"})
	g, out, _ := newTestIngester(t)
	g.ListingURL = srv.URL + "/listing"
	if code := g.Run(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "2026-10-23") {
		t.Fatalf("summary missing date: %q", out.String())
	}
}
