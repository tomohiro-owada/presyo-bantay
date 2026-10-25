package weekly

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
)

// Check statuses and finding reasons (mirrors fbregion).
const (
	StatusPass = "pass"
	StatusFail = "fail"

	ReasonNotBound       = "not bound to item in context"
	ReasonNoMatch        = "no match in signals"
	ReasonMarketMismatch = "market mismatch"
	ReasonRegionMismatch = "region mismatch"
	ReasonWrongPrice     = "wrong price"
	ReasonWrongPct       = "wrong pct"
	ReasonWrongMetric    = "wrong metric"
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

// Report is the weekly check document.
type Report struct {
	Status  string     `json:"status"`
	Total   int        `json:"total"`
	OK      int        `json:"ok"`
	Flagged []Finding  `json:"flagged"`
	Lint    []LintFlag `json:"lint"`
}

// amountRe matches one "₱<num><unit?> (<sign><int>%)" group.
var amountRe = regexp.MustCompile(`₱([0-9][0-9,]*(?:\.[0-9]+)?)(/kg|/piraso|/pc)?\s*[\(（]\s*([−\-+]?)\s*([0-9]+)\s*[%％]\s*[\)）]`)

// barePctRe matches a bare percent in parens: "(−48%)" / "（−48%）".
var barePctRe = regexp.MustCompile(`[\(（]\s*([−\-+]?)\s*([0-9]+)\s*[%％]\s*[\)）]`)

// dealPctRe matches a JA bare deal pct: "−48%（Alabang）".
var dealPctRe = regexp.MustCompile(`([−\-+])\s*([0-9]+)\s*[%％]\s*[\(（]\s*([^\)）]+?)\s*[\)）]`)

type aliasVariant struct {
	variant string
	latin   bool
	targets []string
}

// buildAliases maps display-name variants to canonical names.
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
		if i := strings.Index(n, " ("); i >= 0 {
			add(n[:i], true, n)
		}
	}
	return latin, jp
}

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

func approxPrice(written float64, d int, expected float64) bool {
	tol := 0.5 / math.Pow(10, float64(d))
	if math.Abs(written-expected) <= tol+1e-9 {
		return true
	}
	scale := math.Pow(10, float64(d))
	return math.Abs(math.Round(expected*scale)/scale-written) <= 1e-9
}

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

// splitSegments splits EN/JA lines on "," / "、" / "，" / ";" / "；".
func splitSegments(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '、' || r == '，' || r == ';' || r == '；'
	})
}

var latinRunRe = regexp.MustCompile(`[A-Za-z]+`)

func isLatinTok(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' && r != '-' && r != ' ' {
			return false
		}
	}
	return true
}

// section classifies a content line within a block.
type section int

const (
	secNone section = iota
	secDrops
	secRises
	secArea
	secMissing
)

// areaKey identifies one area deal (region + commodity).
type areaKey struct {
	region string
	name   string
}

// CheckWeekly validates the final weekly post text against weekly signals:
// every ₱ amount and % binds to the commodity (and market/region for area
// deals) on the same line/segment, with the right sign and the right
// metric (week-over-week vs area-deal). Lint: no "$", no nested parens, no
// internal jargon. "Metro Manila" and "マニラ首都圏" are allowed.
func CheckWeekly(sig *Signals, post string) *Report {
	rep := &Report{Status: StatusPass, Flagged: []Finding{}, Lint: []LintFlag{}}

	metroByName := map[string]MetroEntry{}
	var names []string
	for _, m := range sig.Metro {
		metroByName[m.Name] = m
		names = append(names, m.Name)
	}
	areaByKey := map[areaKey]DealEntry{}
	regionTitle := map[string][3]string{} // id -> tl,en,shortJA
	regionOfMarket := map[string]string{}
	allowedLatin := map[string]bool{"DA": true}
	var allowedJA []string
	allowedJA = append(allowedJA, "DA")
	for _, rd := range sig.DealsByRegion {
		regionTitle[rd.Region] = [3]string{rd.TitleTL, rd.TitleEN, rd.ShortJA}
		for _, d := range rd.Deals {
			areaByKey[areaKey{rd.Region, d.Name}] = d
			names = append(names, d.Name)
			regionOfMarket[d.MarketShort] = rd.Region
			allowedLatin[d.MarketShort] = true
			allowedLatin[strings.ToLower(d.MarketShort)] = true
			allowedJA = append(allowedJA, d.MarketShort)
		}
	}
	// Unique names.
	{
		seen := map[string]bool{}
		var uniq []string
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				uniq = append(uniq, n)
			}
		}
		names = uniq
	}
	latin, jp := buildAliases(names)

	lines := strings.Split(post, "\n")
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
		if strings.Contains(ln, "$") {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: ReasonDollar})
		}
		if fbregion.HasNestedParens(ln) {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: ReasonNestedParens})
		}
		lowered := strings.ToLower(ln)
		for _, tok := range []string{"median_30d", "pct_vs", "focus_market", "las_pinas", "nearby_median", "ncr", "basis"} {
			hay := ln
			if isLatinTok(tok) {
				hay = lowered
			}
			// "basis" appears in legit English prose? No — flag it, but
			// avoid flagging inside other words: require word-ish match
			// for the short token.
			if tok == "basis" {
				re := regexp.MustCompile(`\bbasis\b`)
				if re.MatchString(strings.ToLower(ln)) {
					rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "jargon: " + tok})
				}
				continue
			}
			if strings.Contains(hay, tok) {
				// "NCR" uppercase check for ncr.
				if tok == "ncr" && !strings.Contains(ln, "NCR") && !strings.Contains(lowered, "ncr") {
					continue
				}
				rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "jargon: " + tok})
			}
		}
		if block == 2 {
			// Market shorts (Latin) and "DA" are allowed in JA.
			scrubbed := ln
			for _, tok := range allowedJA {
				scrubbed = strings.ReplaceAll(scrubbed, tok, "")
			}
			for _, run := range latinRunRe.FindAllString(scrubbed, -1) {
				rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "latin in Japanese block: " + run})
			}
		}
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") || strings.Contains(ln, "**") || strings.Contains(ln, "|") {
			rep.Lint = append(rep.Lint, LintFlag{Line: lnNo, Snippet: snip, Reason: "markdown"})
		}
	}

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

	secOf := func(ln string) section {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "📉"):
			return secDrops
		case strings.HasPrefix(t, "📈"):
			return secRises
		case strings.HasPrefix(t, "🏪"):
			return secArea
		case strings.HasPrefix(t, "Walang datos"), strings.HasPrefix(t, "No DA data"), strings.HasPrefix(t, "DAデータなし"):
			return secMissing
		}
		if strings.HasPrefix(t, "• ") {
			return secNone // bullets inherit the current header section
		}
		return secNone
	}

	// Track current section per block as we walk lines in order.
	curSec := [4]section{}
	for i, ln := range lines {
		lnNo := i + 1
		if strings.TrimSpace(ln) == Separator {
			continue
		}
		b := blockOf(i)
		if b > 2 {
			continue
		}
		if s := secOf(ln); s == secDrops || s == secRises || s == secArea || s == secMissing {
			curSec[b] = s
		}
		// Header lines (📉/📈/🏪) may themselves carry numbers (EN/JA
		// single-line sections); bullet lines carry numbers too.
		norm := strings.ReplaceAll(ln, "－", "-")
		if !strings.Contains(ln, "₱") && !strings.Contains(ln, "%") && !strings.Contains(ln, "％") {
			continue // no checkable numbers on this line
		}
		trimmed := strings.TrimSpace(ln)
		isBullet := strings.HasPrefix(trimmed, "• ")
		// Determine the effective section: for TL blocks, drops/rises
		// are bullets under their headers; area bullets under 🏪.
		effSec := curSec[b]
		if b == 0 && isBullet {
			effSec = curSec[b]
			if effSec != secArea && effSec != secDrops && effSec != secRises {
				effSec = secNone
			}
		}
		switch {
		case b == 0 && (effSec == secDrops || effSec == secRises):
			ms := amountRe.FindAllStringSubmatch(norm, -1)
			if len(ms) == 0 && strings.Contains(ln, "₱") {
				flag(lnNo, trimmed, 0, advice.KindPeso, ReasonNoMatch, nil)
				continue
			}
			names := matchNames(norm, latin, jp)
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
				matched := false
				for _, n := range names {
					me, ok := metroByName[n]
					if !ok || !approxPrice(price, d, me.DisplayPrice) {
						continue
					}
					// Area-deal value used in a metro line = wrong metric.
					if isAreaValue(n, price, pct, areaByKey) && pct != me.Pct {
						flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongMetric, []string{n})
						matched = true
						break
					}
					if pct != me.Pct {
						flag(lnNo, snip, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
					} else {
						markOK()
					}
					matched = true
					break
				}
				if !matched {
					flag(lnNo, snip, price, advice.KindPeso, ReasonWrongPrice, names)
				}
			}
		case b == 0 && effSec == secArea:
			// TL area lines carry no ₱ ("Sayote · Alabang (−48%)");
			// any ₱ here is a metro value in the wrong metric.
			if strings.Contains(ln, "₱") {
				for _, m := range amountRe.FindAllStringSubmatch(norm, -1) {
					price, _ := parseNum(m[1])
					flag(lnNo, strings.TrimSpace(m[0]), price, advice.KindPeso, ReasonWrongMetric, matchNames(norm, latin, jp))
				}
				continue
			}
			names := matchNames(norm, latin, jp)
			// Region named on this line.
			lineRegion := ""
			for id, titles := range regionTitle {
				if strings.Contains(ln, titles[0]) {
					lineRegion = id
					break
				}
			}
			for _, sg := range splitSegments(strings.TrimSpace(strings.TrimPrefix(trimmed, "• "))) {
				sg = strings.TrimSpace(sg)
				if sg == "" {
					continue
				}
				bm := barePctRe.FindStringSubmatch(strings.ReplaceAll(sg, "－", "-"))
				if bm == nil {
					continue
				}
				sign := bm[1]
				pct, _ := strconv.Atoi(bm[2])
				if sign == "-" || sign == "−" {
					pct = -pct
				} else if sign == "+" {
					pct = +pct
				}
				snames := matchNames(sg, latin, jp)
				if len(snames) == 0 {
					// The "Region: " prefix segment names no item.
					if strings.Contains(sg, ":") && lineRegion != "" {
						continue
					}
					flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonNoMatch, nil)
					continue
				}
				marketShort := ""
				if idx := strings.Index(sg, "·"); idx >= 0 {
					rest := sg[idx+len("·"):]
					for mk := range regionOfMarket {
						if strings.Contains(rest, mk) {
							marketShort = mk
							break
						}
					}
					if marketShort == "" {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonMarketMismatch, snames)
						continue
					}
				}
				matched := false
				for _, n := range snames {
					de, ok := areaByKey[areaKey{lineRegion, n}]
					if lineRegion == "" || !ok {
						found := false
						for k, v := range areaByKey {
							if k.name == n && v.Pct == pct {
								found = true
								flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonRegionMismatch, []string{n})
								break
							}
						}
						if !found {
							if _, ok := metroByName[n]; ok {
								flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongMetric, []string{n})
							} else {
								flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPrice, snames)
							}
						}
						matched = true
						break
					}
					if marketShort != "" && marketShort != de.MarketShort {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonMarketMismatch, []string{n})
					} else if pct != de.Pct {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
					} else {
						markOK()
					}
					matched = true
					break
				}
				if !matched {
					flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPrice, snames)
				}
			}
			_ = names
		case b == 1 || (b == 2 && strings.Contains(ln, "₱")):
			// EN single-line sections and JA metro lines: split into
			// segments; each segment names one item.
			body := trimmed
			segs := splitSegments(body)
			if len(segs) <= 1 && isBullet {
				segs = []string{body}
			}
			// For TL-style bullets (shouldn't happen here) keep whole.
			for _, sg := range segs {
				sg = strings.TrimSpace(sg)
				if sg == "" {
					continue
				}
				if !strings.Contains(sg, "₱") {
					// Area segment without ₱ ("Chayote at Alabang
					// (−48%)"): verify the bare pct. Anything else
					// without ₱ carries no checkable number.
					checkBareAreaPct(rep, sig, lnNo, sg, b, trimmed, curSec[b], body, &markOK, &flag, latin, jp, metroByName, areaByKey, regionTitle)
					continue
				}
				sm := amountRe.FindStringSubmatch(strings.ReplaceAll(sg, "－", "-"))
				if sm == nil {
					continue
				}
				price, d := parseNum(sm[1])
				sign := sm[3]
				pct, _ := strconv.Atoi(sm[4])
				if sign == "-" || sign == "−" {
					pct = -pct
				} else if sign == "+" {
					pct = +pct
				}
				snames := matchNames(sg, latin, jp)
				if len(snames) == 0 {
					// Maybe an area segment: match region + market.
					flag(lnNo, sg, price, advice.KindPeso, ReasonNoMatch, nil)
					continue
				}
				inArea := b == 1 && strings.Contains(body, "Best deals") ||
					b == 1 && strings.Contains(trimmed, "—") && curSec[b] == secArea ||
					b == 2 && strings.Contains(trimmed, "お買い得") && !strings.Contains(trimmed, "値下がり") && !strings.Contains(trimmed, "値上がり")
				// More robust: if the line is the 🏪 line or follows it.
				if curSec[b] == secArea && (strings.HasPrefix(trimmed, "🏪") || !strings.HasPrefix(trimmed, "📉") && !strings.HasPrefix(trimmed, "📈")) {
					inArea = true
				}
				if strings.HasPrefix(trimmed, "📉") || strings.HasPrefix(trimmed, "📈") {
					inArea = false
				}
				bound := false
				for _, n := range snames {
					if inArea {
						// Area segment: find the region from titles.
						segRegion := ""
						for id, titles := range regionTitle {
							if strings.Contains(sg, titles[0]) || strings.Contains(sg, titles[1]) || strings.Contains(sg, titles[2]) {
								segRegion = id
								break
							}
						}
						de, ok := areaByKey[areaKey{segRegion, n}]
						if segRegion == "" || !ok {
							continue
						}
						if !approxPrice(price, d, de.DisplayPrice) {
							continue
						}
						// Market check: segment must name the market.
						if !strings.Contains(sg, de.MarketShort) {
							flag(lnNo, sg, price, advice.KindPeso, ReasonMarketMismatch, []string{n})
						} else if pct != de.Pct {
							flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
						} else {
							markOK()
						}
						bound = true
						break
					}
					me, ok := metroByName[n]
					if !ok || !approxPrice(price, d, me.DisplayPrice) {
						continue
					}
					if pct != me.Pct {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
					} else {
						markOK()
					}
					bound = true
					break
				}
				if !bound {
					// Distinguish wrong-price vs wrong-metric: if the
					// named item's metro value matches the price, the
					// pct is wrong (or vice versa); if only an area
					// value matches, it's the wrong metric.
					priceOK := false
					for _, n := range snames {
						if me, ok := metroByName[n]; ok && approxPrice(price, d, me.DisplayPrice) {
							priceOK = true
						}
						for k, v := range areaByKey {
							if k.name == n && approxPrice(price, d, v.DisplayPrice) {
								flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongMetric, []string{n})
								bound = true
								break
							}
						}
						if bound {
							break
						}
					}
					if !bound {
						if priceOK {
							flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, snames)
						} else {
							flag(lnNo, sg, price, advice.KindPeso, ReasonWrongPrice, snames)
						}
					}
				}
			}
		case b == 2 && curSec[b] == secArea:
			// JA area line without ₱: "南部 サヨテ −45%（Alabang）" joined by 、/；.
			segs := splitSegments(trimmed)
			for _, sg := range segs {
				sg = strings.TrimSpace(sg)
				if sg == "" || !strings.Contains(sg, "%") && !strings.Contains(sg, "％") {
					continue
				}
				m := dealPctRe.FindStringSubmatch(strings.ReplaceAll(sg, "－", "-"))
				if m == nil {
					continue
				}
				sign := m[1]
				pct, _ := strconv.Atoi(m[2])
				if sign == "-" || sign == "−" {
					pct = -pct
				} else if sign == "+" {
					pct = +pct
				}
				marketShort := strings.TrimSpace(m[3])
				snames := matchNames(sg, latin, jp)
				if len(snames) == 0 {
					flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonNoMatch, nil)
					continue
				}
				segRegion := ""
				for id, titles := range regionTitle {
					if strings.Contains(sg, titles[2]) {
						segRegion = id
						break
					}
				}
				if segRegion == "" {
					for _, part := range splitRegionParts(trimmed) {
						if strings.Contains(part, sg) {
							segRegion = regionInText(part, regionTitle)
							break
						}
					}
				}
				if segRegion == "" {
					segRegion = regionOfMarket[marketShort]
				}
				bound := false
				for _, n := range snames {
					de, ok := areaByKey[areaKey{segRegion, n}]
					if segRegion == "" || !ok {
						continue
					}
					if marketShort != de.MarketShort {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonMarketMismatch, []string{n})
					} else if pct != de.Pct {
						flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
					} else {
						markOK()
					}
					bound = true
					break
				}
				if !bound {
					flag(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPrice, snames)
				}
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

// splitRegionParts splits an area line into per-region parts on ";"/"；".
func splitRegionParts(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == '；'
	})
}

// regionInText returns the region id whose TL/EN/JA title appears in s.
func regionInText(s string, regionTitle map[string][3]string) string {
	for id, titles := range regionTitle {
		if strings.Contains(s, titles[0]) || strings.Contains(s, titles[1]) || strings.Contains(s, titles[2]) {
			return id
		}
	}
	return ""
}
func checkBareAreaPct(rep *Report, sig *Signals, lnNo int, sg string, b int, trimmed string, cur section, body string, markOK *func(), flag *func(int, string, float64, string, string, []string), latin []aliasVariant, jp []aliasVariant, metroByName map[string]MetroEntry, areaByKey map[areaKey]DealEntry, regionTitle map[string][3]string) {
	if cur != secArea && !(b == 1 && strings.Contains(body, "Best deals")) {
		return
	}
	if strings.HasPrefix(trimmed, "📉") || strings.HasPrefix(trimmed, "📈") {
		return
	}
	bm := barePctRe.FindStringSubmatch(strings.ReplaceAll(sg, "－", "-"))
	if bm == nil {
		return
	}
	sign := bm[1]
	pct, _ := strconv.Atoi(bm[2])
	if sign == "-" || sign == "−" {
		pct = -pct
	} else if sign == "+" {
		pct = +pct
	}
	snames := matchNames(sg, latin, jp)
	if len(snames) == 0 {
		(*flag)(lnNo, sg, float64(pct), advice.KindPercent, ReasonNoMatch, nil)
		return
	}
	segRegion := regionInText(sg, regionTitle)
	if segRegion == "" {
		// The segment names no region (comma-split item): find the
		// ";"-part of the full line containing it and take its region.
		for _, part := range splitRegionParts(body) {
			if strings.Contains(part, sg) {
				segRegion = regionInText(part, regionTitle)
				break
			}
		}
	}
	bound := false
	for _, n := range snames {
		de, ok := areaByKey[areaKey{segRegion, n}]
		if segRegion == "" || !ok {
			continue
		}
		if !strings.Contains(sg, de.MarketShort) {
			(*flag)(lnNo, sg, float64(pct), advice.KindPercent, ReasonMarketMismatch, []string{n})
		} else if pct != de.Pct {
			(*flag)(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPct, []string{n})
		} else {
			(*markOK)()
		}
		bound = true
		break
	}
	if !bound {
		better := false
		for _, n := range snames {
			if me, ok := metroByName[n]; ok && me.Pct == pct {
				(*flag)(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongMetric, []string{n})
				better = true
				break
			}
		}
		if !better {
			(*flag)(lnNo, sg, float64(pct), advice.KindPercent, ReasonWrongPrice, snames)
		}
	}
}

func isAreaValue(name string, price float64, pct int, areaByKey map[areaKey]DealEntry) bool {
	for k, v := range areaByKey {
		if k.name == name && v.DisplayPrice == price && v.Pct == pct {
			return true
		}
	}
	return false
}
