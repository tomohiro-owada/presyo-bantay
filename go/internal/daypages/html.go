package daypages

import (
	"bytes"
	"embed"
	"html/template"
	"regexp"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

var pageTmpl = template.Must(template.ParseFS(tmplFS,
	"templates/day.html.tmpl", "templates/archive.html.tmpl"))

// pesoRe matches one peso amount (optional sign/wrapping parens and unit)
// so it can be wrapped in a nowrap span.
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

// Block is one rendered text group: a heading line, a plain paragraph
// line, or a bullet list.
type Block struct {
	Kind  string // "head", "para", "list"
	Segs  []Seg  // head/para
	Items []Item // list
}

// toBlocks converts one language block into render groups: the first
// non-empty line is the heading (bold), "• " lines form lists, the rest
// are paragraphs.
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

// RegionView is one region section on a day page.
type RegionView struct {
	ID                           string
	TitleTL, TitleEN, TitleJA    string
	HasData                      bool
	Image                        string
	Eager                        bool
	AltTL, AltEN, AltJA          string
	BlocksTL, BlocksEN, BlocksJA []Block
	NoDataTL, NoDataEN, NoDataJA string
}

// DigestView is the multi-region digest card on a day page.
type DigestView struct {
	Image                        string
	AltTL, AltEN, AltJA          string
	BlocksTL, BlocksEN, BlocksJA []Block
}

// DayView is the data for one day page.
type DayView struct {
	Title                        string
	Description                  string
	Canonical                    string
	OGImage                      string
	OGAlt                        string
	Published                    string
	DateTL, DateEN, DateJA       string
	HasOlder, HasNewer           bool
	OlderPath, NewerPath         string
	OlderTL, OlderEN, OlderJA    string
	NewerTL, NewerEN, NewerJA    string
	Digest                       *DigestView
	Regions                      []RegionView
	FooterTL, FooterEN, FooterJA string
}

// noDataNote is the in-page note for regions without data.
func noDataNote(lang, title, date string) string {
	switch lang {
	case "en":
		return "No data for " + title + " on " + date + "."
	case "ja":
		return title + "：" + date + "のデータなし。"
	default:
		return "Walang data para sa " + title + " noong " + date + "."
	}
}

// renderDayPage builds SITE/d/D/index.html.
func renderDayPage(day *DayDoc, older, newer *DayDoc, base string) (string, error) {
	dl := fbregion.FormatDateLong(day.Date)
	v := DayView{
		Title:     "Sulit sa palengke · " + dl.TL + " — Presyo Bantay",
		Published: day.Date,
		Canonical: base + "d/" + day.Date + "/",
		DateTL:    dl.TL,
		DateEN:    dl.EN,
		DateJA:    dl.JA,
		FooterTL:  day.Footer.TL,
		FooterEN:  day.Footer.EN,
		FooterJA:  day.Footer.JA,
	}
	if f, ok := featured(day); ok {
		v.Description = f.Alt.TL + " · Prices from DA Bantay Presyo"
		v.OGImage = base + "d/" + day.Date + "/" + f.Image
		v.OGAlt = f.Alt.TL
	} else {
		v.Description = dl.TL + " · Prices from DA Bantay Presyo"
	}
	if older != nil {
		ol := fbregion.FormatDateLong(older.Date)
		v.HasOlder = true
		v.OlderPath = "../" + older.Date + "/"
		v.OlderTL, v.OlderEN, v.OlderJA = ol.TL, ol.EN, ol.JA
	}
	if newer != nil {
		nl := fbregion.FormatDateLong(newer.Date)
		v.HasNewer = true
		v.NewerPath = "../" + newer.Date + "/"
		v.NewerTL, v.NewerEN, v.NewerJA = nl.TL, nl.EN, nl.JA
	}
	firstImg := true
	if dg := day.Digest; dg != nil {
		v.Digest = &DigestView{
			Image: dg.Image,
			AltTL: dg.Alt.TL, AltEN: dg.Alt.EN, AltJA: dg.Alt.JA,
			BlocksTL: toBlocks(dg.Text.TL), BlocksEN: toBlocks(dg.Text.EN), BlocksJA: toBlocks(dg.Text.JA),
		}
		v.OGImage = base + "d/" + day.Date + "/" + dg.Image
		v.OGAlt = dg.Alt.TL
		firstImg = false
	}
	for _, r := range day.Regions {
		rv := RegionView{
			ID:      r.ID,
			TitleTL: r.Title.TL, TitleEN: r.Title.EN, TitleJA: r.Title.JA,
			HasData: r.HasData,
			Image:   r.Image,
			AltTL:   r.Alt.TL, AltEN: r.Alt.EN, AltJA: r.Alt.JA,
		}
		if r.HasData {
			rv.BlocksTL = toBlocks(r.Text.TL)
			rv.BlocksEN = toBlocks(r.Text.EN)
			rv.BlocksJA = toBlocks(r.Text.JA)
			if firstImg {
				rv.Eager = true
				firstImg = false
			}
		} else {
			rv.NoDataTL = noDataNote("tl", r.Title.TL, dl.TL)
			rv.NoDataEN = noDataNote("en", r.Title.EN, dl.EN)
			rv.NoDataJA = noDataNote("ja", r.Title.JA, dl.JA)
		}
		v.Regions = append(v.Regions, rv)
	}
	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "day", v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ArchiveDay is one row on the archive page.
type ArchiveDay struct {
	Path                   string
	DateTL, DateEN, DateJA string
	DealTL, DealEN, DealJA string
}

// ArchiveMonth groups archive days under one month heading (newest first).
type ArchiveMonth struct {
	MonthTL, MonthEN, MonthJA string
	Days                      []ArchiveDay
}

// ArchiveView is the data for SITE/d/index.html.
type ArchiveView struct {
	Title       string
	Description string
	Canonical   string
	OGImage     string
	Months      []ArchiveMonth
}

// renderArchivePage builds SITE/d/index.html: days grouped under month
// headings (newest month first, newest day first within each month).
func renderArchivePage(doc DaysDoc, base string) (string, error) {
	v := ArchiveView{
		Title:       "Araw-araw na ulat / Daily reports / 毎日のレポート — Presyo Bantay",
		Description: "Araw-araw na ulat ng presyo sa 5 rehiyon mula sa DA Bantay Presyo. · Daily price reports for 5 areas from DA Bantay Presyo.",
		Canonical:   base + "d/",
	}
	var months []ArchiveMonth
	var curKey string
	for _, e := range doc.Days {
		key := e.Date
		if len(key) >= 7 {
			key = key[:7]
		}
		if len(months) == 0 || key != curKey {
			ml := fbregion.FormatMonthLong(e.Date)
			months = append(months, ArchiveMonth{
				MonthTL: ml.TL, MonthEN: ml.EN, MonthJA: ml.JA,
			})
			curKey = key
		}
		last := &months[len(months)-1]
		last.Days = append(last.Days, ArchiveDay{
			Path:   e.Date + "/",
			DateTL: e.Label.TL,
			DateEN: e.Label.EN,
			DateJA: e.Label.JA,
			DealTL: southDeal(e, "tl"),
			DealEN: southDeal(e, "en"),
			DealJA: southDeal(e, "ja"),
		})
	}
	v.Months = months
	if v.Months == nil {
		v.Months = []ArchiveMonth{}
	}
	if len(doc.Days) > 0 && doc.Days[0].South.Image != "" {
		v.OGImage = base + doc.Days[0].South.Image
	}
	var buf bytes.Buffer
	if err := pageTmpl.ExecuteTemplate(&buf, "archive", v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// southDeal picks the featured top-deal summary for one language from
// the structured days.json south summary, prefixed with the featured
// region name, e.g. "South Metro: Sayote ₱40/kg (−50%) · Alabang".
// The summary is built from signals at render time (no text parsing,
// no emoji).
func southDeal(e DaysEntry, lang string) string {
	var title, deal string
	switch lang {
	case "en":
		title = e.South.Title.EN
		deal = e.South.Summary.EN
	case "ja":
		title = e.South.Title.JA
		deal = e.South.Summary.JA
	default:
		title = e.South.Title.TL
		deal = e.South.Summary.TL
	}
	if deal == "" {
		return ""
	}
	if title != "" {
		return title + ": " + deal
	}
	return deal
}
