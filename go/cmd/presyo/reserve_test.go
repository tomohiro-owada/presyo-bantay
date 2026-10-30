package main

import (
	"testing"
	"time"
)

func TestTrafficHold(t *testing.T) {
	at := func(s string) time.Time {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm.In(manilaZone)
	}
	gap, grace := 75*time.Minute, 5*time.Minute
	empty := &fbPostState{}
	posted := &fbPostState{Posts: []fbPostEntry{{Region: "traffic", AsOf: "2026-10-26-am", PostedAt: "2026-10-26T07:00:10+08:00"}}}

	// Off unless PRESYO_TRAFFIC_RESERVE=1.
	t.Setenv("PRESYO_TRAFFIC_RESERVE", "")
	if why := trafficHold(at("2026-10-26T06:30:00+08:00"), empty, gap, grace); why != "" {
		t.Fatalf("hold while off: %q", why)
	}
	t.Setenv("PRESYO_TRAFFIC_RESERVE", "1")
	for _, tc := range []struct {
		name string
		now  string
		st   *fbPostState
		hold bool
	}{
		{"mon 05:49 before lead", "2026-10-26T05:49:00+08:00", empty, false},
		{"mon 05:50 in lead", "2026-10-26T05:50:00+08:00", empty, true},
		{"mon 07:00 slot not yet posted", "2026-10-26T07:00:00+08:00", empty, true},
		{"mon 07:39 slot still pending", "2026-10-26T07:39:00+08:00", empty, true},
		{"mon 07:40 window over", "2026-10-26T07:40:00+08:00", empty, false},
		{"mon 07:30 slot posted", "2026-10-26T07:30:00+08:00", posted, false},
		{"mon 12:00 midday free", "2026-10-26T12:00:00+08:00", empty, false},
		{"mon 16:19 before pm lead", "2026-10-26T16:19:00+08:00", empty, false},
		{"mon 16:20 pm lead", "2026-10-26T16:20:00+08:00", empty, true},
		{"mon 18:10 pm window over", "2026-10-26T18:10:00+08:00", empty, false},
		{"sat 06:30 weekend free", "2026-10-31T06:30:00+08:00", empty, false},
		{"sun 17:00 weekend free", "2026-11-01T17:00:00+08:00", empty, false},
	} {
		if why := trafficHold(at(tc.now), tc.st, gap, grace); (why != "") != tc.hold {
			t.Errorf("%s: hold=%q want %v", tc.name, why, tc.hold)
		}
	}
}
