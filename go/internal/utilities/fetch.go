package utilities

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Manila offset (UTC+8, no DST) as a fixed zone so the static binary
// needs no system tzdata.
var manilaLoc = time.FixedZone("Asia/Manila", 8*3600)

// Default service bases (overridable for httptest).
const (
	DefaultDOEBase      = "https://doe.gov.ph"
	DefaultDOEMediaBase = "https://d24qbtp4vooyzi.cloudfront.net"
	DefaultMeralcoBase  = "https://company.meralco.com.ph"
	DefaultMayniladBase = "https://www.mayniladwater.com.ph"
)

// Listing page paths behind each base (real paths, verified live:
// the DOE "Page Not Found" page is served with HTTP 200, so callers
// must check page content, not just the status).
const (
	DOEOilMonitorPath = "/data-and-prices/liquid-fuels/oil-monitor"
	DOENCRPumpPath    = "/data-and-prices/liquid-fuels/retail-pump-prices/ncr-pump-prices"
	DOENCRLPGPath     = "/data-and-prices/lpg-monitor/ncr-lpg-prices"
	// DOEPriceAdjustmentsPath is the prior-notice listing (not fetched:
	// Fetch parses the Oil Monitor PDFs per UTILITIES_SPEC.md §1.8).
	DOEPriceAdjustmentsPath = "/data-and-prices/liquid-fuels/retail-pump-prices/price-adjustments"
	MeralcoNewsPath         = "/news-and-advisories/latest-news-and-press-releases"
)

// FetchOptions tunes Fetch.
type FetchOptions struct {
	Client *http.Client
	// CacheDir caches PDFs by sanitized file name; cached files are
	// not re-downloaded. Empty disables caching.
	CacheDir string
	// Now pins FetchedAt/CheckedAt (tests); zero means time.Now().
	Now time.Time
	// MinDelay spaces two requests to the same host (default 5s;
	// negative disables the delay, e.g. in tests).
	MinDelay time.Duration
	// MaxOilMonitors caps oil-monitor PDFs (default 6).
	MaxOilMonitors int
	// MaxRequests caps total HTTP requests (default 30).
	MaxRequests int
	// City is the Maynilad city (default "Las Piñas").
	City string
	// UserAgent is sent on every request.
	UserAgent string
	// DOEBase, DOEMediaBase, MeralcoBase, MayniladBase rewrite the
	// service bases (tests serve fixtures via httptest).
	DOEBase      string
	DOEMediaBase string
	MeralcoBase  string
	MayniladBase string
	// Sleep waits out politeness delays (default time.Sleep).
	Sleep func(time.Duration)
}

// fetcher carries Fetch's running state.
type fetcher struct {
	client   *http.Client
	cacheDir string
	now      time.Time
	minDelay time.Duration
	maxOil   int
	maxReq   int
	city     string
	ua       string
	doeBase  string
	media    string
	meralco  string
	maynilad string
	sleep    func(time.Duration)

	lastHost map[string]time.Time
	requests int
	stopped  bool // MaxRequests reached: no further requests

	snap *Snapshot
}

// errMaxRequests is the sentinel for the request cap.
var errMaxRequests = fmt.Errorf("max requests reached")

// Fetch builds a Snapshot from the official public sources. One
// attempt per URL (no retries); a failure in one source records an
// Error and the rest continues. Partial snapshots are OK.
func Fetch(ctx context.Context, opts FetchOptions) (*Snapshot, error) {
	f := &fetcher{
		client:   opts.Client,
		cacheDir: opts.CacheDir,
		now:      opts.Now,
		minDelay: opts.MinDelay,
		maxOil:   opts.MaxOilMonitors,
		maxReq:   opts.MaxRequests,
		city:     opts.City,
		ua:       opts.UserAgent,
		doeBase:  opts.DOEBase,
		media:    opts.DOEMediaBase,
		meralco:  opts.MeralcoBase,
		maynilad: opts.MayniladBase,
		sleep:    opts.Sleep,
		lastHost: map[string]time.Time{},
		snap: &Snapshot{
			Sources: []Source{},
			Errors:  []string{},
		},
	}
	if f.client == nil {
		f.client = &http.Client{Timeout: 60 * time.Second}
	}
	if f.now.IsZero() {
		f.now = time.Now()
	}
	switch {
	case opts.MinDelay == 0:
		f.minDelay = 5 * time.Second
	case opts.MinDelay < 0:
		f.minDelay = 0
	}
	if f.maxOil <= 0 {
		f.maxOil = 6
	}
	if f.maxReq <= 0 {
		f.maxReq = 30
	}
	if f.city == "" {
		f.city = "Las Piñas"
	}
	if f.doeBase == "" {
		f.doeBase = DefaultDOEBase
	}
	if f.media == "" {
		f.media = DefaultDOEMediaBase
	}
	if f.meralco == "" {
		f.meralco = DefaultMeralcoBase
	}
	if f.maynilad == "" {
		f.maynilad = DefaultMayniladBase
	}
	if f.sleep == nil {
		f.sleep = time.Sleep
	}
	f.snap.FetchedAt = f.now.In(manilaLoc).Format(time.RFC3339)

	if err := ctx.Err(); err != nil {
		return f.snap, err
	}
	f.fetchFuel(ctx)
	f.fetchLPG(ctx)
	f.fetchMeralco(ctx)
	f.fetchWater(ctx)
	return f.snap, nil
}

// fail records a per-source failure; partial snapshots are OK.
func (f *fetcher) fail(format string, args ...any) {
	f.snap.Errors = append(f.snap.Errors, fmt.Sprintf(format, args...))
}

// source records one fetch attempt at its actual request time.
func (f *fetcher) source(name, rawURL string, status, n int, sum []byte, at time.Time, err error) {
	s := Source{
		Name:       name,
		URL:        rawURL,
		FetchedAt:  at.In(manilaLoc).Format(time.RFC3339),
		HTTPStatus: status,
		Bytes:      n,
	}
	if len(sum) > 0 {
		s.SHA256 = hex.EncodeToString(sum)
	}
	if err != nil {
		s.Err = err.Error()
	}
	f.snap.Sources = append(f.snap.Sources, s)
}

// get performs one polite GET (no retries). It honours MinDelay per
// host and the MaxRequests cap, returning the actual request time.
func (f *fetcher) get(ctx context.Context, rawURL string) ([]byte, int, time.Time, error) {
	if f.stopped {
		return nil, 0, time.Time{}, errMaxRequests
	}
	if f.requests >= f.maxReq {
		f.stopped = true
		return nil, 0, time.Time{}, errMaxRequests
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	if f.minDelay > 0 {
		if last, ok := f.lastHost[u.Host]; ok {
			if wait := f.minDelay - time.Since(last); wait > 0 {
				f.sleep(wait)
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	if f.ua != "" {
		req.Header.Set("User-Agent", f.ua)
	}
	f.requests++
	at := time.Now()
	f.lastHost[u.Host] = at
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, at, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, resp.StatusCode, at, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, at, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, resp.StatusCode, at, nil
}

// getPage fetches a listing page, recording its source entry.
func (f *fetcher) getPage(ctx context.Context, name, rawURL string) ([]byte, bool) {
	body, status, at, err := f.get(ctx, rawURL)
	f.source(name, rawURL, status, len(body), sha256Sum(body), at, err)
	if err != nil {
		if err == errMaxRequests {
			f.fail("%s: %v", name, err)
		} else {
			f.fail("%s %s: %v", name, rawURL, err)
		}
		return nil, false
	}
	return body, true
}

// sanitizeName maps a URL to a flat cache file name.
func sanitizeName(rawURL string) string {
	u, err := url.Parse(rawURL)
	name := ""
	if err == nil {
		name = u.Path
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	if name == "" {
		name = "download"
	}
	var b strings.Builder
	for _, r := range name {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// sha256Sum returns the SHA256 of b.
func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// shaFile returns the SHA256 of a file.
func shaFile(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(data)
	return sum[:]
}
