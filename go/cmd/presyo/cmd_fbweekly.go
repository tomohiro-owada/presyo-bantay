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

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekly"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekpages"
)

// frozenWeek is the week.json document plus its PNG bytes.
type frozenWeek struct {
	doc weekpages.WeekDoc
	png []byte
}

// loadFrozenWeek reads SITE/w/<week>/{week.json,week.png} (FB == site).
func loadFrozenWeek(sitePublic, week string) (*frozenWeek, error) {
	data, err := os.ReadFile(filepath.Join(sitePublic, "w", week, "week.json"))
	if err != nil {
		return nil, err
	}
	var doc weekpages.WeekDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Check != "pass" || doc.Text == "" {
		return nil, fmt.Errorf("week %s page not generated (check=%q)", week, doc.Check)
	}
	pngData, err := os.ReadFile(filepath.Join(sitePublic, "w", week, "week.png"))
	if err != nil {
		return nil, err
	}
	return &frozenWeek{doc: doc, png: pngData}, nil
}

// cmdFBWeekly posts the Sunday weekly report to the Facebook Page.
func cmdFBWeekly(argv []string) int {
	fs := flag.NewFlagSet("fbweekly", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	weekFlag := fs.String("week", "", "ISO week id YYYY-Www (default: ISO week containing now in Manila)")
	dryRun := fs.Bool("dry-run", false, "render and check only; no network, token not needed")
	outDir := fs.String("out-dir", "", "output directory (default $PRESYO_HOME/fbweekly/<week>/ or ./fbweekly/<week>)")
	autoMode := fs.Bool("auto", false, "post only when due (Sunday 18:00-20:00 Manila, page generated, gap ok)")
	sitePublic := fs.String("site-public", "", "static site public directory (auto requires the frozen page there)")
	statePath := fs.String("state", "", "state file (default $PRESYO_HOME/state/fbpost.json)")
	envFile := fs.String("env-file", "", "env file with PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN (default $PRESYO_HOME/.config/presyo/fb.env)")
	disableFile := fs.String("disable-file", "", "skip posting when this file exists (default $PRESYO_HOME/.config/presyo/fbpost.disabled)")
	minGap := fs.Duration("min-gap", defaultMinGap, "auto: skip when the last successful post is newer than this")
	windowStart := fs.String("window-start", "18:00", "auto posting window start HH:MM Asia/Manila")
	windowEnd := fs.String("window-end", "20:00", "auto posting window end HH:MM Asia/Manila (exclusive)")
	graphBase := fs.String("graph-base", "https://graph.facebook.com/v21.0", "Graph API base URL (tests use httptest)")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	presyoHome := defaultPresyoHome()

	now := time.Now()
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: bad -now %q\n", *nowFlag)
			return 1
		}
		now = t
	}
	manila := now.In(manilaZone)
	week := *weekFlag
	if week == "" {
		week = weekly.WeekIDOf(manila)
	}
	if _, _, err := weekly.ParseWeekID(week); err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
		return 1
	}

	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
		return 1
	}
	defer db.Close()

	// Resolve the state path early so the lock file is known.
	stPath := *statePath
	if stPath == "" {
		stPath = filepath.Join(presyoHome, "state", "fbpost.json")
	}

	run := func() int {
		return fbweeklyRun(db, fbweeklyOptions{
			week: week, dryRun: *dryRun, outDir: *outDir, autoMode: *autoMode,
			sitePublic: *sitePublic, statePath: stPath, envFile: *envFile,
			disableFile: *disableFile, minGap: *minGap,
			windowStart: *windowStart, windowEnd: *windowEnd,
			graphBase: *graphBase, presyoHome: presyoHome, now: now,
		})
	}
	if *dryRun {
		return run()
	}
	return withStateLock(stPath, run)
}

type fbweeklyOptions struct {
	week        string
	dryRun      bool
	outDir      string
	autoMode    bool
	sitePublic  string
	statePath   string
	envFile     string
	disableFile string
	minGap      time.Duration
	windowStart string
	windowEnd   string
	graphBase   string
	presyoHome  string
	now         time.Time
}

// fbweeklyOutDir resolves the output directory for weekly artifacts.
func fbweeklyOutDir(opt fbweeklyOptions) string {
	if opt.outDir != "" {
		return opt.outDir
	}
	if os.Getenv("PRESYO_HOME") == "" {
		return filepath.Join("fbweekly", opt.week)
	}
	return filepath.Join(opt.presyoHome, "fbweekly", opt.week)
}

// writeFrozenDryRun stages the frozen page text+PNG into the out dir for
// dry-run runs (no network, no credentials, no state write).
func writeFrozenDryRun(opt fbweeklyOptions, frozen *frozenWeek) (txtPath, pngPath string, rc int) {
	out := fbweeklyOutDir(opt)
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
		return "", "", 1
	}
	txtPath = filepath.Join(out, "weekly.txt")
	pngPath = filepath.Join(out, "weekly.png")
	if err := os.WriteFile(txtPath, []byte(frozen.doc.Text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
		return "", "", 1
	}
	if err := os.WriteFile(pngPath, frozen.png, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
		return "", "", 1
	}
	return txtPath, pngPath, 0
}

func fbweeklyRun(db *sql.DB, opt fbweeklyOptions) int {
	manila := opt.now.In(manilaZone)

	// Frozen page output when it exists (FB == site).
	var frozen *frozenWeek
	if opt.sitePublic != "" {
		if fw, err := loadFrozenWeek(opt.sitePublic, opt.week); err == nil {
			frozen = fw
		}
	}

	if opt.autoMode {
		if frozen == nil {
			fmt.Fprintf(os.Stderr, "fbweekly: auto skip: weekly page not generated yet (site-public/w/%s/week.json)\n", opt.week)
			return 0
		}
		// Sunday only.
		if manila.Weekday() != time.Sunday {
			fmt.Fprintf(os.Stderr, "fbweekly: auto skip: not Sunday (Manila %s)\n", manila.Format("Monday"))
			return 0
		}
		// Window [start, end).
		start, err := parseHHMM(opt.windowStart)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		end, err := parseHHMM(opt.windowEnd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		tod := manila.Hour()*60 + manila.Minute()
		if !(start <= tod && tod < end) {
			fmt.Fprintf(os.Stderr, "fbweekly: auto skip: outside posting window %s-%s (now %02d:%02d Manila)\n",
				opt.windowStart, opt.windowEnd, manila.Hour(), manila.Minute())
			return 0
		}
		// Reportable week.
		ok, err := weekly.Reportable(db, opt.week)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "fbweekly: auto skip: week %s not reportable\n", opt.week)
			return 0
		}
		// Disable file.
		disPath := opt.disableFile
		if disPath == "" {
			disPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fbpost.disabled")
		}
		if _, err := os.Stat(disPath); err == nil {
			fmt.Fprintf(os.Stderr, "fbweekly: disabled (%s exists), skip\n", disPath)
			return 0
		}
		// State: no duplicate; min-gap over ANY post.
		st, err := loadFBState(opt.statePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		for _, p := range st.Posts {
			if p.Region == "weekly" && p.AsOf == opt.week {
				fmt.Fprintf(os.Stderr, "fbweekly: already posted (week %s post_id=%s), skip\n", opt.week, p.PostID)
				return 0
			}
		}
		var latest time.Time
		for _, p := range st.Posts {
			if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil {
				if t.After(latest) {
					latest = t
				}
			}
		}
		if !latest.IsZero() && manila.Sub(latest) < opt.minGap {
			fmt.Fprintf(os.Stderr, "fbweekly: auto skip: last post %s ago (< %s)\n", manila.Sub(latest).Truncate(time.Second), opt.minGap)
			return 0
		}
		// Auto + dry-run: stage frozen bytes locally, never post.
		if opt.dryRun {
			txtPath, pngPath, rc := writeFrozenDryRun(opt, frozen)
			if rc != 0 {
				return rc
			}
			fmt.Fprintf(os.Stderr, "fbweekly: auto selected week=%s (dry-run, not posted)\n", opt.week)
			fmt.Printf("dry-run: %s %s\n", txtPath, pngPath)
			return 0
		}
		// Post the frozen page.
		return fbweeklyPost(frozen.doc.Text, frozen.png, opt, nil)
	}

	// Manual mode: frozen page wins when present, else compute fresh.
	var text string
	var pngPath string
	var sigData []byte
	if frozen != nil {
		text = frozen.doc.Text
	} else {
		sig, err := weekly.Compute(db, opt.week)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		ok, err := weekly.Reportable(db, opt.week)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "fbweekly: week %s not reportable (< 3 covered dates this or last week)\n", opt.week)
			return 1
		}
		text = weekly.RenderWeekly(sig)
		rep := weekly.CheckWeekly(sig, text)
		img, err := fbchart.RenderWeekly(sig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		// Write files.
		out := fbweeklyOutDir(opt)
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		txtPath := filepath.Join(out, "weekly.txt")
		pngPath = filepath.Join(out, "weekly.png")
		sigPath := filepath.Join(out, "weekly.signals.json")
		checkPath := filepath.Join(out, "weekly.check.json")
		if err := os.WriteFile(txtPath, []byte(text+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		pngF, err := os.Create(pngPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if err := png.Encode(pngF, img); err != nil {
			pngF.Close()
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		pngF.Close()
		sigData, err = json.MarshalIndent(sig, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(sigPath, append(sigData, '\n'), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		checkData, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(checkPath, append(checkData, '\n'), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if rep.Status != weekly.StatusPass {
			fmt.Fprintf(os.Stderr, "fbweekly: check %s (flagged=%d lint=%d), no post\n",
				rep.Status, len(rep.Flagged), len(rep.Lint))
			return 3
		}
		if opt.dryRun {
			fmt.Printf("dry-run: %s %s %s %s\n", txtPath, pngPath, sigPath, checkPath)
			return 0
		}
	}
	if opt.dryRun && frozen != nil {
		txtPath, pngPath, rc := writeFrozenDryRun(opt, frozen)
		if rc != 0 {
			return rc
		}
		fmt.Printf("dry-run: %s %s\n", txtPath, pngPath)
		return 0
	}
	if frozen != nil {
		// Stage the frozen PNG under the out dir for the uploader.
		out := fbweeklyOutDir(opt)
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		pngPath = filepath.Join(out, "weekly.png")
		if err := os.WriteFile(pngPath, frozen.png, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
		if err := os.WriteFile(filepath.Join(out, "weekly.txt"), []byte(text+"\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
			return 1
		}
	}
	return fbweeklyPost(text, nil, opt, &pngPath)
}

// fbweeklyPost handles disable/state/credentials/upload for one weekly post.
// pngData uploads the bytes directly; when nil the file at *pngPath is read.
func fbweeklyPost(text string, pngData []byte, opt fbweeklyOptions, pngPath *string) int {
	disPath := opt.disableFile
	if disPath == "" {
		disPath = filepath.Join(opt.presyoHome, ".config", "presyo", "fbpost.disabled")
	}
	if _, err := os.Stat(disPath); err == nil {
		fmt.Fprintf(os.Stderr, "fbweekly: disabled (%s exists), skip\n", disPath)
		return 0
	}
	st, err := loadFBState(opt.statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", err.Error())
		return 1
	}
	for _, p := range st.Posts {
		if p.Region == "weekly" && p.AsOf == opt.week {
			fmt.Fprintf(os.Stderr, "fbweekly: already posted (week %s post_id=%s), skip\n", opt.week, p.PostID)
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
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", rerr.Error())
		return 1
	}
	if v := os.Getenv("PRESYO_FB_PAGE_ID"); v != "" {
		pageID = v
	}
	if v := os.Getenv("PRESYO_FB_PAGE_TOKEN"); v != "" {
		token = v
	}
	if pageID == "" || token == "" {
		fmt.Fprintf(os.Stderr, "fbweekly: error: missing PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN\n")
		return 1
	}
	scrub := func(s string) string {
		if token == "" {
			return s
		}
		return strings.ReplaceAll(s, token, "[redacted]")
	}
	// Stage bytes to a temp file for the shared uploader.
	path := ""
	if pngPath != nil {
		path = *pngPath
	}
	if pngData != nil {
		tmp, err := os.CreateTemp("", "fbweekly-*.png")
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", scrub(err.Error()))
			return 1
		}
		tmpName := tmp.Name()
		if _, err := tmp.Write(pngData); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", scrub(err.Error()))
			return 1
		}
		tmp.Close()
		defer os.Remove(tmpName)
		path = tmpName
	}
	postID, photoID, gerr := postPhoto(opt.graphBase, pageID, token, text, path)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", scrub(gerr.Error()))
		return 1
	}
	st.Posts = append(st.Posts, fbPostEntry{
		Region: "weekly", AsOf: opt.week, PostID: postID, PhotoID: photoID,
		PostedAt: opt.now.In(manilaZone).Format(time.RFC3339),
	})
	if err := saveFBState(opt.statePath, st); err != nil {
		fmt.Fprintf(os.Stderr, "fbweekly: error: %s\n", scrub(err.Error()))
		return 1
	}
	fmt.Printf("fbweekly: posted week=%s post_id=%s\n", opt.week, postID)
	return 0
}
