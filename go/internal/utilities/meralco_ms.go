package utilities

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Meralco planned interruptions (maintenance-schedule pages).
//
// Source: {MeralcoCompanyBase}/news-and-advisories/maintenance-schedule
// (server-rendered Drupal view, newest event first, 2 rows per page,
// ?page=N). City filters ?field_service_maintenance_loc_target_id=ID
// (215 Las Piñas, 223 Parañaque, 221 Muntinlupa, 213 Cavite; Bacoor
// has no own id, it is inside Cavite).
//
// Our wording only (ReasonKey); barangay names and times are relayed
// as facts. Tests never touch the network: fixtures live in
// internal/utilities/testdata/meralco_ms/.

// MeralcoMSPath is the maintenance-schedule listing path.
const MeralcoMSPath = "/news-and-advisories/maintenance-schedule"

// MeralcoMSCityIDs are the city filter ids, walked in this order.
var MeralcoMSCityIDs = []int{215, 223, 221, 213}

// MeralcoMSLasPinasID is the Las Piñas city filter id (monthly
// history walk for the trend).
const MeralcoMSLasPinasID = 215

// PowerBlock is one BETWEEN circuit block of a maintenance item.
type PowerBlock struct {
	Circuit   string              `json:"circuit"`
	Windows   []TimeWindow        `json:"windows,omitempty"`
	AreaLines []string            `json:"area_lines,omitempty"`
	Barangays map[string][]string `json:"barangays,omitempty"`
	Roads     []string            `json:"roads,omitempty"`
	// Daily marks nightly repeats over the title date range (block
	// headers without inline dates on a multi-day title).
	Daily         bool   `json:"daily,omitempty"`
	DailyFrom     string `json:"daily_from,omitempty"`
	DailyTo       string `json:"daily_to,omitempty"`
	DateAmbiguous bool   `json:"date_ambiguous,omitempty"`
}

// PowerItem is one maintenance-schedule row.
type PowerItem struct {
	ID        string       `json:"id"`
	URL       string       `json:"url"`
	Title     string       `json:"title"`
	Tags      []string     `json:"tags,omitempty"`
	Cities    []string     `json:"cities,omitempty"`
	DateFrom  string       `json:"date_from"`
	DateTo    string       `json:"date_to"`
	Blocks    []PowerBlock `json:"blocks,omitempty"`
	Reason    string       `json:"reason"`
	ReasonKey string       `json:"reason_key"`
	// Daily marks nightly repeats over the title date range (any
	// block without inline dates on a multi-day title).
	Daily         bool   `json:"daily,omitempty"`
	DailyFrom     string `json:"daily_from,omitempty"`
	DailyTo       string `json:"daily_to,omitempty"`
	DateAmbiguous bool   `json:"date_ambiguous,omitempty"`
	// Beyond14d marks items starting more than 14 days after today
	// (parsed and stored, but only shown on the site up to +21d).
	Beyond14d bool `json:"beyond_14d,omitempty"`
}

// PowerStatus is the fetched maintenance-schedule section.
type PowerStatus struct {
	CheckedAt   string      `json:"checked_at"`
	Items       []PowerItem `json:"items"`
	HistoryFrom string      `json:"history_from,omitempty"`
	LayoutAlarm string      `json:"layout_alarm,omitempty"`
}

// MeralcoMSPage is one parsed maintenance-schedule listing page.
type MeralcoMSPage struct {
	Items []PowerItem
}

// PowerAlert is one postable Las Piñas planned-interruption alert.
type PowerAlert struct {
	ID              string       `json:"id"`
	City            string       `json:"city"`
	Start           time.Time    `json:"start"`
	End             time.Time    `json:"end"`
	Windows         []TimeWindow `json:"windows,omitempty"`
	Daily           bool         `json:"daily,omitempty"`
	DailyFrom       string       `json:"daily_from,omitempty"`
	DailyTo         string       `json:"daily_to,omitempty"`
	Barangays       []string     `json:"barangays"`
	Roads           []string     `json:"roads,omitempty"`
	ReasonKey       string       `json:"reason_key"`
	SourceURL       string       `json:"source_url"`
	NthThisMonth    int          `json:"nth_this_month,omitempty"`
	PrevMonthCount  int          `json:"prev_month_count,omitempty"`
	HistoryComplete bool         `json:"history_complete,omitempty"`
}

// PowerAreasTL/EN/JA are the per-language areas markers when no Las
// Piñas barangay is extractable (mirrors XAreasTL/EN/JA).
const (
	PowerAreasTL = "Ilang barangay — tingnan ang abiso ng Meralco"
	PowerAreasEN = "Some barangays — see the Meralco advisory"
	PowerAreasJA = "一部の地区（詳細は告知をご確認ください）"
)

// Power reason keys (our own wording targets): line and pole works,
// substation maintenance, facility installation, tree trimming, or
// generic Meralco maintenance.
const (
	PowerReasonLine        = "line"
	PowerReasonSubstation  = "substation"
	PowerReasonFacility    = "facility"
	PowerReasonTrimming    = "trimming"
	PowerReasonMaintenance = "maintenance"
)

// powerReasonKeyOf maps a raw REASON phrase to our wording key
// (unknown phrases → maintenance; never copy their sentences).
func powerReasonKeyOf(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "substation"):
		return PowerReasonSubstation
	case strings.Contains(lower, "tree") || strings.Contains(lower, "trimm") || strings.Contains(lower, "prun"):
		return PowerReasonTrimming
	case strings.Contains(lower, "line") || strings.Contains(lower, "pole") ||
		strings.Contains(lower, "reconduct") || strings.Contains(lower, "reconstruc") ||
		strings.Contains(lower, "replac") || strings.Contains(lower, "relocat") ||
		strings.Contains(lower, "retire") || strings.Contains(lower, "upgrad"):
		return PowerReasonLine
	case strings.Contains(lower, "install") || strings.Contains(lower, "facilit"):
		return PowerReasonFacility
	}
	return PowerReasonMaintenance
}

var (
	msTitleAnchorRe = regexp.MustCompile(`(?is)<div class="views-field views-field-title".*?<a\b[^>]*\bhref\s*=\s*"([^"]+)"[^>]*>(.*?)</a>`)
	msTagRe         = regexp.MustCompile(`(?is)<div class="views-field views-field-field-service-maintenance-loc.*?<div class="field-content">(.*?)</div>`)
	msBodyRe        = regexp.MustCompile(`(?is)<div class="views-field views-field-body.*?<div class="field-content">(.*?)</div>\s*</div>`)
	msStrongRe      = regexp.MustCompile(`(?is)<strong>(.*?)</strong>`)
	msReasonMarkRe  = regexp.MustCompile(`(?is)views-field-field-reason(.*)`)

	msTitleSplitRe = regexp.MustCompile(`^(.+?,\s*\d{4})\s*-\s*(.+)$`)
	msRange2MonRe  = regexp.MustCompile(`^([A-Za-z]+\.?)\s+(\d{1,2})\s*-\s*([A-Za-z]+\.?)\s+(\d{1,2}),\s*(\d{4})$`)
	msRange1MonRe  = regexp.MustCompile(`^([A-Za-z]+\.?)\s+(\d{1,2})\s*-\s*(\d{1,2}),\s*(\d{4})$`)
	msSingleRe     = regexp.MustCompile(`^([A-Za-z]+\.?)\s+(\d{1,2}),\s*(\d{4})$`)

	msBetweenRe    = regexp.MustCompile(`(?i)\bBETWEEN\s+`)
	msClockRe      = regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(A\.?M\.?|P\.?M\.?|NN|MN)`)
	msInlineDateRe = regexp.MustCompile(`(?i)\(\s*(?:MON|TUE|WED|THU|FRI|SAT|SUN)\s*\.,?\s*(\d{1,2})/(\d{1,2})/(\d{2,4})\s*\)`)

	msCircuitPhraseRe = regexp.MustCompile(`(?i)(?:portions?\s+of\s+circuits?|whole\s+of\s+circuit)\s*`)
	msDashPrefixRe    = regexp.MustCompile(`^[\s:–—-]+`)

	msBgyInRe    = regexp.MustCompile(`(?i)\bBgys?\.\s*([^.;]+?)\s+in\s+([A-Z][^.;,]*?(?:City|Cavite|Laguna|Bulacan|Manila|Pasay|Makati|Mandaluyong|Taguig|Parañaque|Muntinlupa|Bacoor|Imus|Kawit|Dasmariñas|Biñan|Carmona|Pandi|Malate|Pandacan))`)
	msBgyCommaRe = regexp.MustCompile(`(?i)\bBgy\.\s*([^,;]+?),\s*([A-Z][^.;,]*?(?:City|Cavite|Laguna|Bulacan|Manila|Pasay|Makati|Mandaluyong|Taguig|Parañaque|Muntinlupa|Bacoor|Imus|Kawit|Dasmariñas|Biñan|Carmona|Pandi|Malate|Pandacan))`)
	msBgyBareRe  = regexp.MustCompile(`(?i)\bBgys?\.\s*([^.;]+?)(?:[.;]|$)`)
	msInTrimRe   = regexp.MustCompile(`(?i)\s+in\s+`)
	msBgyPreRe   = regexp.MustCompile(`(?i)^\s*(?:Bgy|Bgys|Brgy|Brgys)\.?\s+`)
	msAndSplitRe = regexp.MustCompile(`(?i)\s+(?:&|and)\s+`)
	msFromCutRe  = regexp.MustCompile(`(?i)\s+from\s+`)
	msPortionRe  = regexp.MustCompile(`(?i)^\s*portions?\s+of\s+(.+?)(?:;|$)`)
	msRoadEndRe  = regexp.MustCompile(`(?i)\b(road|drive|ave\.?|avenue|street|st\.?|highway|boulevard|blvd\.?)$`)
	msNearCutRe  = regexp.MustCompile(`(?i)\s+near\s+`)

	msNumeralRe  = regexp.MustCompile(`^([IVXLCDM]+|\d+[A-Z]?)$`)
	msDashNormRe = regexp.MustCompile(`\s*[–—]\s*`)
)

// msMonthByName extends monthByName with common abbreviations.
var msMonthByName = func() map[string]time.Month {
	m := map[string]time.Month{}
	for k, v := range monthByName {
		m[k] = v
	}
	m["sept"] = time.September
	m["jan"] = time.January
	m["feb"] = time.February
	m["mar"] = time.March
	m["apr"] = time.April
	m["jun"] = time.June
	m["jul"] = time.July
	m["aug"] = time.August
	m["sep"] = time.September
	m["oct"] = time.October
	m["nov"] = time.November
	m["dec"] = time.December
	return m
}()

// msClean flattens incidental whitespace (NBSP included).
func msClean(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return spaceRe.ReplaceAllString(strings.TrimSpace(s), " ")
}

// msLines converts an HTML fragment to clean text lines (keeps <br>
// breaks, drops tags, unescapes entities).
func msLines(frag string) []string {
	s := regexp.MustCompile(`(?is)<br[^>]*>`).ReplaceAllString(frag, "\n")
	s = nbspRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = regexp.MustCompile(`(?is)<[^>]*>`).ReplaceAllString(s, "")
	var out []string
	for _, ln := range strings.Split(html.UnescapeString(s), "\n") {
		if t := strings.TrimSpace(ln); t != "" && t != "&nbsp;" {
			out = append(out, t)
		}
	}
	return out
}

// normalizePowerCity maps the four tracked cities to canonical names
// (accent-insensitive); other cities keep their trimmed text.
func normalizePowerCity(s string) string {
	t := strings.TrimSpace(s)
	switch foldAccent(t) {
	case "las pinas", "las pinas city":
		return "Las Piñas"
	case "paranaque", "paranaque city":
		return "Parañaque"
	case "muntinlupa", "muntinlupa city":
		return "Muntinlupa"
	case "bacoor", "bacoor city", "bacoor, cavite":
		return "Bacoor"
	default:
		return t
	}
}

// isPowerLasPinas reports whether "Las Piñas" is among cities.
func isPowerLasPinas(cities []string) bool {
	for _, c := range cities {
		if foldAccent(c) == "las pinas" {
			return true
		}
	}
	return false
}

// powerMonth resolves a month token (full or abbreviated).
func powerMonth(tok string) (time.Month, bool) {
	m, ok := msMonthByName[strings.ToLower(strings.TrimSuffix(tok, "."))]
	if !ok && strings.HasPrefix(strings.ToLower(tok), "sept") {
		return time.September, true
	}
	return m, ok
}

// parseMSTitleDates parses the date part of a listing title
// ("October 7, 2026", "October 29 - November 2, 2026",
// "November 3 - 4, 2026") into YYYY-MM-DD dates.
func parseMSTitleDates(datePart string) (from, to string, ok bool) {
	if m := msRange2MonRe.FindStringSubmatch(datePart); m != nil {
		mo1, ok1 := powerMonth(m[1])
		mo2, ok2 := powerMonth(m[3])
		if !ok1 || !ok2 {
			return "", "", false
		}
		f, err1 := parseMonthDayYear(m[2], mo1.String(), m[5])
		t, err2 := parseMonthDayYear(m[4], mo2.String(), m[5])
		if err1 != nil || err2 != nil {
			return "", "", false
		}
		return f, t, true
	}
	if m := msRange1MonRe.FindStringSubmatch(datePart); m != nil {
		mo, okm := powerMonth(m[1])
		if !okm {
			return "", "", false
		}
		f, err1 := parseMonthDayYear(m[2], mo.String(), m[4])
		t, err2 := parseMonthDayYear(m[3], mo.String(), m[4])
		if err1 != nil || err2 != nil {
			return "", "", false
		}
		return f, t, true
	}
	if m := msSingleRe.FindStringSubmatch(datePart); m != nil {
		mo, okm := powerMonth(m[1])
		if !okm {
			return "", "", false
		}
		d, err := parseMonthDayYear(m[2], mo.String(), m[3])
		if err != nil {
			return "", "", false
		}
		return d, d, true
	}
	return "", "", false
}

// parseMSClock parses "11:00PM", "10:00AM", "12:00NN" (noon),
// "12:00MN" (midnight) to 24h hour/minute.
func parseMSClock(num, min, ap string) (h, mi int, ok bool) {
	if _, err := fmt.Sscanf(num, "%d", &h); err != nil || h < 1 || h > 12 {
		return 0, 0, false
	}
	mi = 0
	if min != "" {
		if _, err := fmt.Sscanf(min, "%d", &mi); err != nil || mi > 59 {
			return 0, 0, false
		}
	}
	a := strings.ToUpper(strings.ReplaceAll(ap, ".", ""))
	switch a {
	case "AM":
		if h == 12 {
			h = 0
		}
	case "PM":
		if h != 12 {
			h += 12
		}
	case "NN":
		h = 12
	case "MN":
		h = 0
	default:
		return 0, 0, false
	}
	return h, mi, true
}

// parseMSInlineDate parses "11/03/26" (MM/DD/YY) to YYYY-MM-DD.
func parseMSInlineDate(mm, dd, yy string) (string, bool) {
	var m, d, y int
	if _, err := fmt.Sscanf(mm, "%d", &m); err != nil || m < 1 || m > 12 {
		return "", false
	}
	if _, err := fmt.Sscanf(dd, "%d", &d); err != nil || d < 1 || d > 31 {
		return "", false
	}
	if _, err := fmt.Sscanf(yy, "%d", &y); err != nil {
		return "", false
	}
	if y < 100 {
		if y < 50 {
			y += 2000
		} else {
			y += 1900
		}
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if int(t.Month()) != m || t.Day() != d || t.Year() != y {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

// msWindowTimes resolves one segment's clock pair (with optional
// inline dates) to concrete Manila windows.
func msWindowTimes(clocks [][]string, dates []string, dateFrom, dateTo string) []TimeWindow {
	if len(clocks) < 2 {
		return nil
	}
	h1, mi1, ok1 := parseMSClock(clocks[0][1], clocks[0][2], clocks[0][3])
	h2, mi2, ok2 := parseMSClock(clocks[1][1], clocks[1][2], clocks[1][3])
	if !ok1 || !ok2 {
		return nil
	}
	resolve := func(base string) (time.Time, bool) {
		d, ok := mustDate(base)
		if !ok {
			return time.Time{}, false
		}
		return d, true
	}
	var startDate, endDate string
	switch {
	case len(dates) >= 2:
		startDate, endDate = dates[0], dates[1]
	case len(dates) == 1:
		startDate, endDate = dates[0], dates[0]
	default:
		if dateFrom == "" || dateTo == "" || dateFrom != dateTo {
			return nil
		}
		startDate, endDate = dateFrom, dateTo
	}
	d1, ok1 := resolve(startDate)
	d2, ok2 := resolve(endDate)
	if !ok1 || !ok2 {
		return nil
	}
	start := time.Date(d1.Year(), d1.Month(), d1.Day(), h1, mi1, 0, 0, manilaLoc)
	end := time.Date(d2.Year(), d2.Month(), d2.Day(), h2, mi2, 0, 0, manilaLoc)
	if !end.After(start) {
		end = end.Add(24 * time.Hour)
	}
	return []TimeWindow{{Start: start, End: end}}
}

// msDailyWindows expands clock pairs over every night of a multi-day
// title range (no inline dates): one window per night, end next day
// when end <= start.
func msDailyWindows(clocks [][]string, dateFrom, dateTo string) []TimeWindow {
	d1, ok1 := mustDate(dateFrom)
	d2, ok2 := mustDate(dateTo)
	if !ok1 || !ok2 || d2.Before(d1) {
		return nil
	}
	var out []TimeWindow
	for i := 0; i+1 < len(clocks); i += 2 {
		h1, mi1, ok1 := parseMSClock(clocks[i][1], clocks[i][2], clocks[i][3])
		h2, mi2, ok2 := parseMSClock(clocks[i+1][1], clocks[i+1][2], clocks[i+1][3])
		if !ok1 || !ok2 {
			continue
		}
		for d := d1; !d.After(d2); d = d.Add(24 * time.Hour) {
			start := time.Date(d.Year(), d.Month(), d.Day(), h1, mi1, 0, 0, manilaLoc)
			end := time.Date(d.Year(), d.Month(), d.Day(), h2, mi2, 0, 0, manilaLoc)
			if !end.After(start) {
				end = end.Add(24 * time.Hour)
			}
			out = append(out, TimeWindow{Start: start, End: end})
		}
	}
	return out
}

// msSplitSegments splits a header's visible text on BETWEEN.
func msSplitSegments(header string) []string {
	parts := msBetweenRe.Split(header, -1)
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// msCircuitAndTrail extracts the circuit name from a block header and
// any trailing area text the header swallowed (e.g. the Dasmariñas
// row bolds "PORTION OF CIRCUIT ... Villas at ...").
func msCircuitAndTrail(header string) (circuit, trail string) {
	loc := msCircuitPhraseRe.FindStringIndex(header)
	if loc == nil {
		return "", ""
	}
	rest := msDashPrefixRe.ReplaceAllString(header[loc[1]:], "")
	toks := strings.Fields(rest)
	keep := 0
	for _, tok := range toks {
		lower := false
		for _, r := range tok {
			if unicode.IsLower(r) {
				lower = true
				break
			}
		}
		if lower {
			break
		}
		keep++
	}
	circuit = strings.Join(toks[:keep], " ")
	if keep < len(toks) {
		trail = strings.Join(toks[keep:], " ")
	}
	return strings.TrimSpace(circuit), strings.TrimSpace(trail)
}

// msCleanBarangayPart strips Bgy/Bgys prefixes and edge punctuation.
func msCleanBarangayPart(p string) string {
	p = msBgyPreRe.ReplaceAllString(strings.TrimSpace(p), "")
	p = msDashNormRe.ReplaceAllString(p, "-")
	p = strings.Trim(p, " ;:.-–—")
	return strings.TrimSpace(p)
}

// msIncludeSplitRe breaks "X to and including Bgys. Y" lists.
var msIncludeSplitRe = regexp.MustCompile(`(?i)\bto and including\b|\bincluding\b`)

// msTrailingCityRe detects a trailing ", City" in a bare barangay
// list (the city the list belongs to).
var msTrailingCityRe = regexp.MustCompile(`(?i)(City|Cavite|Laguna|Bulacan|Manila|Pasay|Makati|Mandaluyong|Taguig|Parañaque|Muntinlupa|Bacoor|Imus|Kawit|Dasmariñas|Biñan|Carmona|Pandi|Malate|Pandacan)\.?$`)

// msSingleLetterRe drops lone letters ("Alapan I–A & B" → the "B"
// cannot be expanded reliably).
var msSingleLetterRe = regexp.MustCompile(`^[A-Z]$`)

// msSplitBarangayList splits a barangay list on ",", "&" and "and"
// (order kept), expanding bare Roman/arabic numerals ("Almanza I &
// II" → "Almanza I", "Almanza II").
func msSplitBarangayList(raw string) []string {
	raw = msIncludeSplitRe.ReplaceAllString(raw, ",")
	var out []string
	push := func(p string) {
		p = msCleanBarangayPart(p)
		if p == "" || msSingleLetterRe.MatchString(p) {
			return
		}
		if msNumeralRe.MatchString(p) && len(out) > 0 {
			prev := out[len(out)-1]
			fields := strings.Fields(prev)
			if len(fields) > 1 {
				base := strings.Join(fields[:len(fields)-1], " ")
				out = append(out, base+" "+p)
				return
			}
		}
		out = append(out, p)
	}
	for _, semi := range strings.Split(raw, ";") {
		for _, comma := range strings.Split(semi, ",") {
			for _, amp := range msAndSplitRe.Split(comma, -1) {
				push(amp)
			}
		}
	}
	return out
}

// msAddBarangays appends barangays to a city bucket (order kept,
// deduped).
func msAddBarangays(dst map[string][]string, city string, names []string) {
	city = strings.TrimSpace(city)
	if city == "" {
		return
	}
	seen := map[string]bool{}
	for _, n := range dst[city] {
		seen[n] = true
	}
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		dst[city] = append(dst[city], n)
	}
}

// msLineBarangays extracts (city, barangays) from one area line,
// falling back to fallbackCity when the line names no city. Comma
// forms ("Bgy. X, City") and trimmed "in" forms ("... in Bgys. X in
// City") combine; bare "Bgys. ..." lists only apply when nothing
// else matched.
func msLineBarangays(line, fallbackCity string, dst map[string][]string) {
	matched := false
	for _, m := range msBgyCommaRe.FindAllStringSubmatch(line, -1) {
		matched = true
		city := normalizePowerCity(m[2])
		msAddBarangays(dst, city, msSplitBarangayList(m[1]))
	}
	for _, m := range msBgyInRe.FindAllStringSubmatch(line, -1) {
		matched = true
		// Keep the tail after the last " in " (a lazy list can span
		// earlier "X in Bgy. Y, City" clauses of the same line).
		list := m[1]
		if parts := msInTrimRe.Split(list, -1); len(parts) > 1 {
			list = parts[len(parts)-1]
		}
		city := normalizePowerCity(m[2])
		msAddBarangays(dst, city, msSplitBarangayList(list))
	}
	if matched {
		return
	}
	if m := msBgyBareRe.FindStringSubmatch(line); m != nil {
		if msInTrimRe.MatchString(m[1]) {
			return
		}
		city := fallbackCity
		list := m[1]
		// A trailing ", City" names the city the list belongs to.
		if parts := strings.Split(list, ","); len(parts) > 1 {
			if last := strings.TrimSpace(parts[len(parts)-1]); msTrailingCityRe.MatchString(last) {
				city = last
				list = strings.Join(parts[:len(parts)-1], ",")
			}
		}
		msAddBarangays(dst, normalizePowerCity(city), msSplitBarangayList(list))
	}
}

// msLineRoads extracts road names after "Portion of" (first 3 distinct
// per item; ASCII hyphen).
func msLineRoads(line string, seen map[string]bool, out *[]string) {
	m := msPortionRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	rest := msFromCutRe.Split(m[1], 2)[0]
	rest = msNearCutRe.Split(rest, 2)[0]
	for _, semi := range strings.Split(rest, ";") {
		for _, comma := range strings.Split(semi, ",") {
			for _, amp := range msAndSplitRe.Split(comma, -1) {
				p := strings.TrimSpace(amp)
				if !msRoadEndRe.MatchString(p) {
					continue
				}
				p = strings.TrimSuffix(p, ".")
				p = msDashNormRe.ReplaceAllString(p, "-")
				p = spaceRe.ReplaceAllString(p, " ")
				if p == "" || seen[p] {
					continue
				}
				if len(*out) >= 3 {
					return
				}
				seen[p] = true
				*out = append(*out, p)
			}
		}
	}
}

// msNormCircuit lowercases for dedupe keys.
func msNormCircuit(s string) string {
	return msDashNormRe.ReplaceAllString(strings.ToLower(spaceRe.ReplaceAllString(strings.TrimSpace(s), " ")), "-")
}

// dedupeKeyOf builds the cross-page dedupe key: date range plus
// normalised circuit+window text (first URL wins).
func dedupeKeyOf(it PowerItem) string {
	var b strings.Builder
	b.WriteString(it.DateFrom)
	b.WriteString("|")
	b.WriteString(it.DateTo)
	for _, bl := range it.Blocks {
		b.WriteString("|")
		b.WriteString(msNormCircuit(bl.Circuit))
		for _, w := range bl.Windows {
			b.WriteString("|")
			b.WriteString(w.Start.Format(time.RFC3339))
			b.WriteString(">")
			b.WriteString(w.End.Format(time.RFC3339))
		}
	}
	return b.String()
}

// msSplitTopLevel splits on ";" outside parentheses (location
// segments; parentheticals may hold their own ";" lists).
func msSplitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + len(string(r))
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// parseMSLocation builds the ordered city list: tag city, title
// location segments (parentheticals count only when they name a
// city), then canonical cities mentioned in the body.
func parseMSLocation(location, tag string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" {
			return
		}
		f := foldAccent(c)
		if seen[f] {
			return
		}
		seen[f] = true
		out = append(out, c)
	}
	if tag = strings.TrimSpace(tag); tag != "" {
		add(normalizePowerCity(tag))
	}
	for _, seg := range msSplitTopLevel(location) {
		seg = strings.TrimSpace(seg)
		seg = regexp.MustCompile(`(?i)^(and|&)\s+`).ReplaceAllString(seg, "")
		main, paren := seg, ""
		if i := strings.Index(seg, "("); i >= 0 {
			main = strings.TrimSpace(seg[:i])
			paren = seg[i:]
			if j := strings.LastIndex(paren, ")"); j >= 0 {
				paren = paren[1:j]
			} else {
				paren = paren[1:]
			}
		}
		if main != "" {
			add(normalizePowerCity(main))
		}
		for _, ps := range strings.Split(paren, ";") {
			for _, tok := range msAndSplitRe.Split(ps, -1) {
				tok = strings.TrimSpace(tok)
				if tok == "" || !strings.Contains(foldAccent(tok), "city") {
					continue
				}
				add(normalizePowerCity(tok))
			}
		}
	}
	return out
}

// powerIDOf returns the stable ID: first 12 hex of sha1(URL slug).
func powerIDOf(rawURL string) string {
	slug := rawURL
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		slug = slug[i+1:]
	}
	slug = strings.TrimSpace(slug)
	sum := sha1.Sum([]byte(slug))
	return hex.EncodeToString(sum[:])[:12]
}

// parseMSRow parses one listing row chunk into a PowerItem.
func parseMSRow(url, title, tag, bodyHTML, reason string) PowerItem {
	title = msClean(title)
	it := PowerItem{URL: url, Title: title, Reason: reason}
	it.ReasonKey = powerReasonKeyOf(reason)
	it.ID = powerIDOf(url)
	if tag = strings.TrimSpace(tag); tag != "" {
		it.Tags = []string{tag}
	}
	dateFrom, dateTo := "", ""
	if m := msTitleSplitRe.FindStringSubmatch(title); m != nil {
		var ok bool
		dateFrom, dateTo, ok = parseMSTitleDates(msClean(m[1]))
		if ok {
			it.DateFrom, it.DateTo = dateFrom, dateTo
		}
		it.Cities = parseMSLocation(m[2], tag)
	} else {
		it.Cities = parseMSLocation("", tag)
	}
	// Canonical cities mentioned in the body count too (a line
	// mentioning a city that is not the tag still assigns its
	// barangays to the mentioned city).
	bodyText := strings.Join(msLines(bodyHTML), "\n")
	foldedBody := foldAccent(bodyText)
	for _, c := range []string{"Las Piñas", "Parañaque", "Muntinlupa", "Bacoor"} {
		if !strings.Contains(foldedBody, foldAccent(c)) {
			continue
		}
		dup := false
		for _, e := range it.Cities {
			if foldAccent(e) == foldAccent(c) {
				dup = true
				break
			}
		}
		if !dup {
			it.Cities = append(it.Cities, c)
		}
	}
	multiDay := it.DateFrom != "" && it.DateTo != "" && it.DateFrom != it.DateTo
	strongs := msStrongRe.FindAllStringSubmatchIndex(bodyHTML, -1)
	if len(strongs) == 0 {
		return it
	}
	frag := func(a, b int) string {
		if a > b {
			return ""
		}
		return bodyHTML[a:b]
	}
	primaryCity := ""
	if len(it.Cities) > 0 {
		primaryCity = it.Cities[0]
	}
	for i, sm := range strongs {
		header := cellText(bodyHTML[sm[2]:sm[3]])
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(header)), "BETWEEN") {
			continue
		}
		body := ""
		if i+1 < len(strongs) {
			body = frag(sm[1], strongs[i+1][0])
		} else {
			body = frag(sm[1], len(bodyHTML))
		}
		if i == 0 {
			if pre := msLines(frag(0, sm[0])); len(pre) > 0 {
				body = strings.Join(pre, "\n") + "\n" + body
			}
		}
		circuit, trail := msCircuitAndTrail(header)
		lines := msLines(body)
		if trail != "" {
			lines = append([]string{trail}, lines...)
		}
		block := PowerBlock{Circuit: circuit, AreaLines: lines}
		block.Barangays = map[string][]string{}
		roadSeen := map[string]bool{}
		for _, ln := range lines {
			msLineBarangays(ln, primaryCity, block.Barangays)
			msLineRoads(ln, roadSeen, &block.Roads)
		}
		// Roads are collected item-wide (first 3 distinct).
		segments := msSplitSegments(header)
		var wins []TimeWindow
		ambiguous := false
		for _, seg := range segments {
			clocks := msClockRe.FindAllStringSubmatch(seg, -1)
			var dates []string
			for _, dm := range msInlineDateRe.FindAllStringSubmatch(seg, -1) {
				if ds, ok := parseMSInlineDate(dm[1], dm[2], dm[3]); ok {
					dates = append(dates, ds)
				}
			}
			if len(dates) == 0 && multiDay {
				ambiguous = true
				continue
			}
			wins = append(wins, msWindowTimes(clocks, dates, it.DateFrom, it.DateTo)...)
		}
		if ambiguous {
			block.Daily, block.DailyFrom, block.DailyTo = true, it.DateFrom, it.DateTo
			block.DateAmbiguous = true
			// Nightly repeats over the title range for display.
			for _, seg := range segments {
				clocks := msClockRe.FindAllStringSubmatch(seg, -1)
				wins = append(wins, msDailyWindows(clocks, it.DateFrom, it.DateTo)...)
			}
		}
		block.Windows = wins
		if len(block.Barangays) == 0 {
			block.Barangays = nil
		}
		it.Blocks = append(it.Blocks, block)
	}
	if len(it.Blocks) > 0 {
		for _, bl := range it.Blocks {
			if bl.Daily {
				it.Daily = true
				it.DailyFrom = bl.DailyFrom
				it.DailyTo = bl.DailyTo
			}
			if bl.DateAmbiguous {
				it.DateAmbiguous = true
			}
		}
	}
	// Item-wide roads live on blocks (first 3 distinct per block,
	// in area-line order); alerts union them.
	return it
}

// ParseMeralcoMaintenance parses a maintenance-schedule listing page
// into items (document order, deduped by date range and normalised
// circuit+window text keeping the first URL). Zero rows is an empty
// (not error) result; only empty input is an error.
func ParseMeralcoMaintenance(page []byte) (MeralcoMSPage, error) {
	if len(strings.TrimSpace(string(page))) == 0 {
		return MeralcoMSPage{}, fmt.Errorf("utilities: meralco-ms: empty page")
	}
	locs := msTitleAnchorRe.FindAllStringSubmatchIndex(string(page), -1)
	var out MeralcoMSPage
	seen := map[string]bool{}
	for i, loc := range locs {
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		chunk := string(page[loc[0]:end])
		if pi := strings.Index(chunk, `<ul class="js-pager`); pi >= 0 {
			chunk = chunk[:pi]
		}
		url := strings.TrimSpace(string(page[loc[2]:loc[3]]))
		title := cellText(string(page[loc[4]:loc[5]]))
		tag := ""
		if m := msTagRe.FindStringSubmatch(chunk); m != nil {
			tag = cellText(m[1])
		}
		bodyHTML := ""
		if m := msBodyRe.FindStringSubmatch(chunk); m != nil {
			bodyHTML = m[1]
		}
		reason := ""
		if m := msReasonMarkRe.FindStringSubmatch(chunk); m != nil {
			reason = cellText(m[1])
			reason = regexp.MustCompile(`(?i)^[\s>"]*reason\s*:?\s*`).ReplaceAllString(reason, "")
		}
		it := parseMSRow(url, title, tag, bodyHTML, reason)
		key := dedupeKeyOf(it)
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Items = append(out.Items, it)
	}
	if out.Items == nil {
		out.Items = []PowerItem{}
	}
	return out, nil
}

// msPageFileName maps (city filter, page) to the offline fixture name
// (all_pN.html / cityID_pN.html).
func msPageFileName(cityID, page int) string {
	if cityID == 0 {
		return fmt.Sprintf("all_p%d.html", page)
	}
	return fmt.Sprintf("city%d_p%d.html", cityID, page)
}

// msPageURL builds the listing URL (cityID 0 = unfiltered).
func (f *fetcher) msPageURL(cityID, page int) string {
	if cityID == 0 {
		return fmt.Sprintf("%s%s?page=%d", f.msBase, MeralcoMSPath, page)
	}
	return fmt.Sprintf("%s%s?field_service_maintenance_loc_target_id=%d&page=%d", f.msBase, MeralcoMSPath, cityID, page)
}

// fetchPower runs the Fetch section "power" (name "meralco-ms").
//
// Request plan (cap 24 requests/run): (a) the four city filters page
// 0; (b) the Las Piñas (215) filter walked page by page until an
// item is older than 40 days or 8 pages; (c) the unfiltered list
// from page 0, walking until a page has only items whose DateTo is
// before today or 12 pages. Offline (MeralcoMSDir) reads all_pN.html
// / cityID_pN.html instead of HTTP.
func (f *fetcher) fetchPower(ctx context.Context) {
	if f.msDir == "" && !f.ms {
		return
	}
	nowManila := f.now.In(manilaLoc)
	today := nowManila.Format("2006-01-02")
	cutoff40 := nowManila.Add(-40 * 24 * time.Hour).Format("2006-01-02")
	horizon14 := nowManila.Add(14 * 24 * time.Hour).Format("2006-01-02")
	checkedAt := f.snap.FetchedAt

	powerReqs := 0
	var totalRows, goodItems int
	unfilteredP0OK, unfilteredP0Zero := false, false
	seen := map[string]bool{}
	var all []PowerItem
	addItems := func(items []PowerItem) {
		totalRows += len(items)
		for _, it := range items {
			if it.DateFrom != "" {
				for _, bl := range it.Blocks {
					if len(bl.Windows) > 0 {
						goodItems++
						break
					}
				}
			}
			key := dedupeKeyOf(it)
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, it)
		}
	}
	hasOld40 := func(items []PowerItem) bool {
		for _, it := range items {
			if it.DateTo != "" && it.DateTo < cutoff40 {
				return true
			}
		}
		return false
	}
	allOld := func(items []PowerItem) bool {
		if len(items) == 0 {
			return true
		}
		for _, it := range items {
			if it.DateTo == "" || it.DateTo >= today {
				return false
			}
		}
		return true
	}
	// getPage loads one listing page: HTTP (polite, one attempt) or
	// the offline directory. Missing offline files end the walk leg.
	getPage := func(cityID, page int) ([]PowerItem, bool) {
		if f.msDir != "" {
			name := msPageFileName(cityID, page)
			data, err := os.ReadFile(filepath.Join(f.msDir, name))
			if err != nil {
				return nil, false
			}
			pg, perr := ParseMeralcoMaintenance(data)
			if perr != nil {
				f.fail("meralco-ms offline %s: %v", name, perr)
				return nil, false
			}
			u := f.msPageURL(cityID, page)
			f.source("meralco-ms", u, 200, len(data), sha256Sum(data), time.Now(), nil)
			return pg.Items, true
		}
		if powerReqs >= 24 {
			return nil, false
		}
		u := f.msPageURL(cityID, page)
		powerReqs++
		body, ok := f.getPage(ctx, "meralco-ms", u)
		if !ok {
			return nil, false
		}
		pg, perr := ParseMeralcoMaintenance(body)
		if perr != nil {
			f.fail("meralco-ms %s: %v", u, perr)
			return nil, false
		}
		return pg.Items, true
	}
	// (a) the four city filters, page 0.
	for _, id := range MeralcoMSCityIDs {
		items, ok := getPage(id, 0)
		if !ok {
			continue
		}
		addItems(items)
	}
	// (b) Las Piñas history for the monthly trend.
	for p := 1; p < 8; p++ {
		items, ok := getPage(MeralcoMSLasPinasID, p)
		if !ok {
			break
		}
		addItems(items)
		if hasOld40(items) {
			break
		}
	}
	// (c) unfiltered list from page 0.
	for p := 0; p < 12; p++ {
		items, ok := getPage(0, p)
		if !ok {
			break
		}
		if p == 0 {
			unfilteredP0OK = true
			unfilteredP0Zero = len(items) == 0
		}
		addItems(items)
		if allOld(items) {
			break
		}
	}
	if f.msDir == "" && powerReqs == 0 && len(all) == 0 && !unfilteredP0OK {
		return // every request failed: keep the previous section
	}
	if (unfilteredP0OK && unfilteredP0Zero) || (totalRows > 0 && goodItems == 0) {
		// Layout-change alarm: a 200 with zero parsed rows on
		// unfiltered page 0, or rows but no parsable date+window
		// across the whole run. Keep previous items (merge), make
		// every power alert unavailable while set.
		var msg string
		if unfilteredP0OK && unfilteredP0Zero {
			msg = "meralco-ms: layout changed (unfiltered page 0 has no rows; keeping previous items)"
		} else {
			msg = "meralco-ms: layout changed (no parsable date+window across the run; keeping previous items)"
		}
		f.snap.Power = &PowerStatus{CheckedAt: checkedAt, LayoutAlarm: msg}
		f.fail("%s", msg)
		fmt.Fprintln(os.Stderr, "utilities fetch: WARNING "+msg)
		f.stampSection("power")
		return
	}
	if len(all) == 0 {
		return // nothing usable: keep the previous section
	}
	for i := range all {
		if all[i].DateFrom != "" && all[i].DateFrom > horizon14 {
			all[i].Beyond14d = true
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].DateFrom != all[j].DateFrom {
			return all[i].DateFrom > all[j].DateFrom
		}
		if all[i].DateTo != all[j].DateTo {
			return all[i].DateTo > all[j].DateTo
		}
		return all[i].ID < all[j].ID
	})
	historyFrom := ""
	for _, it := range all {
		if !isPowerLasPinas(it.Cities) || it.DateFrom == "" {
			continue
		}
		if historyFrom == "" || it.DateFrom < historyFrom {
			historyFrom = it.DateFrom
		}
	}
	f.snap.Power = &PowerStatus{CheckedAt: checkedAt, Items: all, HistoryFrom: historyFrom}
	f.stampSection("power")
}

// powerMonthStart returns the YYYY-MM-DD first day of month.
func powerMonthStart(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// BuildPowerAlerts builds postable alerts for Las Piñas only: items
// mentioning Las Piñas (tag, title or body), skipping ambiguous or
// windowless items. N is the rank among distinct Las Piñas outage
// dates in the alert's Manila month; PrevMonthCount is the previous
// month's count, only when the history walk reached that month's
// start (HistoryComplete). A set layout alarm yields no alerts.
func BuildPowerAlerts(p *PowerStatus, now time.Time) []PowerAlert {
	if p == nil || p.LayoutAlarm != "" {
		return nil
	}
	type cand struct {
		item       PowerItem
		windows    []TimeWindow
		start, end time.Time
	}
	var cands []cand
	for _, it := range p.Items {
		if !isPowerLasPinas(it.Cities) || it.DateAmbiguous {
			continue
		}
		var wins []TimeWindow
		for _, bl := range it.Blocks {
			wins = append(wins, bl.Windows...)
		}
		if len(wins) == 0 {
			continue
		}
		start, end := wins[0].Start, wins[0].End
		for _, w := range wins[1:] {
			if w.Start.Before(start) {
				start = w.Start
			}
			if w.End.After(end) {
				end = w.End
			}
		}
		cands = append(cands, cand{item: it, windows: wins, start: start, end: end})
	}
	if len(cands) == 0 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool {
		if !cands[i].start.Equal(cands[j].start) {
			return cands[i].start.Before(cands[j].start)
		}
		return cands[i].item.ID < cands[j].item.ID
	})
	monthOf := func(t time.Time) string { return t.In(manilaLoc).Format("2006-01") }
	var out []PowerAlert
	for _, c := range cands {
		month := monthOf(c.start)
		dates := map[string]bool{}
		for _, o := range cands {
			if monthOf(o.start) != month || o.start.After(c.start) {
				continue
			}
			if o.item.DateFrom != "" {
				dates[o.item.DateFrom] = true
			}
		}
		n := len(dates)
		prev := PrevMonth(month)
		prevDates := map[string]bool{}
		for _, o := range cands {
			if monthOf(o.start) != prev || o.item.DateFrom == "" {
				continue
			}
			prevDates[o.item.DateFrom] = true
		}
		prevStart := powerMonthStart(prev)
		complete := p.HistoryFrom != "" && prevStart != "" && p.HistoryFrom <= prevStart
		prevCount := 0
		if complete {
			prevCount = len(prevDates)
		}
		var barangays []string
		seenB := map[string]bool{}
		for _, bl := range c.item.Blocks {
			for _, b := range bl.Barangays["Las Piñas"] {
				if b == "" || seenB[b] {
					continue
				}
				seenB[b] = true
				barangays = append(barangays, b)
			}
		}
		if len(barangays) == 0 {
			// No barangay list in the area lines: use the area name
			// from the title (or slug) parenthetical.
			barangays = msTitleAreas(c.item.Title, c.item.URL)
		}
		if len(barangays) == 0 {
			barangays = []string{PowerAreasTL}
		}
		roadSeen := map[string]bool{}
		var roads []string
		for _, bl := range c.item.Blocks {
			for _, r := range bl.Roads {
				if r == "" || roadSeen[r] || len(roads) >= 3 {
					continue
				}
				roadSeen[r] = true
				roads = append(roads, r)
			}
			if len(roads) >= 3 {
				break
			}
		}
		out = append(out, PowerAlert{
			ID:              c.item.ID,
			City:            "Las Piñas",
			Start:           c.start,
			End:             c.end,
			Windows:         c.windows,
			Daily:           c.item.Daily,
			DailyFrom:       c.item.DailyFrom,
			DailyTo:         c.item.DailyTo,
			Barangays:       barangays,
			Roads:           roads,
			ReasonKey:       c.item.ReasonKey,
			SourceURL:       c.item.URL,
			NthThisMonth:    n,
			PrevMonthCount:  prevCount,
			HistoryComplete: complete,
		})
	}
	return out
}

var (
	msTitleLPRe     = regexp.MustCompile(`(?i)Las\s+Pi[nñ]as(?:\s+City)?\s*\(([^)]*)\)`)
	msTitleAndRe    = regexp.MustCompile(`(?i)\s*;?\s*\band\b\s*`)
	msSlugLPRe      = regexp.MustCompile(`(?i)las-pinas-city-(.+?)(?:-0)?$`)
	msRomanWordRe   = regexp.MustCompile(`(?i)^(i|ii|iii|iv|v|vi|vii|viii|ix|x)$`)
	msSlugAreaBadRe = regexp.MustCompile(`(?i)\b(city|cavite|laguna|bulacan|manila|muntinlupa|paranaque|bacoor)\b`)
)

// msAreaWords title-cases hyphen/space separated words, keeping Roman
// numerals upper case ("talon-i" -> "Talon I", "Ii" -> "II").
func msAreaWords(words []string) string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		if msRomanWordRe.MatchString(w) {
			out = append(out, strings.ToUpper(w))
			continue
		}
		r := []rune(strings.ToLower(w))
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		out = append(out, string(r))
	}
	return strings.Join(out, " ")
}

// msTitleAreas returns the Las Piñas area names from the item title
// parenthetical ("... - Las Piñas City (Talon I; And Talon V)") or,
// when the title has none, from the URL slug
// ("july-22-23-2026-las-pinas-city-talon-i"). Nil when nothing is
// found (the caller then uses the "some barangays" marker).
func msTitleAreas(title, url string) []string {
	var raw string
	if m := msTitleLPRe.FindStringSubmatch(title); m != nil {
		raw = m[1]
	} else {
		slug := url
		if i := strings.LastIndex(slug, "/"); i >= 0 {
			slug = slug[i+1:]
		}
		m := msSlugLPRe.FindStringSubmatch(strings.TrimSpace(slug))
		if m == nil {
			return nil
		}
		raw = strings.ReplaceAll(m[1], "-", " ")
	}
	raw = msTitleAndRe.ReplaceAllString(raw, ",")
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := msAreaWords(strings.Fields(strings.Trim(part, " .;:-")))
		if name == "" || msSlugAreaBadRe.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
