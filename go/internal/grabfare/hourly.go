package grabfare

import (
	"time"
)

// HourPoint is one hour-of-day reading (Manila) of the hourly chart.
type HourPoint struct {
	H   int      `json:"h"`
	Min float64  `json:"min"`
	Max float64  `json:"max"`
	Std *float64 `json:"std,omitempty"`
	// Era counts the pickup changes after this reading: 0 = the current
	// pickup, 1 = the one before it, ... Readings of different eras are not
	// comparable, so charts must not join them (a break marker is drawn).
	Era int `json:"era,omitempty"`
	// Carried marks a point filled forward from the last real reading (bike
	// chart only); charts draw it as part of the line but without a dot.
	Carried bool `json:"carried,omitempty"`
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
// When several readings fall in the same hour the one with the lowest era
// (the current pickup) wins, then the earliest captured (the watcher's :00
// capture). Readings from the future are ignored. Today keeps every era (the
// renderers draw a break marker); yesterday keeps only era-0 readings, which
// are the only ones comparable with today's.
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
		if prev, ok := best[k]; !ok || u.era < prev.era || (u.era == prev.era && u.captured.Before(prev.captured)) {
			best[k] = u
		}
	}
	collect := func(date string, era0Only bool) []HourPoint {
		out := []HourPoint{}
		for h := 0; h < 24; h++ {
			u, ok := best[key{date, h}]
			if !ok || (era0Only && u.era != 0) {
				continue
			}
			out = append(out, HourPoint{H: h, Min: u.min, Max: u.max, Std: u.standard, Era: u.era})
		}
		return out
	}
	return &HourlyOut{TodayDate: today, YesterdayDate: yesterday, Today: collect(today, false), Yesterday: collect(yesterday, true)}
}
