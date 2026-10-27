// X inbox (Maynilad water interruptions from @maynilad via X).
//
// The Maynilad website sits behind a Cloudflare challenge, so the Go
// side no longer fetches it by default. Instead a scheduled routine
// (outside the VPS) reads @maynilad with X's public read tools and
// pushes a normalized v1 JSON file to $PRESYO_HOME/inbox/maynilad_x.json.
//
// v1 inbox format (see go/UTILITIES_MAYNILAD_X.md):
//   {"version":1,"account":"maynilad","account_id":"2992779308",
//    "fetched_at":"<RFC3339 with offset>",
//    "posts":[{"id":"<string>","created_at":"<RFC3339>",
//      "text":"<full text, note_tweet preferred>",
//      "url":"https://x.com/maynilad/status/<id>",
//      "lang":"en","images":["https://..."],"has_video":false}]}

package utilities

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MayniladXAccountURL is the public X account URL recorded as the
// Source URL for the maynilad-x inbox. The local inbox file path must
// never appear in a Source: sources are published verbatim in the
// public site's utilities.json.
const MayniladXAccountURL = "https://x.com/maynilad"

// XPost is one normalized X post.
type XPost struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Text      string    `json:"text"`
	URL       string    `json:"url"`
	Lang      string    `json:"lang,omitempty"`
	Images    []string  `json:"images,omitempty"`
	HasVideo  bool      `json:"has_video,omitempty"`
}

// XInbox is the normalized v1 inbox file.
type XInbox struct {
	Version   int       `json:"version"`
	Account   string    `json:"account"`
	AccountID string    `json:"account_id"`
	FetchedAt time.Time `json:"fetched_at"`
	Posts     []XPost   `json:"posts"`
}

// XAdvisory is one parsed water-interruption window from an X post.
// Barangay names are NOT in the text, so alerts from X carry no
// barangay list (see XAreasTL).
type XAdvisory struct {
	PostID     string    `json:"post_id"`
	PostURL    string    `json:"post_url"`
	PostedAt   time.Time `json:"posted_at"`
	Kind       string    `json:"kind"`
	City       string    `json:"city"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	WindowText string    `json:"window_text"`
	ReasonKey  string    `json:"reason_key"`
}

// LoadXInbox reads a v1 inbox file (strict JSON, version==1).
func LoadXInbox(path string) (*XInbox, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var in XInbox
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("utilities: x inbox %s: %w", path, err)
	}
	if in.Version != 1 {
		return nil, fmt.Errorf("utilities: x inbox %s: bad version %d (want 1)", path, in.Version)
	}
	return &in, nil
}

var (
	xFromAtRe = regexp.MustCompile(`(?i)\bfrom\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})\s+at\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)\s+(?:until|to|thru|through|–|—|-)\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})\s+at\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)`)
	xFromOnRe = regexp.MustCompile(`(?i)\bfrom\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)\s+on\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})\s+(?:to|until|thru|through|–|—|-)\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)\s+on\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})`)
	xMulaRe   = regexp.MustCompile(`(?i)\bmula\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})\s+ng\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)\s+hanggang\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})\s+ng\s+(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)`)
	xResumeRe = regexp.MustCompile(`(?i)(?:resumption|restored|resume|restoration)[^.]*?(\d{1,2}:\d{2}\s*[ap]\.?\s*m\.?)\s+(?:of|on)\s+([A-Za-z]+\s+\d{1,2},\s*\d{4})`)
	xDateRe   = regexp.MustCompile(`(?i)^\s*([A-Za-z]+)\s+(\d{1,2}),\s*(\d{4})\s*$`)
)

// xDateTime combines "October 22, 2026" + "10:00 pm" into Manila time.
func xDateTime(dateStr, hmStr string) (time.Time, bool) {
	m := xDateRe.FindStringSubmatch(dateStr)
	if m == nil {
		return time.Time{}, false
	}
	mon, ok := monthByName[strings.ToLower(m[1])]
	if !ok {
		return time.Time{}, false
	}
	var d, y int
	fmt.Sscanf(m[2], "%d", &d)
	fmt.Sscanf(m[3], "%d", &y)
	h, mi, ok := parseHM(hmStr)
	if !ok {
		return time.Time{}, false
	}
	return time.Date(y, mon, d, h, mi, 0, 0, manilaLoc), true
}

// xParseWindow extracts (start, end, windowText). Emergency resumption
// posts carry only an end time; start is filled by the caller with the
// post created_at. ok=false when nothing parses.
func xParseWindow(text string) (start, end time.Time, windowText string, ok bool) {
	if m := xFromAtRe.FindStringSubmatch(text); m != nil {
		s, ok1 := xDateTime(m[1], m[2])
		e, ok2 := xDateTime(m[3], m[4])
		if ok1 && ok2 {
			return s, e, strings.TrimSpace(m[0]), true
		}
	}
	if m := xFromOnRe.FindStringSubmatch(text); m != nil {
		s, ok1 := xDateTime(m[2], m[1])
		e, ok2 := xDateTime(m[4], m[3])
		if ok1 && ok2 {
			return s, e, strings.TrimSpace(m[0]), true
		}
	}
	if m := xMulaRe.FindStringSubmatch(text); m != nil {
		s, ok1 := xDateTime(m[1], m[2])
		e, ok2 := xDateTime(m[3], m[4])
		if ok1 && ok2 {
			return s, e, strings.TrimSpace(m[0]), true
		}
	}
	if m := xResumeRe.FindStringSubmatch(text); m != nil {
		e, ok2 := xDateTime(m[2], m[1])
		if ok2 {
			return time.Time{}, e, strings.TrimSpace(m[0]), true
		}
	}
	return time.Time{}, time.Time{}, "", false
}

// xIsAdvisory reports whether text looks like an interruption advisory
// (case-insensitive keywords). "scheduled maintenance" / "maintenance
// activities" only count together with a parsed window (checked by the
// caller via xParseWindow).
func xIsAdvisory(lower string) bool {
	for _, k := range []string{"interruption", "low pressure", "no water", "walang tubig", "emergency"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	if strings.Contains(lower, "scheduled maintenance") || strings.Contains(lower, "maintenance activities") {
		return true
	}
	return false
}

// xKind classifies emergency vs scheduled.
func xKind(lower string) string {
	if strings.Contains(lower, "emergency") ||
		strings.Contains(lower, "low pressure to no water supply you may be experiencing") {
		return "emergency"
	}
	return "scheduled"
}

// ParseXAdvisories filters an X inbox for city (accent-insensitive),
// keeping only interruption advisories with a parsed window posted
// within the last 14 days of now. Dedupes by post id, then by
// (kind, start, end) keeping the earliest post. Skips posts whose End
// is before now.
func ParseXAdvisories(in *XInbox, city string, now time.Time) []XAdvisory {
	if in == nil {
		return nil
	}
	nowManila := now.In(manilaLoc)
	want := foldAccent(strings.TrimSpace(city))
	seenID := map[string]bool{}
	type key struct {
		kind  string
		start string
		end   string
	}
	best := map[key]XAdvisory{}
	for _, p := range in.Posts {
		if p.ID == "" {
			continue
		}
		if seenID[p.ID] {
			continue
		}
		seenID[p.ID] = true
		posted := p.CreatedAt.In(manilaLoc)
		if nowManila.Sub(posted) > 14*24*time.Hour {
			continue
		}
		if want != "" && !strings.Contains(foldAccent(p.Text), want) {
			continue
		}
		lower := strings.ToLower(p.Text)
		if !xIsAdvisory(lower) {
			continue
		}
		s, e, wtext, ok := xParseWindow(p.Text)
		if !ok {
			continue
		}
		kind := xKind(lower)
		if e.IsZero() {
			continue
		}
		if s.IsZero() {
			// Emergency resumption: Start = post created_at.
			s = posted
		}
		if e.Before(nowManila) {
			continue
		}
		k := key{kind, s.Format(time.RFC3339), e.Format(time.RFC3339)}
		adv := XAdvisory{
			PostID:     p.ID,
			PostURL:    p.URL,
			PostedAt:   p.CreatedAt,
			Kind:       kind,
			City:       city,
			Start:      s,
			End:        e,
			WindowText: wtext,
			ReasonKey:  reasonKeyOf(p.Text),
		}
		if adv.PostURL == "" {
			adv.PostURL = "https://x.com/maynilad/status/" + p.ID
		}
		if prev, dup := best[k]; dup {
			if !adv.PostedAt.Before(prev.PostedAt) {
				continue
			}
		}
		best[k] = adv
	}
	out := make([]XAdvisory, 0, len(best))
	for _, a := range best {
		out = append(out, a)
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
