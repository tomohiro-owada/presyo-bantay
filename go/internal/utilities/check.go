package utilities

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CheckPost validates a rendered post against its snapshot: every ₱
// amount (and every bare number attached to /L, /kWh, kWh, …) must
// equal a snapshot value or an allowed derivation (10× fuel
// adjustment, LPG month-on-month diff, Impact200, the 200 kWh / 10 L /
// 11 kg constants) with the correct sign, and product lines must carry
// their own product's numbers (swapped products fail). Lint: no "$",
// no nested parentheses (ASCII and full-width), no "NaN", no empty
// sections. kind is "fuel", "bills" or "water" (water checks
// s.Alerts[0]).
func CheckPost(text string, s *Snapshot, kind string) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if kind != "fuel" && kind != "bills" && kind != "water" && kind != "power" {
		return []string{"unknown kind " + strconv.Quote(kind)}
	}
	lines := strings.Split(text, "\n")

	// Lint: dollar signs, NaN, nested parentheses.
	for i, ln := range lines {
		if strings.Contains(ln, "$") {
			add("line %d: dollar sign", i+1)
		}
		if strings.Contains(ln, "NaN") {
			add("line %d: NaN", i+1)
		}
		if nestedParens(ln) {
			add("line %d: nested parentheses", i+1)
		}
	}
	// Lint: no empty sections (TL/EN/JA/footer).
	parts := strings.Split(text, Separator)
	if len(parts) != 4 {
		add("want 4 blocks separated by %q, got %d", Separator, len(parts))
	} else {
		for i, p := range parts {
			if strings.TrimSpace(p) == "" {
				add("block %d is empty", i+1)
			}
		}
	}

	if kind == "water" {
		return append(problems, checkWaterAlert(text, s)...)
	}
	if kind == "power" {
		return append(problems, checkPowerAlert(text, s)...)
	}

	c := newChecker(s, kind)
	for i, ln := range lines {
		for _, hit := range findAmounts(ln) {
			if !c.allowed[numKey(hit.value)] {
				add("line %d: %s not in snapshot", i+1, hit.raw)
				continue
			}
			// Bare-unit numbers (200 kWh, 11 kg) are constants: the
			// global check suffices; product binding is for ₱.
			if !hit.isPeso {
				continue
			}
			if sub := c.lineSubset(ln); sub != nil {
				if !sub[numKey(hit.value)] {
					add("line %d: %s does not belong to this line", i+1, hit.raw)
				}
			}
		}
		if kind == "fuel" {
			for _, p := range c.checkFuelSegments(i+1, ln) {
				problems = append(problems, p)
			}
		}
	}
	return problems
}

// checkFuelSegments binds each product segment of a combined fuel line
// ("Gasoline A · Diesel B", "gasolina X, diesel Y") to its own
// product's numbers, so swapped products fail. Last-week lines bind to
// Adjustments[1], cumulative lines to the sums, pump lines (RON) to
// their city+product cell (now/prev/delta).
func (c *checker) checkFuelSegments(lineNo int, line string) []string {
	if !strings.Contains(line, "₱") {
		return nil
	}
	if strings.Contains(line, "RON") {
		return c.checkPumpSegments(lineNo, line)
	}
	lower := strings.ToLower(line)
	ten := strings.Contains(lower, "10 litro") || strings.Contains(lower, "per 10 l") || strings.Contains(lower, "10lあたり")
	lastWeek := strings.Contains(lower, "nakaraang linggo") || strings.Contains(lower, "last week") || strings.Contains(line, "先週")
	cumul := strings.Contains(lower, "kabuuan") || strings.Contains(lower, "total over") || strings.Contains(line, "合計")
	segs := strings.FieldsFunc(line, func(r rune) bool {
		return r == '·' || r == '・' || r == ',' || r == '、'
	})
	if len(segs) < 2 && !ten && !lastWeek && !cumul && !strings.HasPrefix(strings.TrimSpace(line), "•") {
		return nil
	}
	var out []string
	for _, seg := range segs {
		k := fuelKey(strings.ToLower(seg))
		if k < 0 {
			continue
		}
		var want map[float64]bool
		switch {
		case ten:
			want = c.ten[k]
		case lastWeek:
			want = c.prev[k]
		case cumul:
			want = c.sums
		default:
			want = c.base[k]
		}
		for _, hit := range findAmounts(seg) {
			if !want[numKey(hit.value)] {
				out = append(out, fmt.Sprintf("line %d: %s does not belong to this product", lineNo, hit.raw))
			}
		}
	}
	return out
}

// pumpSegKey maps a pump line segment to RON91/RON95/Diesel.
func pumpSegKey(seg string) string {
	switch {
	case strings.Contains(seg, "RON 91"), strings.Contains(seg, "RON91"):
		return "RON91"
	case strings.Contains(seg, "RON 95"), strings.Contains(seg, "RON95"):
		return "RON95"
	case strings.Contains(seg, "Diesel"), strings.Contains(seg, "軽油"):
		return "Diesel"
	}
	return ""
}

// pumpSegCity maps a pump line to its area ("NCR", city name, or "").
func pumpSegCity(line string) string {
	switch {
	case strings.Contains(line, "NCR"):
		return "NCR"
	case strings.Contains(line, "Parañaque"), strings.Contains(line, "パラニャケ"):
		return "Parañaque"
	case strings.Contains(line, "Muntinlupa"), strings.Contains(line, "ムンティンルパ"):
		return "Muntinlupa"
	}
	return ""
}

// checkPumpSegments binds each pump-line segment to its city+product
// cell (now common, prev common, delta), so a delta under the wrong
// product or city fails.
func (c *checker) checkPumpSegments(lineNo int, line string) []string {
	city := pumpSegCity(line)
	if city == "" || c.snap == nil {
		return nil
	}
	now, prev := pumpPair(c.snap)
	if now == nil {
		return nil
	}
	segs := strings.FieldsFunc(line, func(r rune) bool {
		return r == '·' || r == '・' || r == ',' || r == '、' || r == ';' || r == '；'
	})
	var out []string
	for _, seg := range segs {
		key := pumpSegKey(seg)
		if key == "" {
			continue
		}
		want := map[float64]bool{}
		var nv, pv *float64
		if city == "NCR" {
			if v, ok := ncrCommon(*now, key); ok {
				nv = &v
			}
			if prev != nil {
				if v, ok := ncrCommon(*prev, key); ok {
					vv := v
					pv = &vv
				}
			}
		} else {
			nv, _ = cityProduct(now, city, key)
			pv, _ = cityProduct(prev, city, key)
		}
		if nv != nil {
			want[numKey(*nv)] = true
		}
		if nv != nil && pv != nil {
			want[numKey(*nv-*pv)] = true
		}
		if pv != nil {
			want[numKey(*pv)] = true
		}
		for _, hit := range findAmounts(seg) {
			if !hit.isPeso {
				continue
			}
			if !want[numKey(hit.value)] {
				out = append(out, fmt.Sprintf("line %d: %s does not belong to %s %s", lineNo, hit.raw, city, key))
			}
		}
	}
	return out
}

// nestedParens reports (...(...)...) depth > 1, counting ASCII and
// full-width opens together.
func nestedParens(s string) bool {
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '（':
			depth++
			if depth > 1 {
				return true
			}
		case ')', '）':
			if depth > 0 {
				depth--
			}
		}
	}
	return false
}

// amountHit is one ₱ amount or bare unit number in a line.
type amountHit struct {
	raw    string
	value  float64 // signed
	isPeso bool
}

var pesoRe = regexp.MustCompile(`([−\-+]?)₱([0-9][0-9,]*(?:\.[0-9]+)?)`)

var unitNumRe = regexp.MustCompile(`([0-9][0-9,]*(?:\.[0-9]+)?)\s*(/L|/kWh|kWh|/kg|/piraso|/pc|kg\b)`)

// findAmounts extracts signed ₱ amounts plus bare numbers attached to
// fuel/bill units (skipping the numeric half of a ₱ amount).
func findAmounts(line string) []amountHit {
	var out []amountHit
	covered := make([]bool, len(line))
	for _, m := range pesoRe.FindAllStringSubmatchIndex(line, -1) {
		sign, num := line[m[2]:m[3]], line[m[4]:m[5]]
		v, err := strconv.ParseFloat(strings.ReplaceAll(num, ",", ""), 64)
		if err != nil {
			continue
		}
		if sign == "−" || sign == "-" {
			v = -v
		}
		out = append(out, amountHit{raw: line[m[0]:m[1]], value: v, isPeso: true})
		for i := m[0]; i < m[1] && i < len(covered); i++ {
			covered[i] = true
		}
	}
	for _, m := range unitNumRe.FindAllStringSubmatchIndex(line, -1) {
		skip := false
		for i := m[0]; i < m[1] && i < len(covered); i++ {
			if covered[i] {
				skip = true
				break
			}
		}
		if !skip && strings.HasSuffix(line[:m[0]], "₱") {
			skip = true
		}
		if skip {
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(line[m[2]:m[3]], ",", ""), 64)
		if err != nil {
			continue
		}
		out = append(out, amountHit{raw: line[m[0]:m[1]], value: v})
	}
	return out
}

// numKey normalizes floats for set membership (renders print ≤ 4 dp).
func numKey(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// checker holds the global allowed set plus the per-line subsets.
type checker struct {
	allowed map[float64]bool
	kind    string
	snap    *Snapshot
	// Fuel values: base[i] per-L, ten[i] 10×, for i in gas/diesel/kero.
	base, ten [3]map[float64]bool
	// prev[i] holds last week's values per fuel.
	prev [3]map[float64]bool
	// sums holds the cumulative gas/diesel sums.
	sums map[float64]bool
	// pumpTrend holds now/prev commons plus trend deltas.
	pumpTrend map[float64]bool
	// Bills subsets.
	meralco, lpg, city map[float64]bool
	// water holds rateM/rateP/diff; family the totals and components.
	water, family map[float64]bool
}

func setOf(vals ...float64) map[float64]bool {
	out := map[float64]bool{}
	for _, v := range vals {
		out[numKey(v)] = true
	}
	return out
}

// newChecker builds the allowed sets from a snapshot.
func newChecker(s *Snapshot, kind string) *checker {
	c := &checker{
		allowed: map[float64]bool{},
		kind:    kind,
		snap:    s,
	}
	put := func(v float64) { c.allowed[numKey(v)] = true }
	put(10)
	put(11)
	put(200)
	if len(s.Fuel.Adjustments) > 0 {
		rs := []*Range{s.Fuel.Adjustments[0].Gasoline, s.Fuel.Adjustments[0].Diesel, s.Fuel.Adjustments[0].Kerosene}
		for i, r := range rs {
			c.base[i], c.ten[i] = map[float64]bool{}, map[float64]bool{}
			if r == nil {
				continue
			}
			for _, v := range []float64{r.Lo, r.Hi} {
				c.base[i][numKey(v)] = true
				c.ten[i][numKey(v*10)] = true
				put(v)
				put(v * 10)
			}
		}
	}
	// Last week's adjustment values (trend line, no 10× form).
	if len(s.Fuel.Adjustments) > 1 {
		rs := []*Range{s.Fuel.Adjustments[1].Gasoline, s.Fuel.Adjustments[1].Diesel, s.Fuel.Adjustments[1].Kerosene}
		for i, r := range rs {
			c.prev[i] = map[float64]bool{}
			if r == nil {
				continue
			}
			for _, v := range []float64{r.Lo, r.Hi} {
				c.prev[i][numKey(v)] = true
				put(v)
			}
		}
	}
	// Cumulative sums over the charted adjustments (gasoline + diesel).
	c.sums = map[float64]bool{}
	if _, _, _, gas, die := fuelCumulative(s.Fuel.Adjustments, 6); gas != nil || die != nil {
		for _, r := range []*Range{gas, die} {
			if r == nil {
				continue
			}
			c.sums[numKey(r.Lo)] = true
			c.sums[numKey(r.Hi)] = true
			put(r.Lo)
			put(r.Hi)
		}
	}
	for _, p := range s.Fuel.NCRPump {
		for _, row := range p.Prices {
			put(row.Low)
			put(row.High)
			put(row.Common)
		}
		for _, cp := range p.Cities {
			for _, row := range cp.Prices {
				put(row.Low)
				put(row.High)
				if row.Common != nil {
					put(*row.Common)
				}
			}
		}
	}
	// Trend cells: now/prev commons for NCR + cities, plus deltas.
	c.pumpTrend = map[float64]bool{}
	if now, prev := pumpPair(s); now != nil {
		addCommons := func(sum *PumpSummary) {
			for _, p := range sum.Prices {
				c.pumpTrend[numKey(p.Common)] = true
			}
			for _, cp := range sum.Cities {
				for _, row := range cp.Prices {
					if row.Common != nil {
						c.pumpTrend[numKey(*row.Common)] = true
					}
				}
			}
		}
		addCommons(now)
		if prev != nil {
			addCommons(prev)
			for _, p := range trendProducts {
				if nv, ok := ncrCommon(*now, p.key); ok {
					if pv, ok := ncrCommon(*prev, p.key); ok {
						c.pumpTrend[numKey(nv-pv)] = true
						put(nv - pv)
					}
				}
			}
			for _, ci := range trendCities {
				for _, p := range trendProducts {
					nc, npresent := cityProduct(now, ci.name, p.key)
					pc, _ := cityProduct(prev, ci.name, p.key)
					if npresent && nc != nil && pc != nil {
						c.pumpTrend[numKey(*nc-*pc)] = true
						put(*nc - *pc)
					}
				}
			}
		}
		for v := range c.pumpTrend {
			c.allowed[v] = true
		}
	}
	if len(s.Meralco) > 0 {
		m := s.Meralco[0]
		c.meralco = setOf(m.Adjustment, m.Rate, m.PrevRate, float64(m.Impact200))
		for v := range c.meralco {
			c.allowed[v] = true
		}
	}
	if len(s.LPG) > 0 {
		l := s.LPG[0]
		vals := []float64{l.Low, l.High, l.Common, l.CityLow, l.CityHigh}
		if l.CityCommon != nil {
			vals = append(vals, *l.CityCommon)
		}
		if len(s.LPG) > 1 {
			vals = append(vals, l.Common-s.LPG[1].Common)
			p2 := s.LPG[1]
			vals = append(vals, p2.Common, p2.CityLow, p2.CityHigh)
			if p2.CityCommon != nil {
				vals = append(vals, *p2.CityCommon)
			}
		}
		c.lpg = setOf(vals...)
		for v := range c.lpg {
			c.allowed[v] = true
		}
		c.city = setOf(l.CityLow, l.CityHigh)
		if l.CityCommon != nil {
			c.city[numKey(*l.CityCommon)] = true
		}
		if len(s.LPG) > 1 {
			p2 := s.LPG[1]
			c.city[numKey(p2.CityLow)] = true
			c.city[numKey(p2.CityHigh)] = true
			if p2.CityCommon != nil {
				c.city[numKey(*p2.CityCommon)] = true
			}
		}
	}
	// Water rate old → new and family totals (bills kind).
	if len(s.Meralco) > 0 {
		rateM := WaterRateFor(s.WaterRates, s.Meralco[0].Month)
		rateP := WaterRateFor(s.WaterRates, PrevMonth(s.Meralco[0].Month))
		if rateM != nil {
			c.water = map[float64]bool{numKey(rateM.Rate): true}
			put(rateM.Rate)
			if rateP != nil {
				c.water[numKey(rateP.Rate)] = true
				c.water[numKey(r2(rateM.Rate-rateP.Rate))] = true
				put(rateP.Rate)
				put(r2(rateM.Rate - rateP.Rate))
			}
		}
		if fig, ok := FamilyFigures(s, rateM, rateP); ok {
			c.family = map[float64]bool{}
			for _, v := range []float64{fig.Cur, fig.Power, fig.LpgC, fig.WaterC} {
				c.family[numKey(v)] = true
				put(v)
			}
			if fig.HasPrev {
				c.family[numKey(fig.Prev)] = true
				c.family[numKey(fig.Change)] = true
				put(fig.Prev)
				put(fig.Change)
			}
		}
	}
	return c
}

// fuelKey maps a product keyword to 0=gasoline, 1=diesel, 2=kerosene.
func fuelKey(s string) int {
	switch {
	case strings.Contains(s, "gasolina"), strings.Contains(s, "gasoline"), strings.Contains(s, "ガソリン"):
		return 0
	case strings.Contains(s, "diesel"), strings.Contains(s, "軽油"):
		return 1
	case strings.Contains(s, "kerosene"), strings.Contains(s, "灯油"):
		return 2
	}
	return -1
}

// lineSubset returns the per-line allowed subset for product-bound
// lines, or nil when only the global set applies.
func (c *checker) lineSubset(line string) map[float64]bool {
	if !strings.Contains(line, "₱") {
		return nil
	}
	if c.kind == "bills" {
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(line, "Tantiyang"), strings.Contains(line, "kuryente"),
			strings.Contains(line, "Estimated"), strings.Contains(line, "power"),
			strings.Contains(line, "家計支出"), strings.Contains(line, "電気"):
			return c.family
		case strings.Contains(line, "Meralco"), strings.Contains(lower, "200 kwh"), strings.Contains(lower, "200kwh"):
			return c.meralco
		case strings.Contains(line, "LPG"):
			return c.lpg
		case strings.Contains(line, "Tubig"), strings.Contains(line, "FCDA"),
			strings.Contains(line, "cu.m"), strings.Contains(line, "Maynilad"):
			return c.water
		case strings.Contains(line, "Piñas"), strings.Contains(line, "ラスピニャス"):
			return c.city
		}
		return nil
	}
	// Fuel kind.
	if strings.Contains(line, "RON") {
		return c.pumpTrend
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "10 litro") || strings.Contains(lower, "per 10 l") || strings.Contains(lower, "10lあたり") {
		return nil // per-segment binding is overkill: global + segments below
	}
	if strings.HasPrefix(strings.TrimSpace(line), "•") {
		if i := strings.Index(line, ":"); i >= 0 {
			if k := fuelKey(strings.ToLower(line[:i])); k >= 0 {
				return c.base[k]
			}
		}
		return nil
	}
	if strings.Contains(line, "·") || strings.Contains(line, "・") {
		return nil // combined EN/JA line: bound per segment below
	}
	return nil
}

var (
	waterClockRe = regexp.MustCompile(`(\d{1,2}):(\d{2})(\s*[AaPp]\.?\s*[Mm]\.?)?`)
	waterMDRe    = regexp.MustCompile(`(\d{1,2})/(\d{1,2})`)
	waterIntRe   = regexp.MustCompile(`\b(\d{1,4})\b`)
	waterURLRe   = regexp.MustCompile(`https?://\S+`)
)

// checkWaterAlert verifies a water alert caption comes from s.Alerts[0]:
// areas, city and reason wording must appear; dates/times must match
// the alert window when parsed.
func checkWaterAlert(text string, s *Snapshot) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if len(s.Alerts) == 0 {
		return []string{"no water alert in snapshot"}
	}
	a := s.Alerts[0]
	for _, area := range a.Areas {
		if !strings.Contains(text, area) {
			add("area %q missing", area)
		}
	}
	// The joined area lists must appear verbatim per language, so a
	// renamed area in any block fails. X-sourced alerts store the TL
	// marker and render a per-language label instead.
	areasTL := strings.Join(a.Areas, ", ")
	areasEN := areasTL
	areasJA := strings.Join(a.Areas, "、")
	if len(a.Areas) == 1 && a.Areas[0] == XAreasTL {
		areasTL = XAreasTL
		areasEN = XAreasEN
		areasJA = XAreasJA
	}
	if !strings.Contains(text, "📍 "+areasTL) {
		add("area list mismatch (TL)")
	}
	if !strings.Contains(text, "· "+areasEN+" ·") {
		add("area list mismatch (EN)")
	}
	if !strings.Contains(text, "・"+areasJA+"・") {
		add("area list mismatch (JA)")
	}
	if !strings.Contains(text, a.City) {
		add("city %q missing", a.City)
	}
	for lang := 0; lang < 3; lang++ {
		if w := AlertReason(a.ReasonKey, lang); !strings.Contains(text, w) {
			add("reason %q missing", w)
		}
	}
	if a.Start.IsZero() || a.End.IsZero() {
		return problems
	}
	// Trend line: present with exact wording iff NthThisMonth >= 1.
	if trendTL, trendEN, trendJA, ok := waterTrendLines(&a); ok {
		for _, want := range []string{trendTL, trendEN, trendJA} {
			if !strings.Contains(text, want) {
				add("trend %q missing", want)
			}
		}
	} else if strings.Contains(text, "📊 Ika-") || strings.Contains(text, "water interruption in") || strings.Contains(text, "回目の断水") {
		add("unexpected water trend line")
	}
	wantHM := map[[2]int]bool{
		{a.Start.Hour(), a.Start.Minute()}: true,
		{a.End.Hour(), a.End.Minute()}:     true,
	}
	// Daily alerts repeat nightly: the clock window, not the
	// first-start/last-end span, is printed.
	allowedMD := map[[2]int]bool{
		{int(a.Start.Month()), a.Start.Day()}: true,
		{int(a.End.Month()), a.End.Day()}:     true,
	}
	if a.Daily {
		if len(a.Windows) > 0 {
			wantHM = map[[2]int]bool{
				{a.Windows[0].Start.Hour(), a.Windows[0].Start.Minute()}: true,
				{a.Windows[0].End.Hour(), a.Windows[0].End.Minute()}:     true,
			}
		}
		from, to := a.DailyFrom, a.DailyTo
		if from == "" || to == "" {
			if len(a.Windows) > 0 {
				from = a.Windows[0].Start.In(manilaLoc).Format("2006-01-02")
				to = a.Windows[len(a.Windows)-1].Start.In(manilaLoc).Format("2006-01-02")
			}
		}
		if d1, ok1 := mustDate(from); ok1 {
			if d2, ok2 := mustDate(to); ok2 && !d2.Before(d1) {
				for d := d1; !d.After(d2); d = d.Add(24 * time.Hour) {
					allowedMD[[2]int{int(d.Month()), d.Day()}] = true
				}
			}
		}
	}
	scrubbed := waterURLRe.ReplaceAllString(text, " ")
	scrubbed = waterMDRe.ReplaceAllStringFunc(scrubbed, func(m string) string {
		sub := waterMDRe.FindStringSubmatch(m)
		var mo, d int
		fmt.Sscanf(sub[1], "%d", &mo)
		fmt.Sscanf(sub[2], "%d", &d)
		if allowedMD[[2]int{mo, d}] {
			return " "
		}
		add("date %q not in alert window", m)
		return " "
	})
	scrubbed = waterClockRe.ReplaceAllStringFunc(scrubbed, func(m string) string {
		sub := waterClockRe.FindStringSubmatch(m)
		var h, mi int
		fmt.Sscanf(sub[1], "%d", &h)
		fmt.Sscanf(sub[2], "%d", &mi)
		if sub[3] != "" {
			low := strings.ToLower(strings.ReplaceAll(sub[3], " ", ""))
			if strings.HasPrefix(low, "p") {
				if h != 12 {
					h += 12
				}
			} else if h == 12 {
				h = 0
			}
		}
		if !wantHM[[2]int{h, mi}] {
			add("time %q not in alert window", strings.Join(strings.Fields(m), " "))
		}
		return " "
	})
	allowed := map[string]bool{}
	for _, n := range []int{a.Start.Day(), a.End.Day(), int(a.Start.Month()), int(a.End.Month()), a.Start.Year(), a.End.Year()} {
		allowed[strconv.Itoa(n)] = true
	}
	if a.Daily {
		for md := range allowedMD {
			allowed[strconv.Itoa(md[0])] = true
			allowed[strconv.Itoa(md[1])] = true
		}
	}
	if a.NthThisMonth >= 1 {
		allowed[strconv.Itoa(a.NthThisMonth)] = true
		if a.HasPrev {
			allowed[strconv.Itoa(a.PrevMonthCount)] = true
		}
	}
	for _, area := range a.Areas {
		for _, m := range waterIntRe.FindAllString(area, -1) {
			allowed[m] = true
		}
	}
	for _, m := range waterIntRe.FindAllString(scrubbed, -1) {
		if !allowed[m] {
			add("number %q not in alert", m)
		}
	}
	return problems
}

// checkPowerAlert verifies a power alert caption comes from
// s.PowerAlerts[0]: barangays, roads, city and reason wording must
// appear; dates/times must match the alert windows when parsed.
func checkPowerAlert(text string, s *Snapshot) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if len(s.PowerAlerts) == 0 {
		return []string{"no power alert in snapshot"}
	}
	a := s.PowerAlerts[0]
	for _, area := range a.Barangays {
		if area == PowerAreasTL {
			continue
		}
		if !strings.Contains(text, area) {
			add("area %q missing", area)
		}
	}
	areasTL, areasEN, areasJA := powerAreas(&a)
	roads := shortRoads(a.Roads)
	placeTL := areasTL
	placeEN := areasEN
	placeJA := areasJA
	if len(roads) > 0 {
		placeTL += " · " + strings.Join(roads, ", ")
		placeEN += " · " + strings.Join(roads, ", ")
		placeJA += "・" + strings.Join(roads, "、")
	}
	if !strings.Contains(text, "📍 "+placeTL) {
		add("area list mismatch (TL)")
	}
	if !strings.Contains(text, "· "+placeEN+" ·") {
		add("area list mismatch (EN)")
	}
	if !strings.Contains(text, "・"+placeJA+"・") {
		add("area list mismatch (JA)")
	}
	for _, r := range roads {
		if !strings.Contains(text, r) {
			add("road %q missing", r)
		}
	}
	if !strings.Contains(text, a.City) {
		add("city %q missing", a.City)
	}
	for lang := 0; lang < 3; lang++ {
		if w := PowerReason(a.ReasonKey, lang); !strings.Contains(text, w) {
			add("reason %q missing", w)
		}
	}
	if a.Start.IsZero() || a.End.IsZero() {
		return problems
	}
	// Trend line: present with exact wording iff NthThisMonth >= 1.
	if trendTL, trendEN, trendJA, ok := PowerTrendLines(&a); ok {
		for _, want := range []string{trendTL, trendEN, trendJA} {
			if !strings.Contains(text, want) {
				add("trend %q missing", want)
			}
		}
	} else if strings.Contains(text, "planadong brownout") || strings.Contains(text, "planned brownout in") || strings.Contains(text, "回目の計画停電") {
		add("unexpected power trend line")
	}
	// Expected clock times: daily uses the nightly clock pairs, else
	// every window start/end.
	wantHM := map[[2]int]bool{}
	if a.Daily {
		for _, p := range powerClockPairs(&a) {
			wantHM[[2]int{p[0], p[1]}] = true
			wantHM[[2]int{p[2], p[3]}] = true
		}
	} else {
		wins := a.Windows
		if len(wins) == 0 {
			wins = []TimeWindow{{Start: a.Start, End: a.End}}
		}
		for _, w := range wins {
			wantHM[[2]int{w.Start.Hour(), w.Start.Minute()}] = true
			wantHM[[2]int{w.End.Hour(), w.End.Minute()}] = true
		}
	}
	allowedMD := map[[2]int]bool{
		{int(a.Start.Month()), a.Start.Day()}: true,
		{int(a.End.Month()), a.End.Day()}:     true,
	}
	wins := a.Windows
	if len(wins) == 0 {
		wins = []TimeWindow{{Start: a.Start, End: a.End}}
	}
	for _, w := range wins {
		allowedMD[[2]int{int(w.Start.Month()), w.Start.Day()}] = true
		allowedMD[[2]int{int(w.End.Month()), w.End.Day()}] = true
	}
	if a.Daily {
		from, to := a.DailyFrom, a.DailyTo
		if from == "" || to == "" {
			if len(a.Windows) > 0 {
				from = a.Windows[0].Start.In(manilaLoc).Format("2006-01-02")
				to = a.Windows[len(a.Windows)-1].Start.In(manilaLoc).Format("2006-01-02")
			}
		}
		if d1, ok1 := mustDate(from); ok1 {
			if d2, ok2 := mustDate(to); ok2 && !d2.Before(d1) {
				for d := d1; !d.After(d2); d = d.Add(24 * time.Hour) {
					allowedMD[[2]int{int(d.Month()), d.Day()}] = true
				}
			}
		}
	}
	scrubbed := waterURLRe.ReplaceAllString(text, " ")
	scrubbed = waterMDRe.ReplaceAllStringFunc(scrubbed, func(m string) string {
		sub := waterMDRe.FindStringSubmatch(m)
		var mo, d int
		fmt.Sscanf(sub[1], "%d", &mo)
		fmt.Sscanf(sub[2], "%d", &d)
		if allowedMD[[2]int{mo, d}] {
			return " "
		}
		add("date %q not in alert window", m)
		return " "
	})
	scrubbed = waterClockRe.ReplaceAllStringFunc(scrubbed, func(m string) string {
		sub := waterClockRe.FindStringSubmatch(m)
		var h, mi int
		fmt.Sscanf(sub[1], "%d", &h)
		fmt.Sscanf(sub[2], "%d", &mi)
		if sub[3] != "" {
			low := strings.ToLower(strings.ReplaceAll(sub[3], " ", ""))
			if strings.HasPrefix(low, "p") {
				if h != 12 {
					h += 12
				}
			} else if h == 12 {
				h = 0
			}
		}
		if !wantHM[[2]int{h, mi}] {
			add("time %q not in alert window", strings.Join(strings.Fields(m), " "))
		}
		return " "
	})
	allowed := map[string]bool{}
	for _, n := range []int{a.Start.Day(), a.End.Day(), int(a.Start.Month()), int(a.End.Month()), a.Start.Year(), a.End.Year()} {
		allowed[strconv.Itoa(n)] = true
	}
	if a.Daily {
		for md := range allowedMD {
			allowed[strconv.Itoa(md[0])] = true
			allowed[strconv.Itoa(md[1])] = true
		}
	} else {
		for _, w := range wins {
			allowed[strconv.Itoa(w.Start.Day())] = true
			allowed[strconv.Itoa(w.End.Day())] = true
			allowed[strconv.Itoa(int(w.Start.Month()))] = true
			allowed[strconv.Itoa(int(w.End.Month()))] = true
			allowed[strconv.Itoa(w.Start.Year())] = true
			allowed[strconv.Itoa(w.End.Year())] = true
		}
	}
	if a.NthThisMonth >= 1 {
		allowed[strconv.Itoa(a.NthThisMonth)] = true
		if a.HistoryComplete {
			allowed[strconv.Itoa(a.PrevMonthCount)] = true
		}
	}
	for _, area := range a.Barangays {
		for _, m := range waterIntRe.FindAllString(area, -1) {
			allowed[m] = true
		}
	}
	for _, r := range a.Roads {
		for _, m := range waterIntRe.FindAllString(r, -1) {
			allowed[m] = true
		}
	}
	// The Details link carries the source URL (numbers inside the URL
	// were scrubbed above, so no extra allowance needed).
	if !strings.Contains(text, "Details: "+a.SourceURL) {
		add("details link missing")
	}
	if !strings.Contains(text, "Source: Meralco maintenance schedule") {
		add("source line missing")
	}
	for _, m := range waterIntRe.FindAllString(scrubbed, -1) {
		if !allowed[m] {
			add("number %q not in alert", m)
		}
	}
	return problems
}
