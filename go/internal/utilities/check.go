package utilities

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// CheckPost validates a rendered post against its snapshot: every ₱
// amount (and every bare number attached to /L, /kWh, kWh, …) must
// equal a snapshot value or an allowed derivation (10× fuel
// adjustment, LPG month-on-month diff, Impact200, the 200 kWh / 10 L /
// 11 kg constants) with the correct sign, and product lines must carry
// their own product's numbers (swapped products fail). Lint: no "$",
// no nested parentheses (ASCII and full-width), no "NaN", no empty
// sections. kind is "fuel" or "bills".
func CheckPost(text string, s *Snapshot, kind string) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if kind != "fuel" && kind != "bills" {
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
// product's numbers, so swapped products fail.
func (c *checker) checkFuelSegments(lineNo int, line string) []string {
	if !strings.Contains(line, "₱") || strings.Contains(line, "RON") {
		return nil
	}
	lower := strings.ToLower(line)
	ten := strings.Contains(lower, "10 litro") || strings.Contains(lower, "per 10 l") || strings.Contains(lower, "10lあたり")
	segs := strings.FieldsFunc(line, func(r rune) bool {
		return r == '·' || r == '・' || r == ',' || r == '、'
	})
	if len(segs) < 2 && !ten && !strings.HasPrefix(strings.TrimSpace(line), "•") {
		return nil
	}
	var out []string
	for _, seg := range segs {
		k := fuelKey(strings.ToLower(seg))
		if k < 0 {
			continue
		}
		want := c.base[k]
		if ten {
			want = c.ten[k]
		}
		for _, hit := range findAmounts(seg) {
			if !want[numKey(hit.value)] {
				out = append(out, fmt.Sprintf("line %d: %s does not belong to this product", lineNo, hit.raw))
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
	// Fuel values: base[i] per-L, ten[i] 10×, for i in gas/diesel/kero.
	base, ten [3]map[float64]bool
	// pumpCommon holds the reference pump week's commons.
	pumpCommon map[float64]bool
	// Bills subsets.
	meralco, lpg, city map[float64]bool
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
	for _, p := range s.Fuel.NCRPump {
		for _, row := range p.Prices {
			put(row.Low)
			put(row.High)
			put(row.Common)
		}
	}
	if pump := basePump(s); pump != nil {
		c.pumpCommon = map[float64]bool{}
		if r91, r95, die, ok := pumpCommons(*pump); ok {
			for _, v := range []float64{r91, r95, die} {
				c.pumpCommon[numKey(v)] = true
			}
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
		}
		c.lpg = setOf(vals...)
		for v := range c.lpg {
			c.allowed[v] = true
		}
		c.city = setOf(l.CityLow, l.CityHigh)
		if l.CityCommon != nil {
			c.city[numKey(*l.CityCommon)] = true
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
		case strings.Contains(line, "Meralco"), strings.Contains(lower, "200 kwh"), strings.Contains(lower, "200kwh"):
			return c.meralco
		case strings.Contains(line, "LPG"):
			return c.lpg
		case strings.Contains(line, "Piñas"), strings.Contains(line, "ラスピニャス"):
			return c.city
		}
		return nil
	}
	// Fuel kind.
	lower := strings.ToLower(line)
	if strings.Contains(line, "RON") {
		return c.pumpCommon
	}
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
