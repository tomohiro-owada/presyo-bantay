package utilities

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Meralco listing + release parsing. The listing page links
// https://company.meralco.com.ph/news-and-advisories/<slug> with slugs
// like higher-residential-rates-october-2026; a release page carries the
// rate change in its visible text.

var (
	// ratesSlugRe matches the month-year tail of a rates slug.
	ratesSlugRe = regexp.MustCompile(`rates-(january|february|march|april|may|june|july|august|september|october|november|december)-(\d{4})/?$`)

	announcedRe = regexp.MustCompile(`(?i)MANILA,\s*PHILIPPINES,\s*(\d{1,2})\s+([A-Za-z]+)\s+(\d{4})`)

	// "upward adjustment of P0.3191 per kWh" /
	// "another decrease of P0.0409 per kWh".
	adjustRe = regexp.MustCompile(`(?i)(upward|increase|decrease|downward|reduction|rollback|hike)\w*\s+(?:adjustment\s+)?of\s+P([0-9][0-9,]*\.?[0-9]*)\s+per\s+kWh`)

	// "overall rate for a typical household to P15.0615 from P14.7424
	// per kWh" (also "... to P14.7424 this month from P14.7833 ...").
	rateRe = regexp.MustCompile(`(?i)overall rate for a typical household to\s+P([0-9][0-9,]*\.?[0-9]*)(?:\s+this month)?\s+from\s+P([0-9][0-9,]*\.?[0-9]*)`)

	// "an increase of around P64 in the total electricity bill" /
	// "a reduction of around P8 in the total electricity bill".
	impactRe = regexp.MustCompile(`(?i)(increase|decrease|reduction)\s+of\s+around\s+P([0-9][0-9,]*)\s+in the total electricity bill`)

	// Month + year from a release title ("... this October 2026").
	titleMonthRe = regexp.MustCompile(`(?i)(january|february|march|april|may|june|july|august|september|october|november|december)\s+(\d{4})`)
)

// ParseMeralcoListing returns the absolute rates-release URLs on the
// latest-news page (slug rates-<month>-<year>), deduped, newest month
// first.
func ParseMeralcoListing(page []byte) []string {
	type hit struct {
		url        string
		year, mon  int
		firstOrder int
	}
	seen := map[string]bool{}
	var hits []hit
	for i, m := range anchorRe.FindAllSubmatch(page, -1) {
		raw := strings.TrimSpace(anchorHref(m))
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
			continue
		}
		// Match the slug against the path (query/fragment ignored).
		path := raw
		if j := strings.IndexAny(path, "?#"); j >= 0 {
			path = path[:j]
		}
		sm := ratesSlugRe.FindStringSubmatch(strings.ToLower(path))
		if sm == nil {
			continue
		}
		key := strings.ToLower(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		year, _ := strconv.Atoi(sm[2])
		hits = append(hits, hit{url: raw, year: year, mon: int(monthByName[sm[1]]), firstOrder: i})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].year != hits[j].year {
			return hits[i].year > hits[j].year
		}
		if hits[i].mon != hits[j].mon {
			return hits[i].mon > hits[j].mon
		}
		return hits[i].firstOrder < hits[j].firstOrder
	})
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.url)
	}
	return out
}

// visibleText strips script/style/tags and unescapes entities,
// collapsing whitespace per line.
func visibleText(page []byte) string {
	s := string(page)
	s = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`).ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = wsRe.ReplaceAllString(strings.TrimSpace(ln), " ")
	}
	joined := strings.Join(lines, "\n")
	joined = regexp.MustCompile(`\n{2,}`).ReplaceAllString(joined, "\n")
	return strings.TrimSpace(joined)
}

// releaseTitle extracts the <title> (else first <h1>) text.
func releaseTitle(page []byte) string {
	if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindSubmatch(page); m != nil {
		t := wsRe.ReplaceAllString(strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(string(m[1]), " "))), " ")
		// Drop site suffixes ("... | Meralco").
		if i := strings.Index(t, " | "); i >= 0 {
			t = t[:i]
		}
		if t != "" {
			return t
		}
	}
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindSubmatch(page); m != nil {
		return wsRe.ReplaceAllString(strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(string(m[1]), " "))), " ")
	}
	return ""
}

// ParseMeralcoRelease parses a rates release page's visible text into
// a MeralcoRate, validating that round(Rate-PrevRate, 4) equals the
// signed adjustment and that it covers the 200 kWh impact within P1.
func ParseMeralcoRelease(page []byte) (MeralcoRate, error) {
	var r MeralcoRate
	text := visibleText(page)
	flat := wsRe.ReplaceAllString(strings.ReplaceAll(text, "\n", " "), " ")

	r.Title = releaseTitle(page)
	if r.Title == "" {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco title not found")
	}
	tm := titleMonthRe.FindStringSubmatch(r.Title)
	if tm == nil {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco month not in title %q", r.Title)
	}
	r.Month = fmt.Sprintf("%s-%02d", tm[2], int(monthByName[strings.ToLower(tm[1])]))

	am := announcedRe.FindStringSubmatch(flat)
	if am == nil {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco announced date not found")
	}
	announced, err := parseMonthDayYear(am[1], am[2], am[3])
	if err != nil {
		return MeralcoRate{}, err
	}
	r.Announced = announced

	jm := adjustRe.FindStringSubmatch(flat)
	if jm == nil {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco adjustment not found")
	}
	adj, err := parseAmount(jm[2])
	if err != nil {
		return MeralcoRate{}, err
	}
	up := jm[1]
	positive := up == "upward" || strings.HasPrefix(strings.ToLower(up), "increas") || strings.HasPrefix(strings.ToLower(up), "hike")
	if !positive {
		adj = -adj
	}
	r.Adjustment = adj

	rm := rateRe.FindStringSubmatch(flat)
	if rm == nil {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco rate not found")
	}
	if r.Rate, err = parseAmount(rm[1]); err != nil {
		return MeralcoRate{}, err
	}
	if r.PrevRate, err = parseAmount(rm[2]); err != nil {
		return MeralcoRate{}, err
	}

	im := impactRe.FindStringSubmatch(flat)
	if im == nil {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco 200 kWh impact not found")
	}
	impact, err := strconv.Atoi(strings.ReplaceAll(im[2], ",", ""))
	if err != nil {
		return MeralcoRate{}, err
	}
	if strings.ToLower(im[1]) != "increase" {
		impact = -impact
	}
	r.Impact200 = impact

	// Validation: the increase/decrease wording must agree with the
	// rate difference, which rounded to 4 dp must equal the stated
	// adjustment; the adjustment over 200 kWh must cover the impact.
	diff := math.Round((r.Rate-r.PrevRate)*10000) / 10000
	if diff != r.Adjustment {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco rate diff %.4f != adjustment %.4f", diff, r.Adjustment)
	}
	if math.Abs(r.Adjustment*200-float64(r.Impact200)) > 1 {
		return MeralcoRate{}, fmt.Errorf("utilities: meralco impact %d inconsistent with adjustment %.4f", r.Impact200, r.Adjustment)
	}
	return r, nil
}
