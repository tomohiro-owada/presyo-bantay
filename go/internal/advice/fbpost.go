package advice

import (
	"strings"
)

// Combined Facebook post (ADVICE_V3.md section 7): one post carrying all
// three languages. Taglish first, then a "———" separator, English,
// separator, Japanese, separator, and a final shared footer with the site
// link and DA source note. Each language keeps at most FBMaxDeals deal
// lines and FBMaxPricier pricier lines; the per-language closing source
// line is dropped in favour of the single shared footer.
const (
	FBMaxDeals   = 5
	FBMaxPricier = 2
	// FBSeparator is the separator line between the language blocks and
	// the footer.
	FBSeparator  = "———"
	FBLink       = "https://abalol.xyz/presyo-bantay/"
	FBSourceNote = "Source/Pinagmulan/出典: DA (Department of Agriculture) price monitoring"
)

// RenderFBPost builds the combined post deterministically from the chosen
// per-language texts (LLM or template) by trimming/joining only: it never
// invents numbers, items or markets. The only additions are the "———"
// separators, a 📈 section marker on the pricier header when it has none,
// and the shared footer.
func RenderFBPost(texts Texts) string {
	blocks := []string{
		trimFBBlock(texts.TL),
		trimFBBlock(texts.EN),
		trimFBBlock(texts.JA),
	}
	footer := FBLink + "\n" + FBSourceNote
	return strings.Join(blocks, "\n"+FBSeparator+"\n") + "\n" + FBSeparator + "\n" + footer
}

// RenderFBPostFromSignals renders the template texts from signals and
// trims them into the combined post.
func RenderFBPostFromSignals(signalsData []byte) (string, error) {
	texts, err := RenderTemplateJSON(signalsData)
	if err != nil {
		return "", err
	}
	return RenderFBPost(texts), nil
}

// isPricierHeader reports whether a line is a pricier section header in
// any language.
func isPricierHeader(ln string) bool {
	return strings.Contains(ln, "Mas mahal") ||
		strings.Contains(ln, "Pricier than usual") ||
		strings.Contains(ln, "いつもより高め")
}

// isFBClosing reports whether a non-bullet line is a per-language closing
// source line (replaced by the shared footer), rather than content such
// as the empty-deals note.
func isFBClosing(ln string) bool {
	return strings.Contains(ln, "Batay sa DA") ||
		strings.Contains(ln, "Based on DA") ||
		strings.Contains(ln, "農業省")
}

// trimFBBlock keeps a single language text's title, at most FBMaxDeals
// deal bullets, the pricier header (with a 📈 marker) plus at most
// FBMaxPricier pricier bullets, and any pre-header note (the empty-deals
// line). Per-language closing lines are dropped.
func trimFBBlock(text string) string {
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if ln != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	title := lines[0]
	rest := lines[1:]
	headerIdx := -1
	for i, ln := range rest {
		if isPricierHeader(ln) {
			headerIdx = i
			break
		}
	}
	var pre, post []string
	var header string
	if headerIdx < 0 {
		pre = rest
	} else {
		pre = rest[:headerIdx]
		header = rest[headerIdx]
		post = rest[headerIdx+1:]
	}
	var out []string
	out = append(out, title)
	deals := 0
	for _, ln := range pre {
		if strings.HasPrefix(ln, "• ") || strings.HasPrefix(ln, "- ") {
			if deals >= FBMaxDeals {
				continue
			}
			deals++
			out = append(out, ln)
			continue
		}
		if isFBClosing(ln) {
			continue
		}
		out = append(out, ln)
	}
	if headerIdx >= 0 {
		if !strings.Contains(header, "🛒") && !strings.Contains(header, "📈") {
			header = "📈 " + header
		}
		out = append(out, header)
		pricier := 0
		for _, ln := range post {
			if !(strings.HasPrefix(ln, "• ") || strings.HasPrefix(ln, "- ")) {
				continue // drop closing lines and anything else
			}
			if pricier >= FBMaxPricier {
				continue
			}
			pricier++
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

// splitFBPost splits a combined post on "———" separator lines.
func splitFBPost(post string) []string {
	var parts []string
	var cur []string
	flush := func() {
		// Keep empty parts as empty (a missing block fails the check
		// via missing sections only if headers are absent; here we
		// always re-add headers, so an empty block just checks clean
		// with zero mentions).
		parts = append(parts, strings.Join(cur, "\n"))
		cur = nil
	}
	for _, ln := range strings.Split(post, "\n") {
		if strings.TrimSpace(strings.TrimSuffix(ln, "\r")) == FBSeparator {
			flush()
			continue
		}
		cur = append(cur, ln)
	}
	flush()
	return parts
}

// CheckFBPost runs Check (+ lint) over the combined post, treating each
// language block as its section. The shared footer carries no numbers and
// is excluded from the check (it would otherwise trip the Japanese-section
// Latin lint); each language block keeps its own numbers in context.
func CheckFBPost(signalsData []byte, fbPost string) (*Report, error) {
	parts := splitFBPost(fbPost)
	blocks := []string{"", "", ""}
	for i := 0; i < 3 && i < len(parts); i++ {
		blocks[i] = strings.Trim(parts[i], "\n")
	}
	pseudo := "## Taglish\n" + blocks[0] + "\n## English\n" + blocks[1] + "\n## 日本語\n" + blocks[2] + "\n"
	rep, err := Check(signalsData, pseudo)
	if err != nil {
		return nil, err
	}
	ApplyLint(rep, LintAdviceWithBasis(pseudo, usesNearbyFromSignals(signalsData)))
	return rep, nil
}

// FBPostAndCheck builds the combined post from texts and checks it,
// falling back to the template-derived post when the check fails.
func FBPostAndCheck(signalsData []byte, texts Texts) (string, *Report) {
	post := RenderFBPost(texts)
	if rep, err := CheckFBPost(signalsData, post); err == nil && rep.Status == StatusPass {
		return post, rep
	} else if err == nil {
		if tmpl, terr := RenderTemplateJSON(signalsData); terr == nil {
			tpost := RenderFBPost(tmpl)
			if trep, terr2 := CheckFBPost(signalsData, tpost); terr2 == nil {
				return tpost, trep
			}
		}
		return post, rep
	}
	// Check error (bad signals JSON): return the post with a nil report;
	// callers treat a nil report as "no fb check recorded".
	return post, nil
}
