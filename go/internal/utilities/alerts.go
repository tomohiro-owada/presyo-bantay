package utilities

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Water alerts (event posts) built from Maynilad interruption and
// maintenance rows. Wording is our own (ReasonKey); barangay names and
// times are relayed as facts with a link.

// MayniladAdvisoriesURL is the public advisories page linked from
// alert captions.
const MayniladAdvisoriesURL = "https://www.mayniladwater.com.ph/service-advisories-2/"

// WaterAlert is one postable interruption event.
type WaterAlert struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	City       string    `json:"city"`
	Areas      []string  `json:"areas"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	WindowText string    `json:"window_text"`
	ReasonKey  string    `json:"reason_key"`
	SourceURL  string    `json:"source_url"`
}

// XAreasTL/EN/JA are the per-language areas labels for X-sourced
// alerts (no barangay list in the post text). BuildWaterAlerts stores
// the TL marker; RenderWaterAlertPost and checkWaterAlert expand it
// per language. All three are digit-free.
const (
	XAreasTL = "Ilang barangay — tingnan ang abiso ng Maynilad"
	XAreasEN = "Some barangays — see the Maynilad advisory"
	XAreasJA = "一部の地区（詳細は告知をご確認ください）"
)

// Reason keys and our own wording per language.
var reasonWords = map[string][3]string{
	"facility":  {"maintenance ng pasilidad", "facility maintenance", "設備メンテナンス"},
	"network":   {"maintenance ng linya ng tubig", "pipe network maintenance", "配管網メンテナンス"},
	"repair":    {"pagkukumpuni ng linya", "pipe repair", "配管修理"},
	"limited":   {"limitadong suplay", "limited supply capacity", "供給能力の制約"},
	"audit":     {"water audit", "water audit", "水量点検"},
	"emergency": {"emergency na trabaho", "emergency works", "緊急作業"},
	"generic":   {"trabaho ng Maynilad", "Maynilad work", "Maynilad作業"},
}

// AlertReason returns our own wording for a reason key by language
// index (0 TL, 1 EN, 2 JA); unknown keys yield generic.
func AlertReason(key string, lang int) string {
	r, ok := reasonWords[key]
	if !ok {
		r = reasonWords["generic"]
	}
	if lang < 0 || lang > 2 {
		lang = 0
	}
	return r[lang]
}

// AlertWindowTL returns the big chart window lines: two TL lines when
// parsed ("Miy Ago 26, 10:00 PM" / "hanggang Huw Ago 27, 1:00 PM"),
// else the raw window text as one line (caller wraps it).
func AlertWindowTL(a *WaterAlert) []string {
	if a == nil || a.Start.IsZero() || a.End.IsZero() {
		if a == nil {
			return nil
		}
		return []string{a.WindowText}
	}
	s, e := a.Start, a.End
	return []string{
		fmt.Sprintf("%s %s %d, %s", tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), fmt12(s)),
		fmt.Sprintf("hanggang %s %s %d, %s", tlWeekday[e.Weekday()], tlShort[e.Month()], e.Day(), fmt12(e)),
	}
}

// reasonKeyOf maps a raw reason/activity phrase to our wording key
// (unknown phrases → generic; never copy their sentences).
func reasonKeyOf(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "water audit"):
		return "audit"
	case strings.Contains(lower, "emergency"):
		return "emergency"
	case strings.Contains(lower, "limitation"):
		return "limited"
	case strings.Contains(lower, "repair"):
		return "repair"
	case strings.Contains(lower, "network maintenance"):
		return "network"
	case strings.Contains(lower, "facility maintenance"):
		return "facility"
	case strings.Contains(lower, "pumping station"):
		return "facility"
	case strings.Contains(lower, "facility"):
		return "facility"
	}
	return "generic"
}

var andSplitRe = regexp.MustCompile(`(?i)\s+and\s+`)

// splitAreas splits barangay lists on "," and " and ".
func splitAreas(s string) []string {
	var out []string
	for _, p1 := range strings.Split(s, ",") {
		for _, p2 := range andSplitRe.Split(p1, -1) {
			if t := strings.TrimSpace(p2); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

var hmRe = regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*([ap])\.?\s*m\.?`)

// parseHM parses "10:00 p.m."/"5:00 AM" to 24h hour/min.
func parseHM(s string) (int, int, bool) {
	m := hmRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	var h, mi int
	fmt.Sscanf(m[1], "%d", &h)
	fmt.Sscanf(m[2], "%d", &mi)
	if h < 1 || h > 12 || mi > 59 {
		return 0, 0, false
	}
	if strings.ToLower(m[3]) == "p" {
		if h != 12 {
			h += 12
		}
	} else if h == 12 {
		h = 0
	}
	return h, mi, true
}

var schedPartRe = regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*([ap])\.?\s*m\.?\s+of\s+([A-Za-z]+)\s+(\d{1,2}),\s*(\d{4})`)

// parseSchedTime parses "10:00 p.m. of August 26, 2026".
func parseSchedTime(s string) (time.Time, bool) {
	m := schedPartRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	day, mon, year := m[5], m[4], m[6]
	ds, err := parseMonthDayYear(day, mon, year)
	if err != nil {
		return time.Time{}, false
	}
	t, _ := time.Parse("2006-01-02", ds)
	h, mi, _ := parseHM(m[0])
	return time.Date(t.Year(), t.Month(), t.Day(), h, mi, 0, 0, manilaLoc), true
}

// splitWindow splits "A to B" schedule/time ranges.
func splitWindow(s string) (string, string) {
	if i := strings.Index(s, " to "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+4:])
	}
	return strings.TrimSpace(s), ""
}

// alertID is the stable 12-hex ID of kind+city+barangays+start+end.
func alertID(kind, city, barangays, start, end string) string {
	sum := sha1.Sum([]byte(kind + "|" + city + "|" + barangays + "|" + start + "|" + end))
	return hex.EncodeToString(sum[:])[:12]
}

func fmtAlertTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// BuildWaterAlerts builds postable alerts from every interruption and
// maintenance row in a water status.
func BuildWaterAlerts(w *WaterStatus) []WaterAlert {
	if w == nil {
		return nil
	}
	var out []WaterAlert
	push := func(kind, barangays, windowText, reason, sourceURL string, start, end time.Time) {
		areas := splitAreas(barangays)
		if len(areas) == 0 {
			return
		}
		out = append(out, WaterAlert{
			ID:         alertID(kind, w.City, barangays, fmtAlertTime(start), fmtAlertTime(end)),
			Kind:       kind,
			City:       w.City,
			Areas:      areas,
			Start:      start,
			End:        end,
			WindowText: windowText,
			ReasonKey:  reasonKeyOf(reason),
			SourceURL:  sourceURL,
		})
	}
	lists := []struct {
		kind string
		rows []Interruption
	}{
		{"scheduled", w.Scheduled}, {"emergency", w.Emergency}, {"rotational", w.Rotational},
	}
	for _, l := range lists {
		for _, in := range l.rows {
			windowText := in.FromRaw
			if in.ToRaw != "" && in.ToRaw != in.FromRaw {
				windowText = in.FromRaw + " to " + in.ToRaw
			}
			if in.Time != "" {
				if windowText != "" {
					windowText += ", " + in.Time
				} else {
					windowText = in.Time
				}
			}
			var start, end time.Time
			if d, ok := mustDate(in.From); ok {
				a, b := splitWindow(in.Time)
				if h, mi, ok := parseHM(a); ok {
					start = time.Date(d.Year(), d.Month(), d.Day(), h, mi, 0, 0, manilaLoc)
					endDate := d
					if d2, ok := mustDate(in.To); ok {
						endDate = d2
					}
					if b == "" {
						b = a
					}
					if h2, mi2, ok := parseHM(b); ok {
						end = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), h2, mi2, 0, 0, manilaLoc)
						if !end.After(start) && (in.To == "" || in.To == in.From) {
							end = end.Add(24 * time.Hour)
						}
					}
				}
			}
			push(l.kind, in.Barangays, windowText, in.Reason, MayniladAdvisoriesURL, start, end)
		}
	}
	for _, m := range w.Maintenance {
		var start, end time.Time
		a, b := splitWindow(m.Schedule)
		if t, ok := parseSchedTime(a); ok {
			start = t
		}
		if b != "" {
			if t, ok := parseSchedTime(b); ok {
				end = t
			}
		}
		push("maintenance", m.Barangays, m.Schedule, m.Activity, m.PostURL, start, end)
	}
	for _, adv := range w.Advisories {
		out = append(out, WaterAlert{
			ID:         alertID(adv.Kind, w.City, "x", fmtAlertTime(adv.Start), fmtAlertTime(adv.End)),
			Kind:       adv.Kind,
			City:       w.City,
			Areas:      []string{XAreasTL},
			Start:      adv.Start,
			End:        adv.End,
			WindowText: adv.WindowText,
			ReasonKey:  adv.ReasonKey,
			SourceURL:  adv.PostURL,
		})
	}
	return out
}

var tlWeekday = map[time.Weekday]string{
	time.Sunday: "Lin", time.Monday: "Lun", time.Tuesday: "Mar",
	time.Wednesday: "Miy", time.Thursday: "Huw", time.Friday: "Biy",
	time.Saturday: "Sab",
}

var enWeekday = map[time.Weekday]string{
	time.Sunday: "Sun", time.Monday: "Mon", time.Tuesday: "Tue",
	time.Wednesday: "Wed", time.Thursday: "Thu", time.Friday: "Fri",
	time.Saturday: "Sat",
}

var jaWeekday = map[time.Weekday]string{
	time.Sunday: "日", time.Monday: "月", time.Tuesday: "火",
	time.Wednesday: "水", time.Thursday: "木", time.Friday: "金",
	time.Saturday: "土",
}

// fmt12 prints "10:00 PM".
func fmt12(t time.Time) string {
	h, mi := t.Hour(), t.Minute()
	ap := "AM"
	if h >= 12 {
		ap = "PM"
	}
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d %s", h12, mi, ap)
}

// RenderWaterAlertPost renders the short trilingual alert caption. When
// pastSample is true the first line marks it as old sample data.
func RenderWaterAlertPost(a WaterAlert, pastSample bool) string {
	var tl, en, ja strings.Builder
	if pastSample {
		for _, b := range []*strings.Builder{&tl, &en, &ja} {
			fmt.Fprintln(b, "⚠️ SAMPLE — lumang datos / past data, hindi kasalukuyang abiso")
		}
	}
	reason := reasonWords[a.ReasonKey]
	if reason == [3]string{} {
		reason = reasonWords["generic"]
	}
	areasTL := strings.Join(a.Areas, ", ")
	areasEN := areasTL
	areasJA := strings.Join(a.Areas, "、")
	if len(a.Areas) == 1 && a.Areas[0] == XAreasTL {
		areasTL = XAreasTL
		areasEN = XAreasEN
		areasJA = XAreasJA
	}
	cityJA := jaCity(a.City)
	if a.Start.IsZero() || a.End.IsZero() {
		fmt.Fprintf(&tl, "💧 Walang tubig sa ilang bahagi ng %s\n📅 %s\n📍 %s\n🔧 Dahilan: %s\nMag-ipon ng tubig bago magsimula.\n",
			a.City, a.WindowText, areasTL, reason[0])
		fmt.Fprintf(&en, "🇬🇧 Water interruption in parts of %s\n%s · %s · Reason: %s. Store water beforehand.\n",
			a.City, a.WindowText, areasEN, reason[1])
		fmt.Fprintf(&ja, "🇯🇵 %sの一部で断水\n%s・%s・理由：%s。事前に水の確保を。\n",
			cityJA, a.WindowText, areasJA, reason[2])
	} else {
		s, e := a.Start, a.End
		fmt.Fprintf(&tl, "💧 Walang tubig sa ilang bahagi ng %s\n📅 %s %s %d, %s hanggang %s %s %d, %s\n📍 %s\n🔧 Dahilan: %s\nMag-ipon ng tubig bago magsimula.\n",
			a.City, tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), fmt12(s),
			tlWeekday[e.Weekday()], tlShort[e.Month()], e.Day(), fmt12(e), areasTL, reason[0])
		fmt.Fprintf(&en, "🇬🇧 Water interruption in parts of %s\n%s %s %d, %s to %s %s %d, %s · %s · Reason: %s. Store water beforehand.\n",
			a.City, enWeekday[s.Weekday()], enShort[s.Month()], s.Day(), fmt12(s),
			enWeekday[e.Weekday()], enShort[e.Month()], e.Day(), fmt12(e), areasEN, reason[1])
		fmt.Fprintf(&ja, "🇯🇵 %sの一部で断水\n%d/%d（%s）%02d:%02d〜%d/%d（%s）%02d:%02d・%s・理由：%s。事前に水の確保を。\n",
			cityJA, int(s.Month()), s.Day(), jaWeekday[s.Weekday()], s.Hour(), s.Minute(),
			int(e.Month()), e.Day(), jaWeekday[e.Weekday()], e.Hour(), e.Minute(), areasJA, reason[2])
	}
	details := "Details: " + MayniladAdvisoriesURL
	if strings.HasPrefix(a.SourceURL, "https://x.com/") {
		details = "Details: " + a.SourceURL
		footer := details + "\nSource: Maynilad (X) · Hindi opisyal na Maynilad page"
		return joinAlertBlocks(tl.String(), en.String(), ja.String(), footer)
	}
	if a.Kind == "maintenance" && a.SourceURL != "" && a.SourceURL != MayniladAdvisoriesURL {
		details += " " + a.SourceURL
	}
	footer := details + "\nSource: Maynilad advisory · Hindi opisyal na Maynilad page"
	return joinAlertBlocks(tl.String(), en.String(), ja.String(), footer)
}

// joinAlertBlocks joins TL/EN/JA plus the shared footer with
// separators (no trailing newline).
func joinAlertBlocks(tl, en, ja, footer string) string {
	trim := func(s string) string { return strings.TrimRight(s, "\n") }
	return trim(tl) + "\n" + Separator + "\n" + trim(en) + "\n" + Separator + "\n" + trim(ja) + "\n" + Separator + "\n" + footer
}
