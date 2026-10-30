// Package utilpages publishes the household-utilities snapshot as
// static pages, mirroring weekpages: SITE/u/fuel/<date>/,
// SITE/u/bills/<month>/ and SITE/u/water/<id>/ frozen posts
// ({index.html, post.json, <kind>.png} with the SAME text/PNG used
// for FB), plus SITE/u/index.html and SITE/data/utilities.json.
// Rendering reuses the utilities pipeline (parse results in the
// snapshot -> Render*Post text -> CheckPost -> fbchart render with the
// compact PNG encoder); output is byte-deterministic. A published post
// is FROZEN: an existing post.json is never re-rendered without
// -force.
package utilpages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// PostDoc is a frozen post document (u/<kind>/<id>/post.json).
type PostDoc struct {
	Text        string `json:"text"`
	Kind        string `json:"kind"`
	AsOf        string `json:"as_of"`
	GeneratedAt string `json:"generated_at"`
	Check       string `json:"check"`
	// Water-only extras for fbutil auto windows (parsed alert times).
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	AlertKind string `json:"alert_kind,omitempty"`
	// OpenEnded marks an emergency water alert with no known end
	// time ("until further notice" caption).
	OpenEnded bool `json:"open_ended,omitempty"`
}

// UtilEntry is one latest entry in utilities.json.
type UtilEntry struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// UtilitiesDoc is SITE/data/utilities.json.
type UtilitiesDoc struct {
	GeneratedAt string     `json:"generated_at"`
	Fuel        *UtilEntry `json:"fuel,omitempty"`
	Bills       *UtilEntry `json:"bills,omitempty"`
	Water       *UtilEntry `json:"water,omitempty"`
	Power       *UtilEntry `json:"power,omitempty"`
}

// Config tunes Run.
type Config struct {
	SitePublic string
	Force      bool
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
	fmt.Fprintf(os.Stderr, "utilpages: "+format+"\n", args...)
}

func manilaZone() *time.Location {
	return time.FixedZone("Asia/Manila", 8*3600)
}

// frozenPost describes one post to freeze.
type frozenPost struct {
	kind  string // fuel|bills|water
	id    string
	text  string
	img   *image.RGBA
	check []string
	doc   PostDoc
}

// Run renders frozen posts from the snapshot then rebuilds the index
// and manifest.
func Run(snap *utilities.Snapshot, cfg Config) error {
	base := cfg.BaseURL
	if base == "" {
		return fmt.Errorf("empty base URL")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if snap == nil {
		return fmt.Errorf("no snapshot")
	}
	now := cfg.now().In(manilaZone())
	gen := now.Format(time.RFC3339)

	// Fuel page: latest adjustment week.
	if len(snap.Fuel.Adjustments) > 0 {
		adj := snap.Fuel.Adjustments[0]
		text, err := utilities.RenderFuelPost(snap)
		if err != nil {
			logf("skip fuel: %s", err.Error())
		} else if problems := utilities.CheckPost(text, snap, "fuel"); len(problems) > 0 {
			logf("skip fuel: checker failed (%d problems, unpublished)", len(problems))
		} else {
			img, err := fbchart.RenderFuelWeekly(snap)
			if err != nil {
				return fmt.Errorf("render fuel: %w", err)
			}
			if err := freezePost(cfg, frozenPost{
				kind: "fuel", id: adj.EffectiveFrom, text: text, img: img,
				doc: PostDoc{Text: text, Kind: "fuel", AsOf: adj.EffectiveFrom, GeneratedAt: gen, Check: "pass"},
			}); err != nil {
				return err
			}
		}
	} else {
		logf("skip fuel: no adjustments in snapshot")
	}

	// Bills page: latest Meralco month.
	if len(snap.Meralco) > 0 && len(snap.LPG) > 0 {
		text, err := utilities.RenderBillsPost(snap)
		if err != nil {
			logf("skip bills: %s", err.Error())
		} else if problems := utilities.CheckPost(text, snap, "bills"); len(problems) > 0 {
			logf("skip bills: checker failed (%d problems, unpublished)", len(problems))
		} else {
			img, err := fbchart.RenderBills(snap)
			if err != nil {
				return fmt.Errorf("render bills: %w", err)
			}
			if err := freezePost(cfg, frozenPost{
				kind: "bills", id: snap.Meralco[0].Month, text: text, img: img,
				doc: PostDoc{Text: text, Kind: "bills", AsOf: snap.Meralco[0].Month, GeneratedAt: gen, Check: "pass"},
			}); err != nil {
				return err
			}
		}
	} else {
		logf("skip bills: no meralco/LPG data in snapshot")
	}

	// Water pages: every alert (own wording + advisory link come from
	// the caption itself).
	for _, a := range utilities.BuildWaterAlerts(snap.Water) {
		a := a
		text := utilities.RenderWaterAlertPost(a, false)
		checkSnap := &utilities.Snapshot{Alerts: []utilities.WaterAlert{a}}
		if problems := utilities.CheckPost(text, checkSnap, "water"); len(problems) > 0 {
			logf("skip water %s: checker failed (unpublished)", a.ID)
			continue
		}
		img, err := fbchart.RenderWaterAlert(&a, false)
		if err != nil {
			return fmt.Errorf("render water %s: %w", a.ID, err)
		}
		doc := PostDoc{Text: text, Kind: "water", AsOf: a.ID, GeneratedAt: gen, Check: "pass"}
		if !a.Start.IsZero() {
			doc.Start = a.Start.Format(time.RFC3339)
		}
		if !a.End.IsZero() {
			doc.End = a.End.Format(time.RFC3339)
		}
		if a.OpenEnded {
			doc.OpenEnded = true
		}
		doc.AlertKind = a.Kind
		if err := freezePost(cfg, frozenPost{kind: "water", id: a.ID, text: text, img: img, doc: doc}); err != nil {
			return err
		}
	}

	// Power pages: every Las Piñas planned alert, unless the snapshot
	// carries a layout alarm (then no planned pages are generated).
	// Emergency alerts from X are frozen regardless of the alarm.
	plannedAlerts := utilities.BuildPowerAlerts(snap.Power, now)
	if snap.Power != nil && snap.Power.LayoutAlarm != "" {
		logf("skip power: layout alarm set (no frozen pages)")
	} else {
		for _, a := range plannedAlerts {
			a := a
			// The snapshot keeps ~40 days of Las Piñas history for the
			// monthly trend; only current/upcoming events get pages.
			if !a.End.IsZero() && a.End.Before(now.Add(-24*time.Hour)) {
				continue
			}
			text := utilities.RenderPowerAlertPost(a, false)
			checkSnap := &utilities.Snapshot{PowerAlerts: []utilities.PowerAlert{a}}
			if problems := utilities.CheckPost(text, checkSnap, "power"); len(problems) > 0 {
				logf("skip power %s: checker failed (unpublished)", a.ID)
				continue
			}
			img, err := fbchart.RenderPowerAlert(&a, false)
			if err != nil {
				return fmt.Errorf("render power %s: %w", a.ID, err)
			}
			doc := PostDoc{Text: text, Kind: "power", AsOf: a.ID, GeneratedAt: gen, Check: "pass"}
			if !a.Start.IsZero() {
				doc.Start = a.Start.Format(time.RFC3339)
			}
			if !a.End.IsZero() {
				doc.End = a.End.Format(time.RFC3339)
			}
			doc.AlertKind = "power"
			if err := freezePost(cfg, frozenPost{kind: "power", id: a.ID, text: text, img: img, doc: doc}); err != nil {
				return err
			}
		}
	}
	for _, a := range utilities.BuildMeralcoXAlerts(snap.MeralcoX, plannedAlerts, now) {
		a := a
		if !a.End.IsZero() && a.End.Before(now.Add(-24*time.Hour)) {
			continue
		}
		text := utilities.RenderPowerAlertPost(a, false)
		checkSnap := &utilities.Snapshot{PowerAlerts: []utilities.PowerAlert{a}}
		if problems := utilities.CheckPost(text, checkSnap, "power"); len(problems) > 0 {
			logf("skip power %s: checker failed (unpublished)", a.ID)
			continue
		}
		img, err := fbchart.RenderPowerAlert(&a, false)
		if err != nil {
			return fmt.Errorf("render power %s: %w", a.ID, err)
		}
		doc := PostDoc{Text: text, Kind: "power", AsOf: a.ID, GeneratedAt: gen, Check: "pass"}
		if !a.Start.IsZero() {
			doc.Start = a.Start.Format(time.RFC3339)
		}
		if !a.End.IsZero() {
			doc.End = a.End.Format(time.RFC3339)
		}
		if a.OpenEnded {
			doc.OpenEnded = true
		}
		doc.AlertKind = "emergency"
		if err := freezePost(cfg, frozenPost{kind: "power", id: a.ID, text: text, img: img, doc: doc}); err != nil {
			return err
		}
	}

	return Rebuild(snap, cfg.SitePublic, base, gen)
}

// freezePost writes u/<kind>/<id>/{post.json,<kind>.png} unless frozen
// (existing post.json wins without -force).
func freezePost(cfg Config, p frozenPost) error {
	dir := filepath.Join(cfg.SitePublic, "u", p.kind, p.id)
	docPath := filepath.Join(dir, "post.json")
	if _, err := os.Stat(docPath); err == nil && !cfg.Force {
		logf("skip %s/%s: post.json exists (use -force)", p.kind, p.id)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(docPath, p.doc); err != nil {
		return err
	}
	var pngBuf bytes.Buffer
	if err := fbchart.EncodeCompact(&pngBuf, p.img); err != nil {
		return fmt.Errorf("encode %s/%s: %w", p.kind, p.id, err)
	}
	if err := os.WriteFile(filepath.Join(dir, p.kind+".png"), pngBuf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("utilpages: wrote u/%s/%s\n", p.kind, p.id)
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

// loadPosts reads every frozen post.json under u/, oldest first.
func loadPosts(sitePublic string) ([]PostDoc, error) {
	matches, err := filepath.Glob(filepath.Join(sitePublic, "u", "*", "*", "post.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var out []PostDoc
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var doc PostDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, doc)
	}
	return out, nil
}
