package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilpages"
)

// cmdFBUtil posts frozen household-utility pages (FB == site). Manual
// mode posts one frozen page; -auto picks at most one due post per run
// (priority water > fuel > bills) under the state flock.
func cmdFBUtil(argv []string) int {
	fs := flag.NewFlagSet("fbutil", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	sitePublic := fs.String("site-public", filepath.Join("site", "public"), "static site public directory")
	autoMode := fs.Bool("auto", false, "decide which util post is due (at most one post per run)")
	kindFlag := fs.String("kind", "", "post kind: fuel|bills|water (manual mode, required)")
	idFlag := fs.String("id", "", "post id (default: latest frozen of the kind)")
	dryRun := fs.Bool("dry-run", false, "decide/render only; no network, token not needed, state untouched")
	outDir := fs.String("out-dir", "", "output directory for dry-run copies")
	statePath := fs.String("state", "", "state file (default $PRESYO_HOME/state/fbpost.json)")
	envFile := fs.String("env-file", "", "env file with PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN (default $PRESYO_HOME/.config/presyo/fb.env)")
	disableFile := fs.String("disable-file", "", "skip posting when this file exists (default $PRESYO_HOME/.config/presyo/fbpost.disabled)")
	minGap := fs.Duration("min-gap", 2*time.Hour, "auto: skip when the last successful post is newer than this")
	windowStart := fs.String("window-start", "12:00", "auto posting window start HH:MM Asia/Manila")
	windowEnd := fs.String("window-end", "19:00", "auto posting window end HH:MM Asia/Manila (exclusive)")
	publishDelay := fs.Duration("publish-delay", 10*time.Minute, "auto: frozen pages newer than this are not yet deployed")
	graphBase := fs.String("graph-base", "https://graph.facebook.com/v21.0", "Graph API base URL (tests use httptest)")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if !*autoMode && *kindFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo fbutil -db DB -site-public DIR [-auto] [-kind fuel|bills|water] [-id ID] [-dry-run] [-now RFC3339]")
		return 1
	}
	if *kindFlag != "" && *kindFlag != "fuel" && *kindFlag != "bills" && *kindFlag != "water" {
		fmt.Fprintln(os.Stderr, "usage: presyo fbutil -db DB -site-public DIR [-auto] [-kind fuel|bills|water] [-id ID] [-dry-run] [-now RFC3339]")
		return 1
	}
	presyoHome := os.Getenv("PRESYO_HOME")
	if presyoHome == "" {
		presyoHome = "$HOME"
	}
	now := time.Now()
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: bad -now %q\n", *nowFlag)
			return 1
		}
		now = t
	}
	stPath := *statePath
	if stPath == "" {
		stPath = filepath.Join(presyoHome, "state", "fbpost.json")
	}
	opt := fbutilOptions{
		dbPath: *dbPath, sitePublic: *sitePublic, autoMode: *autoMode,
		kind: *kindFlag, id: *idFlag, dryRun: *dryRun, outDir: *outDir,
		statePath: stPath, envFile: *envFile, disableFile: *disableFile,
		minGap: *minGap, windowStart: *windowStart, windowEnd: *windowEnd,
		publishDelay: *publishDelay, graphBase: *graphBase,
		presyoHome: presyoHome, now: now,
	}
	run := func() int { return fbutilRun(opt) }
	if *dryRun {
		return run()
	}
	return withStateLock(stPath, run)
}

type fbutilOptions struct {
	dbPath       string
	sitePublic   string
	autoMode     bool
	kind         string
	id           string
	dryRun       bool
	outDir       string
	statePath    string
	envFile      string
	disableFile  string
	minGap       time.Duration
	windowStart  string
	windowEnd    string
	publishDelay time.Duration
	graphBase    string
	presyoHome   string
	now          time.Time
}

// utilFrozen is one frozen page (FB == site).
type utilFrozen struct {
	kind string
	id   string
	text string
	png  []byte
	doc  utilpages.PostDoc
}

// loadUtilFrozen reads SITE/u/<kind>/<id>/{post.json,<kind>.png}.
// Empty id selects the latest frozen page of the kind.
func loadUtilFrozen(sitePublic, kind, id string) (*utilFrozen, error) {
	if id == "" {
		latest, err := latestFrozen(sitePublic, kind)
		if err != nil {
			return nil, err
		}
		id = latest
	}
	docData, err := os.ReadFile(filepath.Join(sitePublic, "u", kind, id, "post.json"))
	if err != nil {
		return nil, fmt.Errorf("frozen page u/%s/%s missing (run utilpages first)", kind, id)
	}
	var doc utilpages.PostDoc
	if err := json.Unmarshal(docData, &doc); err != nil {
		return nil, fmt.Errorf("frozen page u/%s/%s: %w", kind, id, err)
	}
	if doc.Check != "pass" || doc.Text == "" {
		return nil, fmt.Errorf("frozen page u/%s/%s not generated (check=%q)", kind, id, doc.Check)
	}
	png, err := os.ReadFile(filepath.Join(sitePublic, "u", kind, id, kind+".png"))
	if err != nil {
		return nil, fmt.Errorf("frozen page u/%s/%s: %w", kind, id, err)
	}
	return &utilFrozen{kind: kind, id: id, text: doc.Text, png: png, doc: doc}, nil
}

// latestFrozen returns the latest frozen post id of a kind (fuel/bills
// by id order; water by generated_at).
func latestFrozen(sitePublic, kind string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(sitePublic, "u", kind, "*", "post.json"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no frozen %s pages (run utilpages first)", kind)
	}
	if kind == "water" {
		best, bestGen := "", ""
		for _, p := range matches {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			var doc utilpages.PostDoc
			if err := json.Unmarshal(data, &doc); err != nil {
				continue
			}
			if doc.GeneratedAt >= bestGen {
				best, bestGen = doc.AsOf, doc.GeneratedAt
			}
		}
		if best == "" {
			return "", fmt.Errorf("no frozen %s pages (run utilpages first)", kind)
		}
		return best, nil
	}
	best := ""
	for _, p := range matches {
		id := filepath.Base(filepath.Dir(p))
		if id > best {
			best = id
		}
	}
	return best, nil
}

// fbutilOutDir resolves the dry-run output directory.
func fbutilOutDir(opt fbutilOptions, kind, id string) string {
	if opt.outDir != "" {
		return opt.outDir
	}
	name := kind + "-" + id
	if os.Getenv("PRESYO_HOME") == "" {
		return filepath.Join("fbutil", name)
	}
	return filepath.Join(opt.presyoHome, "fbutil", name)
}

// fbutilRun implements manual + auto flows.
func fbutilRun(opt fbutilOptions) int {
	manila := opt.now.In(manilaZone)
	if opt.autoMode {
		return fbutilAuto(opt, manila)
	}
	frozen, err := loadUtilFrozen(opt.sitePublic, opt.kind, opt.id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	if opt.dryRun {
		out := fbutilOutDir(opt, frozen.kind, frozen.id)
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, frozen.kind+".txt"), []byte(frozen.text+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, frozen.kind+".png"), frozen.png, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
			return 1
		}
		fmt.Printf("dry-run: %s %s (not posted)\n", filepath.Join(out, frozen.kind+".txt"), filepath.Join(out, frozen.kind+".png"))
		return 0
	}
	return fbutilPost(frozen, opt)
}

// fbutilPost handles disable/state/credentials/upload for one post.
func fbutilPost(frozen *utilFrozen, opt fbutilOptions) int {
	disPath := opt.disableFile
	if disPath == "" {
		disPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fbpost.disabled")
	}
	if _, err := os.Stat(disPath); err == nil {
		fmt.Fprintf(os.Stderr, "fbutil: disabled (%s exists), skip\n", disPath)
		return 0
	}
	region := "util-" + frozen.kind
	st, err := loadFBState(opt.statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	for _, p := range st.Posts {
		if p.Region == region && p.AsOf == frozen.id {
			fmt.Fprintf(os.Stderr, "fbutil: already posted (%s %s post_id=%s), skip\n", region, frozen.id, p.PostID)
			return 0
		}
	}
	envPath := opt.envFile
	if envPath == "" {
		envPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fb.env")
	}
	pageID, token := "", ""
	if data, rerr := readEnvFile(envPath); rerr == nil {
		pageID, token = data["PRESYO_FB_PAGE_ID"], data["PRESYO_FB_PAGE_TOKEN"]
	} else if !os.IsNotExist(rerr) {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", rerr.Error())
		return 1
	}
	if v := os.Getenv("PRESYO_FB_PAGE_ID"); v != "" {
		pageID = v
	}
	if v := os.Getenv("PRESYO_FB_PAGE_TOKEN"); v != "" {
		token = v
	}
	if pageID == "" || token == "" {
		fmt.Fprintf(os.Stderr, "fbutil: error: missing PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN\n")
		return 1
	}
	scrub := func(s string) string {
		if token == "" {
			return s
		}
		return strings.ReplaceAll(s, token, "[redacted]")
	}
	// Stage the frozen PNG for the shared uploader.
	out := fbutilOutDir(opt, frozen.kind, frozen.id)
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", scrub(err.Error()))
		return 1
	}
	pngPath := filepath.Join(out, frozen.kind+".png")
	if err := os.WriteFile(pngPath, frozen.png, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", scrub(err.Error()))
		return 1
	}
	if err := os.WriteFile(filepath.Join(out, frozen.kind+".txt"), []byte(frozen.text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", scrub(err.Error()))
		return 1
	}
	postID, photoID, gerr := postPhoto(opt.graphBase, pageID, token, frozen.text, pngPath)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", scrub(gerr.Error()))
		return 1
	}
	st.Posts = append(st.Posts, fbPostEntry{
		Region: region, AsOf: frozen.id, PostID: postID, PhotoID: photoID,
		PostedAt: opt.now.In(manilaZone).Format(time.RFC3339),
	})
	if err := saveFBState(opt.statePath, st); err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", scrub(err.Error()))
		return 1
	}
	fmt.Printf("fbutil: posted %s=%s post_id=%s\n", region, frozen.id, postID)
	return 0
}

// fbutilAuto picks at most one due post (priority water > fuel >
// bills) and posts it (or dry-runs it).
func fbutilAuto(opt fbutilOptions, manila time.Time) int {
	skip := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "fbutil: auto skip: "+format+"\n", args...)
		return 0
	}
	// Posting window [start, end), Asia/Manila.
	start, err := parseHHMM(opt.windowStart)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	end, err := parseHHMM(opt.windowEnd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	tod := manila.Hour()*60 + manila.Minute()
	if !(start <= tod && tod < end) {
		return skip("outside posting window %s-%s (now %02d:%02d Manila)",
			opt.windowStart, opt.windowEnd, manila.Hour(), manila.Minute())
	}
	// Sunday hold 16:00-20:00 for the weekly post.
	if manila.Weekday() == time.Sunday && tod >= 16*60 && tod < 20*60 {
		return skip("holding for weekly post (Sunday 16:00-20:00 Manila)")
	}
	disPath := opt.disableFile
	if disPath == "" {
		disPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fbpost.disabled")
	}
	if _, err := os.Stat(disPath); err == nil {
		return skip("disabled (%s exists)", disPath)
	}
	st, err := loadFBState(opt.statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	// Global min-gap over ALL entries (DA, weekly, util).
	var latest time.Time
	for _, p := range st.Posts {
		if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil {
			if t.After(latest) {
				latest = t
			}
		}
	}
	if !latest.IsZero() && manila.Sub(latest) < opt.minGap {
		return skip("last post %s ago (< %s)", manila.Sub(latest).Truncate(time.Second), opt.minGap)
	}
	posted := func(region, asOf string) bool {
		for _, p := range st.Posts {
			if p.Region == region && p.AsOf == asOf {
				return true
			}
		}
		return false
	}
	postedToday := func(region string) bool {
		today := manila.Format("2006-01-02")
		for _, p := range st.Posts {
			if p.Region != region {
				continue
			}
			if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil {
				if t.In(manilaZone).Format("2006-01-02") == today {
					return true
				}
			}
		}
		return false
	}
	oldEnough := func(doc utilpages.PostDoc) bool {
		gen, err := time.Parse(time.RFC3339, doc.GeneratedAt)
		if err != nil {
			return false
		}
		return manila.Sub(gen) >= opt.publishDelay
	}

	// Priority 1: water alerts.
	if frozen := fbutilDueWater(opt, manila, st, posted, postedToday, oldEnough); frozen != nil {
		return fbutilAutoPost(opt, frozen, "water")
	}
	// Priority 2/3 need the DA-first gate: when the DB's latest price
	// date D is fresh (≤ 2 days) and some region still owes D, daily
	// posts go first.
	if next, d := fbutilDAPending(opt); next != "" {
		_ = next
		return skip("waiting for DA posts (date %s)", d)
	}
	// Priority 2: fuel.
	if frozen, reason := fbutilDueFuel(opt, manila, posted, oldEnough); frozen != nil {
		_ = reason
		return fbutilAutoPost(opt, frozen, "fuel")
	}
	// Priority 3: bills.
	if frozen, _ := fbutilDueBills(opt, manila, posted, oldEnough); frozen != nil {
		return fbutilAutoPost(opt, frozen, "bills")
	}
	return skip("nothing due")
}

// fbutilAutoPost dry-runs (copies + decision, no state/network) or
// posts the selected frozen page.
func fbutilAutoPost(opt fbutilOptions, frozen *utilFrozen, why string) int {
	if opt.dryRun {
		out := fbutilOutDir(opt, frozen.kind, frozen.id)
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, frozen.kind+".txt"), []byte(frozen.text+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, frozen.kind+".png"), frozen.png, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
			return 1
		}
		fmt.Fprintf(os.Stderr, "fbutil: auto selected %s=%s (dry-run, not posted)\n", "util-"+frozen.kind, frozen.id)
		fmt.Printf("dry-run: %s %s\n", filepath.Join(out, frozen.kind+".txt"), filepath.Join(out, frozen.kind+".png"))
		return 0
	}
	return fbutilPost(frozen, opt)
}

// fbutilDAPending reports whether daily posts still owe the latest DB
// date D ("" when DA is done or stale).
func fbutilDAPending(opt fbutilOptions) (string, string) {
	db, err := openReadOnly(opt.dbPath)
	if err != nil {
		return "", ""
	}
	defer db.Close()
	var d sql.NullString
	if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil || !d.Valid {
		return "", ""
	}
	D := d.String
	dT, err := time.Parse("2006-01-02", D)
	if err != nil {
		return "", ""
	}
	manila := opt.now.In(manilaZone)
	today := time.Date(manila.Year(), manila.Month(), manila.Day(), 0, 0, 0, 0, manilaZone)
	if int(today.Sub(dT).Hours()/24) > 2 {
		return "", "" // stale: fbpost skips anyway
	}
	st, err := loadFBState(opt.statePath)
	if err != nil {
		return "", ""
	}
	for _, id := range regions.IDs() {
		done := false
		for _, p := range st.Posts {
			if p.Region == id && p.AsOf == D {
				done = true
				break
			}
		}
		if !done {
			return id, D
		}
	}
	return "", ""
}

// fbutilDueFuel selects the latest frozen fuel page when due.
func fbutilDueFuel(opt fbutilOptions, manila time.Time, posted func(string, string) bool, oldEnough func(utilpages.PostDoc) bool) (*utilFrozen, string) {
	id, err := latestFrozen(opt.sitePublic, "fuel")
	if err != nil {
		return nil, ""
	}
	// Tue/Wed only.
	if wd := manila.Weekday(); wd != time.Tuesday && wd != time.Wednesday {
		return nil, ""
	}
	from, err := time.Parse("2006-01-02", id)
	if err != nil {
		return nil, ""
	}
	today := time.Date(manila.Year(), manila.Month(), manila.Day(), 0, 0, 0, 0, manilaZone)
	age := int(today.Sub(from).Hours() / 24)
	if age < 0 || age > 2 {
		return nil, ""
	}
	if posted("util-fuel", id) {
		return nil, ""
	}
	frozen, err := loadUtilFrozen(opt.sitePublic, "fuel", id)
	if err != nil {
		return nil, ""
	}
	var doc utilpages.PostDoc
	if err := json.Unmarshal(mustReadFile(frozenDocPath(opt.sitePublic, "fuel", id)), &doc); err != nil {
		return nil, ""
	}
	if !oldEnough(doc) {
		return nil, ""
	}
	return frozen, id
}

// fbutilDueBills selects the current month's frozen bills page when due.
func fbutilDueBills(opt fbutilOptions, manila time.Time, posted func(string, string) bool, oldEnough func(utilpages.PostDoc) bool) (*utilFrozen, string) {
	m := manila.Format("2006-01")
	if posted("util-bills", m) {
		return nil, ""
	}
	frozen, err := loadUtilFrozen(opt.sitePublic, "bills", m)
	if err != nil {
		return nil, ""
	}
	var doc utilpages.PostDoc
	if err := json.Unmarshal(mustReadFile(frozenDocPath(opt.sitePublic, "bills", m)), &doc); err != nil {
		return nil, ""
	}
	if !oldEnough(doc) {
		return nil, ""
	}
	return frozen, m
}

// fbutilDueWater selects the earliest eligible frozen water alert.
func fbutilDueWater(opt fbutilOptions, manila time.Time, st *fbPostState, posted func(string, string) bool, postedToday func(string) bool, oldEnough func(utilpages.PostDoc) bool) *utilFrozen {
	matches, err := filepath.Glob(filepath.Join(opt.sitePublic, "u", "water", "*", "post.json"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	type cand struct {
		doc  utilpages.PostDoc
		path string
	}
	var cands []cand
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc utilpages.PostDoc
		if err := json.Unmarshal(data, &doc); err != nil {
			continue
		}
		if doc.Check != "pass" || doc.Text == "" {
			continue
		}
		cands = append(cands, cand{doc: doc, path: p})
	}
	// Earliest start first (unknown starts last).
	sort.Slice(cands, func(i, j int) bool {
		si, sj := cands[i].doc.Start, cands[j].doc.Start
		if si == "" {
			return false
		}
		if sj == "" {
			return true
		}
		return si < sj
	})
	for _, c := range cands {
		if posted("util-water", c.doc.AsOf) || postedToday("util-water") {
			continue
		}
		if !oldEnough(c.doc) {
			continue
		}
		start, serr := time.Parse(time.RFC3339, c.doc.Start)
		end, _ := time.Parse(time.RFC3339, c.doc.End)
		due := false
		if serr == nil {
			if !start.Before(manila.Add(3*time.Hour)) && !start.After(manila.Add(72*time.Hour)) {
				due = true
			}
			// Emergency not yet ended.
			if !due && c.doc.AlertKind == "emergency" && !end.IsZero() && end.After(manila) {
				due = true
			}
		}
		if !due {
			continue
		}
		frozen, err := loadUtilFrozen(opt.sitePublic, "water", c.doc.AsOf)
		if err != nil {
			continue
		}
		// Cross-source overlap guard: never post a water alert whose
		// window overlaps (same rule, same city) an already-posted
		// util-water entry, read from the posted ids' frozen post.json
		// Start/End. For daily candidates each nightly window is
		// checked, so a feed alert is not re-posted after the same
		// event was posted from X (and vice versa).
		if fbutilWaterOverlapsPosted(opt, st, frozen, manila) {
			continue
		}
		return frozen
	}
	return nil
}

// fbutilWaterOverlapsPosted reports whether frozen's window overlaps
// any already-posted util-water entry (>= 50 % of the shorter
// window, same city). Daily candidates span first-night start to
// last-night end, so a single-window post of the same event lies
// fully inside and is caught. Posted windows come from the posted
// ids' frozen post.json Start/End.
func fbutilWaterOverlapsPosted(opt fbutilOptions, st *fbPostState, frozen *utilFrozen, manila time.Time) bool {
	candStart, serr := time.Parse(time.RFC3339, frozen.doc.Start)
	candEnd, _ := time.Parse(time.RFC3339, frozen.doc.End)
	if serr != nil {
		return false
	}
	_ = manila
	for _, p := range st.Posts {
		if p.Region != "util-water" || p.AsOf == frozen.id {
			continue
		}
		docData, err := os.ReadFile(frozenDocPath(opt.sitePublic, "water", p.AsOf))
		if err != nil {
			continue
		}
		var doc utilpages.PostDoc
		if err := json.Unmarshal(docData, &doc); err != nil {
			continue
		}
		ps, serr := time.Parse(time.RFC3339, doc.Start)
		pe, _ := time.Parse(time.RFC3339, doc.End)
		if serr != nil || pe.IsZero() {
			continue
		}
		if utilities.WindowsOverlap50(candStart, candEnd, ps, pe) {
			return true
		}
	}
	return false
}

func frozenDocPath(sitePublic, kind, id string) string {
	return filepath.Join(sitePublic, "u", kind, id, "post.json")
}

func mustReadFile(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
