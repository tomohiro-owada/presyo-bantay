package utilities

import (
	"fmt"
	"regexp"
	"strings"
)

// NCR pump price summary parsing. The block ends the PDF:
//
//	PREVAILING RETAIL PRICES OF PETROLEUM PRODUCTS NCR
//	(for the week of October 13-19, 2026)
//	Product Overall Range Common Price
//	Gasoline (RON97/100) 84.83 105.40 98.80
//	...

var (
	pumpWeekRe = regexp.MustCompile(`(?i)for the week of ([A-Za-z]+) (\d{1,2})\s*[-–]\s*(?:([A-Za-z]+) )?(\d{1,2}),?\s*(\d{4})`)

	pumpRowRe = regexp.MustCompile(`^(Gasoline \(RON97/100\)|Gasoline \(RON95\)|Gasoline \(RON91\)|Diesel Plus|Diesel|Kerosene)\s*([0-9][0-9,]*\.?[0-9]*)\s+([0-9][0-9,]*\.?[0-9]*)\s+([0-9][0-9,]*\.?[0-9]*)\s*$`)

	// cityRowRe matches a per-city product row; an optional city label
	// may sit glued in front of the product ("Caloocan CityRON 91").
	// The rest may be empty (bare "RON 100" rows carry no numbers).
	cityRowRe = regexp.MustCompile(`^(.*?)(RON 100|RON 97|RON 95|RON 91|DIESEL PLUS|DIESEL|KEROSENE)\s*(.*)$`)

	// cityTrailRe reads the trailing "low - high common" ("#N/A" →
	// no common).
	cityTrailRe = regexp.MustCompile(`([0-9][0-9,]*\.[0-9]+)\s*-\s*([0-9][0-9,]*\.[0-9]+)\s+([0-9][0-9,]*\.[0-9]+|#N/A)\s*$`)
)

// cityProductKey maps per-city row labels to Product keys.
var cityProductKey = map[string]string{
	"RON 100": "RON100", "RON 97": "RON97", "RON 95": "RON95",
	"RON 91": "RON91", "DIESEL PLUS": "Diesel Plus",
	"DIESEL": "Diesel", "KEROSENE": "Kerosene",
}

// pumpCities are the monitored cities (short name → "X City" label).
var pumpCities = []string{
	"Caloocan", "Quezon", "Manila", "Pasig", "Taguig", "Makati",
	"Parañaque", "Muntinlupa", "Pasay", "Marikina", "Valenzuela", "Navotas",
}

// pumpProductKey maps row labels to Product keys.
var pumpProductKey = map[string]string{
	"Gasoline (RON97/100)": "RON97/100",
	"Gasoline (RON95)":     "RON95",
	"Gasoline (RON91)":     "RON91",
	"Diesel":               "Diesel",
	"Diesel Plus":          "Diesel Plus",
	"Kerosene":             "Kerosene",
}

// ParseNCRPumpSummary parses the prevailing-retail-price block of an
// NCR pump price PDF.
func ParseNCRPumpSummary(lines []Line) (PumpSummary, error) {
	var sum PumpSummary
	full := joinLines(lines)
	m := pumpWeekRe.FindStringSubmatch(full)
	if m == nil {
		return PumpSummary{}, fmt.Errorf("utilities: pump week not found")
	}
	month2 := m[1]
	if m[3] != "" {
		month2 = m[3]
	}
	from, err := parseMonthDayYear(m[2], m[1], m[5])
	if err != nil {
		return PumpSummary{}, err
	}
	to, err := parseMonthDayYear(m[4], month2, m[5])
	if err != nil {
		return PumpSummary{}, err
	}
	sum.WeekFrom, sum.WeekTo = from, to
	sum.Cities = parsePumpCities(lines)
	started := false
	for _, ln := range lines {
		t := strings.TrimSpace(ln.Text)
		if !started {
			// "Product Overall Range Common Price", spacing varies
			// with the column gaps, so match spaceless.
			flat := strings.ToLower(strings.ReplaceAll(t, " ", ""))
			if strings.Contains(flat, "product") && strings.Contains(flat, "commonprice") {
				started = true
			}
			continue
		}
		if t == "" {
			continue
		}
		rm := pumpRowRe.FindStringSubmatch(t)
		if rm == nil {
			break
		}
		low, err := parseAmount(rm[2])
		if err != nil {
			return PumpSummary{}, err
		}
		high, err := parseAmount(rm[3])
		if err != nil {
			return PumpSummary{}, err
		}
		common, err := parseAmount(rm[4])
		if err != nil {
			return PumpSummary{}, err
		}
		sum.Prices = append(sum.Prices, PumpPrice{
			Product: pumpProductKey[rm[1]],
			Low:     low, High: high, Common: common,
		})
	}
	if len(sum.Prices) == 0 {
		return PumpSummary{}, fmt.Errorf("utilities: no pump price rows found")
	}
	return sum, nil
}

// blockRow is one buffered per-city product row.
type blockRow struct {
	product      string
	low, high    float64
	common       *float64
	hasNumbers   bool
	embeddedCity string // city label glued on this row, if any
	endsBlock    bool   // KEROSENE row
}

// parsePumpCities parses the per-city table (blocks of up to 7 product
// rows: RON 100 … KEROSENE) that precedes the summary block. The city
// label sits on ONE of the block's rows; a stray standalone label line
// only applies when the block has no row label. Blocks whose city
// cannot be determined are skipped (numbers are never invented).
func parsePumpCities(lines []Line) []CityPump {
	var out []CityPump
	completed := map[string]bool{}
	var buf []blockRow
	var pending string
	flush := func() {
		if len(buf) == 0 {
			return
		}
		city := ""
		for _, r := range buf {
			if r.embeddedCity != "" {
				city = r.embeddedCity
				break
			}
		}
		if city == "" {
			city = pending
		}
		pending = ""
		defer func() { buf = nil }()
		if city == "" || completed[city] {
			return
		}
		var prices []CityPrice
		for _, r := range buf {
			if !r.hasNumbers {
				continue
			}
			prices = append(prices, CityPrice{
				Product: r.product, Low: r.low, High: r.high, Common: r.common,
			})
		}
		completed[city] = true
		out = append(out, CityPump{City: city, Prices: prices})
	}
	for _, ln := range lines {
		t := strings.TrimSpace(ln.Text)
		if t == "" {
			continue
		}
		// The summary block ends the city table.
		flat := strings.ToLower(strings.ReplaceAll(t, " ", ""))
		if strings.Contains(flat, "product") && strings.Contains(flat, "commonprice") {
			break
		}
		if city := matchStandaloneCity(t); city != "" {
			pending = city
			continue
		}
		m := cityRowRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		key, ok := cityProductKey[m[2]]
		if !ok {
			continue
		}
		row := blockRow{product: key, endsBlock: key == "Kerosene"}
		if label := strings.TrimSpace(m[1]); label != "" {
			row.embeddedCity = matchCityLabel(label, completed)
		}
		if trail := cityTrailRe.FindStringSubmatch(m[3]); trail != nil {
			lo, err1 := parseAmount(trail[1])
			hi, err2 := parseAmount(trail[2])
			if err1 == nil && err2 == nil {
				row.low, row.high = lo, hi
				row.hasNumbers = true
				if trail[3] != "#N/A" {
					if c, err := parseAmount(trail[3]); err == nil {
						row.common = &c
					} else {
						row.hasNumbers = false
					}
				}
			}
		}
		buf = append(buf, row)
		if row.endsBlock {
			flush()
		}
	}
	flush()
	if out == nil {
		return []CityPump{}
	}
	return out
}

// matchStandaloneCity matches a bare city label line ("Caloocan City").
func matchStandaloneCity(t string) string {
	for _, city := range pumpCities {
		if strings.EqualFold(t, city+" City") || strings.EqualFold(t, city) {
			return city
		}
	}
	return ""
}

// matchCityLabel finds the city named in a row-embedded label, which
// may be glued ("Caloocan CityRON 91" leaves "Caloocan City") or
// interleaved with a stray overprinted label (Muntinlupa's row also
// carries "Caloocan City" character soup). Exact substring matches win;
// otherwise subsequence matches are collected and a city whose block
// already closed loses (the stray never reopens a finished block).
func matchCityLabel(label string, completed map[string]bool) string {
	var subseq []string
	for _, city := range pumpCities {
		full := city + " City"
		if strings.Contains(label, full) || strings.Contains(label, city) {
			// Prefer full "X City" hits over bare-name hits below.
			return city
		}
		if isSubsequence(full, label) {
			subseq = append(subseq, city)
		}
	}
	for _, city := range subseq {
		if !completed[city] {
			return city
		}
	}
	if len(subseq) > 0 {
		return subseq[0]
	}
	return ""
}

// isSubsequence reports whether pat appears in s in order
// (case-insensitive), allowing interleaved characters.
func isSubsequence(pat, s string) bool {
	pr, sr := []rune(strings.ToLower(pat)), []rune(strings.ToLower(s))
	j := 0
	for _, r := range sr {
		if j < len(pr) && r == pr[j] {
			j++
		}
	}
	return j == len(pr)
}
