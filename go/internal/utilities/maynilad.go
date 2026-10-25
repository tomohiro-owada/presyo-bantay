package utilities

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Maynilad interruption + maintenance parsing. Interruption endpoints
// return a JSON string wrapping an HTML advisory table; the maintenance
// endpoint returns WP REST posts with HTML tables.

var (
	trRe    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	tdRe    = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	spaceRe = regexp.MustCompile(`\s+`)
	nbspRe  = regexp.MustCompile(`(?i)&nbsp;|&#160;|&#xa0;`)
	dateRe  = regexp.MustCompile(`(?i)^\s*([A-Za-z]+)\s+(\d{1,2}),\s*(\d{4})\s*$`)
)

// cellText strips tags, unescapes entities, and collapses whitespace.
func cellText(raw string) string {
	s := nbspRe.ReplaceAllString(raw, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return spaceRe.ReplaceAllString(strings.TrimSpace(s), " ")
}

// normDate converts "October 01, 2026" to YYYY-MM-DD; unparseable input
// comes back unchanged with ok=false.
func normDate(raw string) (string, bool) {
	m := dateRe.FindStringSubmatch(raw)
	if m == nil {
		return strings.TrimSpace(raw), false
	}
	d, err := parseMonthDayYear(m[2], m[1], m[3])
	if err != nil {
		return strings.TrimSpace(raw), false
	}
	return d, true
}

// ParseInterruptions parses one interruptions endpoint body (a JSON
// string containing an HTML table) for the given kind
// ("scheduled"|"emergency"|"rotational"). Columns: City, Barangay,
// Specific Area, From, To, Time, Reason; rotational tables have no To
// column. From/To are normalized to YYYY-MM-DD when parseable, with
// the raw text kept in FromRaw/ToRaw.
func ParseInterruptions(body []byte, kind string) ([]Interruption, error) {
	var inner string
	if err := json.Unmarshal(body, &inner); err != nil {
		return nil, fmt.Errorf("utilities: interruptions: %w", err)
	}
	out := []Interruption{}
	for _, tm := range trRe.FindAllStringSubmatch(inner, -1) {
		row := tm[1]
		if strings.Contains(strings.ToLower(row), "<th") {
			continue // header
		}
		cells := tdRe.FindAllStringSubmatch(row, -1)
		if len(cells) == 0 {
			continue
		}
		texts := make([]string, 0, len(cells))
		for _, c := range cells {
			texts = append(texts, cellText(c[1]))
		}
		var in Interruption
		in.Kind = kind
		switch len(texts) {
		case 7:
			in.City, in.Barangays, in.Area = texts[0], texts[1], texts[2]
			in.From, _ = normDate(texts[3])
			in.FromRaw = texts[3]
			in.To, _ = normDate(texts[4])
			in.ToRaw = texts[4]
			in.Time, in.Reason = texts[5], texts[6]
		case 6:
			// Rotational tables carry no To column.
			in.City, in.Barangays, in.Area = texts[0], texts[1], texts[2]
			in.From, _ = normDate(texts[3])
			in.FromRaw = texts[3]
			in.Time, in.Reason = texts[4], texts[5]
		default:
			continue
		}
		if in.City == "" {
			continue
		}
		out = append(out, in)
	}
	return out, nil
}

// wpPost is one WP REST post (subset of fields).
type wpPost struct {
	ID    int    `json:"id"`
	Date  string `json:"date"`
	Link  string `json:"link"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Content struct {
		Rendered string `json:"rendered"`
	} `json:"content"`
}

// foldAccent lowercases and strips the accents used in city names
// ("Las Piñas" == "Las Pinas").
func foldAccent(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'ñ':
			b.WriteRune('n')
		case 'á', 'à', 'â', 'ä':
			b.WriteRune('a')
		case 'é', 'è', 'ê', 'ë':
			b.WriteRune('e')
		case 'í', 'ì', 'î', 'ï':
			b.WriteRune('i')
		case 'ó', 'ò', 'ô', 'ö':
			b.WriteRune('o')
		case 'ú', 'ù', 'û', 'ü':
			b.WriteRune('u')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseMaintenancePosts parses WP REST maintenance-activity posts,
// returning rows whose City starts with cityPrefix
// (accent-insensitive). Only posts titled "Maintenance activities for
// the upcoming week" are read; rows omitting the City cell (rowspan)
// inherit the previous City.
func ParseMaintenancePosts(body []byte, cityPrefix string) ([]Maintenance, error) {
	var posts []wpPost
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, fmt.Errorf("utilities: maintenance posts: %w", err)
	}
	prefix := foldAccent(strings.TrimSpace(cityPrefix))
	out := []Maintenance{}
	for _, p := range posts {
		if cellText(p.Title.Rendered) != "Maintenance activities for the upcoming week" {
			continue
		}
		city := ""
		for _, tm := range trRe.FindAllStringSubmatch(p.Content.Rendered, -1) {
			row := tm[1]
			if strings.Contains(strings.ToLower(row), "<th") {
				continue
			}
			cells := tdRe.FindAllStringSubmatch(row, -1)
			if len(cells) == 0 {
				continue
			}
			texts := make([]string, 0, len(cells))
			for _, c := range cells {
				texts = append(texts, cellText(c[1]))
			}
			// Header row uses td cells in some posts.
			if len(texts) > 0 && strings.EqualFold(texts[0], "City") {
				continue
			}
			var rowCity, barangays, schedule, activity, location string
			switch len(texts) {
			case 5:
				rowCity, barangays, schedule, activity, location = texts[0], texts[1], texts[2], texts[3], texts[4]
				city = rowCity
			case 4:
				// City cell omitted (rowspan): inherit.
				rowCity = city
				barangays, schedule, activity, location = texts[0], texts[1], texts[2], texts[3]
			default:
				continue
			}
			if rowCity == "" {
				continue
			}
			if !strings.HasPrefix(foldAccent(rowCity), prefix) {
				continue
			}
			out = append(out, Maintenance{
				PostDate:  p.Date,
				PostURL:   p.Link,
				City:      rowCity,
				Barangays: barangays,
				Schedule:  schedule,
				Activity:  activity,
				Location:  location,
			})
		}
	}
	return out, nil
}
