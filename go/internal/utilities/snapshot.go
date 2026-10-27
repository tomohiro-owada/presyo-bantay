package utilities

import (
	"context"
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
// The legacy website fetch is OPTIONAL and DISABLED by default
// (MayniladSite). The X inbox (MayniladXInbox path) is a cheap local
// file read. The FCDA fetch still uses the website; it is unchanged.
func (f *fetcher) fetchWater(ctx context.Context) {
	water := &WaterStatus{City: f.city, CheckedAt: f.snap.FetchedAt}
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
				water.Maintenance = rows
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
		in, err := LoadXInbox(f.xInbox)
		if err != nil {
			if os.IsNotExist(err) {
				// Missing file: warning only, no section failure.
				// (No f.fail so offline runs without an inbox stay clean.)
			} else {
				f.fail("maynilad-x %s: %v", f.xInbox, err)
			}
		} else {
			if staleXInbox(in.FetchedAt, f.now) {
				f.warn("maynilad-x %s: stale inbox (fetched_at %s)", f.xInbox, in.FetchedAt.Format(time.RFC3339))
			}
			advisories = ParseXAdvisories(in, f.city, f.now)
			if advisories == nil {
				advisories = []XAdvisory{}
			}
			f.source("maynilad-x", f.xInbox, 200, len(advisories), nil, time.Now(), nil)
			xOK = true
		}
	}
	if !siteAttempted && !xOK && f.xInbox == "" {
		return // neither source configured: omit Water like today
	}
	if !siteOK && !xOK {
		if siteAttempted && siteFailed == 4 && f.xInbox == "" {
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
	if xOK {
		water.Advisories = advisories
		if water.Advisories == nil {
			water.Advisories = []XAdvisory{}
		}
	}
	f.snap.Water = water
	f.stampSection("water")
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
	"water":   {"maynilad-scheduled", "maynilad-emergency", "maynilad-rotational", "maynilad-maintenance", "maynilad-x"},
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
