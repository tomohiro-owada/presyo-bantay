package grabfare

import (
	"testing"
	"time"
)

func hourlyUsable(hh, mm int, day int, min, max float64) usable {
	t := time.Date(2026, 11, day, hh, mm, 0, 0, Manila)
	std := min + 3
	return usable{captured: t, min: min, max: max, standard: &std}
}

func TestBuildHourlyGroupsByDayAndHour(t *testing.T) {
	now := time.Date(2026, 11, 2, 5, 12, 0, 0, Manila)
	us := []usable{
		hourlyUsable(5, 0, 2, 288, 346),
		hourlyUsable(4, 0, 2, 273, 327),
		hourlyUsable(0, 44, 2, 271, 325),
		hourlyUsable(0, 50, 2, 999, 999), // later reading in the same hour loses
		hourlyUsable(23, 0, 1, 308, 370),
		hourlyUsable(22, 36, 1, 313, 375),
		hourlyUsable(22, 13, 1, 314, 377), // earliest of 22h wins
		hourlyUsable(23, 0, 31, 1, 1),     // two days ago: dropped
		hourlyUsable(7, 0, 2, 1, 1),       // future: dropped
	}
	h := buildHourly(us, now)
	if h.TodayDate != "2026-11-02" || h.YesterdayDate != "2026-11-01" {
		t.Fatalf("dates %s %s", h.TodayDate, h.YesterdayDate)
	}
	hs := func(p []HourPoint) []int {
		var o []int
		for _, x := range p {
			o = append(o, x.H)
		}
		return o
	}
	if got := hs(h.Today); len(got) != 3 || got[0] != 0 || got[1] != 4 || got[2] != 5 {
		t.Errorf("today hours %v", got)
	}
	if h.Today[0].Min != 271 {
		t.Errorf("same hour must keep the earliest reading, got %v", h.Today[0])
	}
	if got := hs(h.Yesterday); len(got) != 2 || got[0] != 22 || got[1] != 23 {
		t.Errorf("yesterday hours %v", got)
	}
	if h.Yesterday[0].Min != 314 {
		t.Errorf("yesterday 22h = %v", h.Yesterday[0])
	}
	if h.Today[2].Std == nil || *h.Today[2].Std != 291 {
		t.Error("standard missing")
	}
}

func TestBuildHourlyEmptyListsNotNull(t *testing.T) {
	h := buildHourly(nil, time.Date(2026, 11, 2, 5, 12, 0, 0, Manila))
	if h.Today == nil || h.Yesterday == nil {
		t.Error("lists must be [] not null")
	}
}
