// Package daypages publishes every data date's 5 regional posts as
// static daily report pages: SITE/d/D/<region>.png + day.json, a
// days.json manifest, per-day index.html pages and a d/ archive index.
// It renders through the exact same pipeline as `presyo fbpost`
// (regionrender: same signals, same text, same checker, same chart);
// only the PNG encoder is compact (fbchart.EncodeCompact).
package daypages

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regionrender"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// GA4ID is the measurement id embedded in every generated page.
const GA4ID = "G-VL047YCMK8"

// LangText holds one string per site language.
type LangText struct {
	TL string `json:"tl"`
	EN string `json:"en"`
	JA string `json:"ja"`
}

// DayRegion is one region entry in day.json.
type DayRegion struct {
	ID         string   `json:"id"`
	Order      int      `json:"order"`
	Title      LangText `json:"title"`
	HasData    bool     `json:"has_data"`
	Image      string   `json:"image"`
	ImageBytes int64    `json:"image_bytes"`
	Alt        LangText `json:"alt"`
	Text       LangText `json:"text"`
	// Summary is the top-deal-only summary per language (no emoji, no
	// title), built from structured signals at render time.
	Summary LangText `json:"summary"`
	Check   string   `json:"check"`
}

// DayDoc is the SITE/d/D/day.json document.
type DayDoc struct {
	Date        string      `json:"date"`
	GeneratedAt string      `json:"generated_at"`
	Regions     []DayRegion `json:"regions"`
	Footer      LangText    `json:"footer"`
}

// DaysSouth is the featured (south, else first with data) region in days.json.
type DaysSouth struct {
	Text LangText `json:"text"`
	// Summary is the top-deal-only summary per language (no emoji, no
	// title), built from structured signals at render time.
	Summary LangText `json:"summary"`
	Image   string   `json:"image"`
	Title   LangText `json:"title"`
}

// DaysEntry is one day in days.json.
type DaysEntry struct {
	Date            string    `json:"date"`
	Path            string    `json:"path"`
	Label           LangText  `json:"label"`
	RegionsWithData int       `json:"regions_with_data"`
	South           DaysSouth `json:"south"`
}

// DaysDoc is the SITE/data/days.json document (newest first).
type DaysDoc struct {
	Latest string      `json:"latest"`
	Days   []DaysEntry `json:"days"`
}

// Config tunes Run.
type Config struct {
	SitePublic string
	Dates      []string
	Force      bool
	HTMLOnly   bool
	BaseURL    string
	// Now pins wall-clock reads (tests); nil means time.Now.
	Now func() time.Time
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "daypages: "+format+"\n", args...)
}

// LatestDate returns the latest price date in the DB.
func LatestDate(db *sql.DB) (string, error) {
	var d sql.NullString
	if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil {
		return "", err
	}
	if !d.Valid {
		return "", fmt.Errorf("no price dates in DB")
	}
	return d.String, nil
}

// DatesInRange expands an inclusive YYYY-MM-DD range.
func DatesInRange(from, to string) ([]string, error) {
	f, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil, fmt.Errorf("bad -from %q", from)
	}
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil, fmt.Errorf("bad -to %q", to)
	}
	if t.Before(f) {
		return nil, fmt.Errorf("-from %s is after -to %s", from, to)
	}
	var out []string
	for d := f; !d.After(t); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out, nil
}

// checkDate validates a YYYY-MM-DD date.
func checkDate(d string) error {
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return fmt.Errorf("bad date %q (want YYYY-MM-DD)", d)
	}
	return nil
}

// hasDataOnDate reports whether at least one of the region's markets has
// any price row on D.
func hasDataOnDate(db *sql.DB, r *regions.Region, date string) (bool, error) {
	placeholders := make([]string, 0, len(r.Markets))
	args := make([]any, 0, len(r.Markets)+1)
	for _, m := range r.Markets {
		placeholders = append(placeholders, "?")
		args = append(args, m.Name)
	}
	args = append(args, date)
	q := `SELECT 1 FROM prices p JOIN markets m ON m.id = p.market_id
		WHERE m.name IN (` + strings.Join(placeholders, ",") + `) AND p.date = ? LIMIT 1`
	var one int
	err := db.QueryRow(q, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Run renders every target date then rebuilds days.json + all HTML.
func Run(db *sql.DB, cfg Config) error {
	base := cfg.BaseURL
	if base == "" {
		return fmt.Errorf("empty base URL")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	for _, d := range cfg.Dates {
		if err := checkDate(d); err != nil {
			return err
		}
	}
	if !cfg.HTMLOnly {
		for _, d := range cfg.Dates {
			if err := renderDay(db, cfg, d); err != nil {
				return err
			}
		}
	}
	return Rebuild(cfg.SitePublic, base)
}

// renderDay renders SITE/d/D/<region>.png + day.json for one date.
func renderDay(db *sql.DB, cfg Config, date string) error {
	dir := filepath.Join(cfg.SitePublic, "d", date)
	dayPath := filepath.Join(dir, "day.json")
	if _, err := os.Stat(dayPath); err == nil && !cfg.Force {
		logf("skip %s: day.json exists (use -force)", date)
		return nil
	}
	has := make([]bool, len(regions.Regions))
	n := 0
	for i := range regions.Regions {
		ok, err := hasDataOnDate(db, &regions.Regions[i], date)
		if err != nil {
			return err
		}
		has[i] = ok
		if ok {
			n++
		}
	}
	if n < 3 {
		logf("skip %s: only %d/5 regions have data", date, n)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	doc := DayDoc{Date: date, Regions: []DayRegion{}}
	for i := range regions.Regions {
		r := &regions.Regions[i]
		entry := DayRegion{
			ID:    r.ID,
			Order: r.Order,
			Title: LangText{TL: r.TitleTL, EN: r.TitleEN, JA: r.TitleJA},
		}
		if !has[i] {
			entry.Alt = noDataAlt(entry.Title)
			entry.Check = "skip"
			doc.Regions = append(doc.Regions, entry)
			continue
		}
		sig, err := signals.ComputeRegion(db, r.ID, signals.RegionOptions{AsOf: date})
		if err != nil {
			logf("skip %s/%s: signals: %s", date, r.ID, err)
			entry.Alt = noDataAlt(entry.Title)
			entry.Check = "skip"
			doc.Regions = append(doc.Regions, entry)
			continue
		}
		text, img, rep, err := regionrender.Render(sig)
		if err != nil {
			return fmt.Errorf("render %s/%s: %w", date, r.ID, err)
		}
		if rep.Status != fbregion.StatusPass {
			logf("skip %s/%s: checker %s (unpublished)", date, r.ID, rep.Status)
			entry.Alt = noDataAlt(entry.Title)
			entry.Check = "skip"
			doc.Regions = append(doc.Regions, entry)
			continue
		}
		var pngBuf bytes.Buffer
		if err := fbchart.EncodeCompact(&pngBuf, img); err != nil {
			return fmt.Errorf("encode %s/%s: %w", date, r.ID, err)
		}
		name := r.ID + ".png"
		if err := os.WriteFile(filepath.Join(dir, name), pngBuf.Bytes(), 0o644); err != nil {
			return err
		}
		blocks := fbregion.SplitPost(text)
		entry.HasData = true
		entry.Image = name
		entry.ImageBytes = int64(pngBuf.Len())
		entry.Alt = LangText{
			TL: fbregion.AltText(sig, "tl"),
			EN: fbregion.AltText(sig, "en"),
			JA: fbregion.AltText(sig, "ja"),
		}
		entry.Text = LangText{TL: blocks.TL, EN: blocks.EN, JA: blocks.JA}
		top := fbregion.SummarizeTopDeal(sig)
		entry.Summary = LangText{TL: top.TL, EN: top.EN, JA: top.JA}
		entry.Check = "pass"
		if doc.Footer == (LangText{}) {
			doc.Footer = LangText{TL: blocks.FooterTL, EN: blocks.FooterEN, JA: blocks.FooterJA}
		}
		doc.Regions = append(doc.Regions, entry)
	}
	doc.GeneratedAt = cfg.now().In(manilaZone()).Format(time.RFC3339)
	if err := writeJSON(dayPath, doc); err != nil {
		return err
	}
	published := 0
	for _, r := range doc.Regions {
		if r.HasData {
			published++
		}
	}
	fmt.Printf("daypages: wrote %s (%d/5 regions)\n", date, published)
	return nil
}

// noDataAlt is the day.json alt for regions without data.
func noDataAlt(title LangText) LangText {
	return LangText{
		TL: title.TL + ": walang data ngayon.",
		EN: title.EN + ": no data today.",
		JA: title.JA + "：本日のデータなし。",
	}
}

func manilaZone() *time.Location {
	return time.FixedZone("Asia/Manila", 8*3600)
}

// writeJSON writes v indented (2 spaces, unescaped UTF-8) + newline.
func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// loadDays reads every SITE/d/D/day.json, oldest first.
func loadDays(sitePublic string) ([]DayDoc, error) {
	matches, err := filepath.Glob(filepath.Join(sitePublic, "d", "*", "day.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var out []DayDoc
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var doc DayDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if err := checkDate(doc.Date); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, doc)
	}
	return out, nil
}

// featured returns the south entry when it has data, else the first
// region with data ("" when none).
func featured(doc *DayDoc) (DayRegion, bool) {
	var first DayRegion
	found := false
	for _, r := range doc.Regions {
		if r.ID == "south" && r.HasData {
			return r, true
		}
		if r.HasData && !found {
			first, found = r, true
		}
	}
	return first, found
}

// Rebuild regenerates SITE/data/days.json, every SITE/d/D/index.html and
// SITE/d/index.html from the day.json files on disk. Output is
// byte-deterministic: no timestamps in HTML, sorted day order.
func Rebuild(sitePublic, baseURL string) error {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	days, err := loadDays(sitePublic)
	if err != nil {
		return err
	}
	// Newest first.
	sort.Slice(days, func(i, j int) bool { return days[i].Date > days[j].Date })
	doc := DaysDoc{Days: []DaysEntry{}}
	for _, d := range days {
		d := d
		dl := fbregion.FormatDateLong(d.Date)
		e := DaysEntry{
			Date:  d.Date,
			Path:  "d/" + d.Date + "/",
			Label: LangText{TL: dl.TL, EN: dl.EN, JA: dl.JA},
		}
		for _, r := range d.Regions {
			if r.HasData {
				e.RegionsWithData++
			}
		}
		if f, ok := featured(&d); ok {
			e.South = DaysSouth{Text: f.Text, Summary: f.Summary, Image: "d/" + d.Date + "/" + f.Image, Title: f.Title}
		} else {
			e.South = DaysSouth{Text: LangText{}, Summary: LangText{}, Image: ""}
		}
		doc.Days = append(doc.Days, e)
	}
	if len(doc.Days) > 0 {
		doc.Latest = doc.Days[0].Date
	}
	if err := writeJSON(filepath.Join(sitePublic, "data", "days.json"), doc); err != nil {
		return err
	}
	for i := range days {
		var older, newer *DayDoc
		if i+1 < len(days) {
			older = &days[i+1]
		}
		if i-1 >= 0 {
			newer = &days[i-1]
		}
		html, err := renderDayPage(&days[i], older, newer, baseURL)
		if err != nil {
			return err
		}
		p := filepath.Join(sitePublic, "d", days[i].Date, "index.html")
		if err := os.WriteFile(p, []byte(html), 0o644); err != nil {
			return err
		}
	}
	archive, err := renderArchivePage(doc, baseURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(sitePublic, "d"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sitePublic, "d", "index.html"), []byte(archive), 0o644)
}
