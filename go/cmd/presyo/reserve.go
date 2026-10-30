package main

import (
	"fmt"
	"os"
	"time"
)

// Traffic slot reservation (TRAFFIC_SPEC.md schedule proposal: weekdays
// 07:00 and 17:30 Manila). The traffic post is gated off in production, so
// this hold is OFF unless PRESYO_TRAFFIC_RESERVE=1. When on, planned posts
// (DA digest, routine utilities) stay out of the minGap window before a
// slot that has not been posted yet, so the 75 min spacing rule can never
// make the traffic map miss its slot.
type trafficSlot struct {
	key       string // state as_of suffix: <YYYY-MM-DD>-<key>
	at        int    // slot time, minutes since midnight
	windowEnd int    // end of the slot's posting window (exclusive)
}

var trafficSlots = []trafficSlot{
	{"am", 7 * 60, 7*60 + 40},
	{"pm", 17*60 + 30, 18*60 + 10},
}

// trafficReserveOn reports whether the reservation is enabled.
func trafficReserveOn() bool { return os.Getenv("PRESYO_TRAFFIC_RESERVE") == "1" }

// trafficHold returns a skip reason while a weekday traffic slot should be
// protected ("" when planned posts may go ahead).
func trafficHold(manila time.Time, st *fbPostState, minGap, grace time.Duration) string {
	if !trafficReserveOn() {
		return ""
	}
	if wd := manila.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return ""
	}
	tod := manila.Hour()*60 + manila.Minute()
	lead := int((minGap - grace) / time.Minute)
	for _, s := range trafficSlots {
		if tod < s.at-lead || tod >= s.windowEnd {
			continue
		}
		key := manila.Format("2006-01-02") + "-" + s.key
		done := false
		for _, p := range st.Posts {
			if p.Region == "traffic" && p.AsOf == key {
				done = true
				break
			}
		}
		if !done {
			return fmt.Sprintf("reserved for the %s traffic slot", s.key)
		}
	}
	return ""
}
