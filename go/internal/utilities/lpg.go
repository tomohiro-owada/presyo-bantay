package utilities

import (
	"fmt"
	"regexp"
	"strings"
)

// NCR LPG monthly parsing. The summary block ends the PDF:
//
//	PREVAILING PRICE OF 11.0 KG HOUSEHOLD LPG AND AUTO-LPG
//	(For October 01-08, 2026)
//	SIZE OVERALL PRICE RANGE COMMON PRICE
//	11.0 kg 1,268.00 - 1,710.00 1,430.00
//	Auto-LPG 58.40 58.40
//
// The city row ("Las Pinas City ... 1,368.00 - 1,569.00 #N/A") carries
// the per-city range near its end.

var (
	lpgPeriodRe = regexp.MustCompile(`\(For ([A-Za-z]+) (\d{1,2})\s*[-–]\s*(\d{1,2}),\s*(\d{4})\)`)

	// Leading "11.0 kg" label of the household summary row.
	lpg11kgRe = regexp.MustCompile(`(?i)^11\.0\s*kg\s+(.+)$`)

	lpgNumRe = regexp.MustCompile(`[0-9][0-9,]*\.[0-9]+`)

	// Last "a - b" range on a line (spaces optional).
	lpgRangeRe = regexp.MustCompile(`([0-9][0-9,]*\.[0-9]+)\s*-\s*([0-9][0-9,]*\.[0-9]+)`)
)

// ParseNCRLPG parses an NCR LPG monthly monitoring PDF.
func ParseNCRLPG(lines []Line) (LPGMonth, error) {
	var out LPGMonth
	full := joinLines(lines)
	m := lpgPeriodRe.FindStringSubmatch(full)
	if m == nil {
		return LPGMonth{}, fmt.Errorf("utilities: LPG period not found")
	}
	from, err := parseMonthDayYear(m[2], m[1], m[4])
	if err != nil {
		return LPGMonth{}, err
	}
	to, err := parseMonthDayYear(m[3], m[1], m[4])
	if err != nil {
		return LPGMonth{}, err
	}
	out.PeriodFrom, out.PeriodTo = from, to

	for _, ln := range lines {
		t := strings.TrimSpace(ln.Text)
		low := strings.ToLower(t)
		switch {
		case strings.HasPrefix(low, "11.0 kg"):
			m11 := lpg11kgRe.FindStringSubmatch(t)
			if m11 == nil {
				continue
			}
			nums := lpgNumRe.FindAllString(m11[1], -1)
			if len(nums) < 3 {
				continue // e.g. a split header fragment
			}
			if out.Low, err = parseAmount(nums[0]); err != nil {
				return LPGMonth{}, err
			}
			if out.High, err = parseAmount(nums[1]); err != nil {
				return LPGMonth{}, err
			}
			if out.Common, err = parseAmount(nums[2]); err != nil {
				return LPGMonth{}, err
			}
		case strings.HasPrefix(low, "auto-lpg"):
			nums := lpgNumRe.FindAllString(t, -1)
			if len(nums) == 0 {
				continue // the table header has no numbers
			}
			// The repeated figure is the prevailing price.
			if out.AutoLPG, err = parseAmount(nums[len(nums)-1]); err != nil {
				return LPGMonth{}, err
			}
		}
	}
	if out.Common == 0 {
		return LPGMonth{}, fmt.Errorf("utilities: LPG summary block not found")
	}
	for _, ln := range lines {
		if !strings.Contains(strings.ToLower(ln.Text), "las pi") {
			continue
		}
		city, cityLow, cityHigh, cityCommon, err := parseLPGCityRow(ln.Text)
		if err != nil {
			return LPGMonth{}, err
		}
		out.City = city
		out.CityLow, out.CityHigh = cityLow, cityHigh
		out.CityCommon = cityCommon
		break
	}
	if out.City == "" {
		return LPGMonth{}, fmt.Errorf("utilities: Las Piñas row not found")
	}
	return out, nil
}

// parseLPGCityRow reads the "a - b" range near the end of a city row;
// the token after the range is the common price ("#N/A" -> nil).
func parseLPGCityRow(text string) (string, float64, float64, *float64, error) {
	matches := lpgRangeRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return "", 0, 0, nil, fmt.Errorf("utilities: no city range in %q", text)
	}
	last := matches[len(matches)-1]
	lo, err := parseAmount(text[last[2]:last[3]])
	if err != nil {
		return "", 0, 0, nil, err
	}
	hi, err := parseAmount(text[last[4]:last[5]])
	if err != nil {
		return "", 0, 0, nil, err
	}
	var common *float64
	rest := strings.TrimSpace(text[last[1]:])
	if f := lpgNumRe.FindString(rest); f != "" && strings.HasPrefix(rest, f) {
		v, err := parseAmount(f)
		if err != nil {
			return "", 0, 0, nil, err
		}
		common = &v
	}
	// City name: leading words before the first amount.
	city := text
	if i := lpgNumRe.FindStringIndex(text); i != nil {
		city = strings.TrimSpace(text[:i[0]])
	}
	city = strings.Join(strings.Fields(city), " ")
	return city, lo, hi, common, nil
}
