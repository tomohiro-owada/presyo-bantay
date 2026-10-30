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
	// OpenEnded marks an emergency advisory with no known end time:
	// captions say "until further notice" instead of an end time.
	OpenEnded bool `json:"open_ended,omitempty"`
	// Daily marks a one-alert-per-row nightly repeat; Windows holds
	// the per-night windows, DailyFrom/To the start-date range.
	Daily     bool         `json:"daily,omitempty"`
	Windows   []TimeWindow `json:"windows,omitempty"`
	DailyFrom string       `json:"daily_from,omitempty"`
	DailyTo   string       `json:"daily_to,omitempty"`
	// Trend: NthThisMonth is this alert's position in its Manila
	// month (>=2 shows the trend line); PrevMonthCount/HasPrev
	// carry the previous month count when the snapshot covers it.
	NthThisMonth   int  `json:"nth_this_month,omitempty"`
	PrevMonthCount int  `json:"prev_month_count,omitempty"`
	HasPrev        bool `json:"has_prev,omitempty"`
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
// an open-ended pair ("Miy Okt 16, 9:51 AM" / "hanggang may abiso")
// when the end is unknown, else the raw window text as one line
// (caller wraps it).
func AlertWindowTL(a *WaterAlert) []string {
	if a == nil {
		return nil
	}
	if a.OpenEnded && !a.Start.IsZero() {
		s := a.Start
		return []string{
			fmt.Sprintf("%s %s %d, %s", tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), fmt12(s)),
			"hanggang may abiso",
		}
	}
	if a.Start.IsZero() || a.End.IsZero() {
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

// WindowsOverlap50 reports whether two windows overlap by >= 50 %
// of the shorter window.
func WindowsOverlap50(aStart, aEnd, bStart, bEnd time.Time) bool {
	if aEnd.IsZero() || bEnd.IsZero() || aStart.IsZero() || bStart.IsZero() {
		return false
	}
	if !aStart.Before(aEnd) || !bStart.Before(bEnd) {
		return false
	}
	latestStart := aStart
	if bStart.After(latestStart) {
		latestStart = bStart
	}
	earliestEnd := aEnd
	if bEnd.Before(earliestEnd) {
		earliestEnd = bEnd
	}
	overlap := earliestEnd.Sub(latestStart)
	if overlap <= 0 {
		return false
	}
	da := aEnd.Sub(aStart)
	db := bEnd.Sub(bStart)
	shorter := da
	if db < shorter {
		shorter = db
	}
	if shorter <= 0 {
		return false
	}
	return float64(overlap) >= 0.5*float64(shorter)
}

// alertWindows returns the comparable windows of a feed alert: the
// per-night windows for daily alerts, else the single Start/End.
func alertWindows(a WaterAlert) []TimeWindow {
	if a.Daily && len(a.Windows) > 0 {
		return a.Windows
	}
	if a.Start.IsZero() || a.End.IsZero() {
		return nil
	}
	return []TimeWindow{{Start: a.Start, End: a.End}}
}

// feedDropsX reports whether an X advisory window is covered by a
// feed-sourced alert window (per night for daily alerts): same city
// and >= 50 % overlap of the shorter window. The feed alert wins.
func feedDropsX(feed WaterAlert, xStart, xEnd time.Time, city string) bool {
	if foldAccent(feed.City) != foldAccent(city) {
		return false
	}
	for _, fw := range alertWindows(feed) {
		if WindowsOverlap50(fw.Start, fw.End, xStart, xEnd) {
			return true
		}
	}
	return false
}

// WaterAlertOverlapsPosted reports whether a candidate water alert
// overlaps an already-posted util-water entry (same rule, same city):
// for daily candidates each nightly window is checked, so a feed
// alert is not re-posted after the same event was posted from X (and
// vice versa).
func WaterAlertOverlapsPosted(candidate WaterAlert, postedStart, postedEnd time.Time) bool {
	for _, cw := range alertWindows(candidate) {
		if WindowsOverlap50(cw.Start, cw.End, postedStart, postedEnd) {
			return true
		}
	}
	return false
}

// BuildWaterAlerts builds postable alerts from every interruption and
// maintenance row in a water status. Daily feed rows yield ONE alert
// per row (not per night). X advisories whose window overlaps a
// feed-sourced alert window (per night) by >= 50 % of the shorter
// window are dropped; the feed alert wins because it has barangays.
func BuildWaterAlerts(w *WaterStatus) []WaterAlert {
	if w == nil {
		return nil
	}
	var out []WaterAlert
	push := func(kind, barangays, windowText, reason, sourceURL string, start, end time.Time, daily bool, windows []TimeWindow, dailyFrom, dailyTo string) {
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
			Daily:      daily,
			Windows:    windows,
			DailyFrom:  dailyFrom,
			DailyTo:    dailyTo,
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
			push(l.kind, in.Barangays, windowText, in.Reason, MayniladAdvisoriesURL, start, end, false, nil, "", "")
		}
	}
	for _, m := range w.Maintenance {
		src := m.PostURL
		if src == "" {
			src = MayniladAdvisoriesURL
		}
		windows, sched := ParseMaintSchedule(m.Schedule)
		// Prefer stored feed windows when present (same parse).
		if len(m.Windows) > 0 && len(windows) == 0 {
			windows = m.Windows
		}
		if len(windows) == 0 {
			push("maintenance", m.Barangays, m.Schedule, m.Activity, src, time.Time{}, time.Time{}, false, nil, "", "")
			continue
		}
		if sched.Daily || m.Daily {
			from, to := sched.DailyFrom, sched.DailyTo
			if from == "" {
				from = m.DailyFrom
			}
			if to == "" {
				to = m.DailyTo
			}
			push("maintenance", m.Barangays, m.Schedule, m.Activity, src,
				windows[0].Start, windows[len(windows)-1].End, true, windows, from, to)
			continue
		}
		push("maintenance", m.Barangays, m.Schedule, m.Activity, src, windows[0].Start, windows[0].End, false, nil, "", "")
	}
	// X advisories, minus feed-covered windows.
	var xAlerts []WaterAlert
	for _, adv := range w.Advisories {
		dropped := false
		for _, f := range out {
			if f.Kind != "maintenance" && f.Kind != "scheduled" && f.Kind != "emergency" && f.Kind != "rotational" {
				continue
			}
			// Only feed-sourced alerts (maintenance rows carry
			// barangays; interruption rows are site-sourced) win
			// over X here; interruption overlap is handled by the
			// fbutil posted guard.
			if f.Kind != "maintenance" {
				continue
			}
			if feedDropsX(f, adv.Start, adv.End, w.City) {
				dropped = true
				break
			}
		}
		if dropped {
			continue
		}
		xAlerts = append(xAlerts, WaterAlert{
			ID:         alertID(adv.Kind, w.City, "x", fmtAlertTime(adv.Start), fmtAlertTime(adv.End)),
			Kind:       adv.Kind,
			City:       w.City,
			Areas:      []string{XAreasTL},
			Start:      adv.Start,
			End:        adv.End,
			WindowText: adv.WindowText,
			ReasonKey:  adv.ReasonKey,
			SourceURL:  adv.PostURL,
			OpenEnded:  adv.OpenEnded,
		})
	}
	out = append(out, xAlerts...)
	assignWaterTrend(out, w)
	return out
}

// assignWaterTrend fills NthThisMonth/PrevMonthCount/HasPrev: N is the
// position among distinct alerts in the same Manila month ordered by
// Start (always shown, from N=1); the previous-month count is shown
// only when the snapshot really covers last month (a feed post or an
// alert in a previous month), never guessed.
func assignWaterTrend(alerts []WaterAlert, w *WaterStatus) {
	monthOf := func(t time.Time) string { return t.In(manilaLoc).Format("2006-01") }
	byMonth := map[string][]int{}
	for i, a := range alerts {
		if a.Start.IsZero() {
			continue
		}
		byMonth[monthOf(a.Start)] = append(byMonth[monthOf(a.Start)], i)
	}
	for m, idxs := range byMonth {
		// Order by Start, then ID for stability.
		ordered := append([]int(nil), idxs...)
		for i := 1; i < len(ordered); i++ {
			for j := i; j > 0; j-- {
				ai, aj := alerts[ordered[j]], alerts[ordered[j-1]]
				if ai.Start.Before(aj.Start) || (ai.Start.Equal(aj.Start) && ai.ID < aj.ID) {
					ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
				} else {
					break
				}
			}
		}
		_ = m
		for pos, ai := range ordered {
			alerts[ai].NthThisMonth = pos + 1
		}
	}
	for i, a := range alerts {
		if a.Start.IsZero() || a.NthThisMonth < 1 {
			continue
		}
		cur := monthOf(a.Start)
		prev := PrevMonth(cur)
		if prev == "" {
			continue
		}
		count := len(byMonth[prev])
		if count == 0 {
			continue
		}
		covered := false
		if w != nil {
			for _, mnt := range w.Maintenance {
				if len(mnt.PostDate) >= 7 && mnt.PostDate[:7] <= prev {
					covered = true
					break
				}
			}
		}
		if !covered {
			for m := range byMonth {
				if m <= prev {
					covered = true
					break
				}
			}
		}
		if !covered {
			continue
		}
		alerts[i].PrevMonthCount = count
		alerts[i].HasPrev = true
	}
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

// dailyClock returns the nightly clock window of a daily alert:
// 12h ends for TL/EN and 24h ends for JA.
func dailyClock(a *WaterAlert) (tl, en, ja string) {
	var sh, sm, eh, em int
	if len(a.Windows) > 0 {
		sh, sm = a.Windows[0].Start.Hour(), a.Windows[0].Start.Minute()
		eh, em = a.Windows[0].End.Hour(), a.Windows[0].End.Minute()
	} else {
		sh, sm = a.Start.Hour(), a.Start.Minute()
		eh, em = a.End.Hour(), a.End.Minute()
	}
	start12 := fmt12(time.Date(2026, 1, 1, sh, sm, 0, 0, manilaLoc))
	end12 := fmt12(time.Date(2026, 1, 1, eh, em, 0, 0, manilaLoc))
	tl = start12 + " hanggang " + end12
	en = start12 + " to " + end12
	ja = fmt.Sprintf("%02d:%02d〜%02d:%02d", sh, sm, eh, em)
	return tl, en, ja
}

// dailyRange3 returns the DailyFrom/DailyTo range per language,
// falling back to the Start/End dates when empty.
func dailyRange3(a *WaterAlert) (tl, en, ja string) {
	from, to := a.DailyFrom, a.DailyTo
	if from == "" || to == "" {
		if len(a.Windows) > 0 {
			from = a.Windows[0].Start.In(manilaLoc).Format("2006-01-02")
			to = a.Windows[len(a.Windows)-1].Start.In(manilaLoc).Format("2006-01-02")
		} else if !a.Start.IsZero() && !a.End.IsZero() {
			from = a.Start.In(manilaLoc).Format("2006-01-02")
			to = a.End.In(manilaLoc).Format("2006-01-02")
		}
	}
	if from == "" || to == "" {
		return a.WindowText, a.WindowText, a.WindowText
	}
	return shortRange(from, to)
}

// waterTrendLines renders the extra trend line per language ("Nth
// water interruption in Las Piñas this month", plus "last month: M"
// only when the snapshot covers last month). Empty when N < 1.
func waterTrendLines(a *WaterAlert) (tl, en, ja string, ok bool) {
	if a.NthThisMonth < 1 || a.Start.IsZero() {
		return "", "", "", false
	}
	m := a.Start.In(manilaLoc)
	tlMon := tlFull[m.Month()]
	enMon := enFull[m.Month()]
	tl = fmt.Sprintf("📊 Ika-%d na water interruption sa %s ngayong %s", a.NthThisMonth, a.City, tlMon)
	en = fmt.Sprintf("📊 %s water interruption in %s this %s", ordinalEN(a.NthThisMonth), a.City, enMon)
	ja = fmt.Sprintf("📊 今月%sで%d回目の断水", jaCity(a.City), a.NthThisMonth)
	if a.HasPrev && a.PrevMonthCount > 0 {
		prev := PrevMonth(m.Format("2006-01"))
		if py, pm, pok := splitMonth(prev); pok {
			pmo := time.Month(pm)
			tl += fmt.Sprintf(", %d noong %s", a.PrevMonthCount, tlFull[pmo])
			en += fmt.Sprintf(", %d last %s", a.PrevMonthCount, enFull[pmo])
			ja += fmt.Sprintf("（先月%d回）", a.PrevMonthCount)
			_ = py
		}
	}
	return tl, en, ja, true
}

// RenderWaterAlertPost renders the short trilingual alert caption. When
// pastSample is true the first line marks it as old sample data.
// Daily alerts say "bawat gabi"/"every night"/"毎晩" with the nightly
// clock window and the date range (never one continuous outage), and
// alerts with NthThisMonth >= 1 carry the monthly trend line.
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
	trendTL, trendEN, trendJA, hasTrend := waterTrendLines(&a)
	if a.Start.IsZero() || (a.End.IsZero() && !a.OpenEnded) {
		fmt.Fprintf(&tl, "💧 Walang tubig sa ilang bahagi ng %s\n📅 %s\n📍 %s\n🔧 Dahilan: %s\nMag-ipon ng tubig bago magsimula.\n",
			a.City, a.WindowText, areasTL, reason[0])
		fmt.Fprintf(&en, "🇬🇧 Water interruption in parts of %s\n%s · %s · Reason: %s. Store water beforehand.\n",
			a.City, a.WindowText, areasEN, reason[1])
		fmt.Fprintf(&ja, "🇯🇵 %sの一部で断水\n%s・%s・理由：%s。事前に水の確保を。\n",
			cityJA, a.WindowText, areasJA, reason[2])
	} else if a.Daily {
		clockTL, clockEN, clockJA := dailyClock(&a)
		rangeTL, rangeEN, rangeJA := dailyRange3(&a)
		fmt.Fprintf(&tl, "💧 Walang tubig sa ilang bahagi ng %s\n📅 bawat gabi %s, %s\n📍 %s\n🔧 Dahilan: %s\n",
			a.City, clockTL, rangeTL, areasTL, reason[0])
		if hasTrend {
			fmt.Fprintf(&tl, "%s\n", trendTL)
		}
		fmt.Fprintf(&tl, "Mag-ipon ng tubig bago magsimula.\n")
		fmt.Fprintf(&en, "🇬🇧 Water interruption in parts of %s\nevery night %s, %s · %s · Reason: %s. Store water beforehand.\n",
			a.City, clockEN, rangeEN, areasEN, reason[1])
		if hasTrend {
			fmt.Fprintf(&en, "%s\n", trendEN)
		}
		fmt.Fprintf(&ja, "🇯🇵 %sの一部で断水\n毎晩%s、%s・%s・理由：%s。事前に水の確保を。\n",
			cityJA, clockJA, rangeJA, areasJA, reason[2])
		if hasTrend {
			fmt.Fprintf(&ja, "%s\n", trendJA)
		}
	} else if a.OpenEnded {
		// Emergency with no known end time: the end reads
		// "until further notice" in all three languages.
		s := a.Start
		fmt.Fprintf(&tl, "💧 Walang tubig sa ilang bahagi ng %s\n📅 %s %s %d, %s hanggang may abiso\n📍 %s\n🔧 Dahilan: %s\n",
			a.City, tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), fmt12(s), areasTL, reason[0])
		if hasTrend {
			fmt.Fprintf(&tl, "%s\n", trendTL)
		}
		fmt.Fprintf(&tl, "Mag-ipon ng tubig bago magsimula.\n")
		fmt.Fprintf(&en, "🇬🇧 Water interruption in parts of %s\n%s %s %d, %s, until further notice · %s · Reason: %s. Store water beforehand.\n",
			a.City, enWeekday[s.Weekday()], enShort[s.Month()], s.Day(), fmt12(s), areasEN, reason[1])
		if hasTrend {
			fmt.Fprintf(&en, "%s\n", trendEN)
		}
		fmt.Fprintf(&ja, "🇯🇵 %sの一部で断水\n%d/%d（%s）%02d:%02d〜追って通知があるまで・%s・理由：%s。事前に水の確保を。\n",
			cityJA, int(s.Month()), s.Day(), jaWeekday[s.Weekday()], s.Hour(), s.Minute(), areasJA, reason[2])
		if hasTrend {
			fmt.Fprintf(&ja, "%s\n", trendJA)
		}
	} else {
		s, e := a.Start, a.End
		fmt.Fprintf(&tl, "💧 Walang tubig sa ilang bahagi ng %s\n📅 %s %s %d, %s hanggang %s %s %d, %s\n📍 %s\n🔧 Dahilan: %s\n",
			a.City, tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), fmt12(s),
			tlWeekday[e.Weekday()], tlShort[e.Month()], e.Day(), fmt12(e), areasTL, reason[0])
		if hasTrend {
			fmt.Fprintf(&tl, "%s\n", trendTL)
		}
		fmt.Fprintf(&tl, "Mag-ipon ng tubig bago magsimula.\n")
		fmt.Fprintf(&en, "🇬🇧 Water interruption in parts of %s\n%s %s %d, %s to %s %s %d, %s · %s · Reason: %s. Store water beforehand.\n",
			a.City, enWeekday[s.Weekday()], enShort[s.Month()], s.Day(), fmt12(s),
			enWeekday[e.Weekday()], enShort[e.Month()], e.Day(), fmt12(e), areasEN, reason[1])
		if hasTrend {
			fmt.Fprintf(&en, "%s\n", trendEN)
		}
		fmt.Fprintf(&ja, "🇯🇵 %sの一部で断水\n%d/%d（%s）%02d:%02d〜%d/%d（%s）%02d:%02d・%s・理由：%s。事前に水の確保を。\n",
			cityJA, int(s.Month()), s.Day(), jaWeekday[s.Weekday()], s.Hour(), s.Minute(),
			int(e.Month()), e.Day(), jaWeekday[e.Weekday()], e.Hour(), e.Minute(), areasJA, reason[2])
		if hasTrend {
			fmt.Fprintf(&ja, "%s\n", trendJA)
		}
	}
	details := "Details: " + MayniladAdvisoriesURL
	if strings.HasPrefix(a.SourceURL, "https://x.com/") {
		details = "Details: " + a.SourceURL
		footer := details + "\nSource: Maynilad (X) · Hindi opisyal na Maynilad page"
		return joinAlertBlocks(tl.String(), en.String(), ja.String(), footer)
	}
	if a.Kind == "maintenance" && a.SourceURL != "" && a.SourceURL != MayniladAdvisoriesURL {
		// Feed alerts link the specific weekly post only.
		details = "Details: " + a.SourceURL
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
