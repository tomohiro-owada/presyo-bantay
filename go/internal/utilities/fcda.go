package utilities

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Maynilad water rate (FCDA notices) parsing. WP REST
// `/wp-json/wp/v2/posts?search=FCDA&per_page=6` posts carry text like:
// "FCDA equivalent to negative 0.02% of the Average Basic Charge of P
// 52.86/cu. m. or an average rebate of P 0.01/cu.m. … effective … on
// 01 October 2026" (₱ or P; "negative"/"rebate" → negative,
// "increase"/"additional" → positive).

// WaterRate is one quarterly FCDA notice.
type WaterRate struct {
	Effective string  `json:"effective"`
	AvgBasic  float64 `json:"avg_basic"`
	FCDAPct   float64 `json:"fcda_pct"`
	FCDA      float64 `json:"fcda"`
	Rate      float64 `json:"rate"`
	PostURL   string  `json:"post_url,omitempty"`
	Posted    string  `json:"posted,omitempty"`
}

var (
	fcdaPctRe  = regexp.MustCompile(`(?i)(negative|positive)?\s*([0-9][0-9,]*\.?[0-9]*)\s*%\s+of the Average Basic Charge`)
	fcdaBaseRe = regexp.MustCompile(`Average Basic Charge of [₱P]\s*([0-9][0-9,]*\.[0-9]+)\s*/cu`)
	fcdaAmtRe  = regexp.MustCompile(`(?i)(rebate|negative|increase|additional|positive)\w*\s+of [₱P]\s*([0-9][0-9,]*\.[0-9]+)\s*/cu\.?\s*m`)
	fcdaEffRe  = regexp.MustCompile(`on (\d{1,2}) ([A-Za-z]+) (\d{4})`)
)

// ParseFCDAPosts parses WP REST FCDA notice posts, newest first.
// Posts without FCDA figures are skipped.
func ParseFCDAPosts(body []byte) ([]WaterRate, error) {
	var posts []wpPost
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, fmt.Errorf("utilities: fcda posts: %w", err)
	}
	var out []WaterRate
	for _, p := range posts {
		text := postVisibleText(p.Content.Rendered)
		rate, ok := parseFCDAPost(text)
		if !ok {
			continue
		}
		rate.PostURL = p.Link
		rate.Posted = p.Date
		out = append(out, rate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Effective > out[j].Effective })
	if out == nil {
		return []WaterRate{}, nil
	}
	return out, nil
}

// postVisibleText strips tags and collapses whitespace of a WP HTML
// fragment.
func postVisibleText(htmlFrag string) string {
	s := tagRe.ReplaceAllString(htmlFrag, " ")
	s = nbspRe.ReplaceAllString(s, " ")
	return spaceRe.ReplaceAllString(strings.TrimSpace(s), " ")
}

// parseFCDAPost reads one notice's figures. ok=false when the text
// carries no FCDA figures.
func parseFCDAPost(text string) (WaterRate, bool) {
	var r WaterRate
	pm := fcdaPctRe.FindStringSubmatch(text)
	bm := fcdaBaseRe.FindStringSubmatch(text)
	am := fcdaAmtRe.FindStringSubmatch(text)
	if pm == nil || bm == nil || am == nil {
		return WaterRate{}, false
	}
	pct, err := parseAmount(pm[2])
	if err != nil {
		return WaterRate{}, false
	}
	if strings.EqualFold(pm[1], "negative") {
		pct = -pct
	}
	base, err := parseAmount(bm[1])
	if err != nil {
		return WaterRate{}, false
	}
	amt, err := parseAmount(am[2])
	if err != nil {
		return WaterRate{}, false
	}
	switch strings.ToLower(am[1]) {
	case "rebate", "negative":
		amt = -amt
	}
	r.FCDAPct, r.AvgBasic, r.FCDA = pct, base, amt
	r.Rate = base + amt
	// Effective date: "on 01 October 2026" after "effective".
	tail := text
	if i := strings.Index(strings.ToLower(text), "effective"); i >= 0 {
		tail = text[i:]
	}
	em := fcdaEffRe.FindStringSubmatch(tail)
	if em == nil {
		return WaterRate{}, false
	}
	eff, err := parseMonthDayYear(em[1], em[2], em[3])
	if err != nil {
		return WaterRate{}, false
	}
	r.Effective = eff
	return r, true
}

// WaterRateFor returns the newest notice with Effective <= the first
// day of month ("2006-01"), or nil.
func WaterRateFor(rates []WaterRate, month string) *WaterRate {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return nil
	}
	first := t.Format("2006-01-02")
	for i := range rates {
		if rates[i].Effective <= first {
			return &rates[i]
		}
	}
	return nil
}

// PrevMonth returns the YYYY-MM month before month.
func PrevMonth(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return ""
	}
	return t.AddDate(0, -1, 0).Format("2006-01")
}
