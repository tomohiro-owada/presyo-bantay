package parse

import (
	"math"
	"math/big"
	"sort"
	"strings"
)

// Sanity thresholds mirroring presyo_bantay/sanity.py.
const (
	MinMarkets                = 20
	MinCommoditiesPerMarket   = 20
	RangeMinCommoditiesPerMkt = 10
)

// RangeCoreCommodities mirrors RANGE_CORE_COMMODITIES.
var RangeCoreCommodities = []string{
	"Well-milled Rice (Local)",
	"Egg (Medium)",
}

// MarketsOfInterest mirrors MARKETS_OF_INTEREST.
var MarketsOfInterest = []string{
	"New Las Piñas City Public Market",
	"Alabang Central Market",
	"La Huerta", // matched by substring
	"Bicutan",   // matched by substring
}

// CoreCommodities mirrors CORE_COMMODITIES.
var CoreCommodities = []string{
	"Premium Rice (Imported)",
	"Well-milled Rice (Imported)",
	"Premium Rice (Local)",
	"Well-milled Rice (Local)",
	"Regular Milled (Local)",
	"Bangus",
	"Egg (Medium)",
}

// BackfillRice mirrors backfill._BACKFILL_RICE.
var BackfillRice = map[string]bool{
	"Premium Rice (Imported)":     true,
	"Well-milled Rice (Imported)": true,
	"Premium Rice (Local)":        true,
	"Well-milled Rice (Local)":    true,
	"Regular Milled (Local)":      true,
}

// Markets maps market -> commodity -> entry (mirrors the Python dict shape).
type Markets map[string]map[string]Entry

func normName(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}

// MatchMarket mirrors sanity.match_market: actual market key matching
// wanted by substring (normalized), either direction.
func MatchMarket(markets map[string]map[string]Entry, wanted string) string {
	needle := normName(wanted)
	for actual := range markets {
		hay := normName(actual)
		if strings.Contains(hay, needle) || strings.Contains(needle, hay) {
			return actual
		}
	}
	return ""
}

// MatchMarketName is MatchMarket over plain name lists (for export, which
// only has names, not parsed markets).
func MatchMarketName(names []string, wanted string) string {
	needle := normName(wanted)
	for _, actual := range names {
		hay := normName(actual)
		if strings.Contains(hay, needle) || strings.Contains(needle, hay) {
			return actual
		}
	}
	return ""
}

// RunSanityChecks mirrors sanity.run_sanity_checks.
func RunSanityChecks(markets map[string]map[string]Entry, layout string) error {
	if layout == "range" {
		return runRangeSanityChecks(markets)
	}
	if layout != "single" {
		return &LayoutChangedError{Msg: "Layout may have changed: unknown layout " + quote(layout)}
	}
	var problems []string
	if len(markets) < MinMarkets {
		problems = append(problems,
			itoa(len(markets))+" markets parsed (minimum "+itoa(MinMarkets)+")")
		// Python message: f"only {len(markets)} markets parsed (minimum {MIN_MARKETS})"
		problems[len(problems)-1] = "only " + problems[len(problems)-1]
	}
	thin := map[string]int{}
	for name, items := range markets {
		if len(items) < MinCommoditiesPerMarket {
			thin[name] = len(items)
		}
	}
	if len(thin) > 0 {
		problems = append(problems, thinDetail(thin, MinCommoditiesPerMarket))
	}
	for _, wanted := range MarketsOfInterest {
		actual := MatchMarket(markets, wanted)
		if actual == "" {
			problems = append(problems, "market of interest not found: "+quote(wanted))
			continue
		}
		var missing []string
		for _, c := range CoreCommodities {
			if _, ok := markets[actual][c]; !ok {
				missing = append(missing, c)
			}
		}
		if len(missing) > 0 {
			problems = append(problems,
				"market "+quote(actual)+" is missing core commodities: "+strings.Join(missing, ", "))
		}
	}
	if len(problems) > 0 {
		return &LayoutChangedError{Msg: "Layout may have changed: " + strings.Join(problems, "; ")}
	}
	return nil
}

func runRangeSanityChecks(markets map[string]map[string]Entry) error {
	var problems []string
	if len(markets) < MinMarkets {
		problems = append(problems,
			"only "+itoa(len(markets))+" markets parsed (minimum "+itoa(MinMarkets)+")")
	}
	nFull := 0
	for _, items := range markets {
		if len(items) >= RangeMinCommoditiesPerMkt {
			nFull++
		}
	}
	if len(markets) > 0 && nFull < (len(markets)+1)/2 {
		thin := map[string]int{}
		for name, items := range markets {
			if len(items) < RangeMinCommoditiesPerMkt {
				thin[name] = len(items)
			}
		}
		problems = append(problems, thinDetailRange(thin, nFull, len(markets)))
	}
	for _, core := range RangeCoreCommodities {
		nHave := 0
		for _, items := range markets {
			if _, ok := items[core]; ok {
				nHave++
			}
		}
		if len(markets) == 0 || nHave < (len(markets)+1)/2 {
			problems = append(problems,
				"core commodity "+quote(core)+" present in only "+
					itoa(nHave)+"/"+itoa(len(markets))+" markets")
		}
	}
	if len(problems) > 0 {
		return &LayoutChangedError{Msg: "Layout may have changed: " + strings.Join(problems, "; ")}
	}
	return nil
}

func thinDetail(thin map[string]int, min int) string {
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range thin {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v < kvs[j].v })
	if len(kvs) > 5 {
		kvs = kvs[:5]
	}
	var parts []string
	for _, e := range kvs {
		parts = append(parts, e.k+" ("+itoa(e.v)+")")
	}
	return itoa(len(thin)) + " market(s) have fewer than " +
		itoa(min) + " commodities: " + strings.Join(parts, ", ")
}

func thinDetailRange(thin map[string]int, nFull, total int) string {
	type kv struct {
		k string
		v int
	}
	var kvs []kv
	for k, v := range thin {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].v < kvs[j].v })
	if len(kvs) > 5 {
		kvs = kvs[:5]
	}
	var parts []string
	for _, e := range kvs {
		parts = append(parts, e.k+" ("+itoa(e.v)+")")
	}
	return "only " + itoa(nFull) + "/" + itoa(total) + " market(s) have at least " +
		itoa(RangeMinCommoditiesPerMkt) + " commodities: " + strings.Join(parts, ", ")
}

// RunBackfillChecks mirrors backfill.run_backfill_checks: hard structural
// checks raise *LayoutChangedError; missing markets of interest are
// returned as warnings.
func RunBackfillChecks(markets map[string]map[string]Entry, layout string) ([]string, error) {
	if layout != "single" && layout != "range" {
		return nil, &LayoutChangedError{Msg: "Layout may have changed: unknown layout " + quote(layout)}
	}
	minCommodities := MinCommoditiesPerMarket
	if layout == "range" {
		minCommodities = RangeMinCommoditiesPerMkt
	}
	var problems []string
	if len(markets) < MinMarkets {
		problems = append(problems,
			"only "+itoa(len(markets))+" markets parsed (minimum "+itoa(MinMarkets)+")")
	}
	thin := map[string]int{}
	for name, items := range markets {
		if len(items) < minCommodities {
			thin[name] = len(items)
		}
	}
	if len(thin) > 0 {
		problems = append(problems, thinDetail(thin, minCommodities))
	}
	present := map[string]bool{}
	for _, items := range markets {
		for c := range items {
			present[c] = true
		}
	}
	if layout == "range" {
		for _, core := range RangeCoreCommodities {
			if !present[core] {
				problems = append(problems, "core commodity missing everywhere: "+quote(core))
			}
		}
	} else {
		hasRice := false
		for r := range BackfillRice {
			if present[r] {
				hasRice = true
				break
			}
		}
		if !hasRice {
			problems = append(problems, "no core rice commodity found in any market")
		}
		for _, core := range []string{"Bangus", "Egg (Medium)"} {
			if !present[core] {
				problems = append(problems, "core commodity missing everywhere: "+quote(core))
			}
		}
	}
	if len(problems) > 0 {
		return nil, &LayoutChangedError{Msg: "Layout may have changed: " + strings.Join(problems, "; ")}
	}
	var warnings []string
	for _, wanted := range MarketsOfInterest {
		if MatchMarket(markets, wanted) == "" {
			warnings = append(warnings, "market of interest not found: "+quote(wanted))
		}
	}
	return warnings, nil
}

func quote(s string) string {
	return "'" + s + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// RoundN rounds to n decimals exactly like Python's round(x, n):
// round-half-even applied to the exact binary value.
func RoundN(x float64, n int) float64 {
	if isNaNOrInf(x) {
		return x
	}
	r := new(big.Rat).SetFloat64(x)
	if r == nil {
		return x
	}
	mult := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	q := new(big.Rat).Mul(r, new(big.Rat).SetInt(mult))
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
	f, _ := new(big.Rat).SetFrac(quo, mult).Float64()
	if neg {
		return -f
	}
	return f
}

func isNaNOrInf(x float64) bool {
	return math.IsNaN(x) || math.IsInf(x, 0)
}
