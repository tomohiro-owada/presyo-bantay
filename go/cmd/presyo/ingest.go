package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/listing"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/parse"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/pyjson"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// Ingester is the testable daily-ingest driver (see SPEC "ingest").
// Zero values are usable except the paths; tests override ListingURL,
// Delay, Sleep, Jitter and Client (httptest).
type Ingester struct {
	ListingURL string
	DBPath     string
	RawDir     string
	JSONDir    string
	Max        int
	Lookback   int
	DryRun     bool

	ListingFile string
	PDFFile     string

	// Politeness knobs.
	Delay  time.Duration
	Sleep  func(time.Duration)
	Jitter func() float64
	Client *http.Client

	Stdout io.Writer
	Stderr io.Writer

	requests int
}

// MaxAttempts mirrors the per-file retry budget.
const MaxAttempts = 3

// BackoffBase mirrors backfill.BACKOFF_BASE.
const BackoffBase = 60 * time.Second

// MaxRetryAfter mirrors backfill.MAX_RETRY_AFTER.
const MaxRetryAfter = 600 * time.Second

func newIngesterFromFlags(argv []string) (*Ingester, int) {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	g := &Ingester{
		ListingURL: listing.ListingURL,
		Delay:      10 * time.Second,
		Sleep:      time.Sleep,
		Jitter:     rand.Float64,
		Client:     &http.Client{Timeout: 30 * time.Second},
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Max:        3,
		Lookback:   7,
	}
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path")
	rawDir := fs.String("raw", filepath.Join("data", "raw"), "raw PDF directory")
	jsonDir := fs.String("json-dir", filepath.Join("data", "json"), "JSON snapshot directory")
	max := fs.Int("max", 3, "download at most N PDFs per run")
	lookback := fs.Int("lookback", 7, "also fill missing dates within the last N days")
	dryRun := fs.Bool("dry-run", false, "list what would be downloaded; no PDF requests")
	listingFile := fs.String("listing-file", "", "use a local listing HTML file instead of the network")
	pdfFile := fs.String("pdf", "", "ingest one local PDF file (no network)")
	// Every flag is read AFTER Parse: dereferencing before would silently
	// ignore user-supplied values. Parse errors are a usage failure (exit
	// 2), never ignored.
	if err := fs.Parse(argv); err != nil {
		return nil, 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "usage: presyo ingest [-db PATH] [-raw DIR] [-json-dir DIR] [-max N] [-lookback N] [-dry-run] [-listing-file FILE] [-pdf FILE]\n")
		return nil, 2
	}
	g.DBPath = *dbPath
	g.RawDir, g.JSONDir = *rawDir, *jsonDir
	g.Max, g.Lookback = *max, *lookback
	g.DryRun = *dryRun
	g.ListingFile, g.PDFFile = *listingFile, *pdfFile
	return g, 0
}

func (g *Ingester) printf(format string, args ...any) {
	fmt.Fprintf(g.Stdout, format, args...)
}

func (g *Ingester) warnf(format string, args ...any) {
	fmt.Fprintf(g.Stderr, format, args...)
}

// politeWait sleeps Delay + jitter*5s before every request but the first.
func (g *Ingester) politeWait() {
	if g.requests > 0 {
		j := 0.0
		if g.Jitter != nil {
			j = g.Jitter()
		}
		g.Sleep(g.Delay + time.Duration(j*float64(5*time.Second)))
	}
}

func (g *Ingester) get(url string) (*http.Response, error) {
	g.politeWait()
	g.requests++
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	return g.Client.Do(req)
}

// retryAfterSeconds parses a Retry-After header (seconds), capped.
func retryAfterSeconds(resp *http.Response) (time.Duration, bool) {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	var secs float64
	if _, err := fmt.Sscanf(raw, "%g", &secs); err != nil || secs < 0 {
		return 0, false
	}
	d := time.Duration(secs * float64(time.Second))
	if d > MaxRetryAfter {
		d = MaxRetryAfter
	}
	return d, true
}

// download fetches url with retries; returns (content, failureReason).
// Empty reason means success.
func (g *Ingester) download(url string) ([]byte, string) {
	backoff := BackoffBase
	var errors int
	for attempt := 0; ; attempt++ {
		resp, err := g.get(url)
		if err != nil {
			errors++
			if errors >= MaxAttempts {
				return nil, fmt.Sprintf("network error: %s", err.Error())
			}
			g.Sleep(backoff)
			backoff *= 2
			continue
		}
		if resp.StatusCode == 404 {
			resp.Body.Close()
			return nil, "HTTP 404"
		}
		if resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode < 600) {
			status := resp.StatusCode
			wait, ok := retryAfterSeconds(resp)
			resp.Body.Close()
			errors++
			if errors >= MaxAttempts {
				return nil, fmt.Sprintf("HTTP %d (gave up after %d tries)", status, errors)
			}
			if !ok {
				wait = backoff
				backoff *= 2
			}
			g.Sleep(wait)
			continue
		}
		if resp.StatusCode != 200 {
			status := resp.StatusCode
			resp.Body.Close()
			return nil, fmt.Sprintf("HTTP %d", status)
		}
		buf, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			errors++
			if errors >= MaxAttempts {
				return nil, fmt.Sprintf("network error: %s", err.Error())
			}
			g.Sleep(backoff)
			backoff *= 2
			continue
		}
		return buf, ""
	}
}

// Run executes the daily ingest; returns the process exit code.
func (g *Ingester) Run() int {
	if g.PDFFile != "" {
		return g.ingestLocalPDF(g.PDFFile)
	}
	var htmlText string
	if g.ListingFile != "" {
		data, err := os.ReadFile(g.ListingFile)
		if err != nil {
			g.warnf("error: %s\n", err.Error())
			return 1
		}
		htmlText = string(data)
	} else {
		resp, err := g.get(g.ListingURL)
		if err != nil {
			g.warnf("error: %s\n", err.Error())
			return 1
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			g.warnf("error: %s\n", err.Error())
			return 1
		}
		if resp.StatusCode != 200 {
			g.warnf("error: listing HTTP %d\n", resp.StatusCode)
			return 1
		}
		htmlText = string(body)
	}

	found := listing.FindAllFromHTML(htmlText, g.ListingURL)

	db, err := store.Connect(g.DBPath)
	if err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	dates, err := store.ListDates(db)
	if err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1
	}
	existing := map[string]bool{}
	maxDate := ""
	for _, d := range dates {
		existing[d] = true
		if d > maxDate {
			maxDate = d
		}
	}
	newest := ""
	for _, l := range found {
		if d := l.Date.ISODate(); d > newest {
			newest = d
		}
	}
	cutoff := ""
	if newest != "" {
		if t, err := time.Parse("2006-01-02", newest); err == nil {
			cutoff = t.AddDate(0, 0, -g.Lookback).Format("2006-01-02")
		}
	}
	var targets []listing.PDFLink
	for _, l := range found {
		d := l.Date.ISODate()
		newer := maxDate == "" || d > maxDate
		missing := !existing[d] && cutoff != "" && d >= cutoff
		if newer || missing {
			targets = append(targets, l)
		}
	}
	if g.Max > 0 && len(targets) > g.Max {
		targets = targets[:g.Max]
	}

	if g.DryRun {
		for _, t := range targets {
			g.printf("would download %s (%s)\n", targetFilename(t.URL), t.URL)
		}
		g.printf("ingest: %d candidate(s), dry run\n", len(targets))
		return 0
	}

	failures := 0
	downloaded, skipped := 0, 0
	var ingestedDates []string
	seenDate := map[string]bool{}
	recordDate := func(date string) {
		if date != "" && !seenDate[date] {
			seenDate[date] = true
			ingestedDates = append(ingestedDates, date)
		}
	}
	for i, t := range targets {
		filename := targetFilename(t.URL)
		tag := fmt.Sprintf("[%d/%d] %s", i+1, len(targets), filename)
		dest := filepath.Join(g.RawDir, filename)
		var content []byte
		if _, err := os.Stat(dest); err == nil {
			skipped++
			g.printf("%s: skipped (already cached)\n", tag)
			// A cached PDF that fails parse/checks counts as a failure
			// but never blocks the remaining (newer or older) dates.
			if code, date := g.ingestCached(dest, t.URL); code != 0 {
				failures++
			} else {
				recordDate(date)
			}
			continue
		}
		content, reason := g.download(t.URL)
		if reason != "" {
			g.warnf("%s: failed (%s)\n", tag, reason)
			failures++
			continue
		}
		downloaded++
		if err := os.MkdirAll(g.RawDir, 0o755); err != nil {
			g.warnf("%s: failed (%s)\n", tag, err.Error())
			failures++
			continue
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			g.warnf("%s: failed (%s)\n", tag, err.Error())
			failures++
			continue
		}
		meta := pyjson.NewObj("source_url", t.URL, "filename", filename)
		if err := os.WriteFile(dest+".meta.json", pyjson.MarshalIndent(meta), 0o644); err != nil {
			g.warnf("%s: warning: %s\n", tag, err.Error())
		}
		if code, date := g.parseAndStore(dest, t.URL); code != 0 {
			g.warnf("%s: failed (parse/store exit %d)\n", tag, code)
			failures++
			continue
		} else {
			recordDate(date)
		}
		g.printf("%s: ok\n", tag)
	}
	datesStr := strings.Join(ingestedDates, ", ")
	if datesStr == "" {
		datesStr = "none"
	}
	g.printf("ingest: %d candidate(s), %d downloaded, %d cached, %d failed, dates: %s\n",
		len(targets), downloaded, skipped, failures, datesStr)
	if failures > 0 {
		return 1
	}
	return 0
}

// ingestCached parses + stores an already-cached PDF (no download).
// Returns (exit code, ingested date or "").
func (g *Ingester) ingestCached(dest, url string) (int, string) {
	return g.parseAndStore(dest, url)
}

// ingestLocalPDF ingests one local PDF file (no network).
func (g *Ingester) ingestLocalPDF(pdfPath string) int {
	if _, err := os.Stat(pdfPath); err != nil {
		g.warnf("error: file not found: %s\n", pdfPath)
		return 1
	}
	code, _ := g.parseAndStore(pdfPath, "")
	return code
}

// parseAndStore parses pdfPath with backfill-style checks, writes the JSON
// snapshot and ingests it. Returns (0 ok, 1 error, 2 layout change) plus
// the ingested report date ("" on failure).
func (g *Ingester) parseAndStore(pdfPath, url string) (int, string) {
	markets, textHint, layout, marketOrder, commodityOrder, err :=
		parse.ParsePDFOrdered(pdfPath)
	if err != nil {
		if _, ok := err.(*parse.LayoutChangedError); ok {
			g.warnf("error: %s\n", err.Error())
			return 2, ""
		}
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	warnings, err := parse.RunBackfillChecks(markets, layout)
	if err != nil {
		g.warnf("error: %s\n", err.Error())
		return 2, ""
	}
	for _, w := range warnings {
		g.printf("warning: %s\n", w)
	}
	date := parse.ParseDateFromFilename(pdfPath)
	if date == nil {
		date = textHint
	}
	if date == nil {
		g.warnf("error: Layout may have changed: could not determine report date from filename or PDF text\n")
		return 2, ""
	}
	src := url
	var srcPtr *string
	if src != "" {
		srcPtr = &src
	} else {
		srcPtr = readMetaSourceURL(g.RawDir, pdfPath)
	}
	if srcPtr != nil {
		src = *srcPtr
	}
	var sourceURL *string
	if src != "" {
		sourceURL = &src
	}
	payload := snapshotJSON(date.String(), sourceURL, layout, markets, marketOrder, commodityOrder)
	if err := os.MkdirAll(g.JSONDir, 0o755); err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	outPath := filepath.Join(g.JSONDir, date.String()+".json")
	if err := os.WriteFile(outPath, pyjson.MarshalIndent(payload), 0o644); err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	db, err := store.Connect(g.DBPath)
	if err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	defer db.Close()
	data, err := os.ReadFile(outPath)
	if err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	p, err := store.DecodePayload(data)
	if err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	if _, _, _, err := store.IngestPayload(db, p, false); err != nil {
		g.warnf("error: %s\n", err.Error())
		return 1, ""
	}
	g.printf("wrote %s\n", outPath)
	return 0, date.String()
}

func targetFilename(rawURL string) string {
	name := rawURL
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.Index(name, "?"); i >= 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "#"); i >= 0 {
		name = name[:i]
	}
	return name
}
