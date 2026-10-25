package utilities

import (
	"sort"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/pdftext"
)

// lineTolPt groups chars whose |Top| differ by at most this many points.
const lineTolPt = 2.5

// wordGapFrac starts a new word when the horizontal gap exceeds this
// fraction of the char size.
const wordGapFrac = 0.25

// Word is one whitespace-delimited run of chars on a visual line.
type Word struct {
	Text   string
	X0, X1 float64
}

// Line is one visual line of a PDF page in reading order.
type Line struct {
	Page  int
	Top   float64
	Words []Word
	Text  string
}

// PDFLines groups every page's pdftext chars into visual lines: same
// page, |Top| within ~2.5 pt, chars sorted by X0. A new word starts
// when the horizontal gap exceeds ~0.25 * char size, and a space char
// ends a word. Text is the words joined with single spaces. Lines come
// back in reading order (page, Top, X).
func PDFLines(path string) ([]Line, error) {
	doc, err := pdftext.Open(path)
	if err != nil {
		return nil, err
	}
	defer doc.Close()
	var out []Line
	for p := 1; p <= doc.NumPages(); p++ {
		pg, err := doc.Page(p)
		if err != nil {
			return nil, err
		}
		out = append(out, pageLines(p, pg.Chars)...)
	}
	return out, nil
}

// pageLines groups one page's chars into lines.
func pageLines(page int, chars []pdftext.Char) []Line {
	if len(chars) == 0 {
		return nil
	}
	sorted := append([]pdftext.Char(nil), chars...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Top != sorted[j].Top {
			return sorted[i].Top < sorted[j].Top
		}
		return sorted[i].X0 < sorted[j].X0
	})
	var groups [][]pdftext.Char
	for _, ch := range sorted {
		if len(groups) == 0 {
			groups = append(groups, []pdftext.Char{ch})
			continue
		}
		last := groups[len(groups)-1]
		if absF(ch.Top-last[0].Top) <= lineTolPt {
			groups[len(groups)-1] = append(last, ch)
		} else {
			groups = append(groups, []pdftext.Char{ch})
		}
	}
	var out []Line
	for _, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i].X0 < g[j].X0 })
		words := splitWords(g)
		if len(words) == 0 {
			continue
		}
		texts := make([]string, 0, len(words))
		for _, w := range words {
			texts = append(texts, w.Text)
		}
		out = append(out, Line{
			Page:  page,
			Top:   g[0].Top,
			Words: words,
			Text:  strings.Join(texts, " "),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Top != out[j].Top {
			return out[i].Top < out[j].Top
		}
		return out[i].Words[0].X0 < out[j].Words[0].X0
	})
	return out
}

// splitWords segments X-sorted chars into words: a space char ends a
// word, and a horizontal gap over ~0.25 * char size starts a new one.
func splitWords(chars []pdftext.Char) []Word {
	var out []Word
	var cur strings.Builder
	var curX0, curX1 float64
	var prevX1 float64
	started := false
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		out = append(out, Word{Text: cur.String(), X0: curX0, X1: curX1})
		cur.Reset()
		started = false
	}
	for _, ch := range chars {
		if strings.TrimSpace(ch.Text) == "" {
			flush()
			continue
		}
		if started && ch.X0-prevX1 > wordGapFrac*ch.Size {
			flush()
		}
		if !started {
			curX0 = ch.X0
			started = true
		}
		cur.WriteString(ch.Text)
		curX1 = ch.X1
		prevX1 = ch.X1
	}
	flush()
	return out
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
