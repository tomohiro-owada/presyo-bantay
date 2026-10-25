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
)

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
