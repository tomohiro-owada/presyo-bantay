package fbregion

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Check statuses and finding reasons.
const (
	StatusPass = "pass"
	StatusFail = "fail"

	ReasonNotBound       = "not bound to item in context"
	ReasonNoMatch        = "no match in signals"
	ReasonMarketMismatch = "market mismatch"
	ReasonWrongPrice     = "wrong price"
	ReasonWrongPct       = "wrong pct"
	ReasonDollar         = "dollar sign"
	ReasonNestedParens   = "nested parentheses"
)

// Finding is one flagged number.
type Finding struct {
	Line        int      `json:"line"`
	Snippet     string   `json:"snippet"`
	Value       float64  `json:"value"`
	Kind        string   `json:"kind"`
	Reason      string   `json:"reason"`
	Commodities []string `json:"commodities"`
}

// LintFlag is one lint finding.
type LintFlag struct {
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
	Reason  string `json:"reason"`
}

// Report is the <region>.check.json document.
type Report struct {
	Status  string     `json:"status"`
	Total   int        `json:"total"`
	OK      int        `json:"ok"`
	Flagged []Finding  `json:"flagged"`
	Lint    []LintFlag `json:"lint"`
}

// amountRe matches one "₱<num><unit?> (<sign><int>%)" or
// "₱<num><unit?>（<sign><int>%）" group: name, price, pct.
var amountRe = regexp.MustCompile(`₱([0-9][0-9,]*(?:\.[0-9]+)?)(/kg|/piraso|/pc)?\s*[\(（]\s*([−\-+]?)\s*([0-9]+)\s*[%％]\s*[\)）]`)

// dealLineRe matches a TL bullet: "• <name> ₱... (...) · <short>".
var dealLineRe = regexp.MustCompile(`^•\s+(.+?)\s+₱[0-9][0-9,]*(?:\.[0-9]+)?(?:/kg|/piraso|/pc)?\s*[\(（]\s*[−\-+]?\s*[0-9]+\s*[%％]\s*[\)）]\s*·\s*(.+?)\s*$`)

// aliasVariant maps one name variant to canonical commodity names.
type aliasVariant struct {
	variant string
	latin   bool
	targets []string
}

// buildAliases builds match variants from the signals' canonical names:
// canonical, TL/EN/JA regional display names, advice table names and
// stripped forms.
func buildAliases(names []string) (latin []aliasVariant, jp []aliasVariant) {
	seen := map[string]bool{}
	add := func(v string, latinQ bool, t string) {
		key := v + "\x00" + t
		if v == "" || seen[key] {
			return
		}
		seen[key] = true
		if latinQ {
			latin = append(latin, aliasVariant{v, true, []string{t}})
		} else {
			jp = append(jp, aliasVariant{v, false, []string{t}})
		}
	}
	for _, n := range names {
		add(n, true, n)
		add(DisplayTL(n), true, n)
		add(DisplayEN(n), true, n)
		add(advice.DisplayTL(n), true, n)
		add(advice.DisplayEN(n), true, n)
		add(advice.DisplayJA(n), false, n)
		add(DisplayJA(n), false, n)
		stripped := advice.DisplayEN(n)
		if stripped != n {
			add(stripped, true, n)
		}
		if i := strings.Index(n, " ("); i >= 0 {
			add(n[:i], true, n)
		}
	}
	return latin, jp
}

// matchNames returns canonical names referenced by s.
func matchNames(s string, latin []aliasVariant, jp []aliasVariant) []string {
	set := map[string]bool{}
	lowered := strings.ToLower(s)
	for _, a := range latin {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(strings.ToLower(a.variant)) + `\b`)
		if re.MatchString(lowered) {
			for _, t := range a.targets {
				set[t] = true
			}
		}
	}
	for _, a := range jp {
		if strings.Contains(s, a.variant) {
			for _, t := range a.targets {
				set[t] = true
			}
		}
	}
	var out []string
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type numExpect struct {
	price float64
	pct   int
}

// approxPrice reports whether written (with d decimals) matches expected.
func approxPrice(written float64, d int, expected float64) bool {
	tol := 0.5 / math.Pow(10, float64(d))
	if math.Abs(written-expected) <= tol+1e-9 {
		return true
	}
	scale := math.Pow(10, float64(d))
	return math.Abs(math.Round(expected*scale)/scale-written) <= 1e-9
}

// CheckPost validates the final regional post text against region signals:
// every ₱ amount and % binds to the item on its line/segment (TL per line;
// EN/JA comma-separated segments each naming one item), deal lines carry
// the deal's own "· <market_short>", and integer percents match pct_rounded
// exactly. Lint: no "$", no jargon, no Latin in the JA block.
func CheckPost(sig *signals.RegionSignals, post string) *Report {
	rep := &Report{Status: StatusPass, Flagged: []Finding{}, Lint: []LintFlag{}}

	dealByName := map[string]signals.RegionDeal{}
	pricierByName := map[string]signals.RegionPricier{}
	var names []string
	for _, d := range sig.Deals {
		dealByName[d.Name] = d
		names = append(names, d.Name)
	}
	for _, p := range sig.Pricier {
		pricierByName[p.Name] = p
		names = append(names, p.Name)
	}
	latin, jp := buildAliases(names)

	lines := strings.Split(post, "\n")
	// Block index by separator lines: 0 TL, 1 EN, 2 JA, 3 footer.
	block := 0
	for i, ln := range lines {
		if strings.TrimSpace(ln) == Separator {
			block++
			continue
		}
		lnNo := i + 1
		snip := ln
		if len(snip) > 160 {
			snip = snip[:160]
		}
		// Lint: dollar sign anywhere.
		if strings.Contains(ln, "$") {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: ReasonDollar})
		}
		// Lint: nested/doubled parentheses (e.g. "(Grace (Pateros))" or
		// "Deals in North (CAMANAVA) (prices of …)"). Headings must use
		// the em dash for the date part, never parens.
		if HasNestedParens(ln) {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: ReasonNestedParens})
		} else if isHeadingLine(ln) && HasHeadingDoubleParens(ln) {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: ReasonNestedParens})
		} else if isHeadingLine(ln) && isOldHeadingFormat(ln) {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: ReasonNestedParens})
		}
		// Lint: jargon anywhere.
		lowered := strings.ToLower(ln)
		for _, tok := range []string{"ncr", "metro manila", "kamaynilaan", "focus_market", "nearby_median", "median_30d", "pct_vs", "las_pinas", "basis", "首都圏", "マニラ首都圏"} {
			if tok == "ncr" {
				if strings.Contains(ln, "NCR") {
					rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "jargon: NCR"})
				}
				continue
			}
			hay := ln
			if tok == strings.ToLower(tok) && isLatinTok(tok) {
				hay = lowered
			}
			if strings.Contains(hay, tok) {
				rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "jargon: " + tok})
			}
		}
		// Lint: Latin in the JA block (block 2), except none allowed at
		// all (item names, headers and footer-adjacent lines are all
		// non-Latin by construction).
		if block == 2 {
			for _, run := range latinRunRe.FindAllString(ln, -1) {
				rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "latin in Japanese block: " + run})
			}
		}
		// Lint: markdown.
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") || strings.Contains(ln, "**") || strings.Contains(ln, "|") {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "markdown"})
		}
	}

	// Number binding per block.
	flag := func(lineNo int, snip string, v float64, kind, reason string, names []string) {
		if names == nil {
			names = []string{}
		}
		rep.Flagged = append(rep.Flagged, Finding{
			Line: lineNo, Snippet: snip, Value: v, Kind: kind,
			Reason: reason, Commodities: names,
		})
		rep.Total++
	}
	markOK := func() { rep.Total++; rep.OK++ }

	blockOf := func(idx int) int {
		b := 0
		for i := 0; i < idx && i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == Separator {
				b++
			}
		}
		return b
	}

	for i, ln := range lines {
		lnNo := i + 1
		if strings.TrimSpace(ln) == Separator {
			continue
		}
		b := blockOf(i)
		if b > 2 {
			continue // footer carries no bound numbers
		}
		norm := strings.ReplaceAll(ln, "－", "-")
		// TL block: whole bullet line is the context.
		if b == 0 {
			if !strings.Contains(ln, "₱") {
				continue
			}
			ms := amountRe.FindAllStringSubmatch(norm, -1)
			if len(ms) == 0 {
				// ₱ present but unparseable: flag as unbound.
				flag(lnNo, strings.TrimSpace(ln), 0, advice.KindPeso, ReasonNoMatch, nil)
				continue
			}
			names := matchNames(norm, latin, jp)
			// Section-aware binding: a bullet matching dealLineRe
			// (ends with "· <market>") is a deal line, otherwise a
			// pricier line. Check the entry of that kind first so a
			// name/price collision binds to the right entry.
			isDealLine := dealLineRe.MatchString(ln)
			for _, m := range ms {
				price, d := parseNum(m[1])
				sign, pctStr := m[3], m[4]
				pct, _ := strconv.Atoi(pctStr)
				if sign == "-" || sign == "−" {
					pct = -pct
				} else if sign == "+" {
					pct = +pct
				}
				snip := strings.TrimSpace(m[0])
				if len(names) == 0 {
					flag(lnNo, snip, price, advice.KindPeso, ReasonNoMatch, nil)
					continue
				}
				kindMatched := false
				if isDealLine {
					for _, n := range names {
						if dEntry, ok := dealByName[n]; ok && approxPrice(price, d, dEntry.Price) {
							if pct != dEntry.PctRounded {
								flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
							} else if mm := dealLineRe.FindStringSubmatch(ln); mm == nil || strings.TrimSpace(mm[2]) != dEntry.MarketShort {
								flag(lnNo, snip, price, advice.KindPeso, ReasonMarketMismatch, []string{n})
							} else {
								markOK()
							}
							kindMatched = true
							break
						}
					}
				} else {
					for _, n := range names {
						if pEntry, ok := pricierByName[n]; ok && approxPrice(price, d, pEntry.PriceMedian) {
							if pct != pEntry.PctRounded {
								flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
							} else {
								markOK()
							}
							kindMatched = true
							break
						}
					}
				}
				if kindMatched {
					continue
				}
				// Fall back to the generic behaviour (deal first then
				// pricier) when this kind has no price match.
				matched := false
				for _, n := range names {
					if dEntry, ok := dealByName[n]; ok && approxPrice(price, d, dEntry.Price) {
						// Deal line: pct must equal pct_rounded exactly,
						// market part must be the deal's short.
						if pct != dEntry.PctRounded {
							flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
						} else if mm := dealLineRe.FindStringSubmatch(ln); mm == nil || strings.TrimSpace(mm[2]) != dEntry.MarketShort {
							flag(lnNo, snip, price, advice.KindPeso, ReasonMarketMismatch, []string{n})
						} else {
							markOK()
						}
						matched = true
						break
					}
					if pEntry, ok := pricierByName[n]; ok && approxPrice(price, d, pEntry.PriceMedian) {
						if pct != pEntry.PctRounded {
							flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
						} else {
							markOK()
						}
						matched = true
						break
					}
				}
				if !matched {
					// Named item(s) but neither price fits.
					priceOK := false
					for _, n := range names {
						if dEntry, ok := dealByName[n]; ok && approxPrice(price, d, dEntry.Price) {
							priceOK = true
						}
						if pEntry, ok := pricierByName[n]; ok && approxPrice(price, d, pEntry.PriceMedian) {
							priceOK = true
						}
					}
					if !priceOK {
						flag(lnNo, snip, price, advice.KindPeso, ReasonWrongPrice, names)
					} else {
						flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongPct, names)
					}
				}
			}
			continue
		}
		// EN/JA blocks: split ✅/⚠️ lines into comma-separated segments,
		// each naming one item.
		if !strings.Contains(ln, "₱") {
			continue
		}
		trimmed := strings.TrimSpace(ln)
		if !(strings.HasPrefix(trimmed, "✅") || strings.HasPrefix(trimmed, "⚠️")) {
			// Numbers outside the deal/pricier lines: must still bind
			// to some item in signals.
			ms := amountRe.FindAllStringSubmatch(norm, -1)
			for _, m := range ms {
				price, d := parseNum(m[1])
				found := false
				for _, de := range sig.Deals {
					if approxPrice(price, d, de.Price) {
						found = true
					}
				}
				for _, pe := range sig.Pricier {
					if approxPrice(price, d, pe.PriceMedian) {
						found = true
					}
				}
				if found {
					markOK()
				} else {
					flag(lnNo, strings.TrimSpace(m[0]), price, advice.KindPeso, ReasonNoMatch, nil)
				}
			}
			continue
		}
		body := strings.TrimPrefix(trimmed, "✅")
		body = strings.TrimPrefix(body, "⚠️")
		body = strings.TrimSpace(body)
		// Strip the "Pricier:" / "高め：" label.
		if idx := strings.Index(body, ":"); idx >= 0 && (strings.HasPrefix(body, "Pricier") || strings.HasPrefix(body, "高め")) {
			body = strings.TrimSpace(body[idx+1:])
		}
		if idx := strings.Index(body, "："); idx >= 0 && strings.HasPrefix(body, "高め") {
			body = strings.TrimSpace(body[idx+len("："):])
		}
		// Section-aware binding: ✅ lines are deal lines, ⚠️ lines are
		// pricier lines. Check the entry of that kind first so a
		// name/price collision binds to the right entry.
		lineIsDeal := strings.HasPrefix(trimmed, "✅")
		segs := splitSegments(body)
		for _, sg := range segs {
			sg = strings.TrimSpace(sg)
			if sg == "" {
				continue
			}
			m := amountRe.FindStringSubmatch(sg)
			if m == nil {
				continue
			}
			price, d := parseNum(m[1])
			sign := m[3]
			pct, _ := strconv.Atoi(m[4])
			if sign == "-" || sign == "−" {
				pct = -pct
			} else if sign == "+" {
				pct = +pct
			}
			snames := matchNames(sg, latin, jp)
			if len(snames) == 0 {
				flag(lnNo, sg, price, advice.KindPeso, ReasonNoMatch, nil)
				continue
			}
			kindBound := false
			if lineIsDeal {
				for _, n := range snames {
					if de, isD := dealByName[n]; isD && approxPrice(price, d, de.Price) {
						if pct != de.PctRounded {
							flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
						} else {
							markOK()
						}
						kindBound = true
						break
					}
				}
			} else {
				for _, n := range snames {
					if pe, isP := pricierByName[n]; isP && approxPrice(price, d, pe.PriceMedian) {
						if pct != pe.PctRounded {
							flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
						} else {
							markOK()
						}
						kindBound = true
						break
					}
				}
			}
			if kindBound {
				continue
			}
			// Fall back to the generic behaviour (deal first then
			// pricier) when this kind has no price match.
			bound := false
			for _, n := range snames {
				if de, isD := dealByName[n]; isD && approxPrice(price, d, de.Price) {
					if pct != de.PctRounded {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
					} else {
						markOK()
					}
					bound = true
					break
				}
				if pe, isP := pricierByName[n]; isP && approxPrice(price, d, pe.PriceMedian) {
					if pct != pe.PctRounded {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
					} else {
						markOK()
					}
					bound = true
					break
				}
			}
			if !bound {
				flag(lnNo, sg, price, advice.KindPeso, ReasonWrongPrice, snames)
			}
		}
	}

	if len(rep.Flagged) > 0 || len(rep.Lint) > 0 {
		rep.Status = StatusFail
	}
	sort.Slice(rep.Flagged, func(i, j int) bool {
		if rep.Flagged[i].Line != rep.Flagged[j].Line {
			return rep.Flagged[i].Line < rep.Flagged[j].Line
		}
		return rep.Flagged[i].Snippet < rep.Flagged[j].Snippet
	})
	return rep
}

// HasNestedParens reports whether s opens a '(' or '（' inside another
// unclosed one of the same kind.
func HasNestedParens(s string) bool {
	depthLatin, depthJA := 0, 0
	for _, r := range s {
		switch r {
		case '(':
			depthLatin++
			if depthLatin > 1 {
				return true
			}
		case ')':
			if depthLatin > 0 {
				depthLatin--
			}
		case '（':
			depthJA++
			if depthJA > 1 {
				return true
			}
		case '）':
			if depthJA > 0 {
				depthJA--
			}
		}
	}
	return false
}

// isHeadingLine reports whether a post line is a TL/EN/JA heading.
func isHeadingLine(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "🛒") || strings.HasPrefix(t, "🇬🇧") || strings.HasPrefix(t, "🇯🇵")
}

// HasHeadingDoubleParens reports whether a heading line carries more than
// one parenthetical group of the same kind, or a close-then-open adjacency
// like ") (" / "）（" (e.g. the old "Deals in North (CAMANAVA) (prices of …)"
// or "…のお買い得（…の価格）" headings).
func HasHeadingDoubleParens(s string) bool {
	if strings.Count(s, "(") > 1 || strings.Count(s, "（") > 1 {
		return true
	}
	if closeOpenRe.MatchString(s) {
		return true
	}
	return false
}

// isOldHeadingFormat reports whether a heading line uses the old
// parenthesised date ("(prices of …)" / "お買い得（…の価格）") instead of
// the em dash.
func isOldHeadingFormat(s string) bool {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "🇬🇧") && strings.Contains(s, "(prices of") {
		return true
	}
	if strings.HasPrefix(t, "🇯🇵") && strings.Contains(s, "お買い得（") {
		return true
	}
	return false
}

var closeOpenRe = regexp.MustCompile(`\)\s*\(|）\s*（|\)\s*（|）\s*\(`)

var latinRunRe = regexp.MustCompile(`[A-Za-z]+`)

func isLatinTok(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' && r != '-' && r != ' ' {
			return false
		}
	}
	return true
}

// pricierLineHint reports whether a TL bullet names only pricier items.
// (Kept simple: the caller treats bullets by price binding; this helper
// documents the pricier-vs-deal distinction for future lint use.)
func pricierLineHint(ln string, deals map[string]signals.RegionDeal, names []string) bool {
	onlyPricier := true
	for _, n := range names {
		if _, ok := deals[n]; ok {
			onlyPricier = false
		}
	}
	return onlyPricier && len(names) > 0
}

// parseNum parses a formatted number with optional thousands separators.
func parseNum(raw string) (float64, int) {
	clean := strings.ReplaceAll(raw, ",", "")
	d := 0
	if i := strings.Index(clean, "."); i >= 0 {
		d = len(clean) - i - 1
	}
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, 0
	}
	return v, d
}

// splitSegments splits EN/JA deal lines on "," / "、" / "，" but not on the
// decimal "." (digits on both sides never split since we only split on
// commas).
func splitSegments(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '、' || r == '，'
	})
}
