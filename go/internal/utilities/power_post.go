package utilities

import (
	"fmt"
	"strings"
	"time"
)

// Power post kind (Part C): trilingual caption + trend, mirroring the
// water alert post. Own wording only (PowerReason*); barangay names,
// road names and times are relayed as facts with a link.

// PowerReason returns our own wording for a power reason key by language
// index (0 TL, 1 EN, 2 JA); unknown keys yield maintenance.
func PowerReason(key string, lang int) string {
	r, ok := powerReasonWords[key]
	if !ok {
		r = powerReasonWords[PowerReasonMaintenance]
	}
	if lang < 0 || lang > 2 {
		lang = 0
	}
	return r[lang]
}

var powerReasonWords = map[string][3]string{
	PowerReasonLine:        {"pagpapalit at pagkukumpuni ng linya at poste", "line and pole works", "電線・電柱工事"},
	PowerReasonSubstation:  {"maintenance ng substation", "substation maintenance", "変電所メンテナンス"},
	PowerReasonFacility:    {"pagkakabit ng pasilidad", "facility installation", "設備の設置"},
	PowerReasonTrimming:    {"pagputol ng sanga", "tree trimming", "樹木の伐採"},
	PowerReasonMaintenance: {"maintenance ng Meralco", "Meralco maintenance", "Meralco作業"},
	PowerReasonEmergency:   {"biglaang aberya sa linya", "emergency line trouble", "緊急の送電線トラブル"},
}

// shortRoadName abbreviates a road suffix for the caption line
// ("Alabang-Zapote Road" -> "Alabang-Zapote Rd").
func shortRoadName(s string) string {
	t := strings.TrimSpace(s)
	repls := []struct{ old, new string }{
		{" Avenue", " Ave"}, {" avenue", " Ave"},
		{" Boulevard", " Blvd"}, {" boulevard", " Blvd"},
		{" Drive", " Dr"},
		{" Highway", " Hwy"}, {" highway", " Hwy"},
		{" Road", " Rd"},
		{" Street", " St"}, {" street", " St"},
		{" Extension", " Ext"},
	}
	for _, r := range repls {
		if strings.HasSuffix(t, r.old) {
			return strings.TrimSuffix(t, r.old) + r.new
		}
	}
	// Mid-string " Ave." etc: normalise trailing period only.
	t = strings.TrimSuffix(t, ".")
	return t
}

func shortRoads(roads []string) []string {
	out := make([]string, 0, len(roads))
	for _, r := range roads {
		if s := shortRoadName(r); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// PowerTrendLines renders the extra trend line per language ("Nth
// planned brownout in Las Piñas this month", plus "last month: M" only
// when HistoryComplete). Empty when N < 1 or start is zero.
func PowerTrendLines(a *PowerAlert) (tl, en, ja string, ok bool) {
	if a.NthThisMonth < 1 || a.Start.IsZero() {
		return "", "", "", false
	}
	m := a.Start.In(manilaLoc)
	tlMon := tlFull[m.Month()]
	enMon := enFull[m.Month()]
	tl = fmt.Sprintf("📊 Ika-%d na planadong brownout sa %s ngayong %s", a.NthThisMonth, a.City, tlMon)
	en = fmt.Sprintf("📊 %s planned brownout in %s this %s", ordinalEN(a.NthThisMonth), a.City, enMon)
	ja = fmt.Sprintf("📊 今月%sで%d回目の計画停電", jaCity(a.City), a.NthThisMonth)
	if a.HistoryComplete && a.PrevMonthCount > 0 {
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

// powerClockPairs returns the distinct nightly clock pairs of an alert
// (for daily alerts over the expanded windows; else the alert windows).
func powerClockPairs(a *PowerAlert) [][4]int {
	seen := map[[4]int]bool{}
	var out [][4]int
	for _, w := range a.Windows {
		if w.Start.IsZero() || w.End.IsZero() {
			continue
		}
		k := [4]int{w.Start.Hour(), w.Start.Minute(), w.End.Hour(), w.End.Minute()}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	if len(out) == 0 && !a.Start.IsZero() && !a.End.IsZero() {
		out = [][4]int{{a.Start.Hour(), a.Start.Minute(), a.End.Hour(), a.End.Minute()}}
	}
	return out
}

func powerFmt12(h, mi int) string { return fmt12(time.Date(2026, 1, 1, h, mi, 0, 0, manilaLoc)) }

func powerFmt24(h, mi int) string { return fmt.Sprintf("%02d:%02d", h, mi) }

// powerDailyRange returns the DailyFrom/DailyTo range per language.
func powerDailyRange(a *PowerAlert) (tl, en, ja string) {
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
		return "", "", ""
	}
	return shortRange(from, to)
}

// powerDateTL/EN/JA render the date + window lines.
func powerDateTL(a *PowerAlert) string {
	if a.Start.IsZero() || a.End.IsZero() {
		return ""
	}
	if a.Daily {
		pairs := powerClockPairs(a)
		var clocks []string
		for _, p := range pairs {
			clocks = append(clocks, powerFmt12(p[0], p[1])+" hanggang "+powerFmt12(p[2], p[3]))
		}
		clock := strings.Join(clocks, ", at ")
		rtl, _, _ := powerDailyRange(a)
		if rtl != "" {
			return "bawat gabi " + clock + ", " + rtl
		}
		return "bawat gabi " + clock
	}
	// Same-date compression: all windows share one date.
	sameDate := a.Start.In(manilaLoc).Format("2006-01-02") == a.End.In(manilaLoc).Format("2006-01-02")
	if sameDate && len(a.Windows) > 0 {
		ok := true
		for _, w := range a.Windows {
			if w.Start.In(manilaLoc).Format("2006-01-02") != a.Start.In(manilaLoc).Format("2006-01-02") ||
				w.End.In(manilaLoc).Format("2006-01-02") != a.Start.In(manilaLoc).Format("2006-01-02") {
				// Allow end next day when end<=start was rolled? For
				// same-date multi-window both ends share the date.
				ok = false
				break
			}
		}
		_ = ok
	}
	if sameDate {
		s := a.Start
		var parts []string
		wins := a.Windows
		if len(wins) == 0 {
			wins = []TimeWindow{{Start: a.Start, End: a.End}}
		}
		for _, w := range wins {
			// Only compress when every window sits on the same date.
			if w.Start.In(manilaLoc).Format("2006-01-02") != s.In(manilaLoc).Format("2006-01-02") ||
				w.End.In(manilaLoc).Format("2006-01-02") != s.In(manilaLoc).Format("2006-01-02") {
				sameDate = false
				break
			}
			parts = append(parts, powerFmt12(w.Start.Hour(), w.Start.Minute())+" hanggang "+powerFmt12(w.End.Hour(), w.End.Minute()))
		}
		if sameDate {
			return fmt.Sprintf("%s %s %d, %s", tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), strings.Join(parts, ", at "))
		}
	}
	// General: each window full.
	wins := a.Windows
	if len(wins) == 0 {
		wins = []TimeWindow{{Start: a.Start, End: a.End}}
	}
	var parts []string
	for _, w := range wins {
		s, e := w.Start, w.End
		if s.In(manilaLoc).Format("2006-01-02") == e.In(manilaLoc).Format("2006-01-02") {
			parts = append(parts, fmt.Sprintf("%s %s %d, %s hanggang %s", tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), powerFmt12(s.Hour(), s.Minute()), powerFmt12(e.Hour(), e.Minute())))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s %d, %s hanggang %s %s %d, %s", tlWeekday[s.Weekday()], tlShort[s.Month()], s.Day(), powerFmt12(s.Hour(), s.Minute()), tlWeekday[e.Weekday()], tlShort[e.Month()], e.Day(), powerFmt12(e.Hour(), e.Minute())))
		}
	}
	return strings.Join(parts, ", at ")
}

func powerDateEN(a *PowerAlert) string {
	if a.Start.IsZero() || a.End.IsZero() {
		return ""
	}
	if a.Daily {
		pairs := powerClockPairs(a)
		var clocks []string
		for _, p := range pairs {
			clocks = append(clocks, powerFmt12(p[0], p[1])+" to "+powerFmt12(p[2], p[3]))
		}
		clock := strings.Join(clocks, ", and ")
		_, ren, _ := powerDailyRange(a)
		if ren != "" {
			return "every night " + clock + ", " + ren
		}
		return "every night " + clock
	}
	sameDate := a.Start.In(manilaLoc).Format("2006-01-02") == a.End.In(manilaLoc).Format("2006-01-02")
	if sameDate {
		s := a.Start
		wins := a.Windows
		if len(wins) == 0 {
			wins = []TimeWindow{{Start: a.Start, End: a.End}}
		}
		var parts []string
		for _, w := range wins {
			if w.Start.In(manilaLoc).Format("2006-01-02") != s.In(manilaLoc).Format("2006-01-02") ||
				w.End.In(manilaLoc).Format("2006-01-02") != s.In(manilaLoc).Format("2006-01-02") {
				sameDate = false
				break
			}
			parts = append(parts, powerFmt12(w.Start.Hour(), w.Start.Minute())+" to "+powerFmt12(w.End.Hour(), w.End.Minute()))
		}
		if sameDate {
			return fmt.Sprintf("%s %s %d, %s", enWeekday[s.Weekday()], enShort[s.Month()], s.Day(), strings.Join(parts, ", and "))
		}
	}
	wins := a.Windows
	if len(wins) == 0 {
		wins = []TimeWindow{{Start: a.Start, End: a.End}}
	}
	var parts []string
	for _, w := range wins {
		s, e := w.Start, w.End
		if s.In(manilaLoc).Format("2006-01-02") == e.In(manilaLoc).Format("2006-01-02") {
			parts = append(parts, fmt.Sprintf("%s %s %d, %s to %s", enWeekday[s.Weekday()], enShort[s.Month()], s.Day(), powerFmt12(s.Hour(), s.Minute()), powerFmt12(e.Hour(), e.Minute())))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s %d, %s to %s %s %d, %s", enWeekday[s.Weekday()], enShort[s.Month()], s.Day(), powerFmt12(s.Hour(), s.Minute()), enWeekday[e.Weekday()], enShort[e.Month()], e.Day(), powerFmt12(e.Hour(), e.Minute())))
		}
	}
	return strings.Join(parts, ", and ")
}

func powerDateJA(a *PowerAlert) string {
	if a.Start.IsZero() || a.End.IsZero() {
		return ""
	}
	if a.Daily {
		pairs := powerClockPairs(a)
		var clocks []string
		for _, p := range pairs {
			clocks = append(clocks, powerFmt24(p[0], p[1])+"〜"+powerFmt24(p[2], p[3]))
		}
		clock := strings.Join(clocks, "、")
		_, _, rja := powerDailyRange(a)
		if rja != "" {
			return "毎晩" + clock + "、" + rja
		}
		return "毎晩" + clock
	}
	sameDate := a.Start.In(manilaLoc).Format("2006-01-02") == a.End.In(manilaLoc).Format("2006-01-02")
	if sameDate {
		s := a.Start
		wins := a.Windows
		if len(wins) == 0 {
			wins = []TimeWindow{{Start: a.Start, End: a.End}}
		}
		var parts []string
		for _, w := range wins {
			if w.Start.In(manilaLoc).Format("2006-01-02") != s.In(manilaLoc).Format("2006-01-02") ||
				w.End.In(manilaLoc).Format("2006-01-02") != s.In(manilaLoc).Format("2006-01-02") {
				sameDate = false
				break
			}
			parts = append(parts, powerFmt24(w.Start.Hour(), w.Start.Minute())+"〜"+powerFmt24(w.End.Hour(), w.End.Minute()))
		}
		if sameDate {
			return fmt.Sprintf("%d/%d（%s）%s", int(s.Month()), s.Day(), jaWeekday[s.Weekday()], strings.Join(parts, "、"))
		}
	}
	wins := a.Windows
	if len(wins) == 0 {
		wins = []TimeWindow{{Start: a.Start, End: a.End}}
	}
	var parts []string
	for _, w := range wins {
		s, e := w.Start, w.End
		parts = append(parts, fmt.Sprintf("%d/%d（%s）%s〜%d/%d（%s）%s",
			int(s.Month()), s.Day(), jaWeekday[s.Weekday()], powerFmt24(s.Hour(), s.Minute()),
			int(e.Month()), e.Day(), jaWeekday[e.Weekday()], powerFmt24(e.Hour(), e.Minute())))
	}
	return strings.Join(parts, "、")
}

// powerAreas resolves the per-language barangay labels.
func powerAreas(a *PowerAlert) (tl, en, ja string) {
	if len(a.Barangays) == 1 && a.Barangays[0] == PowerAreasTL {
		return PowerAreasTL, PowerAreasEN, PowerAreasJA
	}
	tl = strings.Join(a.Barangays, ", ")
	en = tl
	ja = strings.Join(a.Barangays, "、")
	return tl, en, ja
}

// RenderPowerAlertPost renders the short trilingual power caption. When
// pastSample is true the first line marks it as old sample data.
// Daily alerts say "bawat gabi"/"every night"/"毎晩" with the nightly
// clock window and the date range, and alerts with NthThisMonth >= 1
// carry the monthly trend line.
func RenderPowerAlertPost(a PowerAlert, pastSample bool) string {
	var tl, en, ja strings.Builder
	if pastSample {
		for _, b := range []*strings.Builder{&tl, &en, &ja} {
			fmt.Fprintln(b, "⚠️ SAMPLE — lumang datos / past data, hindi kasalukuyang abiso")
		}
	}
	areasTL, areasEN, areasJA := powerAreas(&a)
	roads := shortRoads(a.Roads)
	roadsTL := strings.Join(roads, ", ")
	roadsEN := roadsTL
	roadsJA := strings.Join(roads, "、")
	placeTL := areasTL
	placeEN := areasEN
	placeJA := areasJA
	if roadsTL != "" {
		placeTL += " · " + roadsTL
		placeEN += " · " + roadsEN
		placeJA += "・" + roadsJA
	}
	dateTL, dateEN, dateJA := powerDateTL(&a), powerDateEN(&a), powerDateJA(&a)
	reason := powerReasonWords[a.ReasonKey]
	if reason == [3]string{} {
		reason = powerReasonWords[PowerReasonMaintenance]
	}
	cityJA := jaCity(a.City)
	trendTL, trendEN, trendJA, hasTrend := PowerTrendLines(&a)
	fmt.Fprintf(&tl, "⚡ Walang kuryente sa ilang bahagi ng %s\n📅 %s\n📍 %s\n🔧 Dahilan: %s\n",
		a.City, dateTL, placeTL, reason[0])
	if hasTrend {
		fmt.Fprintf(&tl, "%s\n", trendTL)
	}
	fmt.Fprintf(&tl, "Ihanda ang gadgets at flashlight.\n")
	fmt.Fprintf(&en, "🇬🇧 Power interruption in parts of %s\n%s · %s · Reason: %s\n",
		a.City, dateEN, placeEN, reason[1])
	if hasTrend {
		fmt.Fprintf(&en, "%s\n", trendEN)
	}
	fmt.Fprintf(&en, "Charge gadgets and prepare flashlights.\n")
	fmt.Fprintf(&ja, "🇯🇵 %sの一部で計画停電\n%s・%s・理由：%s\n",
		cityJA, dateJA, placeJA, reason[2])
	if hasTrend {
		fmt.Fprintf(&ja, "%s\n", trendJA)
	}
	fmt.Fprintf(&ja, "端末の充電と懐中電灯の準備を。\n")
	details := "Details: " + a.SourceURL
	footer := details + "\nSource: Meralco maintenance schedule · Hindi opisyal na Meralco page"
	return joinAlertBlocks(tl.String(), en.String(), ja.String(), footer)
}

// ordinalEN renders 1 -> "1st", 2 -> "2nd", 11 -> "11th".
func ordinalEN(n int) string {
	suf := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suf = "st"
		case 2:
			suf = "nd"
		case 3:
			suf = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suf)
}
