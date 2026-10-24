// Package listing ports presyo_bantay/listing.py: scrape the DA
// price-monitoring listing page and pick per-date PDFs.
//
// HTML parsing uses a regexp over <a ... href="..."> (single/double quoted,
// case-insensitive) instead of golang.org/x/net/html, matching bs4
// find_all("a", href=True) semantics for this page; URLs resolve against
// the base via net/url (like urljoin).
package listing

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ListingURL mirrors listing.LISTING_URL.
const ListingURL = "https://www.da.gov.ph/price-monitoring/"

var months = map[string]int{
	"january": 1, "jan": 1,
	"february": 2, "feb": 2,
	"march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"may":  5,
	"june": 6, "jun": 6,
	"july": 7, "jul": 7,
	"august": 8, "aug": 8,
	"september": 9, "sept": 9, "sep": 9,
	"october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12,
}

var filenameRe = regexp.MustCompile(
	`^(?:(?P<prefix>Revised|Updated)-)?` +
		`Price-Monitoring-` +
		`(?P<month>[A-Za-z]+)-(?P<day>\d{1,2})-(?P<year>\d{4})` +
		`(?:-(?P<suffix>\d+))?` +
		`(?:-(?P<revised_suffix>[Rr]evised))?` +
		`\.pdf$`)

// hrefRe matches <a ... href="..."> / href='...' (case-insensitive tag,
// attribute and scheme), mirroring bs4 find_all("a", href=True) for the
// quoted-attribute links on the DA listing page.
var hrefRe = regexp.MustCompile(`(?is)<a\b[^<>]*?\bhref\s*=\s*("([^"]*)"|'([^']*)')`)

// FileDate is a report date parsed from a PDF filename.
type FileDate struct {
	time.Time
}

// ISODate returns YYYY-MM-DD.
func (d FileDate) ISODate() string {
	return d.Time.Format("2006-01-02")
}

func cleanName(filename string) string {
	s := strings.TrimSpace(filename)
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// ParseFilenameDate mirrors listing.parse_filename_date.
func ParseFilenameDate(filename string) *FileDate {
	clean := cleanName(filename)
	m := filenameRe.FindStringSubmatch(clean)
	if m == nil {
		return nil
	}
	month, ok := months[strings.ToLower(m[2])]
	if !ok {
		return nil
	}
	year, err1 := strconv.Atoi(m[4])
	day, err2 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil {
		return nil
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || t.Month() != time.Month(month) || t.Day() != day {
		return nil
	}
	return &FileDate{t}
}

// orderingKey mirrors _ordering_key: latest date wins; ties prefer
// Revised/Updated, then higher numeric suffix.
func orderingKey(filename string, date FileDate) (time.Time, bool, int) {
	clean := cleanName(filename)
	m := filenameRe.FindStringSubmatch(clean)
	revised := false
	suffix := 0
	if m != nil {
		revised = m[1] != "" || m[6] != ""
		if m[5] != "" {
			suffix, _ = strconv.Atoi(m[5])
		}
	}
	return date.Time, revised, suffix
}

func greaterKey(a, b [3]any) bool {
	// Compare (date, revised, suffix) tuples.
	at, bt := a[0].(time.Time), b[0].(time.Time)
	if !at.Equal(bt) {
		return at.After(bt)
	}
	ar, br := a[1].(bool), b[1].(bool)
	if ar != br {
		return ar
	}
	return a[2].(int) > b[2].(int)
}

// IsCandidateFilename mirrors _is_candidate_filename.
func IsCandidateFilename(filename string) bool {
	clean := cleanName(filename)
	lowered := strings.ToLower(clean)
	if strings.Contains(lowered, "cigarette") {
		return false
	}
	if strings.Contains(lowered, "daily-price-index") ||
		strings.Contains(lowered, "weekly-average") {
		return false
	}
	if strings.Contains(lowered, "price-watch") {
		return false
	}
	if strings.Contains(lowered, "retail") {
		return false
	}
	return ParseFilenameDate(clean) != nil
}

// PDFLink is one (url, date) candidate.
type PDFLink struct {
	URL  string
	Date FileDate
}

// FindLatestFromURLs mirrors find_latest_from_urls.
func FindLatestFromURLs(urls []string) *PDFLink {
	var best *PDFLink
	var bestKey [3]any
	for _, u := range urls {
		filename := u
		if i := strings.LastIndex(filename, "/"); i >= 0 {
			filename = filename[i+1:]
		}
		if !IsCandidateFilename(filename) {
			continue
		}
		date := ParseFilenameDate(filename)
		if date == nil {
			continue
		}
		t, rev, suf := orderingKey(filename, *date)
		key := [3]any{t, rev, suf}
		if best == nil || greaterKey(key, bestKey) {
			bestKey = key
			best = &PDFLink{URL: u, Date: *date}
		}
	}
	return best
}

// extractHrefs scrapes hrefs from listing html like
// bs4 find_all("a", href=True): quoted href attrs (case-insensitive),
// HTML-entity unescaped (as bs4's parser does), resolved against base,
// keeping only links containing ".pdf" (case-insensitive).
func extractHrefs(htmlText, baseURL string) []string {
	base, err := url.Parse(baseURL)
	if err != nil {
		base = nil
	}
	var urls []string
	for _, m := range hrefRe.FindAllStringSubmatch(htmlText, -1) {
		href := m[2]
		if strings.HasPrefix(m[1], "'") {
			href = m[3]
		}
		href = strings.TrimSpace(html.UnescapeString(href))
		if href == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(href), ".pdf") {
			continue
		}
		if base != nil {
			if ref, err := url.Parse(href); err == nil {
				href = base.ResolveReference(ref).String()
			}
		}
		urls = append(urls, href)
	}
	return urls
}

// FindLatestFromHTML mirrors find_latest_from_html.
func FindLatestFromHTML(htmlText, baseURL string) *PDFLink {
	return FindLatestFromURLs(extractHrefs(htmlText, baseURL))
}

// FindAllFromURLs mirrors find_all_from_urls: best URL per report date,
// newest first.
func FindAllFromURLs(urls []string) []PDFLink {
	type entry struct {
		link PDFLink
		key  [3]any
	}
	best := map[string]entry{}
	for _, u := range urls {
		filename := u
		if i := strings.LastIndex(filename, "/"); i >= 0 {
			filename = filename[i+1:]
		}
		// Mirror Python: strip query/fragment from the filename for
		// candidate checks (but keep the full URL).
		name := cleanName(filename)
		if !IsCandidateFilename(name) {
			continue
		}
		date := ParseFilenameDate(name)
		if date == nil {
			continue
		}
		t, rev, suf := orderingKey(name, *date)
		key := [3]any{t, rev, suf}
		iso := date.ISODate()
		if cur, ok := best[iso]; !ok || greaterKey(key, cur.key) {
			best[iso] = entry{PDFLink{URL: u, Date: *date}, key}
		}
	}
	var ranked []entry
	for _, e := range best {
		ranked = append(ranked, e)
	}
	sort.Slice(ranked, func(i, j int) bool {
		return greaterKey(ranked[i].key, ranked[j].key)
	})
	out := make([]PDFLink, 0, len(ranked))
	for _, e := range ranked {
		out = append(out, e.link)
	}
	return out
}

// FindAllFromHTML mirrors find_all_from_html.
func FindAllFromHTML(htmlText, baseURL string) []PDFLink {
	return FindAllFromURLs(extractHrefs(htmlText, baseURL))
}
