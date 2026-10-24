package table

import (
	"sort"
	"strings"
	"unicode"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/pdftext"
)

const (
	XTolerance = 3.0
	YTolerance = 3.0
)

// ligatures mirrors pdfplumber LIGATURES.
var ligatures = map[string]string{
	"ﬀ": "ff",
	"ﬃ": "ffi",
	"ﬄ": "ffl",
	"ﬁ": "fi",
	"ﬂ": "fl",
	"ﬆ": "st",
	"ﬅ": "st",
}

// Word is one extracted word with its bbox.
type Word struct {
	Text        string
	X0, X1      float64
	Top, Bottom float64
}

// charBeginsNewWord mirrors WordExtractor.char_begins_new_word for the
// default ltr direction with x/y tolerances.
func charBeginsNewWord(prev, curr pdftext.Char, xTol, yTol float64) bool {
	ax, bx, cx := prev.X0, prev.X1, curr.X0
	ay, cy := prev.Top, curr.Top
	return (cx < ax) || (cx > bx+xTol) || (abs(cy-ay) > yTol)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// isSpace mirrors Python str.isspace for the chars we handle.
func isSpace(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// extractTuples mirrors WordExtractor.iter_extract_tuples for the default
// settings: group consecutive chars by upright, cluster each group into
// lines by top, sort within a line by x0, split into words.
func extractTuples(chars []pdftext.Char) []Word {
	var words []Word
	i := 0
	for i < len(chars) {
		j := i + 1
		for j < len(chars) && chars[j].Upright == chars[i].Upright {
			j++
		}
		group := chars[i:j]
		// iter_chars_to_lines: cluster by top (line_dir ttb default).
		lines := clusterByKey(group, func(c pdftext.Char) float64 { return c.Top }, YTolerance)
		for _, line := range lines {
			// Sort within line by (x0, x0).
			sorted := append([]pdftext.Char(nil), line...)
			sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].X0 < sorted[b].X0 })
			// iter_chars_to_words.
			var cur []pdftext.Char
			flush := func() {
				if len(cur) > 0 {
					words = append(words, mergeChars(cur))
					cur = nil
				}
			}
			for _, ch := range sorted {
				switch {
				case isSpace(ch.Text):
					flush()
				case len(cur) > 0 && charBeginsNewWord(cur[len(cur)-1], ch, XTolerance, YTolerance):
					flush()
					cur = append(cur, ch)
				default:
					cur = append(cur, ch)
				}
			}
			flush()
		}
		i = j
	}
	return words
}

// mergeChars mirrors WordExtractor.merge_chars (text + bbox).
func mergeChars(chars []pdftext.Char) Word {
	var sb strings.Builder
	x0, top, x1, bottom := chars[0].X0, chars[0].Top, chars[0].X1, chars[0].Bottom
	for _, c := range chars {
		if e, ok := ligatures[c.Text]; ok {
			sb.WriteString(e)
		} else {
			sb.WriteString(c.Text)
		}
		if c.X0 < x0 {
			x0 = c.X0
		}
		if c.Top < top {
			top = c.Top
		}
		if c.X1 > x1 {
			x1 = c.X1
		}
		if c.Bottom > bottom {
			bottom = c.Bottom
		}
	}
	return Word{Text: sb.String(), X0: x0, X1: x1, Top: top, Bottom: bottom}
}

// ExtractText mirrors pdfplumber utils.extract_text with default settings
// (non-layout path used by Table.extract).
func ExtractText(chars []pdftext.Char) string {
	if len(chars) == 0 {
		return ""
	}
	words := extractTuples(chars)
	lines := clusterByKey(words, func(w Word) float64 { return w.Top }, YTolerance)
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		w := make([]string, 0, len(line))
		for _, word := range line {
			w = append(w, word.Text)
		}
		parts = append(parts, strings.Join(w, " "))
	}
	return strings.Join(parts, "\n")
}

// ExtractWords mirrors pdfplumber utils.extract_words with defaults.
func ExtractWords(chars []pdftext.Char) []Word {
	return extractTuples(chars)
}

// PageText mirrors Page.extract_text closely enough for the report-date
// hint: words clustered into lines by top (y tolerance 3), words ordered by
// x0 within a line, lines joined by "\n".
func PageText(p pdftext.Page) string {
	words := extractTuples(p.Chars)
	if len(words) == 0 {
		return ""
	}
	lines := clusterByKey(words, func(w Word) float64 { return w.Top }, YTolerance)
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		sorted := append([]Word(nil), line...)
		sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].X0 < sorted[b].X0 })
		w := make([]string, 0, len(sorted))
		for _, word := range sorted {
			w = append(w, word.Text)
		}
		parts = append(parts, strings.Join(w, " "))
	}
	return strings.Join(parts, "\n")
}
