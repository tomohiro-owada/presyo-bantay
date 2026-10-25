package weekpages

import (
	"bytes"
	"embed"
	"html/template"
	"regexp"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

var pageTmpl = template.Must(template.ParseFS(tmplFS,
	"templates/week.html.tmpl", "templates/warchive.html.tmpl"))

// pesoRe matches one peso amount so it can be wrapped in a nowrap span.
var pesoRe = regexp.MustCompile(`[\(（]?[-‐‑‒–—−+]?₱\d[\d,]*(?:\.\d+)?(?:/(?:kg|piraso|pc))?[\)）]?`)

// Seg is one renderable text run.
type Seg struct {
	Text   string
	Nowrap bool
}

// splitPeso splits s so peso amounts render unbroken.
func splitPeso(s string) []Seg {
	var out []Seg
	last := 0
	for _, loc := range pesoRe.FindAllStringIndex(s, -1) {
		if loc[0] > last {
			out = append(out, Seg{Text: s[last:loc[0]]})
		}
		out = append(out, Seg{Text: s[loc[0]:loc[1]], Nowrap: true})
		last = loc[1]
	}
	if last < len(s) {
		out = append(out, Seg{Text: s[last:]})
	}
	if len(out) == 0 {
		out = append(out, Seg{Text: s})
	}
	return out
}

// Item is one bullet line.
type Item struct {
	Segs []Seg
}

// Block is one rendered text group.
type Block struct {
	Kind  string // "head", "para", "list"
	Segs  []Seg  // head/para
	Items []Item // list
}

// toBlocks converts one language block into render groups.
func toBlocks(text string) []Block {
	var out []Block
	var list []Item
	flush := func() {
		if len(list) > 0 {
			out = append(out, Block{Kind: "list", Items: list})
			list = nil
		}
	}
	headed := false
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if !headed {
			out = append(out, Block{Kind: "head", Segs: splitPeso(t)})
			headed = true
			continue
		}
		if strings.HasPrefix(t, "• ") {
			list = append(list, Item{Segs: splitPeso(strings.TrimSpace(t[4:]))})
			continue
		}
		flush()
		out = append(out, Block{Kind: "para", Segs: splitPeso(t)})
	}
	flush()
	if out == nil {
		out = []Block{}
	}
	return out
}

// DayLink is one link to an existing day page of the week.
type DayLink struct {
	Path                   string
	DateTL, DateEN, DateJA string
}

// WeekView is the data for one week page.
type WeekView struct {
	Title                        string
	Description                  string
	Canonical                    string
	OGImage                      string
	OGAlt                        string
	Published                    string // Sunday date
	RangeTL, RangeEN, RangeJA    string
	HasOlder, HasNewer           bool
	OlderPath, NewerPath         string
	OlderTL, OlderEN, OlderJA    string
	NewerTL, NewerEN, NewerJA    string
	BlocksTL, BlocksEN, BlocksJA []Block
	FooterTL, FooterEN, FooterJA string
	DayLinks                     []DayLink
}

// renderWeekPage builds SITE/w/W/index.html.
func renderWeekPage(sitePublic string, doc *WeekDoc, older, newer *WeekDoc, base string) (string, error) {
	dl := doc.Label
	v := WeekView{
		Title:     "Lingguhang ulat · " + dl.TL + " — Presyo Bantay",
		Published: sundayOf(doc.Week),
		Canonical: base + "w/" + doc.Week + "/",
		OGImage:   base + "w/" + doc.Week + "/week.png",
		RangeTL:   dl.TL,
		RangeEN:   dl.EN,
		RangeJA:   dl.JA,
		FooterTL:  doc.Footer.TL,
		FooterEN:  doc.Footer.EN,
		FooterJA:  doc.Footer.JA,
	}
	tl, en, ja := doc.Summary.TL, doc.Summary.EN, doc.Summary.JA
	if tl == "" {
		tl = dl.TL
	}
	v.Description = tl + " · Prices from DA Bantay Presyo"
	v.OGAlt = tl
	_ = en
	_ = ja
	v.BlocksTL = toBlocks(doc.Blocks.TL)
	v.BlocksEN = toBlocks(doc.Blocks.EN)
	v.BlocksJA = toBlocks(doc.Blocks.JA)
	if older != nil {
		v.HasOlder = true
		v.OlderPath = "../" + older.Week + "/"
		v.OlderTL, v.OlderEN, v.OlderJA = older.Label.TL, older.Label.EN, older.Label.JA
	}
	if newer != nil {
		v.HasNewer = true
		v.NewerPath = "../" + newer.Week + "/"
		v.NewerTL, v.NewerEN, v.NewerJA = newer.Label.TL, newer.Label.EN, newer.Label.JA
	}
	for _, d := range doc.Covered {
		if !dayExists(sitePublic, d) {
			continue
		}
		fl := fbregion.FormatDateLong(d)
		v.DayLinks = append(v.DayLinks, DayLink{
			Path: "../../d/" + d + "/", DateTL: fl.TL, DateEN: fl.EN, DateJA: fl.JA,
		})
	}
	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "week", v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// sundayOf returns the Sunday date of the week id.
func sundayOf(week string) string {
	y, w, err := weekly.ParseWeekID(week)
	if err != nil {
		return week
	}
	return weekly.WeekSunday(y, w)
}

// ArchiveWeek is one row on the weeks archive page.
type ArchiveWeek struct {
	Path                      string
	RangeTL, RangeEN, RangeJA string
	SumTL, SumEN, SumJA       string
}

// ArchiveView is the data for SITE/w/index.html.
type ArchiveView struct {
	Title       string
	Description string
	Canonical   string
	OGImage     string
	Weeks       []ArchiveWeek
}

// renderWeeksArchive builds SITE/w/index.html (newest first).
func renderWeeksArchive(doc WeeksDoc, base string) (string, error) {
	v := ArchiveView{
		Title:       "Lingguhang ulat / Weekly reports / 週間レポート — Presyo Bantay",
		Description: "Lingguhang ulat ng presyo mula sa DA Bantay Presyo. · Weekly price reports from DA Bantay Presyo.",
		Canonical:   base + "w/",
	}
	for _, e := range doc.Weeks {
		v.Weeks = append(v.Weeks, ArchiveWeek{
			Path:    e.ID + "/",
			RangeTL: e.Label.TL, RangeEN: e.Label.EN, RangeJA: e.Label.JA,
			SumTL: e.Summary.TL, SumEN: e.Summary.EN, SumJA: e.Summary.JA,
		})
	}
	if v.Weeks == nil {
		v.Weeks = []ArchiveWeek{}
	}
	if len(doc.Weeks) > 0 {
		v.OGImage = base + "w/" + doc.Weeks[0].ID + "/week.png"
	}
	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "warchive", v); err != nil {
		return "", err
	}
	return buf.String(), nil
}
