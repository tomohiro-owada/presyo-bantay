package utilities

import (
	"fmt"
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

// fmtWholeInt prints a signed integer peso amount.
func fmtWholeInt(n int) string {
	if n < 0 {
		return "−₱" + commas(abs64(int64(n)))
	}
	return "+₱" + commas(int64(n))
}

// pumpCommons looks up RON91/RON95/Diesel commons in a pump summary.
func pumpCommons(sum PumpSummary) (ron91, ron95, diesel float64, ok bool) {
	found := map[string]float64{}
	for _, p := range sum.Prices {
		found[p.Product] = p.Common
	}
	var has bool
	if ron91, has = found["RON91"]; !has {
		return 0, 0, 0, false
	}
	if ron95, has = found["RON95"]; !has {
		return 0, 0, 0, false
	}
	if diesel, has = found["Diesel"]; !has {
		return 0, 0, 0, false
	}
	return ron91, ron95, diesel, true
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

	// NCR common pump prices before this change.
	if pump := basePump(s); pump != nil {
		if r91, r95, rdie, ok := pumpCommons(*pump); ok {
			ptl, pen, pja := shortRange(pump.WeekFrom, pump.WeekTo)
			f91, f95, fdie := fmtFuel(r91), fmtFuel(r95), fmtFuel(rdie)
			// Pump commons print unsigned (they are price levels).
			c91, c95, cdie := strings.TrimPrefix(f91, "+"), strings.TrimPrefix(f95, "+"), strings.TrimPrefix(fdie, "+")
			fmt.Fprintf(&tl, "📍 Karaniwang presyo sa NCR bago ang galaw na ito (DOE, %s): RON 91 %s · RON 95 %s · Diesel %s\n",
				ptl, c91, c95, cdie)
			fmt.Fprintf(&en, "NCR common pump prices before this change (DOE, %s): RON 91 %s · RON 95 %s · Diesel %s\n",
				pen, c91, c95, cdie)
			fmt.Fprintf(&ja, "今回の改定前のNCR一般価格（DOE、%s）：RON91 %s・RON95 %s・軽油 %s\n",
				pja, c91, c95, cdie)
		}
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

	adj, rate, prev := fmtRate4(mer.Adjustment), fmtRate4(mer.Rate), fmtRate4(mer.PrevRate)
	rateU := strings.TrimPrefix(rate, "+")
	prevU := strings.TrimPrefix(prev, "+")
	fmt.Fprintf(&tl, "⚡ Meralco: %s/kWh → %s/kWh mula %s\n", adj, rateU, prevU)
	fmt.Fprintf(&en, "⚡ Meralco: %s/kWh → %s/kWh, from %s. At 200 kWh: about %s on the bill\n",
		adj, rateU, prevU, fmtWholeInt(mer.Impact200))
	fmt.Fprintf(&ja, "⚡ Meralco：%s/kWh → %s/kWh（前月 %s）。200kWhで約%s\n",
		adj, rateU, prevU, fmtWholeInt(mer.Impact200))
	fmt.Fprintf(&tl, "   Sa 200 kWh: mga %s sa bill\n", fmtWholeInt(mer.Impact200))

	lpgTL, lpgEN, lpgJA := shortRange(lpg.PeriodFrom, lpg.PeriodTo)
	common := "₱" + commas(int64(lpg.Common+0.5))
	diff := ""
	if len(s.LPG) > 1 {
		prev2 := s.LPG[1]
		d := lpg.Common - prev2.Common
		p2tl, p2en, p2ja := shortRange(prev2.PeriodFrom, prev2.PeriodTo)
		diff = fmt.Sprintf(", %s vs %s", fmtWhole(d), p2tl)
		fmt.Fprintf(&tl, "🔥 LPG 11 kg, karaniwang presyo sa NCR (DOE, %s): %s%s\n", lpgTL, common, diff)
		diff = fmt.Sprintf(", %s vs %s", fmtWhole(d), p2en)
		fmt.Fprintf(&en, "🔥 LPG 11 kg, NCR common price (DOE, %s): %s%s. ", lpgEN, common, diff)
		fmt.Fprintf(&ja, "🔥 LPG 11kg・NCR一般価格（DOE、%s）：%s、%s比 %s。", lpgJA, common, p2ja, fmtWhole(d))
	} else {
		fmt.Fprintf(&tl, "🔥 LPG 11 kg, karaniwang presyo sa NCR (DOE, %s): %s\n", lpgTL, common)
		fmt.Fprintf(&en, "🔥 LPG 11 kg, NCR common price (DOE, %s): %s. ", lpgEN, common)
		fmt.Fprintf(&ja, "🔥 LPG 11kg・NCR一般価格（DOE、%s）：%s。", lpgJA, common)
	}
	cityLine := func(lang string) string {
		lo := "₱" + commas(int64(lpg.CityLow+0.5))
		hi := "₱" + commas(int64(lpg.CityHigh+0.5))
		rng := lo + "–" + hi
		if lang == "ja" {
			rng = lo + "〜" + hi
		}
		extra := ""
		if lpg.CityCommon != nil {
			cc := "₱" + commas(int64(*lpg.CityCommon+0.5))
			switch lang {
			case "ja":
				extra = "、一般 " + cc
			case "en":
				extra = ", common " + cc
			default:
				extra = ", karaniwan " + cc
			}
		}
		return rng + extra
	}
	if lpg.City != "" {
		fmt.Fprintf(&tl, "   Sa Las Piñas: %s\n", cityLine("tl"))
		// EN continues the LPG sentence; JA likewise.
		fmt.Fprintf(&en, "Las Piñas: %s\n", cityLine("en"))
		fmt.Fprintf(&ja, "ラスピニャス：%s\n", cityLine("ja"))
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

	footer := "Source: Meralco, DOE LPG monitoring, Maynilad advisories · Hindi opisyal na page"
	return strings.TrimRight(tl.String(), "\n") + "\n" + Separator + "\n" +
		strings.TrimRight(en.String(), "\n") + "\n" + Separator + "\n" +
		strings.TrimRight(ja.String(), "\n") + "\n" + Separator + "\n" + footer, nil
}
