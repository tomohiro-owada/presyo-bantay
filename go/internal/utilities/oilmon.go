package utilities

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Oil Monitor (weekly adjustment) parsing. The DOMESTIC OIL PRICES
// section is free-form with typos ("Php 1.93L/", "Php 4.60/LPhp
// 4.62/L"), so per-fuel clauses are located by product keyword and
// every Php amount inside the clause is collected; the sign comes from
// the increase/decrease wording in the same clause.

var (
	asOfRe = regexp.MustCompile(`(?i)\bas of (\d{1,2}) ([A-Za-z]+) (\d{4})`)

	// "effective 20 through 26 October 2026" (same month).
	effectiveSameRe = regexp.MustCompile(`(?i)effective (\d{1,2}) (?:through|to|-) (\d{1,2}) ([A-Za-z]+) (\d{4})`)
	// "effective 29 September through 05 October 2026" (cross-month).
	effectiveCrossRe = regexp.MustCompile(`(?i)effective (\d{1,2}) ([A-Za-z]+) (?:through|to|-) (\d{1,2}) ([A-Za-z]+) (\d{4})`)

	movementRe = regexp.MustCompile(`(?i)the (\d+)(?:st|nd|rd|th) price movement`)

	phpAmountRe = regexp.MustCompile(`(?i)php\s*([0-9][0-9,]*\.?[0-9]*)`)

	decreaseRe = regexp.MustCompile(`(?i)decreas\w*`)
	increaseRe = regexp.MustCompile(`(?i)increas\w*`)
)

// maxAdjAbs bounds sanity for a single adjustment value.
const maxAdjAbs = 30.0

// ParseOilMonitor parses the DOMESTIC OIL PRICES section of Oil Monitor
// PDF lines into a weekly adjustment. Nil product ranges mean the fuel
// was not mentioned.
func ParseOilMonitor(lines []Line) (OilAdjustment, error) {
	var adj OilAdjustment
	full := joinLines(lines)
	sec, err := domesticSection(full)
	if err != nil {
		return OilAdjustment{}, err
	}
	asOf, err := parseAsOf(full)
	if err != nil {
		return OilAdjustment{}, err
	}
	adj.AsOf = asOf
	from, to, err := parseEffective(full)
	if err != nil {
		return OilAdjustment{}, err
	}
	adj.EffectiveFrom, adj.EffectiveTo = from, to
	no, err := parseMovementNo(full)
	if err != nil {
		return OilAdjustment{}, err
	}
	adj.MovementNo = no
	for _, fuel := range []struct {
		name string
		set  func(*OilAdjustment, *Range)
	}{
		{"gasoline", func(a *OilAdjustment, r *Range) { a.Gasoline = r }},
		{"diesel", func(a *OilAdjustment, r *Range) { a.Diesel = r }},
		{"kerosene", func(a *OilAdjustment, r *Range) { a.Kerosene = r }},
	} {
		r, found, err := parseFuelRange(sec, fuel.name)
		if err != nil {
			return OilAdjustment{}, fmt.Errorf("%s: %w", fuel.name, err)
		}
		if found {
			fuel.set(&adj, r)
		}
	}
	return adj, nil
}

// joinLines concatenates line texts; bullets stay on their own lines.
func joinLines(lines []Line) string {
	texts := make([]string, 0, len(lines))
	for _, ln := range lines {
		texts = append(texts, ln.Text)
	}
	return strings.Join(texts, "\n")
}

// domesticSection returns the text between DOMESTIC OIL PRICES and the
// year-to-date net-increase paragraph (which carries P/liter amounts
// that must not be read as adjustments).
func domesticSection(full string) (string, error) {
	up := strings.ToUpper(full)
	start := strings.Index(up, "DOMESTIC OIL PRICES")
	if start < 0 {
		return "", fmt.Errorf("utilities: DOMESTIC OIL PRICES section not found")
	}
	tail := full[start:]
	end := len(tail)
	if i := strings.Index(strings.ToLower(tail), "price movement"); i >= 0 {
		end = i
	}
	return tail[:end], nil
}

// parseAsOf reads "As of 20 October 2026" (or the header variant).
func parseAsOf(full string) (string, error) {
	m := asOfRe.FindStringSubmatch(full)
	if m == nil {
		return "", fmt.Errorf("utilities: As of date not found")
	}
	return parseMonthDayYear(m[1], m[2], m[3])
}

// parseEffective reads "effective 20 through 26 October 2026" or
// "effective 29 September through 05 October 2026".
func parseEffective(full string) (string, string, error) {
	if m := effectiveCrossRe.FindStringSubmatch(full); m != nil {
		from, err := parseMonthDayYear(m[1], m[2], m[5])
		if err != nil {
			return "", "", err
		}
		to, err := parseMonthDayYear(m[3], m[4], m[5])
		if err != nil {
			return "", "", err
		}
		return from, to, nil
	}
	if m := effectiveSameRe.FindStringSubmatch(full); m != nil {
		from, err := parseMonthDayYear(m[1], m[3], m[4])
		if err != nil {
			return "", "", err
		}
		to, err := parseMonthDayYear(m[2], m[3], m[4])
		if err != nil {
			return "", "", err
		}
		return from, to, nil
	}
	return "", "", fmt.Errorf("utilities: effective date range not found")
}

// parseMovementNo reads "The 42nd price movement this year".
func parseMovementNo(full string) (int, error) {
	m := movementRe.FindStringSubmatch(full)
	if m == nil {
		return 0, fmt.Errorf("utilities: price movement number not found")
	}
	return strconv.Atoi(m[1])
}

// parseFuelRange finds the clause mentioning fuel ("for gasoline" or a
// Meanwhile sentence), collects its Php amounts, and signs them from
// the increase/decrease wording. found=false when unmentioned.
func parseFuelRange(sec, fuel string) (r *Range, found bool, err error) {
	lower := strings.ToLower(sec)
	if idx := strings.Index(lower, "for "+fuel); idx >= 0 {
		if clause := clauseBefore(sec, idx); phpAmountRe.MatchString(clause) {
			return finishRange(clause, sentenceBefore(sec, idx))
		}
		// Amounts follow the mention
		// ("for diesel decreased by Php 1.30/L").
		if after := clauseAfter(sec, idx+len("for "+fuel)); phpAmountRe.MatchString(after) {
			return finishRange(after, after)
		}
		return nil, false, nil
	}
	// Fallback: a clause naming the fuel without "for"
	// (e.g. "diesel decreased by ...").
	idx := strings.Index(lower, fuel)
	if idx < 0 {
		return nil, false, nil
	}
	clause := clauseBefore(sec, idx)
	if !phpAmountRe.MatchString(clause) {
		return nil, false, nil
	}
	return finishRange(clause, sentenceBefore(sec, idx))
}

// finishRange signs the Php amounts in amountClause from the
// increase/decrease wording (widened to dirClause when silent).
func finishRange(amountClause, dirClause string) (*Range, bool, error) {
	amounts := phpAmountRe.FindAllStringSubmatch(amountClause, -1)
	if len(amounts) == 0 {
		return nil, false, nil
	}
	// The increase/decrease wording usually sits outside the bullet
	// ("Domestic pump prices increased by: • Php ..."), so widen to
	// the enclosing sentence only when the clause itself is silent.
	neg := decreaseRe.MatchString(amountClause)
	pos := increaseRe.MatchString(amountClause)
	if neg == pos {
		neg = decreaseRe.MatchString(dirClause)
		pos = increaseRe.MatchString(dirClause)
	}
	if neg == pos {
		return nil, false, fmt.Errorf("ambiguous direction in %q", amountClause)
	}
	lo, hi := 0.0, 0.0
	for i, m := range amounts {
		v, err := parseAmount(m[1])
		if err != nil {
			return nil, false, err
		}
		if v < 0 || v >= maxAdjAbs {
			return nil, false, fmt.Errorf("adjustment %v out of range", v)
		}
		if i == 0 || v < lo {
			lo = v
		}
		if i == 0 || v > hi {
			hi = v
		}
	}
	if neg {
		lo, hi = -lo, -hi
		return &Range{Lo: lo, Hi: hi}, true, nil
	}
	return &Range{Lo: lo, Hi: hi}, true, nil
}

// clauseAfter returns the text after start up to the next bullet,
// newline, or sentence end (". ").
func clauseAfter(sec string, start int) string {
	end := len(sec)
	for _, sep := range []string{"\n", "•", ". ", " for "} {
		if i := strings.Index(sec[start:], sep); i >= 0 {
			if e := start + i; e < end {
				end = e
			}
		}
	}
	return strings.TrimSpace(sec[start:end])
}

// clauseBefore returns the text of the current clause ending at end:
// back to the previous bullet, sentence start (". ", "Meanwhile, ",
// "Domestic pump prices"), or newline.
func clauseBefore(sec string, end int) string {
	start := 0
	for _, sep := range []string{"\n", "•", ". ", "Meanwhile, ", "Domestic pump prices "} {
		if i := strings.LastIndex(sec[:end], sep); i >= 0 {
			if s := i + len(sep); s > start {
				start = s
			}
		}
	}
	return strings.TrimSpace(sec[start:end])
}

// sentenceBefore is clauseBefore without the bullet/newline cuts: the
// enclosing sentence, where the increase/decrease wording lives when
// the bullet clause itself is silent.
func sentenceBefore(sec string, end int) string {
	start := 0
	for _, sep := range []string{". ", "Meanwhile, ", "Domestic pump prices "} {
		if i := strings.LastIndex(sec[:end], sep); i >= 0 {
			if s := i + len(sep); s > start {
				start = s
			}
		}
	}
	return strings.TrimSpace(sec[start:end])
}

// parseAmount parses "1,710.00" style amounts.
func parseAmount(raw string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
}
