// Package advice implements the LLM-advice side of the prototype:
//   - Check (check.go): verify every peso amount / percentage in the advice
//     text exists in (or is derivable from) signals.json (v2 local basis).
//   - Advise (advise.go): run the opencode CLI over signals.json and check
//     the result, plus the jargon lint (lint.go).
package advice

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// KindPeso and KindPercent are the two mention kinds.
const (
	KindPeso    = "peso"
	KindPercent = "percent"
)

// StatusPass / StatusFail are the report statuses.
const (
	StatusPass = "pass"
	StatusFail = "fail"
)

// Flag reasons recorded per finding.
const (
	// ReasonNotBound is set when the mention's context names one or more
	// commodities but the number matches none of their kind-aware values.
	ReasonNotBound = "not bound to commodity in context"
	// ReasonNoMatch is set when an unbound mention (no commodity in
	// context) matches nothing in the global kind-aware set.
	ReasonNoMatch = "no match in signals"
	// ReasonMarketMismatch is set when a peso amount adjacent to (just
	// after, within ~20 chars of) a market name does not equal that
	// market's market_prices value for a commodity in context.
	ReasonMarketMismatch = "market mismatch"
	// ReasonBasisMismatch is set when a sentence naming NCR / Metro Manila
	// / Kamaynilaan / 首都圏 / マニラ首都圏 contains a number (signals
	// contain no NCR numbers).
	ReasonBasisMismatch = "ncr basis not in signals"
	// ReasonMetricMismatch is set when a percentage in a 7-day context
	// does not match trend_7d_pct, or a percentage in a usual/30-day
	// context (with no 7-day words) does not match pct_vs_median_30d.
	ReasonMetricMismatch = "metric mismatch"
)

// Mention is one extracted peso amount or percentage.
type Mention struct {
	Section  string
	Line     int
	Snippet  string
	Value    float64 // signed parsed value
	Decimals int     // decimals as written
	Kind     string  // "peso" or "percent"
	// Context is the sentence-level segment of the line holding the
	// mention: the bullet/line, split further on ".", "。", ";" (a "."
	// between digits, as in "43.2%", never splits). Commodity binding is
	// resolved against this segment.
	Context string
	// off/end locate the match inside the normalized line; segLo/segHi
	// locate the Context segment. Used for market adjacency and the
	// metric window (words between the previous number and this one).
	// lineText is the full normalized line (unexported).
	off, end     int
	segLo, segHi int
	lineText     string
}

// Finding is a mention that matched nothing (serializes into check.json).
type Finding struct {
	Section     string   `json:"section"`
	Line        int      `json:"line"`
	Snippet     string   `json:"snippet"`
	Value       float64  `json:"value"`
	Kind        string   `json:"kind"`
	Reason      string   `json:"reason"`
	Commodities []string `json:"commodities_in_context"`
}

// SectionStats counts mentions per advice section.
type SectionStats struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Derived int `json:"derived"`
	Flagged int `json:"flagged"`
	Unbound int `json:"unbound"`
}

// Report is the check.json document.
type Report struct {
	Status     string                  `json:"status"`
	Total      int                     `json:"total"`
	OK         int                     `json:"ok"`
	Derived    int                     `json:"derived"`
	Unbound    int                     `json:"unbound"`
	Flagged    []Finding               `json:"flagged"`
	PerSection map[string]SectionStats `json:"per_section"`
	// Warning is set when any extracted number has an empty section
	// (before the first header, e.g. a preamble glued to "## Taglish")
	// or an unknown section. It does not change status by itself.
	Warning string `json:"warning,omitempty"`
	// Lint holds the v2 jargon-lint findings. Any lint flag also makes
	// the status "fail" (see ApplyLint).
	Lint LintReport `json:"lint"`
	// FBPost holds the combined-Facebook-post check (v3 section 7).
	// It is set by advise for advice.md's combined post; advice-check
	// leaves it nil (single-file check only).
	FBPost *Report `json:"fb_post,omitempty"`
}

// ApplyLint attaches a LintReport to a check Report and flips the status
// to "fail" when the lint found anything.
func ApplyLint(rep *Report, lint LintReport) {
	rep.Lint = lint
	if len(lint.Flags) > 0 {
		rep.Status = StatusFail
	}
}

var (
	numSrc = `[+-]?[0-9][0-9,]*(\.[0-9]+)?`

	pesoPrefixRe = regexp.MustCompile(`(?:₱|(?i:php))\s*(` + numSrc + `)`)
	// Bare "P" (followed by digits): the letter boundary is checked in
	// Go code (RE2 has no lookbehind).
	barePRe      = regexp.MustCompile(`P\s*(` + numSrc + `)`)
	pesoSuffixRe = regexp.MustCompile(`(` + numSrc + `)\s*(?:(?i:pesos?)|(?i:piso)|ペソ)`)
	pctRe        = regexp.MustCompile(`(` + numSrc + `)\s*(?:%|％|(?i:percents?)|(?i:porsyento)|パーセント)`)
	headerRe     = regexp.MustCompile(`^##\s+(.*?)\s*$`)
)

// normalizeDigits maps full-width digits/punctuation to ASCII so
// "１８０ペソ" / "５％" parse like "180ペソ" / "5%". Full-width 。/．
// become "." and ， becomes "," (later stripped as a thousands separator).
// ASCII commas are stripped as thousands separators ("1,234.50").
func normalizeDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '０' && r <= '９':
			b.WriteRune('0' + (r - '０'))
		case r == '．' || r == '。':
			b.WriteRune('.')
		case r == '，':
			b.WriteRune(',')
		case r == '－' || r == '−':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type span struct{ lo, hi int }

func overlaps(spans []span, lo, hi int) bool {
	for _, s := range spans {
		if lo < s.hi && hi > s.lo {
			return true
		}
	}
	return false
}

func parseNumber(raw string) (v float64, decimals int, ok bool) {
	clean := strings.ReplaceAll(raw, ",", "")
	decimals = 0
	if i := strings.Index(clean, "."); i >= 0 {
		decimals = len(clean) - i - 1
	}
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, 0, false
	}
	return v, decimals, true
}

// splitSentences cuts a (normalized) line into sentence-level segments on
// ".", "。" (already normalized to "."), ";" and "；". A "." between two
// digits (e.g. the "." in "43.2%") never splits.
func splitSentences(line string) []span {
	var segs []span
	start := 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		delim := false
		switch r {
		case ';', '；', '。':
			delim = true
		case '.':
			prevDigit := i > 0 && line[i-1] >= '0' && line[i-1] <= '9'
			nextDigit := i+1 < len(line) && line[i+1] >= '0' && line[i+1] <= '9'
			if !(prevDigit && nextDigit) {
				delim = true
			}
		}
		if delim {
			segs = append(segs, span{start, i + size})
			start = i + size
		}
		i += size
	}
	segs = append(segs, span{start, len(line)})
	return segs
}

// segment holding byte offset off.
func segFor(segs []span, off int) span {
	for _, s := range segs {
		if off >= s.lo && off < s.hi {
			return s
		}
	}
	if len(segs) > 0 {
		return segs[len(segs)-1]
	}
	return span{0, off}
}

// Extract finds every peso amount and percentage in text, recording the
// 1-indexed line number and the ## section (Taglish/English/日本語).
// Required headers are recognised even when glued mid-line
// ("...JSON.## Taglish"): a newline is inserted before them first, mirroring
// advise.NormalizeAdvice but WITHOUT stripping the preamble, so numbers
// before the first header keep section "" and trigger a warning in Check.
// Each mention also records its sentence-level Context (see Mention) plus
// match/segment offsets for the market and metric checks.
func Extract(text string) []Mention {
	text = insertHeaderNewlines(text)
	var out []Mention
	section := ""
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		if m := headerRe.FindStringSubmatch(raw); m != nil {
			switch strings.ToLower(strings.TrimSpace(m[1])) {
			case "taglish":
				section = "Taglish"
			case "english":
				section = "English"
			case "日本語":
				section = "日本語"
			default:
				section = strings.TrimSpace(m[1])
			}
			continue
		}
		line := normalizeDigits(raw)
		segs := splitSentences(line)
		var claimed []span
		claim := func(re *regexp.Regexp, kind string, bareP bool) {
			for _, idx := range re.FindAllStringSubmatchIndex(line, -1) {
				if len(idx) < 4 || idx[2] < 0 {
					continue
				}
				// Bare "P" must not be part of a longer word ("UP 180").
				if bareP && idx[0] > 0 {
					if prev := line[idx[0]-1]; prev == '_' ||
						('A' <= prev && prev <= 'Z') || ('a' <= prev && prev <= 'z') ||
						('0' <= prev && prev <= '9') {
						continue
					}
				}
				if overlaps(claimed, idx[0], idx[1]) || overlaps(claimed, idx[2], idx[3]) {
					continue
				}
				v, d, ok := parseNumber(line[idx[2]:idx[3]])
				if !ok {
					continue
				}
				claimed = append(claimed, span{idx[0], idx[1]})
				sg := segFor(segs, idx[0])
				ctx := strings.TrimSpace(line[sg.lo:sg.hi])
				out = append(out, Mention{
					Section: section, Line: i + 1,
					Snippet: strings.TrimSpace(line[idx[0]:idx[1]]),
					Value:   v, Decimals: d, Kind: kind,
					Context: ctx,
					off:     idx[2], end: idx[3], segLo: sg.lo, segHi: sg.hi,
					lineText: line,
				})
			}
		}
		// Peso-prefix first (₱/PHP/P) so "₱50%" style overlaps resolve to
		// the peso; percent last. Peso-suffix before percent so "180 pesos"
		// is not misread (it has no % sign, so order is just defensive).
		claim(pesoPrefixRe, KindPeso, false)
		claim(barePRe, KindPeso, true)
		claim(pesoSuffixRe, KindPeso, false)
		claim(pctRe, KindPercent, false)
	}
	// Stable order: by line, then position (claim groups pesos before
	// percents, so re-sort for window/adjacency logic).
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].off < out[j].off
	})
	return out
}

// Matches reports whether a number written with d decimals matches any
// candidate: equal after rounding the candidate to d decimals, or within
// 0.5*10^-d (0.5 for 0-decimal pesos, 0.05/0.5 for 1-/0-decimal percents).
// Comparison is on absolute values (used for peso amounts, which are
// always compared by magnitude; diff_vs_median_30d and cheapest diffs are
// stored signed but match by magnitude).
func Matches(x float64, d int, candidates []float64) bool {
	ax := math.Abs(x)
	abs := make([]float64, 0, len(candidates))
	for _, c := range candidates {
		abs = append(abs, math.Abs(c))
	}
	return anyApprox(ax, d, abs)
}

// approxSigned reports signed equality of x with candidate c at d written
// decimals: equal after rounding c to d decimals, or |x-c| <= 0.5*10^-d.
func approxSigned(x, c float64, d int) bool {
	scale := math.Pow(10, float64(d))
	tol := 0.5 / scale
	if math.Abs(math.Round(c*scale)/scale-x) <= 1e-9 {
		return true
	}
	if math.Abs(x-c) <= tol+1e-9 {
		return true
	}
	return false
}

func anyApprox(x float64, d int, candidates []float64) bool {
	for _, c := range candidates {
		if approxSigned(x, c, d) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Kind-aware candidate pools built from v2 signals.json.
// ---------------------------------------------------------------------------

// Money fields of a v2 commodity object (direct leaves). Counts never
// match (n_days_30d is an int count); dates never match.
var commMoneyKeys = []string{
	"price", "median_30d", "diff_vs_median_30d", "trend_7d_base_value",
}

// Percentage fields of a v2 commodity object (direct leaves).
var commPctKeys = []string{
	"pct_vs_median_30d", "trend_7d_pct",
}

// alias maps one name variant (Filipino, Japanese, short form) to the
// canonical signals.json commodity names it may refer to. Entries naming
// commodities absent from a given signals file are ignored.
type alias struct {
	variant string   // lowercase for latin, as-is for Japanese
	latin   bool     // latin variants match on ASCII word boundaries
	targets []string // canonical commodity names
}

var aliasTable = []alias{
	{"galunggong", true, []string{"Galunggong (Local)"}},
	{"ガルンゴン", false, []string{"Galunggong (Local)"}},
	{"tilapia", true, []string{"Tilapia"}},
	{"ティラピア", false, []string{"Tilapia"}},
	{"bangus", true, []string{"Bangus"}},
	{"バングス", false, []string{"Bangus"}},
	{"chayote", true, []string{"Chayote"}},
	{"sayote", true, []string{"Chayote"}},
	{"ハヤトウリ", false, []string{"Chayote"}},
	{"サヨテ", false, []string{"Chayote"}},
	{"eggplant", true, []string{"Eggplant"}},
	{"talong", true, []string{"Eggplant"}},
	{"ナス", false, []string{"Eggplant"}},
	{"ampalaya", true, []string{"Ampalaya"}},
	{"ニガウリ", false, []string{"Ampalaya"}},
	{"ゴーヤ", false, []string{"Ampalaya"}},
	{"cabbage", true, []string{"Cabbage"}},
	{"repolyo", true, []string{"Cabbage"}},
	{"キャベツ", false, []string{"Cabbage"}},
	{"pechay baguio", true, []string{"Pechay Baguio"}},
	{"pechay", true, []string{"Pechay Baguio"}},
	{"ペチャイ", false, []string{"Pechay Baguio"}},
	{"tomato", true, []string{"Tomato"}},
	{"kamatis", true, []string{"Tomato"}},
	{"トマト", false, []string{"Tomato"}},
	{"white potato", true, []string{"White Potato (Local)"}},
	{"patatas", true, []string{"White Potato (Local)"}},
	{"ジャガイモ", false, []string{"White Potato (Local)"}},
	{"じゃがいも", false, []string{"White Potato (Local)"}},
	{"carrots", true, []string{"Carrots (Local)"}},
	{"carrot", true, []string{"Carrots (Local)"}},
	{"karot", true, []string{"Carrots (Local)"}},
	{"ニンジン", false, []string{"Carrots (Local)"}},
	{"にんじん", false, []string{"Carrots (Local)"}},
	{"fresh whole chicken", true, []string{"Fresh Whole Chicken"}},
	{"whole chicken", true, []string{"Fresh Whole Chicken"}},
	{"chicken", true, []string{"Fresh Whole Chicken"}},
	{"manok", true, []string{"Fresh Whole Chicken"}},
	{"鶏肉", false, []string{"Fresh Whole Chicken"}},
	{"丸鶏", false, []string{"Fresh Whole Chicken"}},
	{"鶏", false, []string{"Fresh Whole Chicken"}},
	{"kasim", true, []string{"Fresh Pork (Kasim) Imported", "Fresh Pork (Kasim) Local", "Fresh Pork Kasim/Pigue"}},
	{"pigue", true, []string{"Fresh Pork Kasim/Pigue"}},
	{"カシム", false, []string{"Fresh Pork (Kasim) Imported", "Fresh Pork (Kasim) Local", "Fresh Pork Kasim/Pigue"}},
	{"豚肩ロース", false, []string{"Fresh Pork (Kasim) Imported", "Fresh Pork (Kasim) Local", "Fresh Pork Kasim/Pigue"}},
	{"liempo", true, []string{"Fresh Pork Liempo", "Fresh Pork Liempo (Imported)", "Fresh Pork Liempo (Local)"}},
	{"リエンポ", false, []string{"Fresh Pork Liempo", "Fresh Pork Liempo (Imported)", "Fresh Pork Liempo (Local)"}},
	{"豚バラ", false, []string{"Fresh Pork Liempo", "Fresh Pork Liempo (Imported)", "Fresh Pork Liempo (Local)"}},
	{"egg", true, []string{"Egg (Medium)"}},
	{"itlog", true, []string{"Egg (Medium)"}},
	{"卵", false, []string{"Egg (Medium)"}},
	{"rice", true, []string{"Premium Rice (Imported)", "Premium Rice (Local)", "Well-milled Rice (Imported)", "Well-milled Rice (Local)", "Regular Milled (Local)"}},
	{"bigas", true, []string{"Premium Rice (Imported)", "Premium Rice (Local)", "Well-milled Rice (Imported)", "Well-milled Rice (Local)", "Regular Milled (Local)"}},
	{"米", false, []string{"Premium Rice (Imported)", "Premium Rice (Local)", "Well-milled Rice (Imported)", "Well-milled Rice (Local)", "Regular Milled (Local)"}},
	{"red onion", true, []string{"Red Onion (Local)"}},
	{"white onion", true, []string{"White Onion (Local)"}},
	{"sibuyas", true, []string{"Red Onion (Local)", "White Onion (Local)"}},
	{"玉ねぎ", false, []string{"Red Onion (Local)", "White Onion (Local)"}},
	{"calamansi", true, []string{"Calamansi"}},
	{"kalamansi", true, []string{"Calamansi"}},
	{"カラマンシー", false, []string{"Calamansi"}},
	{"sugar", true, []string{"Sugar (Refined)", "Sugar (Washed)"}},
	{"asukal", true, []string{"Sugar (Refined)", "Sugar (Washed)"}},
	{"砂糖", false, []string{"Sugar (Refined)", "Sugar (Washed)"}},
	{"chili", true, []string{"Chili (Green)", "Red Chili (Local)"}},
	{"sili", true, []string{"Chili (Green)", "Red Chili (Local)"}},
	{"唐辛子", false, []string{"Chili (Green)", "Red Chili (Local)"}},
	{"青唐辛子", false, []string{"Chili (Green)"}},
	{"赤唐辛子", false, []string{"Red Chili (Local)"}},
}

// fxKeywordsLatin marks a context as being about the USD/PHP rate: FX
// rates (money) only match peso mentions in such a context.
var fxKeywordsLatin = []string{
	"usd", "dollar", "dolyar", "fx", "forex", "exchange",
}
var fxKeywordsJP = []string{"為替", "ドル"}

// ncrTokens flags the basis-mismatch check: any sentence containing one of
// these that also contains a number is flagged (v2 signals hold no NCR
// numbers).
var ncrTokensLatin = []string{"ncr", "metro manila", "kamaynilaan"}
var ncrTokensJP = []string{"首都圏", "マニラ首都圏"}

// sevenDayWords / usualWords drive the metric-mismatch check (matched in
// the window around the mention within the same sentence — between the
// previous and next numbers; case-insensitive for Latin). They cover the
// v3 template phrasings: "down/up X% vs last week", "bumaba/tumaas ng X%
// mula noong nakaraang linggo", "先週比 ±X%", and "below/above usual",
// "mas mura/mahal kaysa karaniwan", "いつもよりX%安い/高い".
var sevenDayWordsLatin = []string{"7 araw", "isang linggo", "nakaraang linggo", "linggo", "7-day", "7 day", "week", "last week"}
var sevenDayWordsJP = []string{"1週間", "7日", "先週", "先週比"}
var usualWordsLatin = []string{"karaniwan", "kaysa karaniwan", "usual", "below usual", "above usual", "mas mura", "mas mahal", "30-day", "30 day", "30 araw"}
var usualWordsJP = []string{"いつも", "いつもより", "普段", "30日"}

// marketVariant maps one market name variant to a market_prices short key.
type marketVariant struct {
	variant string // lowercase for latin, as-is for Japanese
	latin   bool
	short   string
}

var marketVariants = []marketVariant{
	{"las piñas", true, "Las Piñas"},
	{"las pinas", true, "Las Piñas"},
	{"las pina", true, "Las Piñas"},
	{"laspiñas", true, "Las Piñas"},
	{"ラス・ピニャス", false, "Las Piñas"},
	{"ラスピニャス", false, "Las Piñas"},
	{"alabang", true, "Alabang"},
	{"アラバン", false, "Alabang"},
	{"la huerta", true, "La Huerta"},
	{"lahuerta", true, "La Huerta"},
	{"ラ・ウエルタ", false, "La Huerta"},
	{"ラウエルタ", false, "La Huerta"},
	{"bicutan", true, "Bicutan"},
	{"ビクタン", false, "Bicutan"},
	{"pasay", true, "Pasay"},
	{"パサイ", false, "Pasay"},
}

// marketHit is one market-name occurrence inside a segment.
type marketHit struct {
	short string
	end   int // byte offset just past the name in segment coordinates
}

type commPool struct {
	name string
	// Direct leaves.
	money []float64
	pct   []float64
	// trend holds trend_7d_pct values (for the metric check).
	trend []float64
	// medians holds pct_vs_median_30d values (for the metric check).
	medians []float64
	// Derived: |price - median| / |price - market price| money gaps and
	// their as-percent forms.
	derivMoney []float64
	derivPct   []float64
	// market maps market_prices short name -> price.
	market map[string]float64
	price  float64
	hasP   bool
	median float64
	hasM   bool
}

type pools struct {
	comms   []*commPool
	byName  map[string]*commPool
	idx     map[string]int
	fxRates []float64 // latest_rate, rate_30d_ago (money, FX context only)
	fxPct   []float64 // fx_change_30d_pct

	globalMoneyDirect []float64
	globalMoneyDeriv  []float64
	globalPctDirect   []float64
	globalPctDeriv    []float64
	globalTrend       []float64
	globalMedianPct   []float64

	latinRes         []*regexp.Regexp // parallel to latinTargets
	latinTg          [][]string
	jpVars           []string
	jpTg             [][]string
	canonSub         []string // canonical full names, lowered (substring)
	canonStrippedRes []*regexp.Regexp
	canonStrippedTg  [][]string
	fxRes            []*regexp.Regexp
}

func f64(m map[string]any, k string) (float64, bool) {
	v, ok := m[k]
	if !ok || v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// stripParen returns the commodity name without "(...)" qualifiers,
// collapsing whitespace ("White Potato (Local)" -> "White Potato").
var parenRe = regexp.MustCompile(`\([^)]*\)`)

func stripParen(name string) string {
	s := parenRe.ReplaceAllString(name, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func buildPools(signalsData []byte) (*pools, error) {
	var top map[string]any
	if err := json.Unmarshal(signalsData, &top); err != nil {
		return nil, fmt.Errorf("signals JSON: %w", err)
	}
	p := &pools{
		byName: map[string]*commPool{},
		idx:    map[string]int{},
	}
	var commObjs []map[string]any
	if arr, ok := top["commodities"].([]any); ok {
		for _, e := range arr {
			if o, ok := e.(map[string]any); ok {
				commObjs = append(commObjs, o)
			}
		}
	}
	for _, o := range commObjs {
		name, _ := o["name"].(string)
		if name == "" {
			continue
		}
		c := &commPool{name: name, market: map[string]float64{}}
		for _, k := range commMoneyKeys {
			if f, ok := f64(o, k); ok {
				c.money = append(c.money, f)
			}
		}
		for _, k := range commPctKeys {
			if f, ok := f64(o, k); ok {
				c.pct = append(c.pct, f)
			}
		}
		if f, ok := f64(o, "trend_7d_pct"); ok {
			c.trend = append(c.trend, f)
		}
		if f, ok := f64(o, "pct_vs_median_30d"); ok {
			c.medians = append(c.medians, f)
		}
		if mp, ok := o["market_prices"].(map[string]any); ok {
			for mkt, v := range mp {
				if f, ok := v.(float64); ok {
					c.market[mkt] = f
					c.money = append(c.money, f)
				}
			}
		}
		if cn, ok := o["cheapest_nearby"].(map[string]any); ok {
			if f, ok := f64(cn, "price"); ok {
				c.money = append(c.money, f)
			}
			if f, ok := f64(cn, "diff_vs_basis"); ok {
				c.money = append(c.money, f)
			}
		}
		if f, ok := f64(o, "price"); ok {
			c.price, c.hasP = f, true
		}
		if f, ok := f64(o, "median_30d"); ok {
			c.median, c.hasM = f, true
		}
		// Derived within-commodity gaps: price vs median and price vs
		// each market price (money gaps and their as-percent forms).
		addDeriv := func(a, b float64) {
			d := math.Abs(a - b)
			c.derivMoney = append(c.derivMoney, d)
			if a != 0 {
				c.derivPct = append(c.derivPct, d/math.Abs(a)*100)
			}
			if b != 0 {
				c.derivPct = append(c.derivPct, d/math.Abs(b)*100)
			}
		}
		if c.hasP && c.hasM {
			addDeriv(c.price, c.median)
		}
		if c.hasP {
			for _, mp := range c.market {
				addDeriv(c.price, mp)
			}
		}
		p.idx[name] = len(p.comms)
		p.comms = append(p.comms, c)
		p.byName[name] = c
	}
	if fx, ok := top["fx"].(map[string]any); ok {
		if f, ok := f64(fx, "latest_rate"); ok {
			p.fxRates = append(p.fxRates, f)
		}
		if f, ok := f64(fx, "rate_30d_ago"); ok {
			p.fxRates = append(p.fxRates, f)
		}
		if f, ok := f64(fx, "fx_change_30d_pct"); ok {
			p.fxPct = []float64{f}
		}
	}
	// Global kind-aware pools (unbound fallback).
	for _, c := range p.comms {
		p.globalMoneyDirect = append(p.globalMoneyDirect, c.money...)
		p.globalMoneyDeriv = append(p.globalMoneyDeriv, c.derivMoney...)
		p.globalPctDirect = append(p.globalPctDirect, c.pct...)
		p.globalPctDeriv = append(p.globalPctDeriv, c.derivPct...)
		p.globalTrend = append(p.globalTrend, c.trend...)
		p.globalMedianPct = append(p.globalMedianPct, c.medians...)
	}
	p.globalPctDirect = append(p.globalPctDirect, p.fxPct...)
	// Name matcher patterns.
	for _, c := range p.comms {
		p.canonSub = append(p.canonSub, strings.ToLower(c.name))
		stripped := stripParen(c.name)
		if stripped != "" && !strings.EqualFold(stripped, c.name) {
			p.canonStrippedRes = append(p.canonStrippedRes,
				regexp.MustCompile(`\b`+regexp.QuoteMeta(strings.ToLower(stripped))+`\b`))
			p.canonStrippedTg = append(p.canonStrippedTg, []string{c.name})
		}
	}
	for _, a := range aliasTable {
		var tg []string
		for _, t := range a.targets {
			if _, ok := p.byName[t]; ok {
				tg = append(tg, t)
			}
		}
		if len(tg) == 0 {
			continue
		}
		if a.latin {
			p.latinRes = append(p.latinRes,
				regexp.MustCompile(`\b`+regexp.QuoteMeta(a.variant)+`\b`))
			p.latinTg = append(p.latinTg, tg)
		} else {
			p.jpVars = append(p.jpVars, a.variant)
			p.jpTg = append(p.jpTg, tg)
		}
	}
	// Single name table (names.go) shared by template, prompt hints and
	// checker: TL/EN/JA display names always bind to their canonical item.
	for _, tv := range tableVariants() {
		if _, ok := p.byName[tv.target]; !ok {
			continue
		}
		if tv.latin {
			p.latinRes = append(p.latinRes,
				regexp.MustCompile(`\b`+regexp.QuoteMeta(strings.ToLower(tv.variant))+`\b`))
			p.latinTg = append(p.latinTg, []string{tv.target})
		} else {
			p.jpVars = append(p.jpVars, tv.variant)
			p.jpTg = append(p.jpTg, []string{tv.target})
		}
	}
	for _, k := range fxKeywordsLatin {
		p.fxRes = append(p.fxRes, regexp.MustCompile(`\b`+regexp.QuoteMeta(k)+`\b`))
	}
	return p, nil
}

// matchCommodities returns the sorted canonical commodity names referenced
// by ctx (matched against canonical names, parenthetical-stripped names and
// the Filipino/Japanese alias table).
func (p *pools) matchCommodities(ctx string) []string {
	set := map[string]bool{}
	lowered := strings.ToLower(ctx)
	for _, c := range p.comms {
		if strings.Contains(lowered, strings.ToLower(c.name)) {
			set[c.name] = true
		}
	}
	for i, re := range p.canonStrippedRes {
		if re.MatchString(lowered) {
			for _, t := range p.canonStrippedTg[i] {
				set[t] = true
			}
		}
	}
	for i, re := range p.latinRes {
		if re.MatchString(lowered) {
			for _, t := range p.latinTg[i] {
				set[t] = true
			}
		}
	}
	for i, v := range p.jpVars {
		if strings.Contains(ctx, v) {
			for _, t := range p.jpTg[i] {
				set[t] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// isFXContext reports whether ctx reads as being about the USD/PHP rate.
func (p *pools) isFXContext(ctx string) bool {
	lowered := strings.ToLower(ctx)
	for _, re := range p.fxRes {
		if re.MatchString(lowered) {
			return true
		}
	}
	for _, k := range fxKeywordsJP {
		if strings.Contains(ctx, k) {
			return true
		}
	}
	return false
}

// hasNCRToken reports whether the segment names an NCR-wide basis.
func hasNCRToken(ctx string) bool {
	lowered := strings.ToLower(ctx)
	for _, t := range ncrTokensLatin {
		if t == "ncr" {
			// Case-sensitive NCR to avoid matching "ncr" inside
			// unrelated lowercase words; "NCR" as an acronym is
			// always uppercase in practice.
			if strings.Contains(ctx, "NCR") {
				return true
			}
			continue
		}
		if strings.Contains(lowered, t) {
			return true
		}
	}
	for _, t := range ncrTokensJP {
		if strings.Contains(ctx, t) {
			return true
		}
	}
	return false
}

// marketHits finds market-name occurrences in the segment, in segment
// coordinates (the segment is already normalized like the line).
func marketHits(seg string) []marketHit {
	var hits []marketHit
	lowered := strings.ToLower(seg)
	for _, mv := range marketVariants {
		if mv.latin {
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(mv.variant) + `\b`)
			for _, idx := range re.FindAllStringIndex(lowered, -1) {
				hits = append(hits, marketHit{short: mv.short, end: idx[1]})
			}
		} else {
			for start := 0; ; {
				j := strings.Index(seg[start:], mv.variant)
				if j < 0 {
					break
				}
				hits = append(hits, marketHit{short: mv.short, end: start + j + len(mv.variant)})
				start += j + len(mv.variant)
			}
		}
	}
	return hits
}

// windowHas reports whether the metric window holds 7-day words, and
// whether it holds usual/30-day words.
func windowHas(window string) (seven, usual bool) {
	lowered := strings.ToLower(window)
	for _, w := range sevenDayWordsLatin {
		if strings.Contains(lowered, w) {
			seven = true
			break
		}
	}
	if !seven {
		for _, w := range sevenDayWordsJP {
			if strings.Contains(window, w) {
				seven = true
				break
			}
		}
	}
	for _, w := range usualWordsLatin {
		if strings.Contains(lowered, w) {
			usual = true
			break
		}
	}
	if !usual {
		for _, w := range usualWordsJP {
			if strings.Contains(window, w) {
				usual = true
				break
			}
		}
	}
	return seven, usual
}

// matchPctSigned checks a percentage mention with sign handling: a written
// "-x%" must match a field equal to -x; an unsigned "x%" may match +x or
// -x. Drop/rise words in the context are deliberately NOT consulted beyond
// the dash. It reports whether the match came from a direct leaf (ok) as
// opposed to a derived value.
func matchPctSigned(written float64, d int, direct, derived []float64) (isDirect, ok bool) {
	if written < 0 {
		if anyApprox(written, d, direct) {
			return true, true
		}
		if anyApprox(written, d, derived) {
			return false, true
		}
		return false, false
	}
	if anyApprox(written, d, direct) || anyApprox(-written, d, direct) {
		return true, true
	}
	if anyApprox(written, d, derived) || anyApprox(-written, d, derived) {
		return false, true
	}
	return false, false
}

// boundPools gathers the kind-aware candidates for a context naming the
// given commodities: each commodity's own object values (price, medians,
// market prices, cheapest) plus derived within-commodity gaps.
func (p *pools) boundPools(names []string) (mDirect, mDeriv, pDirect, pDeriv, trend, med []float64) {
	for _, n := range names {
		c := p.byName[n]
		if c == nil {
			continue
		}
		mDirect = append(mDirect, c.money...)
		mDeriv = append(mDeriv, c.derivMoney...)
		pDirect = append(pDirect, c.pct...)
		pDeriv = append(pDeriv, c.derivPct...)
		trend = append(trend, c.trend...)
		med = append(med, c.medians...)
	}
	return mDirect, mDeriv, pDirect, pDeriv, trend, med
}

// Check verifies every peso amount / percentage in adviceText against the
// v2 numbers in signalsData: kind-aware, commodity-bound, and additionally
// market-bound (peso next to a market name), basis-bound (no NCR numbers)
// and metric-bound (7-day vs usual/30-day percentages). When a
// sentence/segment names no commodity, the commodities named earlier in
// the same bullet line (bullet = line starting with "•" or "- ", else the
// line) are inherited, and market binding applies to these inherited
// contexts too (incl. the JA order "<マーケット>が最安 ₱X"). Mentions on
// truly commodity-less lines fall back to the global kind-aware set and
// are counted as unbound (not a failure).
func Check(signalsData []byte, adviceText string) (*Report, error) {
	p, err := buildPools(signalsData)
	if err != nil {
		return nil, err
	}
	mentions := Extract(adviceText)
	rep := &Report{Status: StatusPass, Flagged: []Finding{}, PerSection: map[string]SectionStats{}, Lint: LintReport{Flags: []LintFlag{}}}
	allowedSections := map[string]bool{"Taglish": true, "English": true, "日本語": true}
	badSection := 0

	// Index mentions by line for the market/metric window logic.
	byLine := map[int][]int{}
	for i, m := range mentions {
		byLine[m.Line] = append(byLine[m.Line], i)
	}

	// marketBound[i] is set when mention i is the peso amount nearest to
	// (just after, within ~20 chars of) a market name in its sentence,
	// with the required market short name. Offsets are in normalized
	// line coordinates.
	marketBound := map[int]string{}
	for _, idxs := range byLine {
		// Group by segment within the line.
		segGroups := map[int][]int{}
		for _, i := range idxs {
			segGroups[mentions[i].segLo] = append(segGroups[mentions[i].segLo], i)
		}
		for _, idxs := range segGroups {
			lineText := mentions[idxs[0]].lineText
			segLo, segHi := mentions[idxs[0]].segLo, mentions[idxs[0]].segHi
			seg := ""
			if segLo >= 0 && segHi <= len(lineText) && segLo < segHi {
				seg = lineText[segLo:segHi]
			}
			for _, h := range marketHits(seg) {
				marketEnd := segLo + h.end
				best := -1
				bestGap := -1
				for _, i := range idxs {
					if mentions[i].Kind != KindPeso {
						continue
					}
					gap := mentions[i].off - marketEnd
					if gap < 0 || gap > 20 {
						continue
					}
					if best < 0 || gap < bestGap {
						best, bestGap = i, gap
					}
				}
				if best >= 0 {
					if _, taken := marketBound[best]; !taken {
						marketBound[best] = h.short
					}
				}
			}
		}
	}

	flag := func(m Mention, reason string, names []string) {
		st := rep.PerSection[m.Section]
		st.Total++
		st.Flagged++
		rep.Total++
		rep.Flagged = append(rep.Flagged, Finding{
			Section: m.Section, Line: m.Line,
			Snippet: m.Snippet, Value: m.Value, Kind: m.Kind,
			Reason: reason, Commodities: names,
		})
		rep.PerSection[m.Section] = st
	}

	// effNames[i] is the commodity binding for mention i: the direct
	// segment match, or — when the segment names no commodity — the
	// commodities named earlier in the same bullet line (bullet = line
	// starting with "•" or "- ", else the line). Only truly
	// commodity-less lines stay unbound (empty). The scope is the line
	// prefix before the mention's segment, so a "; cheapest at <Market>:
	// ₱X" clause (or JA "。<マーケット>が最安 ₱X") inherits the item
	// from the first clause of the same bullet line.
	effNames := make([][]string, len(mentions))
	for _, idxs := range byLine {
		ordered := append([]int(nil), idxs...)
		sort.Slice(ordered, func(a, b int) bool {
			return mentions[ordered[a]].off < mentions[ordered[b]].off
		})
		for _, i := range ordered {
			direct := p.matchCommodities(mentions[i].Context)
			if len(direct) > 0 {
				effNames[i] = direct
				continue
			}
			lineText := mentions[i].lineText
			segLo := mentions[i].segLo
			prefix := ""
			if segLo > 0 && segLo <= len(lineText) {
				prefix = lineText[:segLo]
			}
			effNames[i] = p.matchCommodities(prefix)
		}
	}

	for i, m := range mentions {
		if !allowedSections[m.Section] {
			badSection++
		}
		names := effNames[i]
		fxCtx := p.isFXContext(m.Context)
		nc := append([]string(nil), names...)
		if nc == nil {
			nc = []string{}
		}

		// 1. Basis check: NCR-naming sentences must not carry numbers.
		if hasNCRToken(m.Context) {
			flag(m, ReasonBasisMismatch, nc)
			continue
		}

		// 2. Market check: peso amount just after a market name must
		// equal that market's price for a commodity in context.
		if m.Kind == KindPeso && len(names) > 0 {
			if short, ok := marketBound[i]; ok {
				var cands []float64
				for _, n := range names {
					if c := p.byName[n]; c != nil {
						if v, ok := c.market[short]; ok {
							cands = append(cands, v)
						}
					}
				}
				if !Matches(m.Value, m.Decimals, cands) {
					flag(m, ReasonMarketMismatch, nc)
					continue
				}
				st := rep.PerSection[m.Section]
				st.Total++
				st.OK++
				rep.Total++
				rep.OK++
				rep.PerSection[m.Section] = st
				continue
			}
		}

		// 3. Metric check (percentages with window keywords).
		if m.Kind == KindPercent {
			// Window: words around the mention within the same
			// bullet/sentence — between the previous number and
			// the next number (or segment bounds). Both sides
			// matter because the v3 template puts the metric
			// words after the number ("14.3% below usual",
			// "down 40.9% vs last week", "bumaba ng 40.9% mula
			// noong nakaraang linggo"). The after-side stops at
			// the next comma (","/"、"/"，"") so a following
			// clause's keywords (e.g. JA "、先週比" belonging to
			// the 7-day number) are not attributed to the
			// 30-day number.
			window := ""
			prevEnd := m.segLo
			nextStart := m.segHi
			for _, j := range byLine[m.Line] {
				other := mentions[j]
				if other.segLo != m.segLo {
					continue
				}
				if other.off < m.off && other.end > prevEnd {
					prevEnd = other.end
				}
				if other.off > m.off && other.off < nextStart {
					nextStart = other.off
				}
			}
			afterEnd := nextStart
			for _, c := range []string{",", "、", "，"} {
				if idx := strings.Index(m.lineText[m.end:afterEnd], c); idx >= 0 {
					if cand := m.end + idx; cand < afterEnd {
						afterEnd = cand
					}
				}
			}
			if prevEnd <= m.off && m.end <= afterEnd && prevEnd >= m.segLo && afterEnd <= m.segHi && m.off <= len(m.lineText) && m.end <= len(m.lineText) {
				window = m.lineText[prevEnd:m.off] + " " + m.lineText[m.end:afterEnd]
			}
			seven, usual := windowHas(window)
			if seven || (usual && !seven) {
				var cands []float64
				if len(names) > 0 {
					_, _, _, _, tr, md := p.boundPools(names)
					if seven {
						cands = tr
					} else {
						cands = md
					}
				} else {
					if seven {
						cands = p.globalTrend
					} else {
						cands = p.globalMedianPct
					}
				}
				if _, ok := matchPctSigned(m.Value, m.Decimals, cands, nil); !ok {
					flag(m, ReasonMetricMismatch, nc)
					continue
				}
				st := rep.PerSection[m.Section]
				st.Total++
				if len(names) == 0 {
					st.Unbound++
					rep.Unbound++
				} else {
					st.OK++
					rep.OK++
				}
				rep.Total++
				rep.PerSection[m.Section] = st
				continue
			}
		}

		// 4. Normal kind-aware + commodity-bound matching.
		if len(names) == 0 {
			mD := p.globalMoneyDirect
			if fxCtx {
				mD = append(append([]float64{}, mD...), p.fxRates...)
			}
			switch m.Kind {
			case KindPeso:
				switch {
				case Matches(m.Value, m.Decimals, mD):
					st := rep.PerSection[m.Section]
					st.Total++
					st.Unbound++
					rep.Total++
					rep.Unbound++
					rep.PerSection[m.Section] = st
				case Matches(m.Value, m.Decimals, p.globalMoneyDeriv):
					st := rep.PerSection[m.Section]
					st.Total++
					st.Unbound++
					rep.Total++
					rep.Unbound++
					rep.PerSection[m.Section] = st
				default:
					flag(m, ReasonNoMatch, []string{})
				}
			default: // percent
				if _, ok := matchPctSigned(m.Value, m.Decimals, p.globalPctDirect, p.globalPctDeriv); ok {
					st := rep.PerSection[m.Section]
					st.Total++
					st.Unbound++
					rep.Total++
					rep.Unbound++
					rep.PerSection[m.Section] = st
				} else {
					flag(m, ReasonNoMatch, []string{})
				}
			}
			continue
		}
		mD, mDer, pD, pDer, _, _ := p.boundPools(names)
		if fxCtx {
			mD = append(mD, p.fxRates...)
		}
		switch m.Kind {
		case KindPeso:
			switch {
			case Matches(m.Value, m.Decimals, mD):
				st := rep.PerSection[m.Section]
				st.Total++
				st.OK++
				rep.Total++
				rep.OK++
				rep.PerSection[m.Section] = st
			case Matches(m.Value, m.Decimals, mDer):
				st := rep.PerSection[m.Section]
				st.Total++
				st.Derived++
				rep.Total++
				rep.Derived++
				rep.PerSection[m.Section] = st
			default:
				flag(m, ReasonNotBound, nc)
			}
		default: // percent
			if isDirect, ok := matchPctSigned(m.Value, m.Decimals, pD, pDer); ok {
				st := rep.PerSection[m.Section]
				st.Total++
				if isDirect {
					st.OK++
					rep.OK++
				} else {
					st.Derived++
					rep.Derived++
				}
				rep.Total++
				rep.PerSection[m.Section] = st
			} else {
				flag(m, ReasonNotBound, nc)
			}
		}
	}
	if len(rep.Flagged) > 0 {
		rep.Status = StatusFail
	}
	if badSection > 0 {
		rep.Warning = fmt.Sprintf("%d mention(s) with empty/unknown section (before first header or under unknown header)", badSection)
	}
	// Stable output: sort findings by line.
	sort.Slice(rep.Flagged, func(i, j int) bool {
		if rep.Flagged[i].Line != rep.Flagged[j].Line {
			return rep.Flagged[i].Line < rep.Flagged[j].Line
		}
		return rep.Flagged[i].Snippet < rep.Flagged[j].Snippet
	})
	return rep, nil
}
