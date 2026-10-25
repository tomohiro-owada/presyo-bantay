package utilities

import (
	"fmt"
	"strings"
	"time"
)

// monthByName maps English month names (case-insensitive) to months.
var monthByName = map[string]time.Month{
	"january": time.January, "february": time.February,
	"march": time.March, "april": time.April, "may": time.May,
	"june": time.June, "july": time.July, "august": time.August,
	"september": time.September, "october": time.October,
	"november": time.November, "december": time.December,
}

// parseMonthDayYear builds YYYY-MM-DD from day/month-name/year parts.
func parseMonthDayYear(day, month, year string) (string, error) {
	m, ok := monthByName[strings.ToLower(month)]
	if !ok {
		return "", fmt.Errorf("bad month %q", month)
	}
	var d, y int
	if _, err := fmt.Sscanf(day, "%d", &d); err != nil {
		return "", fmt.Errorf("bad day %q", day)
	}
	if _, err := fmt.Sscanf(year, "%d", &y); err != nil {
		return "", fmt.Errorf("bad year %q", year)
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || t.Month() != m || t.Year() != y {
		return "", fmt.Errorf("bad date %s %s %s", day, month, year)
	}
	return t.Format("2006-01-02"), nil
}
