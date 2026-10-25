// Package weekpages publishes the Sunday weekly report as static pages:
// SITE/w/W/<week>.png + week.json, per-week index.html pages, a w/
// archive index and a data/weeks.json manifest. Rendering reuses the
// weekly pipeline (signals -> RenderWeekly text -> CheckWeekly ->
// fbchart.RenderWeekly with the compact PNG encoder); output is
// byte-deterministic. A published week is FROZEN: an existing
// w/<week>/week.json is never re-rendered without -force.
package weekpages

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

	"github.com/tomohiro-owada/presyo-bantay/go/internal/daypages"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

// LangText holds one string per site language.
type LangText struct {
	TL string `json:"tl"`
	EN string `json:"en"`
	JA string `json:"ja"`
}

// WeekDoc is the SITE/w/W/week.json document (frozen page output).
type WeekDoc struct {
	Week        string   `json:"week"`
	GeneratedAt string   `json:"generated_at"`
	Covered     []string `json:"covered_dates"`
	PrevCovered []string `json:"prev_covered_dates"`
	Missing     []string `json:"missing_dates"`
	Reportable  bool     `json:"reportable"`
	// Text is the full combined post (FB == site).
	Text   string   `json:"text"`
	Blocks LangText `json:"blocks"`
	Footer LangText `json:"footer"`
	Label  LangText `json:"label"`
	// Summary is the top-drop-only summary per language (no emoji).
	Summary LangText `json:"summary"`
	Check   string   `json:"check"`
}

// WeeksEntry is one week in weeks.json.
type WeeksEntry struct {
	ID      string   `json:"id"`
	Path    string   `json:"path"`
	Label   LangText `json:"label"`
	Summary LangText `json:"summary"`
}

// WeeksDoc is the SITE/data/weeks.json document (newest first).
type WeeksDoc struct {
	Latest string       `json:"latest"`
	Weeks  []WeeksEntry `json:"weeks"`
}

// Config tunes Run.
type Config struct {
	SitePublic string
	Weeks      []string
	FromWeek   string
	ToWeek     string
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
	fmt.Fprintf(os.Stderr, "weekpages: "+format+"\n", args...)
}

func manilaZone() *time.Location {
	return time.FixedZone("Asia/Manila", 8*3600)
}

// LatestReportableWeek returns the latest reportable week whose Sunday is
// on or before today in Asia/Manila (walks back at most 104 weeks).
func LatestReportableWeek(db *sql.DB, today time.Time) (string, error) {
	manila := today.In(manilaZone())
	// Start from the week containing today; step back while its Sunday is
	// after today (i.e. the current week hasn't ended).
	w := weekly.WeekIDOf(manila)
	for i := 0; i < 105; i++ {
		y, ww, err := weekly.ParseWeekID(w)
		if err != nil {
			return "", err
		}
		sun, err := time.Parse("2006-01-02", weekly.WeekSunday(y, ww))
		if err != nil {
			return "", err
		}
		if !sun.After(manila) {
			ok, err := weekly.Reportable(db, w)
			if err != nil {
				return "", err
			}
			if ok {
				return w, nil
			}
		}
		// Step back one week.
		mon, _ := time.Parse("2006-01-02", weekly.MonSat(y, ww)[0])
		w = weekly.WeekIDOf(mon.AddDate(0, 0, -7))
	}
	return "", fmt.Errorf("no reportable week found")
}

// WeeksInRange expands an inclusive ISO-week range.
func WeeksInRange(from, to string) ([]string, error) {
	yf, wf, err := weekly.ParseWeekID(from)
	if err != nil {
		return nil, fmt.Errorf("bad -from-week: %w", err)
	}
	yt, wt, err := weekly.ParseWeekID(to)
	if err != nil {
		return nil, fmt.Errorf("bad -to-week: %w", err)
	}
	start, _ := time.Parse("2006-01-02", weekly.MonSat(yf, wf)[0])
	end, _ := time.Parse("2006-01-02", weekly.MonSat(yt, wt)[0])
	if end.Before(start) {
		return nil, fmt.Errorf("-from-week %s is after -to-week %s", from, to)
	}
	var out []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 7) {
		out = append(out, weekly.WeekIDOf(d))
	}
	return out, nil
}

// Run renders every target week then rebuilds weeks.json + all HTML.
func Run(db *sql.DB, cfg Config) error {
	base := cfg.BaseURL
	if base == "" {
		return fmt.Errorf("empty base URL")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	weeks := cfg.Weeks
	if len(weeks) == 0 && (cfg.FromWeek != "" || cfg.ToWeek != "") {
		if cfg.FromWeek == "" || cfg.ToWeek == "" {
			return fmt.Errorf("need both -from-week and -to-week")
		}
		var err error
		weeks, err = WeeksInRange(cfg.FromWeek, cfg.ToWeek)
		if err != nil {
			return err
		}
	}
	if len(weeks) == 0 && !cfg.HTMLOnly {
		latest, err := LatestReportableWeek(db, cfg.now())
		if err != nil {
			return err
		}
		weeks = []string{latest}
	}
	for _, w := range weeks {
		if _, _, err := weekly.ParseWeekID(w); err != nil {
			return err
		}
	}
	if !cfg.HTMLOnly {
		for _, w := range weeks {
			if err := renderWeek(db, cfg, w); err != nil {
				return err
			}
		}
	}
	return Rebuild(cfg.SitePublic, base)
}

// renderWeek renders SITE/w/W/{week.png,week.json} for one week.
func renderWeek(db *sql.DB, cfg Config, week string) error {
	dir := filepath.Join(cfg.SitePublic, "w", week)
	docPath := filepath.Join(dir, "week.json")
	if _, err := os.Stat(docPath); err == nil && !cfg.Force {
		logf("skip %s: week.json exists (use -force)", week)
		return nil
	}
	ok, err := weekly.Reportable(db, week)
	if err != nil {
		return err
	}
	if !ok {
		logf("skip %s: not reportable (< 3 covered dates this or last week)", week)
		return nil
	}
	sig, err := weekly.Compute(db, week)
	if err != nil {
		return err
	}
	text := weekly.RenderWeekly(sig)
	rep := weekly.CheckWeekly(sig, text)
	if rep.Status != weekly.StatusPass {
		logf("skip %s: checker %s (unpublished)", week, rep.Status)
		return nil
	}
	img, err := fbchart.RenderWeekly(sig)
	if err != nil {
		return fmt.Errorf("render %s: %w", week, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var pngBuf bytes.Buffer
	if err := fbchart.EncodeCompact(&pngBuf, img); err != nil {
		return fmt.Errorf("encode %s: %w", week, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "week.png"), pngBuf.Bytes(), 0o644); err != nil {
		return err
	}
	blocks := weekly.SplitWeekly(text)
	rng := weekly.RangeOf(sig)
	tl, en, ja := weekly.TopDropSummary(sig)
	doc := WeekDoc{
		Week: week, Covered: sig.Covered, PrevCovered: sig.PrevCovered,
		Missing: sig.Missing, Reportable: sig.Reportable,
		Text:    text,
		Blocks:  LangText{TL: blocks.TL, EN: blocks.EN, JA: blocks.JA},
		Footer:  LangText{TL: blocks.FooterTL, EN: blocks.FooterEN, JA: blocks.FooterJA},
		Label:   LangText{TL: rng.TL, EN: rng.EN, JA: rng.JA},
		Summary: LangText{TL: tl, EN: en, JA: ja},
		Check:   "pass",
	}
	if doc.Covered == nil {
		doc.Covered = []string{}
	}
	if doc.PrevCovered == nil {
		doc.PrevCovered = []string{}
	}
	if doc.Missing == nil {
		doc.Missing = []string{}
	}
	doc.GeneratedAt = cfg.now().In(manilaZone()).Format(time.RFC3339)
	if err := writeJSON(docPath, doc); err != nil {
		return err
	}
	fmt.Printf("weekpages: wrote %s\n", week)
	return nil
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

// loadWeeks reads every SITE/w/W/week.json, oldest first.
func loadWeeks(sitePublic string) ([]WeekDoc, error) {
	matches, err := filepath.Glob(filepath.Join(sitePublic, "w", "*", "week.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var out []WeekDoc
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var doc WeekDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if _, _, err := weekly.ParseWeekID(doc.Week); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, doc)
	}
	return out, nil
}

// dayExists reports whether SITE/d/D/day.json exists.
func dayExists(sitePublic, date string) bool {
	_, err := os.Stat(filepath.Join(sitePublic, "d", date, "day.json"))
	return err == nil
}

// Rebuild regenerates SITE/data/weeks.json, every SITE/w/W/index.html,
// SITE/w/index.html, and refreshes the day archive link.
func Rebuild(sitePublic, baseURL string) error {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	weeks, err := loadWeeks(sitePublic)
	if err != nil {
		return err
	}
	// Newest first.
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].Week > weeks[j].Week })
	doc := WeeksDoc{Weeks: []WeeksEntry{}}
	for _, w := range weeks {
		w := w
		doc.Weeks = append(doc.Weeks, WeeksEntry{
			ID: w.Week, Path: "w/" + w.Week + "/",
			Label: w.Label, Summary: w.Summary,
		})
	}
	if len(doc.Weeks) > 0 {
		doc.Latest = doc.Weeks[0].ID
	}
	if err := writeJSON(filepath.Join(sitePublic, "data", "weeks.json"), doc); err != nil {
		return err
	}
	for i := range weeks {
		var older, newer *WeekDoc
		if i+1 < len(weeks) {
			older = &weeks[i+1]
		}
		if i-1 >= 0 {
			newer = &weeks[i-1]
		}
		html, err := renderWeekPage(sitePublic, &weeks[i], older, newer, baseURL)
		if err != nil {
			return err
		}
		p := filepath.Join(sitePublic, "w", weeks[i].Week, "index.html")
		if err := os.WriteFile(p, []byte(html), 0o644); err != nil {
			return err
		}
	}
	archive, err := renderWeeksArchive(doc, baseURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(sitePublic, "w"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(sitePublic, "w", "index.html"), []byte(archive), 0o644); err != nil {
		return err
	}
	// Refresh the day archive so it links the weekly reports (day pages
	// themselves are byte-identical; only d/index.html gains the link).
	if _, err := os.Stat(filepath.Join(sitePublic, "d")); err == nil {
		if err := daypages.Rebuild(sitePublic, baseURL); err != nil {
			return fmt.Errorf("refresh day archive: %w", err)
		}
	}
	return nil
}
