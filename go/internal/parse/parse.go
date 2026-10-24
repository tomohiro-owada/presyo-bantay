// Package parse ports presyo_bantay/pdfparse.py: single and range table
// layouts, price parsing, commodity normalization and report dates.
package parse

import (
	"fmt"
	"math"
	"math/big"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/pdftext"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/table"
)

// Entry is one parsed {market, commodity} price point.
type Entry struct {
	Spec      *string
	Unit      string
	Price     *float64
	PriceLow  *float64
	PriceHigh *float64
}

// NotAvailableTokens mirrors NOT_AVAILABLE_TOKENS.
var notAvailableTokens = map[string]bool{
	"NOT AVAILABLE":  true,
	"NOT AVAILALBLE": true,
	"N/A":            true,
	"NA":             true,
	"-":              true,
	"--":             true,
}

var months = map[string]int{
	"january": 1, "jan": 1,
	"february": 2, "feb": 2,
	"march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"may":  5,
	"june": 6, "jun": 6,
	"july": 7, "jul": 7,
	"august": 8, "aug": 8,
	"september": 9, "sept": 9, "sep": 9,
	"october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12,
}

var dateTextRes = []*regexp.Regexp{
	regexp.MustCompile(`(\d{1,2})\s+([A-Za-z]+)\s+(\d{4})`),
	regexp.MustCompile(`([A-Za-z]+)\s+(\d{1,2}),?\s+(\d{4})`),
}

var filenameDateRe = regexp.MustCompile(`Price-Monitoring-([A-Za-z]+)-(\d{1,2})-(\d{4})`)

// spaceRe collapses whitespace like Python re "\s+".
var spaceRe = regexp.MustCompile(`\s+`)

// NormalizeCommodity mirrors normalize_commodity.
func NormalizeCommodity(raw string) string {
	text := strings.ReplaceAll(raw, "\n", " ")
	text = spaceRe.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(strings.TrimSpace(text), "*")
	text = strings.TrimSpace(text)
	return spaceRe.ReplaceAllString(text, " ")
}

var parenRe = regexp.MustCompile(`\(([^)]+)\)`)

// InferSpec mirrors infer_spec.
func InferSpec(commodity string) *string {
	parens := parenRe.FindAllStringSubmatch(commodity, -1)
	after := ""
	if i := strings.LastIndex(commodity, ")"); i >= 0 {
		after = commodity[i+1:]
	}
	var parts []string
	for _, m := range parens {
		if s := strings.TrimSpace(m[1]); s != "" {
			parts = append(parts, s)
		}
	}
	if strings.TrimSpace(after) != "" {
		parts = append(parts, strings.TrimSpace(after))
	}
	if len(parts) == 0 {
		return nil
	}
	s := strings.Join(parts, ", ")
	return &s
}

// InferUnit mirrors infer_unit.
func InferUnit(rawHeader string) string {
	text := strings.TrimSpace(strings.ReplaceAll(rawHeader, "\n", " "))
	if strings.HasPrefix(text, "*") {
		return "per piece"
	}
	if strings.ToLower(NormalizeCommodity(rawHeader)) == "egg (medium)" {
		return "per piece"
	}
	return "per kg"
}

// ParsePrice mirrors parse_price.
func ParsePrice(raw *string) *float64 {
	if raw == nil {
		return nil
	}
	text := strings.ReplaceAll(strings.TrimSpace(*raw), ",", "")
	if text == "" {
		return nil
	}
	if notAvailableTokens[strings.ToUpper(text)] {
		return nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	return &f
}

var rangeRe = regexp.MustCompile(`^\s*([0-9]+(?:,[0-9]{3})*(?:\.[0-9]+)?)\s*-\s*([0-9]+(?:,[0-9]{3})*(?:\.[0-9]+)?)\s*$`)

// round2 mirrors Python round(x, 2): round-half-even applied to the exact
// binary value (then the nearest float64 to the decimal result).
func round2(x float64) float64 {
	return Round2(x)
}

// Round2 rounds to 2 decimals exactly like Python's round(x, 2).
func Round2(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	r := new(big.Rat).SetFloat64(x)
	if r == nil {
		return x
	}
	q := new(big.Rat).Mul(r, big.NewRat(100, 1))
	num := q.Num()
	den := q.Denom()
	neg := num.Sign() < 0
	abs := new(big.Int).Abs(num)
	quo, rem := new(big.Int).QuoRem(abs, den, new(big.Int))
	twice := new(big.Int).Lsh(rem, 1)
	switch c := twice.Cmp(den); {
	case c > 0:
		quo.Add(quo, big.NewInt(1))
	case c == 0:
		if quo.Bit(0) == 1 {
			quo.Add(quo, big.NewInt(1))
		}
	}
	f, _ := new(big.Rat).SetFrac(quo, big.NewInt(100)).Float64()
	if neg {
		return -f
	}
	return f
}

// ParseRangePrice mirrors parse_range_price.
func ParseRangePrice(raw *string) (price, low, high *float64) {
	if raw == nil {
		return nil, nil, nil
	}
	text := strings.TrimSpace(*raw)
	if text == "" {
		return nil, nil, nil
	}
	squished := strings.ReplaceAll(text, ",", "")
	if notAvailableTokens[strings.ToUpper(squished)] {
		return nil, nil, nil
	}
	if m := rangeRe.FindStringSubmatch(squished); m != nil {
		lo, err1 := strconv.ParseFloat(m[1], 64)
		hi, err2 := strconv.ParseFloat(m[2], 64)
		if err1 != nil || err2 != nil {
			return nil, nil, nil
		}
		mid := round2((lo + hi) / 2.0)
		return &mid, &lo, &hi
	}
	single := ParsePrice(&text)
	if single == nil {
		return nil, nil, nil
	}
	return single, single, single
}

// Date is a calendar date (YYYY-MM-DD).
type Date struct {
	Year, Month, Day int
}

func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func validDate(y, m, day int) *Date {
	if m < 1 || m > 12 || day < 1 || day > 31 || y < 2020 || y > 2035 {
		return nil
	}
	// Mirror datetime.date validation for day/month combos.
	dim := daysInMonth(y, m)
	if day > dim {
		return nil
	}
	return &Date{y, m, day}
}

func daysInMonth(y, m int) int {
	switch m {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
			return 29
		}
		return 28
	}
	return 0
}

// ParseDateFromFilename mirrors parse_date_from_filename.
func ParseDateFromFilename(pdfPath string) *Date {
	base := filepath.Base(pdfPath)
	m := filenameDateRe.FindStringSubmatch(base)
	if m == nil {
		return nil
	}
	month, ok := months[strings.ToLower(m[1])]
	if !ok {
		return nil
	}
	y, err1 := strconv.Atoi(m[3])
	day, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return nil
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return nil
	}
	if dim := daysInMonth(y, month); day > dim {
		return nil
	}
	return &Date{y, month, day}
}

// ParseDateFromText mirrors parse_date_from_text.
func ParseDateFromText(text string) *Date {
	for i, pattern := range dateTextRes {
		for _, m := range pattern.FindAllStringSubmatch(text, -1) {
			var dayS, monS, yearS string
			if i == 0 {
				dayS, monS, yearS = m[1], m[2], m[3]
			} else {
				monS, dayS, yearS = m[1], m[2], m[3]
			}
			month, ok := months[strings.ToLower(monS)]
			if !ok {
				continue
			}
			y, err1 := strconv.Atoi(yearS)
			day, err2 := strconv.Atoi(dayS)
			if err1 != nil || err2 != nil {
				continue
			}
			if d := validDate(y, month, day); d != nil {
				return d
			}
		}
	}
	return nil
}

// parseMarketTable mirrors _parse_market_table: one pdfplumber table into
// (commodities, units, data rows).
func parseMarketTable(t [][]*string) (commodities, units []string, rows [][]*string) {
	if len(t) < 4 {
		return nil, nil, nil
	}
	header := t[1]
	rawHeaders := make([]string, 0, len(header)-1)
	for _, c := range header[1:] {
		if c == nil {
			rawHeaders = append(rawHeaders, "")
		} else {
			rawHeaders = append(rawHeaders, *c)
		}
	}
	for _, h := range rawHeaders {
		commodities = append(commodities, NormalizeCommodity(h))
		units = append(units, InferUnit(h))
	}
	for _, row := range t[3:] {
		if len(row) == 0 {
			continue
		}
		first := ""
		if row[0] != nil {
			first = *row[0]
		}
		if strings.TrimSpace(first) == "" {
			continue
		}
		market := strings.Join(strings.Fields(first), " ")
		nr := []*string{&market}
		nr = append(nr, row[1:]...)
		rows = append(rows, nr)
	}
	return commodities, units, rows
}

// parseRangeMarketTable mirrors _parse_range_market_table.
func parseRangeMarketTable(t [][]*string) (commodities, units []string, rows [][]*string) {
	if len(t) < 3 {
		return nil, nil, nil
	}
	header := t[1]
	rawHeaders := make([]string, 0, len(header)-1)
	for _, c := range header[1:] {
		if c == nil {
			rawHeaders = append(rawHeaders, "")
		} else {
			rawHeaders = append(rawHeaders, *c)
		}
	}
	var groups [][]int
	for idx, raw := range rawHeaders {
		name := NormalizeCommodity(raw)
		if name == "" && len(groups) > 0 {
			groups[len(groups)-1] = append(groups[len(groups)-1], idx)
			continue
		}
		if name == "" {
			continue
		}
		groups = append(groups, []int{idx})
		commodities = append(commodities, name)
		units = append(units, InferUnit(raw))
	}
	for _, row := range t[2:] {
		if len(row) == 0 {
			continue
		}
		first := ""
		if row[0] != nil {
			first = *row[0]
		}
		if strings.TrimSpace(first) == "" {
			continue
		}
		market := strings.Join(strings.Fields(first), " ")
		cells := row[1:]
		merged := make([]*string, 0, len(groups))
		for _, g := range groups {
			var sb strings.Builder
			for _, i := range g {
				if i < len(cells) && cells[i] != nil {
					sb.WriteString(*cells[i])
				}
			}
			text := sb.String()
			if strings.TrimSpace(text) == "" {
				merged = append(merged, nil)
			} else {
				cp := text
				merged = append(merged, &cp)
			}
		}
		any := false
		for _, c := range merged {
			if c != nil && strings.TrimSpace(*c) != "" {
				any = true
				break
			}
		}
		if !any {
			continue
		}
		nr := []*string{&market}
		nr = append(nr, merged...)
		rows = append(rows, nr)
	}
	return commodities, units, rows
}

// SingleMarkers mirrors _SINGLE_MARKERS (lowercased).
var singleMarkers = map[string]bool{
	"premium rice (imported)":     true,
	"well-milled rice (imported)": true,
	"premium rice (local)":        true,
	"regular milled (local)":      true,
	"bangus":                      true,
}

// layoutFromCommodityNames mirrors _layout_from_commodity_names.
func layoutFromCommodityNames(names map[string]bool) string {
	lowered := map[string]bool{}
	for n := range names {
		if n != "" {
			lowered[n] = true
		}
	}
	for n := range lowered {
		if singleMarkers[n] {
			return "single"
		}
	}
	if lowered["well-milled rice (local)"] && lowered["egg (medium)"] && len(lowered) <= 16 {
		return "range"
	}
	if len(lowered) >= 20 {
		return "single"
	}
	return ""
}

// LayoutChangedError mirrors sanity.LayoutChangedError.
type LayoutChangedError struct {
	Msg string
}

func (e *LayoutChangedError) Error() string { return e.Msg }

// pageTables extracts pdfplumber-style tables for the first 3 pages.
func pageTables(doc *pdftext.Document) ([][][]*string, []string, error) {
	n := doc.NumPages()
	if n > 3 {
		n = 3
	}
	var tables [][][]*string
	var texts []string
	for i := 1; i <= n; i++ {
		pg, err := doc.Page(i)
		if err != nil {
			continue // mirror "except Exception: continue"
		}
		tables = append(tables, table.ExtractTables(pg)...)
		texts = append(texts, table.PageText(pg))
	}
	return tables, texts, nil
}

// DetectLayout mirrors detect_layout.
func DetectLayout(pdfPath string) (string, error) {
	doc, err := pdftext.Open(pdfPath)
	if err != nil {
		return "", err
	}
	defer doc.Close()
	tables, _, err := pageTables(doc)
	if err != nil {
		return "", err
	}
	names := map[string]bool{}
	for _, t := range tables {
		if len(t) < 2 {
			continue
		}
		header := t[1]
		for _, cell := range header[1:] {
			s := ""
			if cell != nil {
				s = *cell
			}
			name := NormalizeCommodity(s)
			if name != "" && strings.ToUpper(name) != "MARKET" {
				names[strings.ToLower(name)] = true
			}
		}
	}
	layout := layoutFromCommodityNames(names)
	if layout == "" {
		return "", &LayoutChangedError{Msg: fmt.Sprintf(
			"Layout may have changed: unrecognized commodity headers (%d commodities)", len(names))}
	}
	return layout, nil
}

// InferLayout mirrors infer_layout.
func InferLayout(markets map[string]map[string]Entry) (string, error) {
	names := map[string]bool{}
	for _, items := range markets {
		for k := range items {
			names[strings.Join(strings.Fields(strings.ToLower(k)), " ")] = true
		}
	}
	layout := layoutFromCommodityNames(names)
	if layout == "" {
		return "", &LayoutChangedError{Msg: fmt.Sprintf(
			"Layout may have changed: unrecognized commodity set (%d commodities)", len(names))}
	}
	return layout, nil
}

func parseSinglePDF(doc *pdftext.Document) (map[string]map[string]Entry, *Date) {
	markets, hint, _, _ := parseSinglePDFOrdered(doc)
	return markets, hint
}

// parseSinglePDFOrdered parses like parseSinglePDF and additionally records
// insertion order (market first-seen order and per-market commodity
// first-seen order), mirroring Python dict insertion order for JSON output.
func parseSinglePDFOrdered(doc *pdftext.Document) (map[string]map[string]Entry, *Date, []string, map[string][]string) {
	markets := map[string]map[string]Entry{}
	var marketOrder []string
	commodityOrder := map[string][]string{}
	seenCommodity := map[string]map[string]bool{}
	var dateHint *Date
	tables, texts, _ := pageTables(doc)
	for _, t := range tables {
		commodities, units, rows := parseMarketTable(t)
		if len(commodities) == 0 {
			continue
		}
		anyC := false
		for _, c := range commodities {
			if c != "" {
				anyC = true
				break
			}
		}
		if !anyC {
			continue
		}
		for _, row := range rows {
			market := *row[0]
			cells := row[1:]
			entry := markets[market]
			if entry == nil {
				entry = map[string]Entry{}
				markets[market] = entry
				marketOrder = append(marketOrder, market)
				seenCommodity[market] = map[string]bool{}
			}
			for j := range commodities {
				if j >= len(cells) {
					break
				}
				commodity, unit, cell := commodities[j], units[j], cells[j]
				if commodity == "" || strings.ToUpper(commodity) == "MARKET" {
					continue
				}
				var s *string
				if cell != nil {
					s = cell
				} else {
					e := ""
					s = &e
				}
				price := ParsePrice(s)
				entry[commodity] = Entry{Spec: InferSpec(commodity), Unit: unit, Price: price, PriceLow: price, PriceHigh: price}
				if !seenCommodity[market][commodity] {
					seenCommodity[market][commodity] = true
					commodityOrder[market] = append(commodityOrder[market], commodity)
				}
			}
		}
	}
	// date_hint from the first page whose text yields a date (mirrors the
	// per-page "if date_hint is None" logic).
	for _, text := range texts {
		if d := ParseDateFromText(text); d != nil {
			dateHint = d
			break
		}
	}
	return markets, dateHint, marketOrder, commodityOrder
}

func parseRangePDF(doc *pdftext.Document) (map[string]map[string]Entry, *Date) {
	markets, hint, _, _ := parseRangePDFOrdered(doc)
	return markets, hint
}

func parseRangePDFOrdered(doc *pdftext.Document) (map[string]map[string]Entry, *Date, []string, map[string][]string) {
	markets := map[string]map[string]Entry{}
	var marketOrder []string
	commodityOrder := map[string][]string{}
	seenCommodity := map[string]map[string]bool{}
	tables, texts, _ := pageTables(doc)
	for _, t := range tables {
		commodities, units, rows := parseRangeMarketTable(t)
		if len(commodities) == 0 {
			continue
		}
		anyC := false
		for _, c := range commodities {
			if c != "" {
				anyC = true
				break
			}
		}
		if !anyC {
			continue
		}
		for _, row := range rows {
			market := *row[0]
			cells := row[1:]
			entry := markets[market]
			if entry == nil {
				entry = map[string]Entry{}
				markets[market] = entry
				marketOrder = append(marketOrder, market)
				seenCommodity[market] = map[string]bool{}
			}
			for j := range commodities {
				if j >= len(cells) {
					break
				}
				commodity, unit, cell := commodities[j], units[j], cells[j]
				if commodity == "" || strings.ToUpper(commodity) == "MARKET" {
					continue
				}
				var s *string
				if cell != nil {
					s = cell
				} else {
					e := ""
					s = &e
				}
				price, low, high := ParseRangePrice(s)
				entry[commodity] = Entry{Spec: InferSpec(commodity), Unit: unit, Price: price, PriceLow: low, PriceHigh: high}
				if !seenCommodity[market][commodity] {
					seenCommodity[market][commodity] = true
					commodityOrder[market] = append(commodityOrder[market], commodity)
				}
			}
		}
	}
	var dateHint *Date
	for _, text := range texts {
		if d := ParseDateFromText(text); d != nil {
			dateHint = d
			break
		}
	}
	return markets, dateHint, marketOrder, commodityOrder
}

// ParsePDFWithLayout mirrors parse_pdf_with_layout.
func ParsePDFWithLayout(pdfPath string) (map[string]map[string]Entry, *Date, string, error) {
	markets, hint, layout, _, _, err := ParsePDFOrdered(pdfPath)
	return markets, hint, layout, err
}

// ParsePDFOrdered parses like ParsePDFWithLayout and additionally returns
// market insertion order and per-market commodity insertion order (mirroring
// Python dict order) for stable JSON output.
func ParsePDFOrdered(pdfPath string) (map[string]map[string]Entry, *Date, string, []string, map[string][]string, error) {
	layout, err := DetectLayout(pdfPath)
	if err != nil {
		return nil, nil, "", nil, nil, err
	}
	doc, err := pdftext.Open(pdfPath)
	if err != nil {
		return nil, nil, "", nil, nil, err
	}
	defer doc.Close()
	if layout == "range" {
		m, h, mo, co := parseRangePDFOrdered(doc)
		return m, h, layout, mo, co, nil
	}
	m, h, mo, co := parseSinglePDFOrdered(doc)
	return m, h, layout, mo, co, nil
}

// ReportDate mirrors cli/cmd_parse date rule: filename first, then PDF text.
func ReportDate(pdfPath string, textHint *Date) *Date {
	if d := ParseDateFromFilename(pdfPath); d != nil {
		return d
	}
	return textHint
}

// SortedMarkets returns market names in sorted order (for stable output).
func SortedMarkets(markets map[string]map[string]Entry) []string {
	out := make([]string, 0, len(markets))
	for m := range markets {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
