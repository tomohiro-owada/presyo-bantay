package utilities

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Snapshot assembly: the per-source fetch stages behind Fetch.
// PDFs go through the CacheDir (cached files are not re-downloaded).

// rewriteHost swaps a default service host for its configured base
// (tests point the cloudfront/meralco hosts at httptest).
func (f *fetcher) rewriteHost(rawURL, defBase, base string) string {
	if base == defBase {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	def, err := url.Parse(defBase)
	if err != nil {
		return rawURL
	}
	if !strings.EqualFold(u.Host, def.Host) {
		return rawURL
	}
	nb, err := url.Parse(base)
	if err != nil {
		return rawURL
	}
	u.Scheme = nb.Scheme
	u.Host = nb.Host
	return u.String()
}

// fetchPDF downloads one PDF (or reads it from the cache) and records
// its source entry. It returns the local path.
func (f *fetcher) fetchPDF(ctx context.Context, name, rawURL string) (string, bool) {
	if f.cacheDir != "" {
		if err := os.MkdirAll(f.cacheDir, 0o755); err != nil {
			f.fail("%s: cache dir: %v", name, err)
			return "", false
		}
		cached := filepath.Join(f.cacheDir, sanitizeName(rawURL))
		if fi, err := os.Stat(cached); err == nil && fi.Size() > 0 {
			f.source(name, rawURL, 200, int(fi.Size()), shaFile(cached), time.Now(), nil)
			return cached, true
		}
		body, status, at, err := f.get(ctx, rawURL)
		sum := sha256Sum(body)
		f.source(name, rawURL, status, len(body), sum, at, err)
		if err != nil {
			f.fail("%s %s: %v", name, rawURL, err)
			return "", false
		}
		if err := os.WriteFile(cached, body, 0o644); err != nil {
			f.fail("%s: cache write: %v", name, err)
			return "", false
		}
		return cached, true
	}
	body, status, at, err := f.get(ctx, rawURL)
	sum := sha256Sum(body)
	f.source(name, rawURL, status, len(body), sum, at, err)
	if err != nil {
		f.fail("%s %s: %v", name, rawURL, err)
		return "", false
	}
	tmp, err := os.CreateTemp("", "utilities-*.pdf")
	if err != nil {
		f.fail("%s: temp file: %v", name, err)
		return "", false
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		f.fail("%s: temp write: %v", name, err)
		return "", false
	}
	tmp.Close()
	return tmpName, true
}

// pageTitleRe extracts the HTML title for not-found detection.
var pageTitleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// isNotFoundPage reports whether a 200-OK page body is actually a
// "Page Not Found" page (the DOE site serves those with HTTP 200).
func isNotFoundPage(page []byte) bool {
	m := pageTitleRe.FindSubmatch(page)
	if m == nil {
		return false
	}
	title := strings.ToLower(spaceRe.ReplaceAllString(string(m[1]), " "))
	return strings.Contains(title, "page not found")
}

// checkListing validates a fetched listing page: not-found content or
// zero extracted links are recorded, never silently empty.
func (f *fetcher) checkListing(name, pageURL string, page []byte, nLinks int) bool {
	if isNotFoundPage(page) {
		f.fail("%s %s: page not found (served with HTTP 200)", name, pageURL)
		return false
	}
	if nLinks == 0 {
		f.fail("%s %s: no links found", name, pageURL)
		return false
	}
	return true
}

// fetchFuel runs Fetch step 1: oil monitors (section "fuel").
func (f *fetcher) fetchFuel(ctx context.Context) {
	// 1. DOE oil-monitor page → newest MaxOilMonitors PDFs.
	if page, ok := f.getPage(ctx, "doe-oil-monitor", f.doeBase+DOEOilMonitorPath); ok {
		atts := ParseAttachments(page, f.doeBase)
		if !f.checkListing("doe-oil-monitor", f.doeBase+DOEOilMonitorPath, page, len(atts)) {
			atts = nil
		}
		if len(atts) > f.maxOil {
			atts = atts[:f.maxOil]
		}
		for _, a := range atts {
			pdfURL := f.rewriteHost(a.URL, DefaultDOEMediaBase, f.media)
			path, ok := f.fetchPDF(ctx, "doe-oil-monitor-pdf", pdfURL)
			if !ok {
				continue
			}
			lines, err := PDFLines(path)
			if err != nil {
				f.fail("doe-oil-monitor-pdf %s: %v", pdfURL, err)
				continue
			}
			adj, err := ParseOilMonitor(lines)
			if err != nil {
				f.fail("doe-oil-monitor-pdf %s: %v", pdfURL, err)
				continue
			}
			adj.SourceURL = pdfURL
			f.snap.Fuel.Adjustments = append(f.snap.Fuel.Adjustments, adj)
		}
		sort.Slice(f.snap.Fuel.Adjustments, func(i, j int) bool {
			return f.snap.Fuel.Adjustments[i].EffectiveFrom > f.snap.Fuel.Adjustments[j].EffectiveFrom
		})
		if len(f.snap.Fuel.Adjustments) == 0 {
			f.fail("doe-oil-monitor: no adjustments parsed")
		} else {
			f.stampSection("fuel")
		}
	}
}

// fetchPump runs Fetch step 2: NCR pump (section "pump").
func (f *fetcher) fetchPump(ctx context.Context) {
	// 2. DOE NCR pump page → up to the 4 first-listed PDFs, newest 2.
	if page, ok := f.getPage(ctx, "doe-ncr-pump", f.doeBase+DOENCRPumpPath); ok {
		atts := ParseAttachments(page, f.doeBase)
		if !f.checkListing("doe-ncr-pump", f.doeBase+DOENCRPumpPath, page, len(atts)) {
			atts = nil
		}
		if len(atts) > 4 {
			atts = atts[:4]
		}
		var sums []PumpSummary
		for _, a := range atts {
			pdfURL := f.rewriteHost(a.URL, DefaultDOEMediaBase, f.media)
			path, ok := f.fetchPDF(ctx, "doe-ncr-pump-pdf", pdfURL)
			if !ok {
				continue
			}
			lines, err := PDFLines(path)
			if err != nil {
				f.fail("doe-ncr-pump-pdf %s: %v", pdfURL, err)
				continue
			}
			sum, err := ParseNCRPumpSummary(lines)
			if err != nil {
				f.fail("doe-ncr-pump-pdf %s: %v", pdfURL, err)
				continue
			}
			sum.SourceURL = pdfURL
			sums = append(sums, sum)
		}
		sort.Slice(sums, func(i, j int) bool { return sums[i].WeekFrom > sums[j].WeekFrom })
		if len(sums) > 2 {
			sums = sums[:2]
		}
		f.snap.Fuel.NCRPump = sums
		if len(sums) == 0 {
			f.fail("doe-ncr-pump: no pump summaries parsed")
		} else {
			f.stampSection("pump")
		}
	}
}

// fetchLPG runs Fetch step 3 (NCR LPG, 2 newest by PeriodFrom).
func (f *fetcher) fetchLPG(ctx context.Context) {
	page, ok := f.getPage(ctx, "doe-ncr-lpg", f.doeBase+DOENCRLPGPath)
	if !ok {
		return
	}
	atts := ParseAttachments(page, f.doeBase)
	if !f.checkListing("doe-ncr-lpg", f.doeBase+DOENCRLPGPath, page, len(atts)) {
		f.fail("doe-ncr-lpg: no LPG months parsed")
		return
	}
	if len(atts) > 4 {
		atts = atts[:4]
	}
	var months []LPGMonth
	for _, a := range atts {
		pdfURL := f.rewriteHost(a.URL, DefaultDOEMediaBase, f.media)
		path, ok := f.fetchPDF(ctx, "doe-ncr-lpg-pdf", pdfURL)
		if !ok {
			continue
		}
		lines, err := PDFLines(path)
		if err != nil {
			f.fail("doe-ncr-lpg-pdf %s: %v", pdfURL, err)
			continue
		}
		month, err := ParseNCRLPG(lines)
		if err != nil {
			f.fail("doe-ncr-lpg-pdf %s: %v", pdfURL, err)
			continue
		}
		month.SourceURL = pdfURL
		months = append(months, month)
	}
	sort.Slice(months, func(i, j int) bool { return months[i].PeriodFrom > months[j].PeriodFrom })
	if len(months) > 2 {
		months = months[:2]
	}
	f.snap.LPG = months
	if len(months) == 0 {
		f.fail("doe-ncr-lpg: no LPG months parsed")
	} else {
		f.stampSection("lpg")
	}
}

// fetchMeralco runs Fetch step 4 (newest 2 releases).
func (f *fetcher) fetchMeralco(ctx context.Context) {
	page, ok := f.getPage(ctx, "meralco-news", f.meralco+MeralcoNewsPath)
	if !ok {
		return
	}
	urls := ParseMeralcoListing(page)
	if !f.checkListing("meralco-news", f.meralco+MeralcoNewsPath, page, len(urls)) {
		f.fail("meralco: no releases parsed")
		return
	}
	// Walk the listing (newest first) until 2 releases parse, trying
	// at most 3 so one bad release page cannot halve the dataset.
	attempts := 0
	for _, u := range urls {
		if len(f.snap.Meralco) >= 2 || attempts >= 3 {
			break
		}
		attempts++
		u = f.rewriteHost(u, DefaultMeralcoBase, f.meralco)
		body, ok := f.getPage(ctx, "meralco-release", u)
		if !ok {
			continue
		}
		rate, err := ParseMeralcoRelease(body)
		if err != nil {
			f.fail("meralco-release %s: %v", u, err)
			continue
		}
		rate.SourceURL = u
		f.snap.Meralco = append(f.snap.Meralco, rate)
	}
	if len(f.snap.Meralco) == 0 {
		f.fail("meralco: no releases parsed")
	} else {
		f.stampSection("meralco")
	}
}

// fetchWater runs Fetch step 5 (Maynilad water status).
// The weekly-schedule RSS feed (MayniladFeed/File) is the primary
// source for weekly rows; the legacy website fetch is OPTIONAL and
// DISABLED by default (MayniladSite). The X inbox (MayniladXInbox
// path) is a cheap local file read. The FCDA fetch still uses the
// website; it is unchanged.
func (f *fetcher) fetchWater(ctx context.Context) {
	water := &WaterStatus{City: f.city, CheckedAt: f.snap.FetchedAt}
	// Weekly-schedule feed first (primary source for weekly rows).
	feedOK := f.fetchMayniladFeed(ctx, water)
	siteOK := false
	siteFailed := 0
	siteAttempted := f.site
	if f.site {
		for _, kind := range []string{"scheduled", "emergency", "rotational"} {
			u := f.maynilad + "/wp-json/interruptions/" + kind + "?id=&city=" + url.QueryEscape(f.city)
			body, ok := f.getPage(ctx, "maynilad-"+kind, u)
			if !ok {
				siteFailed++
				continue
			}
			rows, err := ParseInterruptions(body, kind)
			if err != nil {
				f.fail("maynilad-%s %s: %v", kind, u, err)
				siteFailed++
				continue
			}
			switch kind {
			case "scheduled":
				water.Scheduled = rows
			case "emergency":
				water.Emergency = rows
			case "rotational":
				water.Rotational = rows
			}
		}
		postsURL := f.maynilad + "/wp-json/wp/v2/posts?search=maintenance%20activities&_fields=id,date,link,title,content&per_page=3"
		if body, ok := f.getPage(ctx, "maynilad-maintenance", postsURL); ok {
			rows, err := ParseMaintenancePosts(body, f.city)
			if err != nil {
				f.fail("maynilad-maintenance %s: %v", postsURL, err)
				siteFailed++
			} else {
				water.Maintenance = append(water.Maintenance, rows...)
			}
		} else {
			siteFailed++
		}
		if siteFailed < 4 {
			siteOK = true
		}
	}
	// X inbox source.
	xOK := false
	var advisories []XAdvisory
	if f.xInbox != "" {
		// The inbox path is local and unpublished: sources and errors
		// are stored verbatim in the snapshot, so only the base name
		// (never the full path) may appear in them.
		inboxLabel := filepath.Base(f.xInbox)
		in, err := LoadXInbox(f.xInbox)
		if err != nil {
			if os.IsNotExist(err) {
				// Missing file: warning only, no section failure.
				// (No f.fail so offline runs without an inbox stay clean.)
			} else {
				f.fail("maynilad-x %s: %v", inboxLabel, sanitizeInboxErr(err, f.xInbox, inboxLabel))
			}
		} else {
			if staleXInbox(in.FetchedAt, f.now) {
				f.warn("maynilad-x %s: stale inbox (fetched_at %s)", inboxLabel, in.FetchedAt.Format(time.RFC3339))
			}
			advisories = ParseXAdvisories(in, f.city, f.now)
			if advisories == nil {
				advisories = []XAdvisory{}
			}
			f.source("maynilad-x", MayniladXAccountURL, 200, len(advisories), nil, time.Now(), nil)
			xOK = true
		}
	}
	if !siteAttempted && !xOK && f.xInbox == "" && !feedOK && f.feedFile == "" && !f.feed {
		return // neither source configured: omit Water like today
	}
	if !siteOK && !xOK && !feedOK {
		if siteAttempted && siteFailed == 4 && f.xInbox == "" && !f.feed && f.feedFile == "" {
			f.fail("maynilad: no water status parsed")
		}
		return // water fetch failed entirely: omit the water status
	}
	// A site-wide 403 no longer fails the whole section when the X
	// inbox produced data: siteOK==false + xOK==true still stamps.
	if water.Scheduled == nil {
		water.Scheduled = []Interruption{}
	}
	if water.Emergency == nil {
		water.Emergency = []Interruption{}
	}
	if water.Rotational == nil {
		water.Rotational = []Interruption{}
	}
	if water.Maintenance == nil {
		water.Maintenance = []Maintenance{}
	}
	if water.Nearby == nil {
		water.Nearby = []Maintenance{}
	}
	if xOK {
		water.Advisories = advisories
		if water.Advisories == nil {
			water.Advisories = []XAdvisory{}
		}
	}
	f.snap.Water = water
	f.stampSection("water")
}

// feedBlocked reports a Maynilad bot-challenge response: HTTP 403,
// a "cf-mitigated: challenge" header, or a "Just a moment" body.
func feedBlocked(status int, header http.Header, body []byte) bool {
	if status == 403 {
		return true
	}
	if header != nil && strings.EqualFold(header.Get("cf-mitigated"), "challenge") {
		return true
	}
	return strings.Contains(string(body), "Just a moment")
}

// countWeeklyFeedPosts counts weekly maintenance posts in a feed body.
func countWeeklyFeedPosts(body []byte) int {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return 0
	}
	n := 0
	for _, it := range feed.Channel.Items {
		if weeklyTitleRe.MatchString(it.Title) {
			n++
		}
	}
	return n
}

// getFeedRaw performs one polite GET capturing status/headers/body
// even for non-2xx (bot-challenge detection needs them).
func (f *fetcher) getFeedRaw(ctx context.Context, rawURL string) ([]byte, int, http.Header, time.Time, error) {
	if f.stopped {
		return nil, 0, nil, time.Time{}, errMaxRequests
	}
	if f.requests >= f.maxReq {
		f.stopped = true
		return nil, 0, nil, time.Time{}, errMaxRequests
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, nil, time.Time{}, err
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
		return nil, 0, nil, time.Time{}, err
	}
	if f.ua != "" {
		req.Header.Set("User-Agent", f.ua)
	}
	f.requests++
	at := time.Now()
	f.lastHost[u.Host] = at
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, 0, nil, at, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, at, err
	}
	return body, resp.StatusCode, resp.Header, at, nil
}

// fetchMayniladFeed fills water.Maintenance (Las Piñas rows) and
// water.Nearby (Parañaque/Muntinlupa/Bacoor rows) from the weekly RSS
// feed, with retention. True when the feed produced data (even zero
// rows from a valid feed). A blocked feed records its Source, prints
// ONE stderr notice, adds nothing to Errors, and keeps the X inbox.
func (f *fetcher) fetchMayniladFeed(ctx context.Context, water *WaterStatus) bool {
	if f.feedFile != "" {
		data, err := os.ReadFile(f.feedFile)
		if err != nil {
			f.fail("maynilad-feed %s: %v", filepath.Base(f.feedFile), err)
			return false
		}
		rows, skipped, err := ParseMayniladFeedDetailed(data)
		if err != nil {
			f.fail("maynilad-feed %s: %v", filepath.Base(f.feedFile), err)
			return false
		}
		if skipped > 0 {
			f.warn("maynilad-feed %s: %d table(s) without parsable header skipped", filepath.Base(f.feedFile), skipped)
		}
		main, nearby := SplitFeedRows(rows, f.now)
		water.Maintenance = append(water.Maintenance, main...)
		water.Nearby = append(water.Nearby, nearby...)
		f.source("maynilad-feed", f.maynilad+"/feed/", 200, len(data), sha256Sum(data), time.Now(), nil)
		return true
	}
	if !f.feed {
		return false
	}
	feedURL := strings.TrimSuffix(f.maynilad, "/") + "/feed/"
	body, status, header, at, err := f.getFeedRaw(ctx, feedURL)
	sum := sha256Sum(body)
	if err != nil {
		if err == errMaxRequests {
			f.fail("maynilad-feed: %v", err)
		} else {
			f.fail("maynilad-feed %s: %v", feedURL, err)
		}
		f.source("maynilad-feed", feedURL, status, len(body), sum, at, err)
		return false
	}
	if feedBlocked(status, header, body) {
		f.source("maynilad-feed", feedURL, status, len(body), sum, at, fmt.Errorf("blocked; using X inbox"))
		fmt.Fprintln(os.Stderr, "utilities fetch: maynilad feed blocked; using X inbox")
		return false
	}
	if status < 200 || status >= 300 {
		f.fail("maynilad-feed %s: HTTP %d", feedURL, status)
		f.source("maynilad-feed", feedURL, status, len(body), sum, at, fmt.Errorf("HTTP %d", status))
		return false
	}
	rows, skipped, err := ParseMayniladFeedDetailed(body)
	if err != nil {
		f.fail("maynilad-feed %s: %v", feedURL, err)
		f.source("maynilad-feed", feedURL, status, len(body), sum, at, err)
		return false
	}
	f.source("maynilad-feed", feedURL, status, len(body), sum, at, nil)
	if skipped > 0 {
		f.warn("maynilad-feed: %d table(s) without parsable header skipped", skipped)
	}
	// Page 2 only when page 1 holds no weekly post (max 2 requests).
	if countWeeklyFeedPosts(body) == 0 {
		pagedURL := feedURL + "?paged=2"
		body2, status2, header2, at2, err2 := f.getFeedRaw(ctx, pagedURL)
		sum2 := sha256Sum(body2)
		if err2 != nil {
			if err2 == errMaxRequests {
				f.fail("maynilad-feed: %v", err2)
			} else {
				f.fail("maynilad-feed %s: %v", pagedURL, err2)
			}
			f.source("maynilad-feed", pagedURL, status2, len(body2), sum2, at2, err2)
		} else if feedBlocked(status2, header2, body2) {
			f.source("maynilad-feed", pagedURL, status2, len(body2), sum2, at2, fmt.Errorf("blocked; using X inbox"))
			fmt.Fprintln(os.Stderr, "utilities fetch: maynilad feed blocked; using X inbox")
		} else if status2 < 200 || status2 >= 300 {
			f.fail("maynilad-feed %s: HTTP %d", pagedURL, status2)
			f.source("maynilad-feed", pagedURL, status2, len(body2), sum2, at2, fmt.Errorf("HTTP %d", status2))
		} else if rows2, skipped2, err2 := ParseMayniladFeedDetailed(body2); err2 != nil {
			f.fail("maynilad-feed %s: %v", pagedURL, err2)
			f.source("maynilad-feed", pagedURL, status2, len(body2), sum2, at2, err2)
		} else {
			f.source("maynilad-feed", pagedURL, status2, len(body2), sum2, at2, nil)
			if skipped2 > 0 {
				f.warn("maynilad-feed: %d table(s) without parsable header skipped", skipped2)
			}
			rows = append(rows, rows2...)
		}
	}
	main, nearby := SplitFeedRows(rows, f.now)
	water.Maintenance = append(water.Maintenance, main...)
	water.Nearby = append(water.Nearby, nearby...)
	return true
}

// sanitizeInboxErr rewrites an inbox load error for publication: the
// wrapped error (os.ReadFile, LoadXInbox) embeds the full local path,
// so every occurrence is replaced with the file's base name.
func sanitizeInboxErr(err error, fullPath, base string) string {
	if err == nil {
		return ""
	}
	if fullPath == "" || fullPath == base {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), fullPath, base)
}

// staleXInbox reports whether fetched_at is older than 12h at fetch time.
func staleXInbox(fetchedAt, now time.Time) bool {
	if fetchedAt.IsZero() {
		return true
	}
	return now.Sub(fetchedAt.In(manilaLoc)) > 12*time.Hour
}

// fetchFCDA runs the Maynilad FCDA notices fetch (section "fcda").
func (f *fetcher) fetchFCDA(ctx context.Context) {
	u := f.maynilad + "/wp-json/wp/v2/posts?search=FCDA&per_page=6&_fields=id,date,link,title,content"
	body, ok := f.getPage(ctx, "maynilad-fcda", u)
	if !ok {
		return
	}
	rates, err := ParseFCDAPosts(body)
	if err != nil {
		f.fail("maynilad-fcda %s: %v", u, err)
		return
	}
	f.snap.WaterRates = rates
	if len(rates) == 0 {
		f.fail("maynilad-fcda: no water rates parsed")
	} else {
		f.stampSection("fcda")
	}
}

// Empty reports whether nothing was parsed at all.
func (s *Snapshot) Empty() bool {
	return len(s.Fuel.Adjustments) == 0 && len(s.Fuel.NCRPump) == 0 &&
		len(s.LPG) == 0 && len(s.Meralco) == 0 && s.Water == nil
}

// sectionSourcePrefixes maps sections to their Source.Name prefixes,
// so a merge can drop stale entries of re-fetched sections.
var sectionSourcePrefixes = map[string][]string{
	"fuel":    {"doe-oil-monitor"},
	"pump":    {"doe-ncr-pump"},
	"lpg":     {"doe-ncr-lpg"},
	"meralco": {"meralco-"},
	"water":   {"maynilad-scheduled", "maynilad-emergency", "maynilad-rotational", "maynilad-maintenance", "maynilad-x", "maynilad-feed"},
	"fcda":    {"maynilad-fcda"},
}

// freshSection reports whether fresh carries records for section.
func freshSection(fresh *Snapshot, section string) bool {
	switch section {
	case "fuel":
		return len(fresh.Fuel.Adjustments) > 0
	case "pump":
		return len(fresh.Fuel.NCRPump) > 0
	case "lpg":
		return len(fresh.LPG) > 0
	case "meralco":
		return len(fresh.Meralco) > 0
	case "water":
		return fresh.Water != nil
	case "fcda":
		return len(fresh.WaterRates) > 0
	}
	return false
}

// MergeSnapshots merges a fresh section-limited fetch into an existing
// snapshot: requested sections with fresh records overwrite (keeping
// their new per-section fetched_at); untouched sections keep their old
// data and timestamps. Sources of re-fetched sections are replaced;
// errors accumulate.
func MergeSnapshots(base, fresh *Snapshot, sections []string) *Snapshot {
	out := &Snapshot{}
	if base != nil {
		*out = *base
	}
	// Deep-copy shared containers so the base is never mutated.
	out.Sources = append([]Source(nil), out.Sources...)
	out.Errors = append(out.Errors[:0:0], out.Errors...)
	freshStamps := map[string]string{}
	for k, v := range fresh.SectionFetchedAt {
		freshStamps[k] = v
	}
	out.SectionFetchedAt = map[string]string{}
	if base != nil {
		for k, v := range base.SectionFetchedAt {
			out.SectionFetchedAt[k] = v
		}
	}
	refreshed := map[string]bool{}
	for _, s := range sections {
		if !freshSection(fresh, s) {
			continue
		}
		refreshed[s] = true
		switch s {
		case "fuel":
			out.Fuel.Adjustments = fresh.Fuel.Adjustments
		case "pump":
			out.Fuel.NCRPump = fresh.Fuel.NCRPump
		case "lpg":
			out.LPG = fresh.LPG
		case "meralco":
			out.Meralco = fresh.Meralco
		case "water":
			out.Water = fresh.Water
		case "fcda":
			out.WaterRates = fresh.WaterRates
		}
		if ts, ok := freshStamps[s]; ok {
			out.SectionFetchedAt[s] = ts
		}
	}
	if len(refreshed) > 0 {
		var kept []Source
		for _, src := range out.Sources {
			drop := false
			for s := range refreshed {
				for _, pre := range sectionSourcePrefixes[s] {
					if strings.HasPrefix(src.Name, pre) {
						drop = true
						break
					}
				}
				if drop {
					break
				}
			}
			if !drop {
				kept = append(kept, src)
			}
		}
		out.Sources = append(kept, fresh.Sources...)
	}
	out.Errors = append(out.Errors, fresh.Errors...)
	out.FetchedAt = fresh.FetchedAt
	return out
}
