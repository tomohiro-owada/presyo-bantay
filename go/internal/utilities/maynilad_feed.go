package utilities

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Maynilad weekly schedule from the WordPress RSS feed
// ({MayniladBase}/feed/). Weekly posts are titled "Maintenance
// activities for the upcoming week" (also seen singular "activity").
// content:encoded holds one figure/table with a header row
// (City | Barangay | Schedule | Activity | Location of Activity;
// column order changed between posts) and cells with rowspan, or
// blank/missing City cells that inherit the previous city. Our
// wording only; barangay names and times are relayed as facts.

// weeklyTitleRe matches the weekly maintenance post titles.
var weeklyTitleRe = regexp.MustCompile(`(?i)maintenance activit(y|ies) for the upcoming week`)

// rssFeed captures the subset of RSS needed for the weekly schedule.
type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
	Encoded string `xml:"encoded"`
}

// MaintSchedule is the parsed form of a Maintenance.Schedule string.
type MaintSchedule struct {
	Raw        string
	Daily      bool
	DailyFrom  string // YYYY-MM-DD
	DailyTo    string // YYYY-MM-DD
	ClockStart string // "11:01 PM"
	ClockEnd   string // "3:00 AM"
}

// normalizeFeedCity maps feed city cells to our canonical names.
func normalizeFeedCity(s string) string {
	t := strings.TrimSpace(s)
	f := foldAccent(t)
	// Strip common suffixes for matching.
	base := f
	base = strings.TrimSuffix(base, " city")
	base = strings.TrimSuffix(base, ", cavite")
	base = strings.TrimSpace(base)
	switch base {
	case "las pinas":
		return "Las Piñas"
	case "paranaque":
		return "Parañaque"
	case "muntinlupa":
		return "Muntinlupa"
	case "bacoor":
		return "Bacoor"
	default:
		return t
	}
}

// feedNearbyCities are the cities detected in Barangay/Location cells.
var feedNearbyCities = []string{"Las Piñas", "Parañaque", "Muntinlupa", "Bacoor"}

// alsoCitiesOf returns normalised cities (other than own) named in the
// barangay or location text.
func alsoCitiesOf(own, barangays, location string) []string {
	hay := foldAccent(barangays + " " + location)
	ownF := foldAccent(own)
	var out []string
	for _, c := range feedNearbyCities {
		cf := foldAccent(c)
		if cf == ownF {
			continue
		}
		if cityMentioned(hay, cf) {
			out = append(out, c)
		}
	}
	return out
}

// cityMentioned reports whether the folded city name appears in the
// folded haystack (accepting "bacoor city", "paranaque", ...).
func cityMentioned(hay, cityFold string) bool {
	if strings.Contains(hay, cityFold) {
		return true
	}
	return false
}

// rowspanRe extracts rowspan="N" from a td/th open tag.
var rowspanRe = regexp.MustCompile(`(?i)rowspan\s*=\s*["']?(\d+)`)

// feedCell is one parsed table cell.
type feedCell struct {
	text    string
	rowspan int
}

// feedRowCells splits a <tr> into cells with rowspan counts.
func feedRowCells(rowHTML string) []feedCell {
	var out []feedCell
	// Find each td/th element with its attributes and inner HTML.
	re := regexp.MustCompile(`(?is)<t[dh]([^>]*)>(.*?)</t[dh]>`)
	for _, m := range re.FindAllStringSubmatch(rowHTML, -1) {
		attrs, inner := m[1], m[2]
		n := 1
		if rm := rowspanRe.FindStringSubmatch(attrs); rm != nil {
			fmt.Sscanf(rm[1], "%d", &n)
			if n < 1 {
				n = 1
			}
		}
		out = append(out, feedCell{text: cellText(inner), rowspan: n})
	}
	return out
}

// feedHeader maps a header row to column indexes.
type feedHeader struct {
	city, barangay, schedule, activity, location int
	ok                                           bool
}

// parseFeedHeader maps header names (case-insensitive;
// "Location of Activity" -> Location, "Type of Activity" -> Activity).
func parseFeedHeader(cells []feedCell) feedHeader {
	var h feedHeader
	h.city, h.barangay, h.schedule, h.activity, h.location = -1, -1, -1, -1, -1
	for i, c := range cells {
		switch strings.ToLower(strings.TrimSpace(c.text)) {
		case "city":
			h.city = i
		case "barangay":
			h.barangay = i
		case "schedule":
			h.schedule = i
		case "activity", "type of activity":
			h.activity = i
		case "location of activity", "location":
			h.location = i
		}
	}
	h.ok = h.city >= 0 && h.barangay >= 0 && h.schedule >= 0 && h.activity >= 0 && h.location >= 0
	return h
}

// feedPubDate converts pubDate to a Manila YYYY-MM-DD date.
func feedPubDate(pub string) string {
	pub = strings.TrimSpace(pub)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if t, err := time.Parse(layout, pub); err == nil {
			return t.In(manilaLoc).Format("2006-01-02")
		}
	}
	return pub
}

// ParseMayniladFeedDetailed parses all weekly posts and all cities
// from an RSS feed body. Tables without a parsable header are skipped
// and counted in skipped. Error only when the body is not RSS/XML at
// all; zero weekly posts is an empty (not error) result.
func ParseMayniladFeedDetailed(body []byte) (rows []Maintenance, skipped int, err error) {
	var feed rssFeed
	if xerr := xml.Unmarshal(body, &feed); xerr != nil {
		return nil, 0, fmt.Errorf("utilities: maynilad feed: %w", xerr)
	}
	// A body that parses as XML but has no channel is not an RSS feed.
	if len(feed.Channel.Items) == 0 {
		trimmed := strings.TrimSpace(string(body))
		if !strings.Contains(trimmed, "<rss") && !strings.Contains(trimmed, "<feed") && !strings.Contains(trimmed, "<channel") {
			return nil, 0, fmt.Errorf("utilities: maynilad feed: not RSS/XML")
		}
		return nil, 0, nil
	}
	for _, it := range feed.Channel.Items {
		if !weeklyTitleRe.MatchString(it.Title) {
			continue
		}
		postDate := feedPubDate(it.PubDate)
		tables := tableRe.FindAllString(it.Encoded, -1)
		if len(tables) == 0 {
			tables = tableRe.FindAllString(it.Title, -1)
		}
		foundTable := false
		for _, tb := range tables {
			trs := trRe.FindAllString(tb, -1)
			if len(trs) == 0 {
				continue
			}
			hdr := parseFeedHeader(feedRowCells(trs[0]))
			if !hdr.ok {
				// Header row uses td cells; also try the first two
				// rows in case of a stray caption row.
				skipped++
				continue
			}
			foundTable = true
			ncols := len(feedRowCells(trs[0]))
			carryVal := make([]string, ncols)
			carryRem := make([]int, ncols)
			prevCity := ""
			for _, tr := range trs[1:] {
				cells := feedRowCells(tr)
				if len(cells) == 0 {
					continue
				}
				vals := make([]string, ncols)
				ci := 0
				for col := 0; col < ncols; col++ {
					if carryRem[col] > 0 {
						vals[col] = carryVal[col]
						carryRem[col]--
						if carryRem[col] == 0 {
							carryVal[col] = ""
						}
						continue
					}
					if ci < len(cells) {
						vals[col] = cells[ci].text
						if cells[ci].rowspan > 1 {
							carryVal[col] = cells[ci].text
							carryRem[col] = cells[ci].rowspan - 1
						}
						ci++
						continue
					}
					vals[col] = ""
				}
				cityRaw := vals[hdr.city]
				if strings.TrimSpace(cityRaw) == "" {
					cityRaw = prevCity
				}
				if strings.TrimSpace(cityRaw) == "" {
					continue
				}
				prevCity = cityRaw
				city := normalizeFeedCity(cityRaw)
				barangays := vals[hdr.barangay]
				schedule := vals[hdr.schedule]
				activity := vals[hdr.activity]
				location := vals[hdr.location]
				if strings.TrimSpace(schedule) == "" {
					continue
				}
				windows, sched := ParseMaintSchedule(schedule)
				rows = append(rows, Maintenance{
					PostDate:   postDate,
					PostURL:    strings.TrimSpace(it.Link),
					City:       city,
					Barangays:  strings.TrimSpace(barangays),
					Schedule:   strings.TrimSpace(schedule),
					Activity:   strings.TrimSpace(activity),
					Location:   strings.TrimSpace(location),
					AlsoCities: alsoCitiesOf(city, barangays, location),
					Windows:    windows,
					Daily:      sched.Daily,
					DailyFrom:  sched.DailyFrom,
					DailyTo:    sched.DailyTo,
				})
			}
		}
		_ = foundTable
	}
	if rows == nil {
		rows = []Maintenance{}
	}
	return rows, skipped, nil
}

// ParseMayniladFeed parses all weekly posts and all cities from an
// RSS feed body. Error only when the body is not RSS/XML at all;
// zero weekly posts is an empty result.
func ParseMayniladFeed(body []byte) ([]Maintenance, error) {
	rows, _, err := ParseMayniladFeedDetailed(body)
	return rows, err
}

// tableRe extracts <table> blocks from content:encoded HTML.
var tableRe = regexp.MustCompile(`(?is)<table[^>]*>.*?</table>`)

// --- schedule parsing (Manila time) ---

// hmToken is one parsed clock time.
type hmToken struct {
	h, mi int
}

// flexHMRe matches "10:00 p.m.", "10 p.m.", "3 a.m.", "12:01 a.m".
var flexHMRe = regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*([ap])\.?\s*m\.?`)

// parseFlexHM parses the first clock time in s.
func parseFlexHM(s string) (hmToken, bool) {
	m := flexHMRe.FindStringSubmatch(s)
	if m == nil {
		return hmToken{}, false
	}
	var h, mi int
	fmt.Sscanf(m[1], "%d", &h)
	mi = 0
	if m[2] != "" {
		fmt.Sscanf(m[2], "%d", &mi)
	}
	if h < 1 || h > 12 || mi > 59 {
		return hmToken{}, false
	}
	if strings.ToLower(m[3]) == "p" {
		if h != 12 {
			h += 12
		}
	} else if h == 12 {
		h = 0
	}
	return hmToken{h: h, mi: mi}, true
}

// flexHMs returns all clock times in s.
func flexHMs(s string) []hmToken {
	var out []hmToken
	for _, m := range flexHMRe.FindAllStringSubmatch(s, -1) {
		var h, mi int
		fmt.Sscanf(m[1], "%d", &h)
		mi = 0
		if m[2] != "" {
			fmt.Sscanf(m[2], "%d", &mi)
		}
		if h < 1 || h > 12 || mi > 59 {
			continue
		}
		if strings.ToLower(m[3]) == "p" {
			if h != 12 {
				h += 12
			}
		} else if h == 12 {
			h = 0
		}
		out = append(out, hmToken{h: h, mi: mi})
	}
	return out
}

// dateTokenRe matches "of August 26, 2026", "from January 25, 2026",
// ", June 12, 2026" and bare "March 22" fragments.
var dateTokenRe = regexp.MustCompile(`(?i)(?:of\s+|from\s+|,\s*)?([A-Za-z]+)\s+(\d{1,2})(?:\s*,?\s*(\d{4}))?`)

// parseDateToken resolves month/day/year (year may be inherited).
func parseDateToken(mon, day, year, defYear string) (string, bool) {
	if year == "" {
		year = defYear
	}
	if year == "" {
		return "", false
	}
	ds, err := parseMonthDayYear(day, mon, year)
	if err != nil {
		return "", false
	}
	return ds, true
}

// dailyRangeRe matches "from March 22 to March 29, 2026",
// "from June 8 to 17, 2026" and
// "from January 25, 2026 to February 1, 2026".
var (
	dailyBothYearRe = regexp.MustCompile(`(?i)from\s+([A-Za-z]+)\s+(\d{1,2})\s*,?\s*(\d{4})\s+to\s+([A-Za-z]+)\s+(\d{1,2})\s*,?\s*(\d{4})`)
	dailyBothMonRe  = regexp.MustCompile(`(?i)from\s+([A-Za-z]+)\s+(\d{1,2})\s+to\s+([A-Za-z]+)\s+(\d{1,2})\s*,?\s*(\d{4})`)
	dailyOneMonRe   = regexp.MustCompile(`(?i)from\s+([A-Za-z]+)\s+(\d{1,2})\s+to\s+(\d{1,2})\s*,?\s*(\d{4})`)
)

// dailyRange resolves the from/to dates of a daily schedule.
func dailyRange(s string) (from, to string, ok bool) {
	if m := dailyBothYearRe.FindStringSubmatch(s); m != nil {
		f, err1 := parseMonthDayYear(m[2], m[1], m[3])
		t, err2 := parseMonthDayYear(m[5], m[4], m[6])
		if err1 != nil || err2 != nil {
			return "", "", false
		}
		return f, t, true
	}
	if m := dailyBothMonRe.FindStringSubmatch(s); m != nil {
		f, err1 := parseMonthDayYear(m[2], m[1], m[5])
		t, err2 := parseMonthDayYear(m[4], m[3], m[5])
		if err1 != nil || err2 != nil {
			return "", "", false
		}
		return f, t, true
	}
	if m := dailyOneMonRe.FindStringSubmatch(s); m != nil {
		f, err1 := parseMonthDayYear(m[2], m[1], m[4])
		t, err2 := parseMonthDayYear(m[3], m[1], m[4])
		if err1 != nil || err2 != nil {
			return "", "", false
		}
		return f, t, true
	}
	return "", "", false
}

// clockText formats "11:01 PM".
func clockText(h, mi int) string {
	ap := "AM"
	if h >= 12 {
		ap = "PM"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d %s", h12, mi, ap)
}

// ParseMaintSchedule parses a feed Schedule string (Manila time):
//   - single window: "10:00 p.m. of August 26, 2026 to 1:00 p.m. of August 27, 2026"
//   - same-day short window: "1:00 p.m. to 7:00 p.m. of August 24, 2026"
//   - daily range: "11:01 p.m. to 3:00 a.m. daily, from March 22 to March 29, 2026"
//     (one TimeWindow per night, end next day when end<=start).
//
// Anything unparsable keeps the raw text with empty Windows.
func ParseMaintSchedule(s string) ([]TimeWindow, MaintSchedule) {
	raw := strings.TrimSpace(s)
	meta := MaintSchedule{Raw: raw}
	if raw == "" {
		return nil, meta
	}
	// Daily ranges first.
	if strings.Contains(strings.ToLower(raw), "daily") {
		hms := flexHMs(raw)
		if len(hms) == 2 {
			from, to, ok := dailyRange(raw)
			if ok {
				t1, ok1 := mustDate(from)
				t2, ok2 := mustDate(to)
				if ok1 && ok2 && !t2.Before(t1) {
					var out []TimeWindow
					for d := t1; !d.After(t2); d = d.Add(24 * time.Hour) {
						st := time.Date(d.Year(), d.Month(), d.Day(), hms[0].h, hms[0].mi, 0, 0, manilaLoc)
						en := time.Date(d.Year(), d.Month(), d.Day(), hms[1].h, hms[1].mi, 0, 0, manilaLoc)
						if !en.After(st) {
							en = en.Add(24 * time.Hour)
						}
						out = append(out, TimeWindow{Start: st, End: en})
					}
					if len(out) > 0 {
						meta.Daily = true
						meta.DailyFrom = from
						meta.DailyTo = to
						meta.ClockStart = clockText(hms[0].h, hms[0].mi)
						meta.ClockEnd = clockText(hms[1].h, hms[1].mi)
						return out, meta
					}
				}
			}
		}
		return nil, meta
	}
	// Single windows: split on " to ".
	idx := strings.Index(raw, " to ")
	if idx < 0 {
		return nil, meta
	}
	a, b := strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+4:])
	ahm, aok := parseFlexHM(a)
	bhm, bok := parseFlexHM(b)
	if !aok || !bok {
		return nil, meta
	}
	am := dateTokenRe.FindStringSubmatch(a)
	bm := dateTokenRe.FindStringSubmatch(b)
	var aDate, bDate string
	if am != nil {
		aDate, _ = parseDateToken(am[1], am[2], am[3], "")
	}
	if bm != nil {
		bDate, _ = parseDateToken(bm[1], bm[2], bm[3], "")
	}
	// Inherit the single date to the dateless side.
	if aDate == "" {
		aDate = bDate
	}
	if bDate == "" {
		bDate = aDate
	}
	if aDate == "" || bDate == "" {
		return nil, meta
	}
	ad, ok1 := mustDate(aDate)
	bd, ok2 := mustDate(bDate)
	if !ok1 || !ok2 {
		return nil, meta
	}
	start := time.Date(ad.Year(), ad.Month(), ad.Day(), ahm.h, ahm.mi, 0, 0, manilaLoc)
	end := time.Date(bd.Year(), bd.Month(), bd.Day(), bhm.h, bhm.mi, 0, 0, manilaLoc)
	if !end.After(start) {
		end = end.Add(24 * time.Hour)
	}
	return []TimeWindow{{Start: start, End: end}}, meta
}

// isFeedCity reports whether s normalises to want (accent-insensitive
// plus the AlsoCities form).
func isFeedCity(s, want string) bool {
	return foldAccent(normalizeFeedCity(s)) == foldAccent(want)
}

// SplitFeedRows applies retention and splits feed rows: Las Piñas rows
// (City or AlsoCities) go to main (WaterStatus.Maintenance); Parañaque,
// Muntinlupa and Bacoor rows go to nearby (WaterStatus.Nearby, site
// only, never posted). Retention keeps rows whose last window end is
// >= now-24h, or unparsed rows whose post is <= 8 days old.
func SplitFeedRows(rows []Maintenance, now time.Time) (main, nearby []Maintenance) {
	nowManila := now.In(manilaLoc)
	cutoff := nowManila.Add(-24 * time.Hour)
	for _, r := range rows {
		keep := false
		if len(r.Windows) > 0 {
			last := r.Windows[len(r.Windows)-1].End
			if !last.IsZero() && !last.Before(cutoff) {
				keep = true
			}
		} else {
			if d, ok := mustDate(r.PostDate); ok {
				// PostDate is a Manila date; keep posts <= 8 days old.
				pd := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, manilaLoc)
				today := time.Date(nowManila.Year(), nowManila.Month(), nowManila.Day(), 0, 0, 0, 0, manilaLoc)
				if int(today.Sub(pd).Hours()/24) <= 8 {
					keep = true
				}
			}
		}
		if !keep {
			continue
		}
		if isFeedCity(r.City, "Las Piñas") {
			main = append(main, r)
			continue
		}
		lasInAlso := false
		for _, c := range r.AlsoCities {
			if foldAccent(c) == "las pinas" {
				lasInAlso = true
				break
			}
		}
		if lasInAlso {
			main = append(main, r)
			continue
		}
		isNearby := isFeedCity(r.City, "Parañaque") || isFeedCity(r.City, "Muntinlupa") || isFeedCity(r.City, "Bacoor")
		if !isNearby {
			for _, c := range r.AlsoCities {
				if isFeedCity(c, "Parañaque") || isFeedCity(c, "Muntinlupa") || isFeedCity(c, "Bacoor") {
					isNearby = true
					break
				}
			}
		}
		if isNearby {
			nearby = append(nearby, r)
		}
	}
	if main == nil {
		main = []Maintenance{}
	}
	if nearby == nil {
		nearby = []Maintenance{}
	}
	return main, nearby
}
