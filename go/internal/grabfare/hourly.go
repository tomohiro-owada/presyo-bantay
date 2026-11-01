package grabfare

import (
	"sort"
	"time"
)

// HourPoint is one hour-of-day reading (Manila) of the hourly chart.
type HourPoint struct {
	H   int      `json:"h"`
	Min float64  `json:"min"`
	Max float64  `json:"max"`
	Std *float64 `json:"std,omitempty"`
}

// HourlyOut carries today's and yesterday's readings per Manila hour, for the
// site's hour-of-day chart. Missing hours are simply absent from the lists.
type HourlyOut struct {
	TodayDate     string      `json:"today_date"`
	YesterdayDate string      `json:"yesterday_date"`
	Today         []HourPoint `json:"today"`
	Yesterday     []HourPoint `json:"yesterday"`
}

// buildHourly groups the usable (plausible) readings by Manila date and hour.
// When several readings fall in the same hour the earliest one is used (the
// watcher's :00 capture). Readings from the future are ignored.
func buildHourly(usables []usable, now time.Time) *HourlyOut {
	nowM := now.In(Manila)
	today := nowM.Format("2006-01-02")
	yesterday := nowM.AddDate(0, 0, -1).Format("2006-01-02")
	type key struct {
		date string
		h    int
	}
	best := map[key]usable{}
	for _, u := range usables {
		t := u.captured.In(Manila)
		if t.After(now.Add(10 * time.Minute)) {
			continue
		}
		d := t.Format("2006-01-02")
		if d != today && d != yesterday {
			continue
		}
		k := key{d, t.Hour()}
		if prev, ok := best[k]; !ok || u.captured.Before(prev.captured) {
			best[k] = u
		}
	}
	collect := func(date string) []HourPoint {
		out := []HourPoint{}
		for h := 0; h < 24; h++ {
			if u, ok := best[key{date, h}]; ok {
				out = append(out, HourPoint{H: h, Min: u.min, Max: u.max, Std: u.standard})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].H < out[j].H })
		return out
	}
	return &HourlyOut{TodayDate: today, YesterdayDate: yesterday, Today: collect(today), Yesterday: collect(yesterday)}
}
