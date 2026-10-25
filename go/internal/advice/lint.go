package advice

import (
	"regexp"
	"strings"
)

// LintFlag is one jargon finding in the advice text.
type LintFlag struct {
	Section string `json:"section"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
	Reason  string `json:"reason"`
}

// LintReport holds the jargon-lint findings (serialized under "lint" in
// check.json).
type LintReport struct {
	Flags []LintFlag `json:"flags"`
}

// jargonTokens are forbidden anywhere in the advice text (case handling
// per token: NCR and friends are matched case-sensitively as written,
// the rest case-insensitively).
var jargonTokens = []struct {
	token string
	lower bool
}{
	{"NCR-wide", false},
	{"NCR", false},
	{"Metro Manila", true},
	{"focus_market", false},
	{"nearby_median", false},
	{"median_30d", false},
	{"pct_vs", false},
	{"las_pinas", true},
	{"basis", true},
}

var latinRunRe = regexp.MustCompile(`[A-Za-z]+`)

// stillFallingPhrases are banned anywhere (v3): the model must not claim a
// trend keeps falling.
var stillFallingPhrases = []string{
	"still falling",
	"still down",
	"tuloy-baba",
}

// nearbyFootnoteTokens are only allowed when uses_nearby_basis is true.
var nearbyFootnoteTokens = []string{
	"近隣市場",
	"kalapit na palengke",
	"nearby markets",
}

// LintAdvice runs the v2 "jargon lint" over the advice text: it flags any
// forbidden identifier/jargon token anywhere, plus any Latin letter run
// inside the 日本語 section other than the allowed "kg" unit, plus v3
// additions (still-falling phrases, markdown in bodies). The nearby-footnote
// check needs signals context; see LintAdviceWithBasis.
func LintAdvice(text string) LintReport {
	return LintAdviceWithBasis(text, true)
}

// LintAdviceWithBasis is LintAdvice plus the v3 nearby-footnote check:
// when usesNearby is false, any mention of the nearby footnote marker
// words (in any section) is flagged.
func LintAdviceWithBasis(text string, usesNearby bool) LintReport {
	rep := LintReport{Flags: []LintFlag{}}
	text = insertHeaderNewlines(text)
	lines := strings.Split(text, "\n")
	section := ""
	inJapanese := false
	for i, raw := range lines {
		if m := headerRe.FindStringSubmatch(raw); m != nil {
			switch strings.ToLower(strings.TrimSpace(m[1])) {
			case "taglish":
				section, inJapanese = "Taglish", false
			case "english":
				section, inJapanese = "English", false
			case "日本語":
				section, inJapanese = "日本語", true
			default:
				section, inJapanese = strings.TrimSpace(m[1]), false
			}
			continue
		}
		lineNo := i + 1
		snip := strings.TrimSpace(raw)
		if len(snip) > 160 {
			snip = snip[:160]
		}
		lowered := strings.ToLower(raw)
		for _, jt := range jargonTokens {
			hay := raw
			needle := jt.token
			if jt.lower {
				hay, needle = lowered, strings.ToLower(jt.token)
			}
			if strings.Contains(hay, needle) {
				rep.Flags = append(rep.Flags, LintFlag{
					Section: section, Line: lineNo,
					Snippet: snip, Reason: "jargon: " + jt.token,
				})
			}
		}
		if inJapanese {
			for _, run := range latinRunRe.FindAllString(raw, -1) {
				lr := strings.ToLower(run)
				if lr == "kg" || run == "DA" {
					continue
				}
				rep.Flags = append(rep.Flags, LintFlag{
					Section: section, Line: lineNo,
					Snippet: snip, Reason: "latin in Japanese section: " + run,
				})
			}
		}
		// v3: still-falling phrases anywhere.
		for _, ph := range stillFallingPhrases {
			if strings.Contains(lowered, ph) {
				rep.Flags = append(rep.Flags, LintFlag{
					Section: section, Line: lineNo,
					Snippet: snip, Reason: "banned phrase: " + ph,
				})
			}
		}
		// v3: markdown in bodies. "• " and "- " bullets are allowed;
		// flag '#'-led lines, '**' bold and '|' tables.
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "#") {
			rep.Flags = append(rep.Flags, LintFlag{
				Section: section, Line: lineNo,
				Snippet: snip, Reason: "markdown header",
			})
		}
		if strings.HasPrefix(trimmed, "|") || strings.Contains(raw, "|") {
			// '|' only appears in tables in this context.
			rep.Flags = append(rep.Flags, LintFlag{
				Section: section, Line: lineNo,
				Snippet: snip, Reason: "markdown table",
			})
		}
		if strings.Contains(raw, "**") {
			rep.Flags = append(rep.Flags, LintFlag{
				Section: section, Line: lineNo,
				Snippet: snip, Reason: "markdown bold",
			})
		}
		// v3: nearby footnote markers require uses_nearby_basis.
		if !usesNearby {
			for _, tok := range nearbyFootnoteTokens {
				if strings.Contains(strings.ToLower(raw), strings.ToLower(tok)) {
					rep.Flags = append(rep.Flags, LintFlag{
						Section: section, Line: lineNo,
						Snippet: snip, Reason: "nearby footnote without nearby basis: " + tok,
					})
					break
				}
			}
		}
	}
	if rep.Flags == nil {
		rep.Flags = []LintFlag{}
	}
	return rep
}
