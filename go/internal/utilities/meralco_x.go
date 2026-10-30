package utilities

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Meralco emergency line-trouble posts from @meralco via X.
//
// A scheduled-maintenance advisory ("ADVISORY: May scheduled power
// interruptions ...") is NOT an emergency; only posts matching the
// "Your area in ... may have been affected by an emergency line
// trouble which/that occurred at ..." pattern yield events, and only
// when the area text mentions Las Pinas. Restoration/resolution posts
// never yield events.

// Meralco X inbox identity.
const (
	MeralcoXAccountURL = "https://x.com/meralco"
	MeralcoXAccount    = "meralco"
	MeralcoXAccountID  = "15371662"
)

// MeralcoXEvent is one parsed Las Pinas emergency line-trouble window.
type MeralcoXEvent struct {
	PostID       string    `json:"post_id"`
	PostURL      string    `json:"post_url"`
	PostedAt     time.Time `json:"posted_at"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end,omitempty"`
	City         string    `json:"city"`
	AreaText     string    `json:"area_text"`
	OccurredText string    `json:"occurred_text"`
	ETRHours     float64   `json:"etr_hours,omitempty"`
	OpenEnded    bool      `json:"open_ended,omitempty"`
}

// MeralcoXStatus is the fetched meralcox section.
type MeralcoXStatus struct {
	CheckedAt string          `json:"checked_at"`
	Events    []MeralcoXEvent `json:"events"`
}

var (
	meralcoXAreaRe = regexp.MustCompile(`(?i)your\s+area\s+in\s+(.+?)\s+may\s+have\s+been\s+affected\s+by\s+an\s+emergency\s+line\s+trouble\s+(?:which|that)\s+occurred\s+at\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)`)
	meralcoXETRRe  = regexp.MustCompile(`(?i)estimated\s+restoration\s+time\s+of\s+(\d+(?:\.\d+)?)\s*(?:hrs?|hours?)`)
)

// meralcoXRestorationMarkers are resolution phrases. An (a)-pattern
// post containing any of them is dropped; non-(a) posts never yield
// events anyway.
var meralcoXRestorationMarkers = []string{
	"restored",
	"has resumed",
	"have resumed",
	"back to normal",
	"naibalik",
	"bumalik na",
	"tapos na",
	"lifted",
}

// CheckMeralcoXInbox reports whether the inbox is a @meralco inbox.
func CheckMeralcoXInbox(in *XInbox) error {
	if in == nil {
		return fmt.Errorf("utilities: meralco-x: nil inbox")
	}
	if in.Account != MeralcoXAccount {
		return fmt.Errorf("utilities: meralco-x: unexpected account %q (want %q)", in.Account, MeralcoXAccount)
	}
	return nil
}

// meralcoXHasRestoration reports whether lower-cased text carries a
// resolution marker.
func meralcoXHasRestoration(lower string) bool {
	for _, k := range meralcoXRestorationMarkers {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// ParseMeralcoXEvents filters an X inbox for @meralco emergency
// line-trouble posts mentioning Las Pinas (accent-insensitive),
// returning events sorted by Start ascending. The inbox account must
// be "meralco"; otherwise nil is returned (the caller reports the
// error via CheckMeralcoXInbox).
func ParseMeralcoXEvents(in *XInbox, now time.Time) []MeralcoXEvent {
	if in == nil || in.Account != MeralcoXAccount {
		return nil
	}
	nowManila := now.In(manilaLoc)
	type key struct {
		day   string
		area  string
		clock string
		etr   string
	}
	best := map[key]MeralcoXEvent{}
	seenID := map[string]bool{}
	for _, p := range in.Posts {
		if p.ID == "" {
			continue
		}
		if seenID[p.ID] {
			continue
		}
		seenID[p.ID] = true
		posted := p.CreatedAt.In(manilaLoc)
		if nowManila.Sub(posted) > 7*24*time.Hour {
			continue
		}
		m := meralcoXAreaRe.FindStringSubmatch(p.Text)
		if m == nil {
			continue
		}
		areaText := strings.TrimSpace(spaceRe.ReplaceAllString(m[1], " "))
		if areaText == "" {
			continue
		}
		if !strings.Contains(foldAccent(areaText), "las pinas") {
			continue
		}
		lower := strings.ToLower(p.Text)
		if meralcoXHasRestoration(lower) {
			continue
		}
		clockText := strings.TrimSpace(spaceRe.ReplaceAllString(m[2], " "))
		h, mi, ok := parseHM(clockText)
		if !ok {
			continue
		}
		start := time.Date(posted.Year(), posted.Month(), posted.Day(), h, mi, 0, 0, manilaLoc)
		if start.Sub(posted) > 30*time.Minute {
			start = start.Add(-24 * time.Hour)
		}
		if start.After(posted.Add(time.Hour)) {
			continue
		}
		if nowManila.Sub(start) > 48*time.Hour {
			continue
		}
		var end time.Time
		var etrHours float64
		openEnded := true
		if em := meralcoXETRRe.FindStringSubmatch(p.Text); em != nil {
			var v float64
			if _, err := fmt.Sscanf(em[1], "%g", &v); err == nil && v > 0 {
				etrHours = v
				end = start.Add(time.Duration(v * float64(time.Hour)))
				openEnded = false
			}
		}
		if !openEnded && !end.After(start) {
			continue
		}
		postURL := p.URL
		if postURL == "" {
			postURL = "https://x.com/meralco/status/" + p.ID
		}
		ev := MeralcoXEvent{
			PostID:       p.ID,
			PostURL:      postURL,
			PostedAt:     p.CreatedAt,
			Start:        start,
			End:          end,
			City:         "Las Piñas",
			AreaText:     areaText,
			OccurredText: clockText,
			ETRHours:     etrHours,
			OpenEnded:    openEnded,
		}
		k := key{
			day:   start.Format("2006-01-02"),
			area:  foldAccent(areaText),
			clock: strings.ToUpper(spaceRe.ReplaceAllString(clockText, " ")),
			etr:   fmt.Sprintf("%g", etrHours),
		}
		if prev, dup := best[k]; dup {
			if !ev.PostedAt.Before(prev.PostedAt) {
				continue
			}
		}
		best[k] = ev
	}
	out := make([]MeralcoXEvent, 0, len(best))
	for _, ev := range best {
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].PostID < out[j].PostID
	})
	if out == nil {
		return nil
	}
	return out
}

// BuildMeralcoXAlerts builds one emergency PowerAlert per event: ID
// "x-"+PostID, Kind "emergency", EstimatedEnd when End came from the
// ETR, Barangays marker, ReasonKey emergency. An event is skipped
// when it overlaps any planned alert window by >= 50% of the shorter
// window, when it ended more than 24h ago, or (open-ended) when posted
// more than 24h ago. Nothing else filters.
func BuildMeralcoXAlerts(st *MeralcoXStatus, planned []PowerAlert, now time.Time) []PowerAlert {
	if st == nil {
		return nil
	}
	var out []PowerAlert
	for _, ev := range st.Events {
		skip := false
		if !ev.OpenEnded && !ev.End.IsZero() {
			if ev.End.Before(now.Add(-24 * time.Hour)) {
				continue
			}
			for _, p := range planned {
				wins := p.Windows
				if len(wins) == 0 && !p.Start.IsZero() && !p.End.IsZero() {
					wins = []TimeWindow{{Start: p.Start, End: p.End}}
				}
				for _, w := range wins {
					if WindowsOverlap50(w.Start, w.End, ev.Start, ev.End) {
						skip = true
						break
					}
				}
				if skip {
					break
				}
			}
			if skip {
				continue
			}
		} else {
			if now.Sub(ev.PostedAt) > 24*time.Hour {
				continue
			}
		}
		a := PowerAlert{
			ID:           "x-" + ev.PostID,
			Kind:         "emergency",
			City:         "Las Piñas",
			Start:        ev.Start,
			End:          ev.End,
			OpenEnded:    ev.OpenEnded,
			EstimatedEnd: !ev.OpenEnded && !ev.End.IsZero(),
			Barangays:    []string{PowerAreasTL},
			ReasonKey:    PowerReasonEmergency,
			SourceURL:    ev.PostURL,
		}
		if !ev.End.IsZero() {
			a.Windows = []TimeWindow{{Start: ev.Start, End: ev.End}}
		}
		out = append(out, a)
	}
	return out
}
