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
// (priority water > power > fuel > bills) under the state flock.
func cmdFBUtil(argv []string) int {
	fs := flag.NewFlagSet("fbutil", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	sitePublic := fs.String("site-public", filepath.Join("site", "public"), "static site public directory")
	autoMode := fs.Bool("auto", false, "decide which util post is due (at most one post per run)")
	kindFlag := fs.String("kind", "", "post kind: fuel|bills|water|power (manual mode, required)")
	idFlag := fs.String("id", "", "post id (default: latest frozen of the kind)")
	dryRun := fs.Bool("dry-run", false, "decide/render only; no network, token not needed, state untouched")
	outDir := fs.String("out-dir", "", "output directory for dry-run copies")
	statePath := fs.String("state", "", "state file (default $PRESYO_HOME/state/fbpost.json)")
	envFile := fs.String("env-file", "", "env file with PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN (default $PRESYO_HOME/.config/presyo/fb.env)")
	disableFile := fs.String("disable-file", "", "skip posting when this file exists (default $PRESYO_HOME/.config/presyo/fbpost.disabled)")
	minGap := fs.Duration("min-gap", 2*time.Hour, "auto: skip when the last successful post is newer than this")
	gapGrace := fs.Duration("gap-grace", 5*time.Minute, "auto: min-gap skips only when since < min-gap - grace")
	emergencyGap := fs.Duration("emergency-gap", 30*time.Minute, "auto: urgent alerts skip when the last successful post is newer than this")
	windowStart := fs.String("window-start", "05:30", "auto posting window start HH:MM Asia/Manila")
	windowEnd := fs.String("window-end", "20:00", "auto posting window end HH:MM Asia/Manila (exclusive)")
	routineWindowStart := fs.String("routine-window-start", "12:00", "auto routine posting window start HH:MM Asia/Manila (fuel/bills only)")
	routineWindowEnd := fs.String("routine-window-end", "19:00", "auto routine posting window end HH:MM Asia/Manila (exclusive, fuel/bills only)")
	publishDelay := fs.Duration("publish-delay", 10*time.Minute, "auto: frozen pages newer than this are not yet deployed")
	billsMaxWait := fs.Duration("bills-max-wait", 24*time.Hour, "auto: bills bypass the DA-first gate when the frozen page is at least this old")
	graphBase := fs.String("graph-base", "https://graph.facebook.com/v21.0", "Graph API base URL (tests use httptest)")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if !*autoMode && *kindFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo fbutil -db DB -site-public DIR [-auto] [-kind fuel|bills|water|power] [-id ID] [-dry-run] [-now RFC3339]")
		return 1
	}
	if *kindFlag != "" && *kindFlag != "fuel" && *kindFlag != "bills" && *kindFlag != "water" && *kindFlag != "power" {
		fmt.Fprintln(os.Stderr, "usage: presyo fbutil -db DB -site-public DIR [-auto] [-kind fuel|bills|water|power] [-id ID] [-dry-run] [-now RFC3339]")
		return 1
	}
	presyoHome := defaultPresyoHome()
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
		minGap: *minGap, gapGrace: *gapGrace, emergencyGap: *emergencyGap, windowStart: *windowStart, windowEnd: *windowEnd,
		routineWindowStart: *routineWindowStart, routineWindowEnd: *routineWindowEnd,
		publishDelay: *publishDelay, billsMaxWait: *billsMaxWait, graphBase: *graphBase,
		presyoHome: presyoHome, now: now,
	}
	run := func() int { return fbutilRun(opt) }
	if *dryRun {
		return run()
	}
	return withStateLock(stPath, run)
}

type fbutilOptions struct {
	dbPath             string
	sitePublic         string
	autoMode           bool
	kind               string
	id                 string
	dryRun             bool
	outDir             string
	statePath          string
	envFile            string
	disableFile        string
	minGap             time.Duration
	gapGrace           time.Duration
	emergencyGap       time.Duration
	windowStart        string
	windowEnd          string
	routineWindowStart string
	routineWindowEnd   string
	publishDelay       time.Duration
	billsMaxWait       time.Duration
	graphBase          string
	presyoHome         string
	now                time.Time
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
// by id order; water/power by generated_at).
func latestFrozen(sitePublic, kind string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(sitePublic, "u", kind, "*", "post.json"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no frozen %s pages (run utilpages first)", kind)
	}
	if kind == "water" || kind == "power" {
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

// fbutilAuto picks at most one due post (priority water > power >
// fuel > bills) and posts it (or dry-runs it).
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
	// Global min-gap over ALL entries (DA, weekly, util). Urgent
	// alerts bypass the full gap but keep a short one.
	var latest time.Time
	for _, p := range st.Posts {
		if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil {
			if t.After(latest) {
				latest = t
			}
		}
	}
	emergencyOnlyGap := false
	if !latest.IsZero() {
		sinceLast := manila.Sub(latest)
		if sinceLast < opt.emergencyGap-opt.gapGrace {
			return skip("last post %s ago (< %s)", sinceLast.Truncate(time.Second), opt.emergencyGap)
		}
		if sinceLast < opt.minGap-opt.gapGrace {
			emergencyOnlyGap = true
		}
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
			// Emergency power posts (x- ids) never block a planned
			// post and never count toward the planned one-per-day.
			if region == "util-power" && strings.HasPrefix(p.AsOf, "x-") {
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

	// Sunday hold 16:00-20:00 for the weekly post. Emergency alerts
	// bypass the hold; ordinary items still hold.
	isHold := manila.Weekday() == time.Sunday && tod >= 16*60 && tod < 20*60
	if isHold {
		// Priority 1: water emergencies only.
		if frozen := fbutilDueWater(opt, manila, st, posted, postedToday, oldEnough, true); frozen != nil {
			return fbutilAutoPost(opt, frozen, "water")
		}
		// Priority 2: power emergencies only.
		if frozen := fbutilDuePower(opt, manila, st, posted, postedToday, oldEnough, true); frozen != nil {
			return fbutilAutoPost(opt, frozen, "power")
		}
		return skip("holding for weekly post (Sunday 16:00-20:00 Manila)")
	}
	if emergencyOnlyGap {
		// Only emergency candidates may be posted; everything else
		// keeps the existing min-gap skip message.
		if frozen := fbutilDueWater(opt, manila, st, posted, postedToday, oldEnough, true); frozen != nil {
			return fbutilAutoPost(opt, frozen, "water")
		}
		if frozen := fbutilDuePower(opt, manila, st, posted, postedToday, oldEnough, true); frozen != nil {
			return fbutilAutoPost(opt, frozen, "power")
		}
		return skip("last post %s ago (< %s)", manila.Sub(latest).Truncate(time.Second), opt.minGap)
	}

	// Priority 1: water alerts (main window only).
	if frozen := fbutilDueWater(opt, manila, st, posted, postedToday, oldEnough, false); frozen != nil {
		return fbutilAutoPost(opt, frozen, "water")
	}
	// Priority 2: power alerts (DA-exempt like water, main window only;
	// emergency first inside fbutilDuePower).
	if frozen := fbutilDuePower(opt, manila, st, posted, postedToday, oldEnough, false); frozen != nil {
		return fbutilAutoPost(opt, frozen, "power")
	}
	// Priority 3: fuel and bills only inside the routine window
	// [routine-window-start, routine-window-end) Manila; outside it
	// they are not evaluated and the run falls through to
	// "nothing due". Water and power above are unaffected.
	rStartStr, rEndStr := opt.routineWindowStart, opt.routineWindowEnd
	if rStartStr == "" {
		rStartStr = "12:00"
	}
	if rEndStr == "" {
		rEndStr = "19:00"
	}
	rStart, err := parseHHMM(rStartStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	rEnd, err := parseHHMM(rEndStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbutil: error: %s\n", err.Error())
		return 1
	}
	if rStart <= tod && tod < rEnd {
		// Fuel, evaluated before the DA-first gate.
		fuelFrozen, _ := fbutilDueFuel(opt, manila, posted, oldEnough)
		if fuelFrozen != nil {
			return fbutilAutoPost(opt, fuelFrozen, "fuel")
		}
		// Bills are evaluated before the gate for the starvation guard.
		billsFrozen, _ := fbutilDueBills(opt, manila, posted, oldEnough)
		// DA-first gate: when the DB's latest price date D is fresh
		// (≤ 2 days) and some region still owes D, daily posts go first.
		// A stale bills page (older than -bills-max-wait) bypasses the
		// gate so bills are not starved by a stalled DA queue.
		if next, d := fbutilDAPending(opt); next != "" {
			_ = next
			if billsFrozen != nil {
				if gen, err := time.Parse(time.RFC3339, billsFrozen.doc.GeneratedAt); err == nil {
					if manila.Sub(gen) >= opt.billsMaxWait {
						return fbutilAutoPost(opt, billsFrozen, "bills")
					}
				}
			}
			return skip("waiting for DA posts (date %s)", d)
		}
		if billsFrozen != nil {
			return fbutilAutoPost(opt, billsFrozen, "bills")
		}
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

// fbutilDueFuel selects the latest frozen fuel page when due: the
// page id is the effective_from date, unposted, 0 <= age <= 6 days,
// check=pass, old enough (publish-delay). Latest page only, any
// weekday.
func fbutilDueFuel(opt fbutilOptions, manila time.Time, posted func(string, string) bool, oldEnough func(utilpages.PostDoc) bool) (*utilFrozen, string) {
	id, err := latestFrozen(opt.sitePublic, "fuel")
	if err != nil {
		return nil, ""
	}
	from, err := time.Parse("2006-01-02", id)
	if err != nil {
		return nil, ""
	}
	today, err := time.Parse("2006-01-02", manila.Format("2006-01-02"))
	if err != nil {
		return nil, ""
	}
	age := int(today.Sub(from).Hours() / 24)
	if age < 0 || age > 6 {
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
// When emergencyOnly is true, only AlertKind "emergency" alerts are
// considered (gap and Sunday-hold bypass).
func fbutilDueWater(opt fbutilOptions, manila time.Time, st *fbPostState, posted func(string, string) bool, postedToday func(string) bool, oldEnough func(utilpages.PostDoc) bool, emergencyOnly bool) *utilFrozen {
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
		if emergencyOnly && c.doc.AlertKind != "emergency" {
			continue
		}
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
			if c.doc.OpenEnded && c.doc.AlertKind == "emergency" {
				// Open-ended emergency (no known end): due while
				// posted within the last 24h, using the frozen
				// created time (GeneratedAt or Start).
				created := start
				if gen, gerr := time.Parse(time.RFC3339, c.doc.GeneratedAt); gerr == nil {
					created = gen
				}
				if !manila.After(created.Add(24 * time.Hour)) {
					due = true
				}
			} else if !end.IsZero() && end.After(manila) && !start.After(manila.Add(72*time.Hour)) {
				// Not yet ended and starting within 72h:
				// short-notice (under 3h away) and
				// already-started-but-not-ended alerts post.
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

// fbutilDuePower selects the earliest eligible frozen power alert.
// Planned pages (AlertKind != "emergency") keep exactly the old rules:
// check=pass, not yet ended, Start within 72h, not already posted, at
// most one util-power post per Manila day (emergency x- ids never
// count; see postedToday). Emergency pages (AlertKind == "emergency")
// are tried first (earliest Start first): due when End is set and
// after now, or OpenEnded and now <= GeneratedAt+24h (no 72h rule),
// capped at 3 emergency posts per Manila day, exempt from the planned
// one-per-day rule. Both kinds dedupe against posted windows
// (>=50% overlap): emergencies against any posted util-power entry,
// planned against posted emergency entries. Cross-source overlap with
// a posted water window is NOT relevant for power. When emergencyOnly
// is true, planned pages are never considered (gap/Sunday-hold).
func fbutilDuePower(opt fbutilOptions, manila time.Time, st *fbPostState, posted func(string, string) bool, postedToday func(string) bool, oldEnough func(utilpages.PostDoc) bool, emergencyOnly bool) *utilFrozen {
	matches, err := filepath.Glob(filepath.Join(opt.sitePublic, "u", "power", "*", "post.json"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	type cand struct {
		doc  utilpages.PostDoc
		path string
	}
	var emergencies, planned []cand
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
		if doc.AlertKind == "emergency" {
			emergencies = append(emergencies, cand{doc: doc, path: p})
		} else {
			planned = append(planned, cand{doc: doc, path: p})
		}
	}
	byStart := func(a, b cand) bool {
		si, sj := a.doc.Start, b.doc.Start
		if si == "" {
			return false
		}
		if sj == "" {
			return true
		}
		return si < sj
	}
	sort.Slice(emergencies, func(i, j int) bool { return byStart(emergencies[i], emergencies[j]) })
	sort.Slice(planned, func(i, j int) bool { return byStart(planned[i], planned[j]) })

	// Emergency cap: at most 3 emergency (x- AsOf) util-power posts
	// per Manila day.
	emergencyCountToday := 0
	today := manila.Format("2006-01-02")
	for _, p := range st.Posts {
		if p.Region != "util-power" || !strings.HasPrefix(p.AsOf, "x-") {
			continue
		}
		if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil {
			if t.In(manilaZone).Format("2006-01-02") == today {
				emergencyCountToday++
			}
		}
	}
	if emergencyCountToday < 3 {
		for _, c := range emergencies {
			if posted("util-power", c.doc.AsOf) {
				continue
			}
			if !oldEnough(c.doc) {
				continue
			}
			end, _ := time.Parse(time.RFC3339, c.doc.End)
			due := false
			if !end.IsZero() && end.After(manila) {
				due = true
			} else if c.doc.OpenEnded {
				if gen, gerr := time.Parse(time.RFC3339, c.doc.GeneratedAt); gerr == nil {
					if !manila.After(gen.Add(24 * time.Hour)) {
						due = true
					}
				}
			}
			if !due {
				continue
			}
			frozen, err := loadUtilFrozen(opt.sitePublic, "power", c.doc.AsOf)
			if err != nil {
				continue
			}
			if fbutilPowerOverlapsPosted(opt, st, frozen, false) {
				continue
			}
			return frozen
		}
	}
	if emergencyOnly {
		return nil
	}
	for _, c := range planned {
		if posted("util-power", c.doc.AsOf) || postedToday("util-power") {
			continue
		}
		if !oldEnough(c.doc) {
			continue
		}
		start, serr := time.Parse(time.RFC3339, c.doc.Start)
		if serr != nil {
			continue
		}
		end, _ := time.Parse(time.RFC3339, c.doc.End)
		if end.IsZero() || !end.After(manila) {
			continue
		}
		if start.After(manila.Add(72 * time.Hour)) {
			continue
		}
		frozen, err := loadUtilFrozen(opt.sitePublic, "power", c.doc.AsOf)
		if err != nil {
			continue
		}
		if fbutilPowerOverlapsPosted(opt, st, frozen, true) {
			continue
		}
		return frozen
	}
	return nil
}

// fbutilPowerWindow resolves a power doc window for overlap tests:
// Start plus End, or GeneratedAt+24h when OpenEnded. ok=false when
// the window cannot be compared.
func fbutilPowerWindow(doc utilpages.PostDoc) (time.Time, time.Time, bool) {
	start, serr := time.Parse(time.RFC3339, doc.Start)
	if serr != nil {
		return time.Time{}, time.Time{}, false
	}
	if end, _ := time.Parse(time.RFC3339, doc.End); !end.IsZero() {
		if !start.Before(end) {
			return time.Time{}, time.Time{}, false
		}
		return start, end, true
	}
	if doc.OpenEnded {
		if gen, gerr := time.Parse(time.RFC3339, doc.GeneratedAt); gerr == nil {
			end := gen.Add(24 * time.Hour)
			if !start.Before(end) {
				return time.Time{}, time.Time{}, false
			}
			return start, end, true
		}
	}
	return time.Time{}, time.Time{}, false
}

// fbutilPowerOverlapsPosted reports whether frozen's power window
// overlaps (>=50% of the shorter window) any already-posted
// util-power entry, reading posted windows from the posted ids'
// frozen post.json Start/End (OpenEnded uses GeneratedAt+24h).
// When emergencyOnlyPosted is true, only posted emergency (x- AsOf)
// entries are checked (planned symmetry); otherwise all posted
// util-power entries are checked.
func fbutilPowerOverlapsPosted(opt fbutilOptions, st *fbPostState, frozen *utilFrozen, emergencyOnlyPosted bool) bool {
	candStart, candEnd, ok := fbutilPowerWindow(frozen.doc)
	if !ok {
		return false
	}
	for _, p := range st.Posts {
		if p.Region != "util-power" || p.AsOf == frozen.id {
			continue
		}
		if emergencyOnlyPosted && !strings.HasPrefix(p.AsOf, "x-") {
			continue
		}
		docData, err := os.ReadFile(frozenDocPath(opt.sitePublic, "power", p.AsOf))
		if err != nil {
			continue
		}
		var doc utilpages.PostDoc
		if err := json.Unmarshal(docData, &doc); err != nil {
			continue
		}
		ps, pe, ok := fbutilPowerWindow(doc)
		if !ok {
			continue
		}
		if utilities.WindowsOverlap50(candStart, candEnd, ps, pe) {
			return true
		}
	}
	return false
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
