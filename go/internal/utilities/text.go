package utilities

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Post text rendering (template only, no LLM): RenderFuelPost covers
// the newest oil adjustment (+ the NCR pump week before it) and
// RenderBillsPost covers Meralco + LPG + Maynilad. TL first, then EN,
// then JA, separated by Separator. No "$", no nested parentheses, no
// markdown. Minus is U+2212, like the weekly post.

// Separator divides the TL/EN/JA blocks (same bar as weekly posts).
const Separator = "━━━━━━━━━━"

// Minus is U+2212 (typographic minus, never hyphen).
const Minus = "−"

var tlShort = map[time.Month]string{
	time.January: "Ene", time.February: "Peb", time.March: "Mar",
	time.April: "Abr", time.May: "May", time.June: "Hun",
	time.July: "Hul", time.August: "Ago", time.September: "Set",
	time.October: "Okt", time.November: "Nob", time.December: "Dis",
}

var tlFull = map[time.Month]string{
	time.January: "Enero", time.February: "Pebrero", time.March: "Marso",
	time.April: "Abril", time.May: "Mayo", time.June: "Hunyo",
	time.July: "Hulyo", time.August: "Agosto", time.September: "Setyembre",
	time.October: "Oktubre", time.November: "Nobyembre", time.December: "Disyembre",
}

var enShort = map[time.Month]string{
	time.January: "Jan", time.February: "Feb", time.March: "Mar",
	time.April: "Apr", time.May: "May", time.June: "Jun",
	time.July: "Jul", time.August: "Aug", time.September: "Sep",
	time.October: "Oct", time.November: "Nov", time.December: "Dec",
}

var enFull = map[time.Month]string{
	time.January: "January", time.February: "February", time.March: "March",
	time.April: "April", time.May: "May", time.June: "June",
	time.July: "July", time.August: "August", time.September: "September",
	time.October: "October", time.November: "November", time.December: "December",
}

// mustDate parses YYYY-MM-DD (or its date prefix).
func mustDate(s string) (time.Time, bool) {
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// dayRange formats EffectiveFrom..EffectiveTo with the year
// ("Okt 20–26, 2026" / "Oct 20–26, 2026" / "10/20〜10/26").
func dayRange(from, to string) (tl, en, ja string) {
	t1, ok1 := mustDate(from)
	t2, ok2 := mustDate(to)
	if !ok1 || !ok2 {
		return from + "–" + to, from + "–" + to, from + "〜" + to
	}
	if t1.Month() == t2.Month() && t1.Year() == t2.Year() {
		tl = fmt.Sprintf("%s %d–%d, %d", tlShort[t1.Month()], t1.Day(), t2.Day(), t1.Year())
		en = fmt.Sprintf("%s %d–%d, %d", enShort[t1.Month()], t1.Day(), t2.Day(), t1.Year())
	} else if t1.Year() == t2.Year() {
		tl = fmt.Sprintf("%s %d–%s %d, %d", tlShort[t1.Month()], t1.Day(), tlShort[t2.Month()], t2.Day(), t1.Year())
		en = fmt.Sprintf("%s %d–%s %d, %d", enShort[t1.Month()], t1.Day(), enShort[t2.Month()], t2.Day(), t1.Year())
	} else {
		tl = fmt.Sprintf("%s %d, %d–%s %d, %d", tlShort[t1.Month()], t1.Day(), t1.Year(), tlShort[t2.Month()], t2.Day(), t2.Year())
		en = fmt.Sprintf("%s %d, %d–%s %d, %d", enShort[t1.Month()], t1.Day(), t1.Year(), enShort[t2.Month()], t2.Day(), t2.Year())
	}
	ja = fmt.Sprintf("%d/%d〜%d/%d", int(t1.Month()), t1.Day(), int(t2.Month()), t2.Day())
	return tl, en, ja
}

// ShortRange3 formats a week/period without the year in all languages
// ("Okt 13–19" / "Oct 13–19" / "10/13〜10/19"), for site rendering.
func ShortRange3(from, to string) (tl, en, ja string) { return shortRange(from, to) }

// DayRange3 formats an effective range with the year in all languages,
// for site rendering.
func DayRange3(from, to string) (tl, en, ja string) { return dayRange(from, to) }

// MonthTitles formats a Meralco month in all languages, for site rendering.
func MonthTitles(month string) (tl, en, ja string, ok bool) { return monthTitle(month) }

// NCRCommon looks up one NCR summary product common price, for site rendering.
func NCRCommon(sum PumpSummary, product string) (float64, bool) { return ncrCommon(sum, product) }

// CityCommon looks up one city product common price (nil for #N/A),
// reporting row presence, for site rendering.
func CityCommon(sum *PumpSummary, city, product string) (*float64, bool) {
	return cityProduct(sum, city, product)
}

// TrendTable returns the pump now/previous summaries for site
// rendering (nil previous when only one week is known).
func TrendTable(s *Snapshot) (now, prev *PumpSummary) { return pumpPair(s) }

// shortRange formats a week/period without the year
// ("Okt 13–19" / "Oct 13–19" / "10/13〜10/19").
func shortRange(from, to string) (tl, en, ja string) {
	t1, ok1 := mustDate(from)
	t2, ok2 := mustDate(to)
	if !ok1 || !ok2 {
		return from + "–" + to, from + "–" + to, from + "〜" + to
	}
	if t1.Month() == t2.Month() {
		tl = fmt.Sprintf("%s %d–%d", tlShort[t1.Month()], t1.Day(), t2.Day())
		en = fmt.Sprintf("%s %d–%d", enShort[t1.Month()], t1.Day(), t2.Day())
	} else {
		tl = fmt.Sprintf("%s %d–%s %d", tlShort[t1.Month()], t1.Day(), tlShort[t2.Month()], t2.Day())
		en = fmt.Sprintf("%s %d–%s %d", enShort[t1.Month()], t1.Day(), enShort[t2.Month()], t2.Day())
	}
	ja = fmt.Sprintf("%d/%d〜%d/%d", int(t1.Month()), t1.Day(), int(t2.Month()), t2.Day())
	return tl, en, ja
}

// ShortDay formats one date ("Okt 26" / "Oct 26" / "10/26").
func ShortDay(s string) (tl, en, ja string) {
	return shortDay(s)
}

// shortDay formats one date ("Okt 26" / "Oct 26" / "10/26").
func shortDay(s string) (tl, en, ja string) {
	t, ok := mustDate(s)
	if !ok {
		return s, s, s
	}
	return fmt.Sprintf("%s %d", tlShort[t.Month()], t.Day()),
		fmt.Sprintf("%s %d", enShort[t.Month()], t.Day()),
		fmt.Sprintf("%d/%d", int(t.Month()), t.Day())
}

// DayRangeTL formats EffectiveFrom..EffectiveTo TL ("Okt 20–26, 2026").
func DayRangeTL(from, to string) string {
	tl, _, _ := dayRange(from, to)
	return tl
}

// ShortRangeTL formats a week/period TL without the year ("Okt 13–19").
func ShortRangeTL(from, to string) string {
	tl, _, _ := shortRange(from, to)
	return tl
}

// DayLabelTL formats one date TL ("Okt 20").
func DayLabelTL(date string) string {
	t, ok := mustDate(date)
	if !ok {
		return date
	}
	return fmt.Sprintf("%s %d", tlShort[t.Month()], t.Day())
}

// MonthShortTL returns the short TL month name for a YYYY-MM-DD date
// ("Set" for September), for chart detail lines.
func MonthShortTL(date string) (string, bool) {
	t, ok := mustDate(date)
	if !ok {
		return "", false
	}
	return tlShort[t.Month()], true
}

// MonthTitleTL formats a Meralco month ("2026-10" → "Oktubre 2026").
func MonthTitleTL(month string) (string, bool) {
	tl, _, _, ok := monthTitle(month)
	return tl, ok
}

// fmtFuel prints a signed per-litre amount with 2 decimals.
func fmtFuel(v float64) string {
	if v < 0 {
		return fmt.Sprintf("−₱%.2f", -v)
	}
	return fmt.Sprintf("+₱%.2f", v)
}

// fmtFuelRange prints a fuel adjustment ("−₱4.30" or the
// "−₱3.00 hanggang −₱3.02" / "to" / "〜" range form).
func fmtFuelRange(r *Range, lang string) string {
	if r.Lo == r.Hi {
		return fmtFuel(r.Lo)
	}
	switch lang {
	case "ja":
		return fmtFuel(r.Lo) + "〜" + fmtFuel(r.Hi)
	case "en":
		return fmtFuel(r.Lo) + " to " + fmtFuel(r.Hi)
	default:
		return fmtFuel(r.Lo) + " hanggang " + fmtFuel(r.Hi)
	}
}

// fmtRate4 prints a signed Meralco amount with 4 decimals.
func fmtRate4(v float64) string {
	if v < 0 {
		return fmt.Sprintf("−₱%.4f", -v)
	}
	return fmt.Sprintf("+₱%.4f", v)
}

// commas groups whole pesos with thousands separators.
func commas(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return Minus + s
	}
	return s
}

// fmtWhole prints signed whole pesos with thousands separators.
func fmtWhole(v float64) string {
	rounded := int64(v + 0.5)
	if v < 0 {
		rounded = int64(v - 0.5)
	}
	sign := "+"
	if rounded < 0 {
		sign = ""
	}
	return sign + "₱" + commas(abs64(rounded))
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// fmtWholeU prints unsigned whole pesos with thousands separators.
func fmtWholeU(v float64) string {
	if v < 0 {
		v = -v
	}
	return "₱" + commas(int64(v+0.5))
}

// fmtWholeInt prints a signed integer peso amount.
func fmtWholeInt(n int) string {
	if n < 0 {
		return "−₱" + commas(abs64(int64(n)))
	}
	return "+₱" + commas(int64(n))
}

// basePump returns the newest NCR pump summary whose WeekTo predates
// the adjustment's effective date ("before this change").
func basePump(s *Snapshot) *PumpSummary {
	if len(s.Fuel.Adjustments) == 0 {
		return nil
	}
	eff := s.Fuel.Adjustments[0].EffectiveFrom
	var best *PumpSummary
	for i := range s.Fuel.NCRPump {
		p := &s.Fuel.NCRPump[i]
		if p.WeekTo < eff && (best == nil || p.WeekFrom > best.WeekFrom) {
			best = p
		}
	}
	return best
}

// pumpPair returns the newest pump week ending before the adjustment
// plus the pump week before it (for now-vs-previous comparisons).
func pumpPair(s *Snapshot) (now, prev *PumpSummary) {
	now = basePump(s)
	if now == nil {
		return nil, nil
	}
	for i := range s.Fuel.NCRPump {
		p := &s.Fuel.NCRPump[i]
		if p.WeekFrom < now.WeekFrom && (prev == nil || p.WeekFrom > prev.WeekFrom) {
			prev = p
		}
	}
	return now, prev
}

// ncrCommon looks up one NCR summary product common price.
func ncrCommon(sum PumpSummary, product string) (float64, bool) {
	for _, p := range sum.Prices {
		if p.Product == product {
			return p.Common, true
		}
	}
	return 0, false
}

// cityProduct looks up one city product row: its common price (nil for
// #N/A) and whether the row exists at all.
func cityProduct(sum *PumpSummary, city, product string) (common *float64, present bool) {
	if sum == nil {
		return nil, false
	}
	for i := range sum.Cities {
		if sum.Cities[i].City != city {
			continue
		}
		for j := range sum.Cities[i].Prices {
			if sum.Cities[i].Prices[j].Product == product {
				return sum.Cities[i].Prices[j].Common, true
			}
		}
	}
	return nil, false
}

// trendProducts are the pump products shown now vs previous week.
var trendProducts = []struct {
	key, tl, ja string
}{
	{"RON91", "RON 91", "RON91"},
	{"RON95", "RON 95", "RON95"},
	{"Diesel", "Diesel", "軽油"},
}

// trendCities are the nearby cities shown (Las Piñas is not in DOE's
// monitoring).
var trendCities = []struct {
	name, ja string
}{
	{"Parañaque", "パラニャケ"},
	{"Muntinlupa", "ムンティンルパ"},
}

// fmtPrice2 prints an unsigned price level with 2 decimals.
func fmtPrice2(v float64) string {
	if v < 0 {
		v = -v
	}
	return fmt.Sprintf("₱%.2f", v)
}

// fuelCumulative sums signed per-week minima and maxima over the first
// n adjustments (gasoline and diesel; kerosene is omitted from the
// line). It returns the weeks counted, the oldest-from..newest-to
// range, and the cumulative ranges (nil when the fuel never appears).
func fuelCumulative(adjs []OilAdjustment, n int) (weeks int, from, to string, gas, die *Range) {
	if n > len(adjs) {
		n = len(adjs)
	}
	if n == 0 {
		return 0, "", "", nil, nil
	}
	shown := adjs[:n]
	sum := func(get func(OilAdjustment) *Range) *Range {
		var out Range
		first := true
		for _, a := range shown {
			r := get(a)
			if r == nil {
				continue
			}
			lo, hi := r.Lo, r.Hi
			if hi < lo {
				lo, hi = hi, lo
			}
			if first {
				out = Range{Lo: lo, Hi: hi}
				first = false
			} else {
				out.Lo += lo
				out.Hi += hi
			}
		}
		if first {
			return nil
		}
		return &out
	}
	gas = sum(func(a OilAdjustment) *Range { return a.Gasoline })
	die = sum(func(a OilAdjustment) *Range { return a.Diesel })
	return n, shown[len(shown)-1].EffectiveFrom, shown[0].EffectiveTo, gas, die
}

// lastWeekLine renders the previous adjustment line per language
// (empty when Adjustments[1] is missing).
func lastWeekLine(s *Snapshot) (tl, en, ja string, ok bool) {
	if len(s.Fuel.Adjustments) < 2 {
		return "", "", "", false
	}
	prev := s.Fuel.Adjustments[1]
	var fuels []struct {
		tl, en, ja string
		r          *Range
	}
	for _, f := range fuelEntries(prev) {
		if f.r != nil {
			fuels = append(fuels, f)
		}
	}
	if len(fuels) == 0 {
		return "", "", "", false
	}
	ptl, pen, pja := shortRange(prev.EffectiveFrom, prev.EffectiveTo)
	var tsegs, esegs, jsegs []string
	for _, f := range fuels {
		tsegs = append(tsegs, fmt.Sprintf("%s %s", strings.ToLower(f.tl), fmtFuelRange(f.r, "tl")))
		esegs = append(esegs, fmt.Sprintf("%s %s", strings.ToLower(f.en), fmtFuelRange(f.r, "en")))
		jsegs = append(jsegs, fmt.Sprintf("%s %s", f.ja, fmtFuelRange(f.r, "ja")))
	}
	tl = fmt.Sprintf("Noong nakaraang linggo (%s): %s", ptl, strings.Join(tsegs, " · "))
	en = fmt.Sprintf("Last week (%s): %s", pen, strings.Join(esegs, " · "))
	ja = fmt.Sprintf("先週（%s）：%s", pja, strings.Join(jsegs, "・"))
	return tl, en, ja, true
}

// cumulativeLine renders the multi-week total line per language.
func cumulativeLine(s *Snapshot) (tl, en, ja string, ok bool) {
	n, from, to, gas, die := fuelCumulative(s.Fuel.Adjustments, 6)
	if n == 0 || (gas == nil && die == nil) {
		return "", "", "", false
	}
	ctl, cen, cja := shortRange(from, to)
	var tsegs, esegs, jsegs []string
	if gas != nil {
		tsegs = append(tsegs, fmt.Sprintf("gasolina %s", fmtFuelRange(gas, "tl")))
		esegs = append(esegs, fmt.Sprintf("gasoline %s", fmtFuelRange(gas, "en")))
		jsegs = append(jsegs, fmt.Sprintf("ガソリン %s", fmtFuelRange(gas, "ja")))
	}
	if die != nil {
		tsegs = append(tsegs, fmt.Sprintf("diesel %s", fmtFuelRange(die, "tl")))
		esegs = append(esegs, fmt.Sprintf("diesel %s", fmtFuelRange(die, "en")))
		jsegs = append(jsegs, fmt.Sprintf("軽油 %s", fmtFuelRange(die, "ja")))
	}
	tl = fmt.Sprintf("Kabuuan sa huling %d na linggo (%s): %s", n, ctl, strings.Join(tsegs, " · "))
	weeksWord := fmt.Sprintf("last %d weeks", n)
	if n == 1 {
		weeksWord = "last 1 week"
	}
	en = fmt.Sprintf("Total over the %s (%s): %s", weeksWord, cen, strings.Join(esegs, " · "))
	ja = fmt.Sprintf("直近%d週間の合計（%s）：%s", n, cja, strings.Join(jsegs, "・"))
	return tl, en, ja, true
}

// pumpTrendLines renders the compact pump block per language: ONE
// header with both periods, compact value cells without repeated
// "vs …", one row per area, and the nearby-cities note. Never two
// parenthesised groups back to back, never nested parens.
func pumpTrendLines(s *Snapshot) (tl, en, ja string, ok bool) {
	now, prev := pumpPair(s)
	if now == nil {
		return "", "", "", false
	}
	ntl, nen, nja := shortRange(now.WeekFrom, now.WeekTo)
	tlH := "📍 Presyo sa bomba, DOE " + ntl
	enH := "📍 Pump prices, DOE " + nen
	jaH := "📍 店頭価格（DOE、" + nja
	if prev != nil {
		prtl, pren, prja := shortRange(prev.WeekFrom, prev.WeekTo)
		tlH += " vs " + prtl
		enH += " vs " + pren
		jaH += "、前週" + prja + "比"
	}
	tlH += ", bago ang galaw ngayong linggo:"
	enH += ", before this week's change:"
	jaH += "、今回の改定前）："

	// One row per area; cells joined " · " / "・". A #N/A row yields
	// the no-common-price wording instead of a range substitution.
	rowCells := func(get func(key string) (now, prev *float64, present bool)) (tl, en, ja []string, any bool) {
		for _, p := range trendProducts {
			nowV, prevV, present := get(p.key)
			if !present {
				continue
			}
			any = true
			mk := func(label, nilWord string) string {
				if nowV == nil {
					return label + " " + nilWord
				}
				c := label + " " + fmtPrice2(*nowV)
				if prevV != nil {
					c += " " + fmtFuel(*nowV-*prevV)
				}
				return c
			}
			tl = append(tl, mk(p.tl, "walang common price"))
			en = append(en, mk(p.tl, "no common price"))
			ja = append(ja, mk(p.ja, "一般価格なし"))
		}
		return tl, en, ja, any
	}
	var tlRows, enRows, jaRows []string
	if t, e, j, ok := rowCells(func(key string) (*float64, *float64, bool) {
		nv, nok := ncrCommon(*now, key)
		if !nok {
			return nil, nil, false
		}
		var pv *float64
		if prev != nil {
			if v, ok := ncrCommon(*prev, key); ok {
				vv := v
				pv = &vv
			}
		}
		return &nv, pv, true
	}); ok {
		tlRows = append(tlRows, "NCR common: "+strings.Join(t, " · "))
		enRows = append(enRows, "NCR common: "+strings.Join(e, " · "))
		jaRows = append(jaRows, "NCR一般："+strings.Join(j, "・"))
	}
	for _, c := range trendCities {
		if t, e, j, ok := rowCells(func(key string) (*float64, *float64, bool) {
			nv, present := cityProduct(now, c.name, key)
			if !present {
				return nil, nil, false
			}
			pv, _ := cityProduct(prev, c.name, key)
			return nv, pv, true
		}); ok {
			tlRows = append(tlRows, c.name+": "+strings.Join(t, " · "))
			enRows = append(enRows, c.name+": "+strings.Join(e, " · "))
			jaRows = append(jaRows, c.ja+"："+strings.Join(j, "・"))
		}
	}
	if len(tlRows) == 0 {
		return "", "", "", false
	}
	tl = tlH + "\n" + strings.Join(tlRows, "\n") + "\nParañaque at Muntinlupa: malapit sa Las Piñas; wala sa monitoring ng DOE ang Las Piñas"
	en = enH + "\n" + strings.Join(enRows, "\n") + "\nParañaque and Muntinlupa are nearby; Las Piñas is not in DOE's monitoring"
	ja = jaH + "\n" + strings.Join(jaRows, "\n") + "\nパラニャケ・ムンティンルパは近隣市。ラスピニャスはDOE調査対象外"
	return tl, en, ja, true
}

// fuelEntries returns the mentioned fuels in fixed order.
func fuelEntries(adj OilAdjustment) []struct {
	tl, en, ja string
	r          *Range
} {
	return []struct {
		tl, en, ja string
		r          *Range
	}{
		{"Gasolina", "Gasoline", "ガソリン", adj.Gasoline},
		{"Diesel", "Diesel", "軽油", adj.Diesel},
		{"Kerosene", "Kerosene", "灯油", adj.Kerosene},
	}
}

// RenderFuelPost renders the fuel price post for a snapshot.
func RenderFuelPost(s *Snapshot) (string, error) {
	if len(s.Fuel.Adjustments) == 0 {
		return "", fmt.Errorf("utilities: no oil adjustments")
	}
	adj := s.Fuel.Adjustments[0]
	var fuels []struct {
		tl, en, ja string
		r          *Range
	}
	for _, f := range fuelEntries(adj) {
		if f.r != nil {
			fuels = append(fuels, f)
		}
	}
	if len(fuels) == 0 {
		return "", fmt.Errorf("utilities: adjustment mentions no fuel")
	}
	tlRange, enRange, jaRange := dayRange(adj.EffectiveFrom, adj.EffectiveTo)

	var tl, en, ja strings.Builder
	fmt.Fprintf(&tl, "⛽ Presyo ng langis — epektibo %s\n", tlRange)
	fmt.Fprintf(&en, "🇬🇧 Fuel prices — effective %s\n", enRange)
	fmt.Fprintf(&ja, "🇯🇵 燃料価格 — %s適用\n", jaRange)

	var enSegs, jaSegs []string
	for _, f := range fuels {
		fmt.Fprintf(&tl, "• %s: %s/L\n", f.tl, fmtFuelRange(f.r, "tl"))
		enSegs = append(enSegs, fmt.Sprintf("%s %s/L", f.en, fmtFuelRange(f.r, "en")))
		jaSegs = append(jaSegs, fmt.Sprintf("%s %s/L", f.ja, fmtFuelRange(f.r, "ja")))
	}
	fmt.Fprintf(&en, "%s\n", strings.Join(enSegs, " · "))
	fmt.Fprintf(&ja, "%s\n", strings.Join(jaSegs, "・"))

	// Per-10-litre derivations for gasoline + diesel.
	if adj.Gasoline != nil && adj.Diesel != nil {
		g10 := &Range{Lo: adj.Gasoline.Lo * 10, Hi: adj.Gasoline.Hi * 10}
		d10 := &Range{Lo: adj.Diesel.Lo * 10, Hi: adj.Diesel.Hi * 10}
		fmt.Fprintf(&tl, "💡 Sa 10 litro: gasolina %s, diesel %s\n", fmtFuelRange(g10, "tl"), fmtFuelRange(d10, "tl"))
		fmt.Fprintf(&en, "Per 10 L: gasoline %s, diesel %s\n", fmtFuelRange(g10, "en"), fmtFuelRange(d10, "en"))
		fmt.Fprintf(&ja, "10Lあたり：ガソリン %s、軽油 %s\n", fmtFuelRange(g10, "ja"), fmtFuelRange(d10, "ja"))
	}

	// Last week's adjustment (trend).
	if ltl, lel, lja, ok := lastWeekLine(s); ok {
		fmt.Fprintf(&tl, "%s\n", ltl)
		fmt.Fprintf(&en, "%s\n", lel)
		fmt.Fprintf(&ja, "%s\n", lja)
	} else {
		fmt.Fprintf(&tl, "Walang datos ng nakaraang linggo.\n")
		fmt.Fprintf(&en, "Previous week not available.\n")
		fmt.Fprintf(&ja, "前回データなし。\n")
	}

	// Multi-week cumulative over the charted adjustments.
	if ctl, cen, cja, ok := cumulativeLine(s); ok {
		fmt.Fprintf(&tl, "%s\n", ctl)
		fmt.Fprintf(&en, "%s\n", cen)
		fmt.Fprintf(&ja, "%s\n", cja)
	}

	// Pump prices now vs previous week (NCR + nearby cities).
	if ptl, pen, pja, ok := pumpTrendLines(s); ok {
		fmt.Fprintf(&tl, "%s\n", ptl)
		fmt.Fprintf(&en, "%s\n", pen)
		fmt.Fprintf(&ja, "%s\n", pja)
	}

	footer := "Source: DOE Oil Monitor & NCR pump price monitoring (doe.gov.ph) · Hindi opisyal na DOE page"
	return strings.TrimRight(tl.String(), "\n") + "\n" + Separator + "\n" +
		strings.TrimRight(en.String(), "\n") + "\n" + Separator + "\n" +
		strings.TrimRight(ja.String(), "\n") + "\n" + Separator + "\n" + footer, nil
}

// monthTitle formats Meralco Month ("2026-10") per language.
func monthTitle(month string) (tl, en, ja string, ok bool) {
	var y, m int
	if _, err := fmt.Sscanf(month, "%d-%d", &y, &m); err != nil || m < 1 || m > 12 {
		return "", "", "", false
	}
	mo := time.Month(m)
	return fmt.Sprintf("%s %d", tlFull[mo], y),
		fmt.Sprintf("%s %d", enFull[mo], y),
		fmt.Sprintf("%d年%d月", y, m), true
}

// cityRows collects up to 3 displayable Maynilad rows for the status
// city (interruptions across kinds, then maintenance).
func cityRows(w *WaterStatus) []struct {
	barangays, when, why string
} {
	norm := func(s string) string {
		s = foldAccent(strings.TrimSpace(s))
		s = strings.TrimSuffix(s, " city")
		return s
	}
	want := norm(w.City)
	var out []struct {
		barangays, when, why string
	}
	add := func(barangays, when, why string) {
		if len(out) >= 3 {
			return
		}
		out = append(out, struct {
			barangays, when, why string
		}{barangays, when, why})
	}
	lists := [][]Interruption{w.Scheduled, w.Emergency, w.Rotational}
	for _, list := range lists {
		for _, in := range list {
			if norm(in.City) != want {
				continue
			}
			when := ""
			if t, ok := mustDate(in.From); ok {
				when = fmt.Sprintf("%s %d", tlShort[t.Month()], t.Day())
				if in.To != "" && in.To != in.From {
					if t2, ok := mustDate(in.To); ok {
						when += fmt.Sprintf("–%s %d", tlShort[t2.Month()], t2.Day())
					}
				}
				if in.Time != "" {
					when += ", " + in.Time
				}
			} else if in.FromRaw != "" {
				when = in.FromRaw
				if in.Time != "" {
					when += ", " + in.Time
				}
			} else {
				when = in.Time
			}
			add(in.Barangays, when, in.Reason)
		}
	}
	for _, mnt := range w.Maintenance {
		add(mnt.Barangays, mnt.Schedule, mnt.Activity)
	}
	return out
}

// jaCity renders the status city for the Japanese block.
func jaCity(city string) string {
	if foldAccent(city) == "las pinas" {
		return "ラスピニャス"
	}
	return city
}

// r2 rounds to cents.
func r2(v float64) float64 { return math.Round(v*100) / 100 }

// fmtCents prints unsigned with 2 decimals and thousands separators
// ("₱5,499.30").
func fmtCents(v float64) string {
	v = r2(v)
	if v < 0 {
		v = -v
	}
	ip := int64(v)
	fp := int64(math.Round((v - float64(ip)) * 100))
	if fp == 100 {
		ip++
		fp = 0
	}
	return "₱" + commas(ip) + fmt.Sprintf(".%02d", fp)
}

// fmtCentsSigned prints signed with 2 decimals ("+₱157.62", "−₱8.00"
// with U+2212).
func fmtCentsSigned(v float64) string {
	if r2(v) < 0 {
		return "−" + fmtCents(v)
	}
	return "+" + fmtCents(v)
}

// splitMonth parses "2006-01".
func splitMonth(month string) (y, m int, ok bool) {
	if _, err := fmt.Sscanf(month, "%d-%d", &y, &m); err != nil || m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, m, true
}

// prevMonthFull returns the full TL/EN names of the month before M
// ("Setyembre", "September").
func prevMonthFull(month string) (tl, en string, ok bool) {
	y, m, ok := splitMonth(PrevMonth(month))
	if !ok {
		return "", "", false
	}
	_ = y
	return tlFull[time.Month(m)], enFull[time.Month(m)], true
}

// prevMonthShort returns the short TL/EN names of the month before M.
func prevMonthShort(month string) (tl, en string, ok bool) {
	y, m, ok := splitMonth(PrevMonth(month))
	if !ok {
		return "", "", false
	}
	_ = y
	return tlShort[time.Month(m)], enShort[time.Month(m)], true
}

// PrevMonthShortTL returns the short TL name of the month before M
// ("Set" for "2026-10"), for chart chips.
func PrevMonthShortTL(month string) (string, bool) {
	tl, _, ok := prevMonthShort(month)
	return tl, ok
}

// prevMonthNum returns the calendar month number before M (for JA "9月").
func prevMonthNum(month string) (int, bool) {
	_, m, ok := splitMonth(PrevMonth(month))
	if !ok {
		return 0, false
	}
	return m, true
}

// familyFig is the family-total math: exact rounded components.
type familyFig struct {
	Cur, Prev, Change   float64
	Power, LpgC, WaterC float64 // change components
	HasPrev             bool
}

// FamilyFigures computes current = 200×Rate + 1×LPG common + 20×water
// rate(M) and previous from PrevRate + LPG[1] + water rate(M−1).
// ok=false when the current total cannot be built.
func FamilyFigures(s *Snapshot, rateM, rateP *WaterRate) (familyFig, bool) {
	if len(s.Meralco) == 0 || len(s.LPG) == 0 || rateM == nil {
		return familyFig{}, false
	}
	mer, lpg := s.Meralco[0], s.LPG[0]
	var f familyFig
	pc := r2(200 * mer.Rate)
	lc := r2(lpg.Common)
	wc := r2(20 * rateM.Rate)
	f.Cur = pc + lc + wc
	if len(s.LPG) > 1 && rateP != nil {
		pp := r2(200 * mer.PrevRate)
		pl := r2(s.LPG[1].Common)
		pw := r2(20 * rateP.Rate)
		f.Prev = pp + pl + pw
		f.Power = pc - pp
		f.LpgC = lc - pl
		f.WaterC = wc - pw
		f.Change = f.Cur - f.Prev
		f.HasPrev = true
	}
	return f, true
}

// waterRateLines renders the Maynilad average-basic-charge + FCDA
// old→new line per language (rateM required; rateP optional).
func waterRateLines(s *Snapshot, month string) (tl, en, ja string, ok bool) {
	rateM := WaterRateFor(s.WaterRates, month)
	if rateM == nil {
		return "", "", "", false
	}
	rateP := WaterRateFor(s.WaterRates, PrevMonth(month))
	cur := fmtCents(rateM.Rate)
	day := DayLabelTL(rateM.Effective)
	_, dayEN, dayJA := ShortDay(rateM.Effective)
	if rateP == nil {
		tl = fmt.Sprintf("💧 Tubig Maynilad, average basic charge + FCDA: %s/cu.m", cur)
		en = fmt.Sprintf("💧 Maynilad water, average basic charge + FCDA: %s/cu.m", cur)
		ja = fmt.Sprintf("💧 Maynilad水道・基本料金＋FCDA：%s/cu.m", cur)
		return tl, en, ja, true
	}
	if rateM.Rate == rateP.Rate {
		tl = fmt.Sprintf("💧 Tubig Maynilad, average basic charge + FCDA: %s/cu.m — walang pagbabago ngayong buwan", cur)
		en = fmt.Sprintf("💧 Maynilad water, average basic charge + FCDA: %s/cu.m — no change this month", cur)
		ja = fmt.Sprintf("💧 Maynilad水道・基本料金＋FCDA：%s/cu.m — 今月変更なし", cur)
		return tl, en, ja, true
	}
	d := fmtCentsSigned(r2(rateM.Rate - rateP.Rate))
	tl = fmt.Sprintf("💧 Tubig Maynilad, average basic charge + FCDA: %s → %s/cu.m, %s mula %s",
		fmtCents(rateP.Rate), cur, d, day)
	en = fmt.Sprintf("💧 Maynilad water, average basic charge + FCDA: %s → %s/cu.m, %s from %s",
		fmtCents(rateP.Rate), cur, d, dayEN)
	ja = fmt.Sprintf("💧 Maynilad水道・基本料金＋FCDA：%s → %s/cu.m、%s（%sから）",
		fmtCents(rateP.Rate), cur, d, dayJA)
	return tl, en, ja, true
}

// RenderBillsPost renders the household-bills post for a snapshot.
func RenderBillsPost(s *Snapshot) (string, error) {
	if len(s.Meralco) == 0 {
		return "", fmt.Errorf("utilities: no meralco rates")
	}
	if len(s.LPG) == 0 {
		return "", fmt.Errorf("utilities: no LPG months")
	}
	mer := s.Meralco[0]
	lpg := s.LPG[0]
	tlTitle, enTitle, jaTitle, ok := monthTitle(mer.Month)
	if !ok {
		return "", fmt.Errorf("utilities: bad meralco month %q", mer.Month)
	}

	var tl, en, ja strings.Builder
	fmt.Fprintf(&tl, "🏠 Bayarin sa bahay — %s\n", tlTitle)
	fmt.Fprintf(&en, "🇬🇧 Household bills — %s\n", enTitle)
	fmt.Fprintf(&ja, "🇯🇵 家計の光熱費 — %s\n", jaTitle)

	prevTL, prevEN, _ := prevMonthFull(mer.Month)
	prevNum, _ := prevMonthNum(mer.Month)

	// Meralco old → new.
	rateU := strings.TrimPrefix(fmtRate4(mer.Rate), "+")
	prevU := strings.TrimPrefix(fmtRate4(mer.PrevRate), "+")
	fmt.Fprintf(&tl, "⚡ Meralco: %s → %s/kWh, %s vs %s\n", prevU, rateU, fmtRate4(mer.Adjustment), prevTL)
	fmt.Fprintf(&en, "⚡ Meralco: %s → %s/kWh, %s vs %s\n", prevU, rateU, fmtRate4(mer.Adjustment), prevEN)
	fmt.Fprintf(&ja, "⚡ Meralco：%s → %s/kWh、%s（%d月比）\n", prevU, rateU, fmtRate4(mer.Adjustment), prevNum)
	fmt.Fprintf(&tl, "Sa 200 kWh: mga %s sa bill\n", fmtWholeInt(mer.Impact200))
	fmt.Fprintf(&en, "At 200 kWh: about %s on the bill\n", fmtWholeInt(mer.Impact200))
	fmt.Fprintf(&ja, "200kWhで約%s\n", fmtWholeInt(mer.Impact200))

	// LPG old → new.
	common := fmtWholeU(lpg.Common)
	if len(s.LPG) > 1 {
		prev2 := s.LPG[1]
		d := lpg.Common - prev2.Common
		p2tl, p2en, p2ja := shortRange(prev2.PeriodFrom, prev2.PeriodTo)
		p2m := ""
		if _, m, ok := splitMonth(prev2.PeriodFrom[:7]); ok {
			p2m = tlShort[time.Month(m)]
		}
		p2me := ""
		if _, m, ok := splitMonth(prev2.PeriodFrom[:7]); ok {
			p2me = enShort[time.Month(m)]
		}
		p2mj := 0
		if _, m, ok := splitMonth(prev2.PeriodFrom[:7]); ok {
			p2mj = m
		}
		fmt.Fprintf(&tl, "🔥 LPG 11 kg NCR, karaniwang presyo (DOE): %s → %s, %s vs %s\n",
			fmtWholeU(prev2.Common), common, fmtWhole(d), p2tl)
		fmt.Fprintf(&en, "🔥 LPG 11 kg NCR, common price (DOE): %s → %s, %s vs %s\n",
			fmtWholeU(prev2.Common), common, fmtWhole(d), p2en)
		fmt.Fprintf(&ja, "🔥 LPG 11kg・NCR一般価格（DOE）：%s → %s、%s（%s比）\n",
			fmtWholeU(prev2.Common), common, fmtWhole(d), p2ja)
		lo := "₱" + commas(int64(lpg.CityLow+0.5))
		hi := "₱" + commas(int64(lpg.CityHigh+0.5))
		plo := "₱" + commas(int64(prev2.CityLow+0.5))
		phi := "₱" + commas(int64(prev2.CityHigh+0.5))
		extra := ""
		if lpg.CityCommon != nil {
			extra = fmt.Sprintf(", karaniwan ₱%s", commas(int64(*lpg.CityCommon+0.5)))
		}
		fmt.Fprintf(&tl, "Sa Las Piñas: %s–%s, %s: %s–%s%s\n", lo, hi, p2m, plo, phi, extra)
		fmt.Fprintf(&en, "Las Piñas: %s–%s, %s: %s–%s\n", lo, hi, p2me, plo, phi)
		fmt.Fprintf(&ja, "ラスピニャス：%s〜%s、%d月：%s〜%s\n", lo, hi, p2mj, plo, phi)
	} else {
		fmt.Fprintf(&tl, "🔥 LPG 11 kg NCR, karaniwang presyo (DOE): %s\n", common)
		fmt.Fprintf(&en, "🔥 LPG 11 kg NCR, common price (DOE): %s\n", common)
		fmt.Fprintf(&ja, "🔥 LPG 11kg・NCR一般価格（DOE）：%s\n", common)
		if lpg.City != "" {
			lo := "₱" + commas(int64(lpg.CityLow+0.5))
			hi := "₱" + commas(int64(lpg.CityHigh+0.5))
			fmt.Fprintf(&tl, "Sa Las Piñas: %s–%s\n", lo, hi)
			fmt.Fprintf(&en, "Las Piñas: %s–%s\n", lo, hi)
			fmt.Fprintf(&ja, "ラスピニャス：%s〜%s\n", lo, hi)
		}
	}

	// Water rate old → new plus the Las Piñas interruption status.
	rateM := WaterRateFor(s.WaterRates, mer.Month)
	if wtl, wen, wja, ok := waterRateLines(s, mer.Month); ok {
		fmt.Fprintf(&tl, "%s\n", wtl)
		fmt.Fprintf(&en, "%s\n", wen)
		fmt.Fprintf(&ja, "%s\n", wja)
	}
	if s.Water != nil {
		w := s.Water
		rows := cityRows(w)
		ctl, cen, cja := shortDay(w.CheckedAt)
		if len(rows) == 0 {
			fmt.Fprintf(&tl, "💧 Maynilad, Las Piñas: walang naka-schedule na water interruption — tiningnan %s\n", ctl)
			fmt.Fprintf(&en, "💧 Maynilad, Las Piñas: no scheduled water interruptions — checked %s\n", cen)
			fmt.Fprintf(&ja, "💧 Maynilad・%s：計画断水なし（%s確認）\n", jaCity(w.City), cja)
		} else {
			for _, r := range rows {
				fmt.Fprintf(&tl, "💧 Water interruption, Las Piñas: %s, %s, %s\n", r.barangays, r.when, r.why)
				fmt.Fprintf(&en, "💧 Water interruption, Las Piñas: %s, %s, %s\n", r.barangays, r.when, r.why)
				fmt.Fprintf(&ja, "💧 断水・%s：%s、%s、%s\n", jaCity(w.City), r.barangays, r.when, r.why)
			}
		}
	}

	// Family total (exact cents so the sums add up).
	if fig, ok := FamilyFigures(s, rateM, WaterRateFor(s.WaterRates, PrevMonth(mer.Month))); ok {
		prevNameTL, prevNameEN, _ := prevMonthFull(mer.Month)
		prevN, _ := prevMonthNum(mer.Month)
		fmt.Fprintf(&tl, "👪 Tantiyang buwanang gastos ng isang pamilya:\n")
		fmt.Fprintf(&en, "👪 Estimated monthly family spending:\n")
		fmt.Fprintf(&ja, "👪 1か月の家計支出（目安）：\n")
		if fig.HasPrev {
			fmt.Fprintf(&tl, "%s → %s, %s vs %s · kuryente %s, LPG %s, tubig %s\n",
				fmtCents(fig.Prev), fmtCents(fig.Cur), fmtCentsSigned(fig.Change), prevNameTL,
				fmtCentsSigned(fig.Power), fmtCentsSigned(fig.LpgC), fmtCentsSigned(fig.WaterC))
			fmt.Fprintf(&en, "%s → %s, %s vs %s · power %s, LPG %s, water %s\n",
				fmtCents(fig.Prev), fmtCents(fig.Cur), fmtCentsSigned(fig.Change), prevNameEN,
				fmtCentsSigned(fig.Power), fmtCentsSigned(fig.LpgC), fmtCentsSigned(fig.WaterC))
			fmt.Fprintf(&ja, "%s → %s、%s（%d月比）・電気 %s・LPG %s・水道 %s\n",
				fmtCents(fig.Prev), fmtCents(fig.Cur), fmtCentsSigned(fig.Change), prevN,
				fmtCentsSigned(fig.Power), fmtCentsSigned(fig.LpgC), fmtCentsSigned(fig.WaterC))
		} else {
			fmt.Fprintf(&tl, "%s\nWalang datos ng nakaraang kabuuan.\n", fmtCents(fig.Cur))
			fmt.Fprintf(&en, "%s\nPrevious total not available.\n", fmtCents(fig.Cur))
			fmt.Fprintf(&ja, "%s\n前回合計データなし。\n", fmtCents(fig.Cur))
		}
		fmt.Fprintf(&tl, "Palagay: 200 kWh kuryente sa typical-household rate ng Meralco · 1 tangke LPG 11 kg sa NCR common price ng DOE · 20 cu.m tubig sa average basic charge + FCDA ng Maynilad, hindi kasama ang ibang singil at VAT ng tubig · hindi kasama ang gasolina\n")
		fmt.Fprintf(&en, "Assumptions: 200 kWh at Meralco's typical-household rate · one 11-kg LPG tank at DOE's NCR common price · 20 cu.m water at Maynilad's average basic charge + FCDA, excluding other water charges and VAT · fuel not included\n")
		fmt.Fprintf(&ja, "前提：200kWhをMeralco一般家庭料金で・11kg LPG 1本をDOE・NCR一般価格で・水20cu.mをMaynilad基本料金＋FCDAで（その他水道料金・VAT・燃料は含まず）\n")
	}

	footer := "Source: Meralco, DOE LPG monitoring, Maynilad advisories · Hindi opisyal na page"
	return strings.TrimRight(tl.String(), "\n") + "\n" + Separator + "\n" +
		strings.TrimRight(en.String(), "\n") + "\n" + Separator + "\n" +
		strings.TrimRight(ja.String(), "\n") + "\n" + Separator + "\n" + footer, nil
}
