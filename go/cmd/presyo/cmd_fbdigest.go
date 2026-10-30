package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/digest"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
)

// digestRegion is the state-file region value of a DA digest post.
const digestRegion = "da-digest"

// defaultMinGap is the minimum spacing between any two planned posts
// (DA digest, utilities, weekly, traffic). Emergencies keep their own
// shorter gap (fbutil -emergency-gap).
const defaultMinGap = 75 * time.Minute

// fbdigestForceFail, when non-nil, forces the named region to fail the
// digest checks (tests).
var fbdigestForceFail func(regionID string) bool

type fbdigestOptions struct {
	dbPath       string
	date         string
	auto         bool
	dryRun       bool
	outDir       string
	statePath    string
	envFile      string
	disableFile  string
	sitePublic   string
	graphBase    string
	windowStart  string
	weekdayStart string
	windowEnd    string
	minGap       time.Duration
	gapGrace     time.Duration
	digestGap    time.Duration
	maxPerDay    int
	publishDelay time.Duration
	presyoHome   string
	now          time.Time
}

// cmdFBDigest posts (or dry-runs) the multi-region DA digest: ONE image +
// trilingual caption replacing the per-region posts. See DIGEST_SPEC.md.
func cmdFBDigest(argv []string) int {
	fs := flag.NewFlagSet("fbdigest", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	date := fs.String("date", "", "data date YYYY-MM-DD (default: latest price date)")
	auto := fs.Bool("auto", false, "decide whether a digest is due now (window, daily cap, gaps, coverage)")
	dryRun := fs.Bool("dry-run", false, "render + check only; no network, state untouched")
	outDir := fs.String("out-dir", "", "output directory (default $PRESYO_HOME/fbdigest/<date>-<hhmm>)")
	statePath := fs.String("state", "", "state file (default $PRESYO_HOME/state/fbpost.json)")
	envFile := fs.String("env-file", "", "env file with PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN (default $PRESYO_HOME/.config/presyo/fb.env)")
	disableFile := fs.String("disable-file", "", "skip when this file exists (default $PRESYO_HOME/.config/presyo/fbpost.disabled)")
	sitePublic := fs.String("site-public", "", "site public dir: when set, wait until d/<date>/day.json exists and is older than -publish-delay")
	graphBase := fs.String("graph-base", "https://graph.facebook.com/v21.0", "Graph API base URL (tests use httptest)")
	windowStart := fs.String("window-start", "07:00", "auto window start HH:MM Asia/Manila")
	weekdayStart := fs.String("weekday-window-start", "", "auto window start on Mon-Fri (default: -window-start)")
	windowEnd := fs.String("window-end", "19:00", "auto window end HH:MM Asia/Manila (exclusive)")
	minGap := fs.Duration("min-gap", defaultMinGap, "auto: skip when ANY post is newer than this")
	gapGrace := fs.Duration("gap-grace", 5*time.Minute, "auto: min-gap skips only when since < min-gap - grace")
	digestGap := fs.Duration("digest-gap", 3*time.Hour, "auto: minimum spacing between two digests")
	maxPerDay := fs.Int("max-per-day", 2, "auto: at most this many digests per Manila day")
	publishDelay := fs.Duration("publish-delay", 3*time.Minute, "with -site-public: day page must be at least this old")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests, simulations)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	now := time.Now()
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbdigest: error: bad -now %q\n", *nowFlag)
			return 1
		}
		now = t
	}
	presyoHome := defaultPresyoHome()
	stPath := *statePath
	if stPath == "" {
		stPath = filepath.Join(presyoHome, "state", "fbpost.json")
	}
	opt := fbdigestOptions{
		dbPath: *dbPath, date: *date, auto: *auto, dryRun: *dryRun, outDir: *outDir,
		statePath: stPath, envFile: *envFile, disableFile: *disableFile,
		sitePublic: *sitePublic, graphBase: *graphBase, windowStart: *windowStart,
		weekdayStart: *weekdayStart, windowEnd: *windowEnd, minGap: *minGap,
		gapGrace: *gapGrace, digestGap: *digestGap, maxPerDay: *maxPerDay,
		publishDelay: *publishDelay, presyoHome: presyoHome, now: now,
	}
	run := func() int { return fbdigestRun(opt) }
	if *dryRun {
		return run()
	}
	return withStateLock(stPath, run)
}

func fbdigestRun(opt fbdigestOptions) int {
	skip := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "fbdigest: skip: "+format+"\n", args...)
		return 0
	}
	manila := opt.now.In(manilaZone)
	tod := manila.Hour()*60 + manila.Minute()

	disPath := opt.disableFile
	if disPath == "" {
		disPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fbpost.disabled")
	}
	if _, err := os.Stat(disPath); err == nil {
		return skip("disabled (%s exists)", disPath)
	}

	db, err := openReadOnly(opt.dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}
	defer db.Close()

	D := opt.date
	if D == "" {
		var d sql.NullString
		if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil || !d.Valid {
			return skip("no price dates in DB")
		}
		D = d.String
	}
	dT, err := time.Parse("2006-01-02", D)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: bad date %q\n", D)
		return 1
	}
	st, err := loadFBState(opt.statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}

	// Coverage already published for D: digest entries plus legacy
	// per-region posts (so the cut-over never reposts a region).
	covered := map[string]bool{}
	for _, p := range st.Posts {
		if p.AsOf != D {
			continue
		}
		if p.Region == digestRegion {
			for _, r := range p.Regions {
				covered[r] = true
			}
		}
		if _, ok := regions.ByID(p.Region); ok {
			covered[p.Region] = true
		}
	}

	if opt.auto {
		today := time.Date(manila.Year(), manila.Month(), manila.Day(), 0, 0, 0, 0, manilaZone)
		if int(today.Sub(dT).Hours()/24) > 2 {
			return skip("data date %s older than 2 days", D)
		}
		wsStr := opt.windowStart
		if opt.weekdayStart != "" && manila.Weekday() != time.Saturday && manila.Weekday() != time.Sunday {
			wsStr = opt.weekdayStart
		}
		start, err := parseHHMM(wsStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
			return 1
		}
		end, err := parseHHMM(opt.windowEnd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
			return 1
		}
		if !(start <= tod && tod < end) {
			return skip("outside digest window %s-%s (now %02d:%02d Manila)", wsStr, opt.windowEnd, manila.Hour(), manila.Minute())
		}
		// Sunday hold for the weekly report (same rule as fbpost -auto).
		if manila.Weekday() == time.Sunday && tod >= 16*60 && tod < 20*60 {
			w := weekly.WeekIDOf(manila)
			has := false
			for _, p := range st.Posts {
				if p.Region == "weekly" && p.AsOf == w {
					has = true
					break
				}
			}
			if !has {
				return skip("holding for weekly post (week %s)", w)
			}
		}
		// Daily cap and digest spacing.
		var lastAny, lastDigest time.Time
		perDay := 0
		for _, p := range st.Posts {
			t, err := time.Parse(time.RFC3339, p.PostedAt)
			if err != nil {
				continue
			}
			if t.After(lastAny) {
				lastAny = t
			}
			if p.Region == digestRegion {
				if t.After(lastDigest) {
					lastDigest = t
				}
				if t.In(manilaZone).Format("2006-01-02") == manila.Format("2006-01-02") {
					perDay++
				}
			}
		}
		if why := trafficHold(manila, st, opt.minGap, opt.gapGrace); why != "" {
			return skip("%s", why)
		}
		if perDay >= opt.maxPerDay {
			return skip("daily digest cap reached (%d/%d)", perDay, opt.maxPerDay)
		}
		if !lastDigest.IsZero() && manila.Sub(lastDigest) < opt.digestGap-opt.gapGrace {
			return skip("last digest %s ago (< %s)", manila.Sub(lastDigest).Truncate(time.Second), opt.digestGap)
		}
		if !lastAny.IsZero() && manila.Sub(lastAny) < opt.minGap-opt.gapGrace {
			return skip("last post %s ago (< %s)", manila.Sub(lastAny).Truncate(time.Second), opt.minGap)
		}
		if len(covered) >= len(regions.IDs()) {
			return skip("all regions already covered for %s", D)
		}
		if opt.sitePublic != "" {
			if msg := daypageNotReady(opt.sitePublic, D, manila, opt.publishDelay); msg != "" {
				return skip("%s", msg)
			}
		}
	}

	d, err := digest.Build(db, D, digest.Options{ForceFail: fbdigestForceFail})
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}
	inc := d.Included()
	if len(inc) < digest.MinRegions {
		return skip("only %d region(s) pass for %s (need %d)", len(inc), D, digest.MinRegions)
	}
	var fresh []string
	for _, id := range inc {
		if !covered[id] {
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 {
		return skip("nothing new for %s (covered: %s)", D, strings.Join(inc, ","))
	}
	fmt.Fprintf(os.Stderr, "fbdigest: %s included=%s new=%s dropped=%d\n", D, strings.Join(inc, ","), strings.Join(fresh, ","), len(d.Dropped()))

	text := digest.RenderCaption(d)
	rep := digest.CheckCaption(d, text)
	if rep.Status != "pass" {
		fmt.Fprintf(os.Stderr, "fbdigest: caption check failed: %s\n", strings.Join(rep.Flagged, "; "))
		return 3
	}
	img, err := fbchart.RenderDigest(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}
	out := opt.outDir
	if out == "" {
		name := D + "-" + manila.Format("1504")
		if os.Getenv("PRESYO_HOME") == "" {
			out = filepath.Join("fbdigest", name)
		} else {
			out = filepath.Join(opt.presyoHome, "fbdigest", name)
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}
	pngPath := filepath.Join(out, "digest.png")
	f, err := os.Create(pngPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
		return 1
	}
	f.Close()
	docData, _ := json.MarshalIndent(d, "", "  ")
	for name, data := range map[string][]byte{
		"digest.txt":         []byte(text),
		"digest.json":        append(docData, '\n'),
		"caption.check.json": mustJSON(rep),
	} {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", err.Error())
			return 1
		}
	}
	if opt.dryRun {
		fmt.Printf("dry-run: %s %s (not posted)\n", filepath.Join(out, "digest.txt"), pngPath)
		return 0
	}

	// Credentials.
	envPath := opt.envFile
	if envPath == "" {
		envPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fb.env")
	}
	pageID, token := "", ""
	if data, rerr := readEnvFile(envPath); rerr == nil {
		pageID, token = data["PRESYO_FB_PAGE_ID"], data["PRESYO_FB_PAGE_TOKEN"]
	} else if !os.IsNotExist(rerr) {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", rerr.Error())
		return 1
	}
	if v := os.Getenv("PRESYO_FB_PAGE_ID"); v != "" {
		pageID = v
	}
	if v := os.Getenv("PRESYO_FB_PAGE_TOKEN"); v != "" {
		token = v
	}
	if pageID == "" || token == "" {
		fmt.Fprintf(os.Stderr, "fbdigest: error: missing PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN\n")
		return 1
	}
	scrub := func(s string) string {
		if token == "" {
			return s
		}
		return strings.ReplaceAll(s, token, "[redacted]")
	}
	postID, photoID, gerr := postPhoto(opt.graphBase, pageID, token, text, pngPath)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", scrub(gerr.Error()))
		return 1
	}
	st.Posts = append(st.Posts, fbPostEntry{
		Region: digestRegion, AsOf: D, PostID: postID, PhotoID: photoID,
		PostedAt: opt.now.In(manilaZone).Format(time.RFC3339), Regions: inc,
	})
	if err := saveFBState(opt.statePath, st); err != nil {
		fmt.Fprintf(os.Stderr, "fbdigest: error: %s\n", scrub(err.Error()))
		return 1
	}
	fmt.Printf("fbdigest: posted as-of=%s regions=%s post_id=%s\n", D, strings.Join(inc, ","), postID)
	return 0
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

// daypageNotReady returns a skip message while the day page for D is
// missing or younger than delay ("" when it is ready to be linked).
func daypageNotReady(sitePublic, D string, now time.Time, delay time.Duration) string {
	data, err := os.ReadFile(filepath.Join(sitePublic, "d", D, "day.json"))
	if err != nil {
		return fmt.Sprintf("day page d/%s not generated yet", D)
	}
	var doc struct {
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Sprintf("day page d/%s unreadable", D)
	}
	gen, err := time.Parse(time.RFC3339, doc.GeneratedAt)
	if err != nil {
		return fmt.Sprintf("day page d/%s has no generated_at", D)
	}
	if now.Sub(gen) < delay {
		return fmt.Sprintf("day page d/%s is %s old (< %s publish delay)", D, now.Sub(gen).Truncate(time.Second), delay)
	}
	return ""
}
