package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// grabRegion is the post-state region of the hourly Grab fare post.
// Grab posts are exempt from every other command's min-gap ("last post")
// rule, see lastNonGrabPost.
const grabRegion = "grab"

// Auto-mode defaults (GRABCARD_SPEC.md, "Hourly auto post").
const (
	grabDefaultWindow   = "05:00-22:00"
	grabAutoMaxAge      = 40 * time.Minute
	grabTickFirst       = 10 // :10 first try (maps)
	grabTickFareOnlyAt  = 20 // from :20 a fare-only card is allowed
	grabTickLast        = 30 // exclusive: after :29 the hour is skipped
	grabAutoGateEnv     = "PRESYO_GRAB_FB"
	grabAutoWindowEnv   = "PRESYO_GRAB_POST_WINDOW"
	grabAutoNoFreshExit = grabcardNoRun
)

type grabAutoOptions struct {
	dryRun      bool
	simRecord   bool
	statePath   string
	fbEnvFile   string
	disableFile string
	graphBase   string
	window      string
	minGrabGap  time.Duration
	otherGap    time.Duration
	gapGrace    time.Duration
	presyoHome  string
}

// isGrabPost reports whether a state entry is an hourly Grab post.
func isGrabPost(p fbPostEntry) bool { return p.Region == grabRegion }

// lastNonGrabPost returns the newest post time over every entry that is not
// a Grab post. Every "last post" min-gap rule uses it, so the hourly Grab
// posts can never starve the DA digest, utilities or weekly post.
func lastNonGrabPost(st *fbPostState) time.Time {
	var latest time.Time
	for _, p := range st.Posts {
		if isGrabPost(p) {
			continue
		}
		if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

func parseWindow(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("bad window %q (want HH:MM-HH:MM)", s)
	}
	start, err := parseHHMM(strings.TrimSpace(a))
	if err != nil {
		return 0, 0, err
	}
	end, err := parseHHMM(strings.TrimSpace(b))
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// grabAuto is `grabcard -auto`: one cron tick of the hourly Grab post.
//
//	:10-:19  card with maps from the CURRENT hour's run; no fresh maps -> exit 4, wait
//	:20-:29  maps if they have appeared, else a fare-only card of the current hour
//	:30-     skip the hour (never a stale reading)
//
// At most one Grab post per Manila hour (state entry region "grab", as_of
// "YYYY-MM-DDTHH"), >= min-grab-gap since the previous Grab post, and the hour
// is skipped while another post is younger than other-gap.
func grabAuto(p grabcardParams, o grabAutoOptions) int {
	manila := p.now.In(manilaZone)
	skip := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "grabcard: auto skip: "+format+"\n", args...)
		return 0
	}
	if !o.dryRun && os.Getenv(grabAutoGateEnv) != "1" {
		return skip("%s is not 1 (Grab FB posting is off)", grabAutoGateEnv)
	}
	win := o.window
	if win == "" {
		win = os.Getenv(grabAutoWindowEnv)
	}
	if win == "" {
		win = grabDefaultWindow
	}
	start, end, err := parseWindow(win)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grabcard: error: %s\n", err.Error())
		return 1
	}
	tod := manila.Hour()*60 + manila.Minute()
	if !(start <= tod && tod < end) {
		return skip("outside posting window %s (now %02d:%02d Manila)", win, manila.Hour(), manila.Minute())
	}
	min := manila.Minute()
	if min < grabTickFirst || min >= grabTickLast {
		return skip("outside the :%02d-:%02d tick minutes (now :%02d)", grabTickFirst, grabTickLast-1, min)
	}
	disPath := o.disableFile
	if disPath == "" {
		disPath = filepath.Join(o.presyoHome, ".config", "presyo", "fbpost.disabled")
	}
	if _, err := os.Stat(disPath); err == nil {
		return skip("disabled (%s exists)", disPath)
	}
	if !o.dryRun && p.sample {
		fmt.Fprintln(os.Stderr, "grabcard: error: -sample is not allowed when posting")
		return 1
	}
	stPath := o.statePath
	if stPath == "" {
		stPath = filepath.Join(o.presyoHome, "state", "fbpost.json")
	}
	body := func() int { return grabAutoBody(p, o, stPath, manila, min, skip) }
	if o.dryRun {
		return body()
	}
	return withStateLock(stPath, body)
}

func grabAutoBody(p grabcardParams, o grabAutoOptions, stPath string, manila time.Time, min int, skip func(string, ...any) int) int {
	st, err := loadFBState(stPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grabcard: error: %s\n", err.Error())
		return 1
	}
	hourKey := manila.Format("2006-01-02T15")
	var lastGrab time.Time
	for _, e := range st.Posts {
		if !isGrabPost(e) {
			continue
		}
		if e.AsOf == hourKey {
			return skip("already posted for %s (post_id=%s)", hourKey, e.PostID)
		}
		if t, err := time.Parse(time.RFC3339, e.PostedAt); err == nil && t.After(lastGrab) {
			lastGrab = t
		}
	}
	if !lastGrab.IsZero() && manila.Sub(lastGrab) < o.minGrabGap-o.gapGrace {
		return skip("last Grab post %s ago (< %s)", manila.Sub(lastGrab).Truncate(time.Second), o.minGrabGap)
	}
	if lastOther := lastNonGrabPost(st); !lastOther.IsZero() && manila.Sub(lastOther) < o.otherGap {
		return skip("another post %s ago (< %s), skipping this tick", manila.Sub(lastOther).Truncate(time.Second), o.otherGap)
	}

	// Render: current hour's data only, never older than 40 min.
	if p.maxAge > grabAutoMaxAge {
		p.maxAge = grabAutoMaxAge
	}
	if p.staleAfter > grabAutoMaxAge {
		p.staleAfter = grabAutoMaxAge
	}
	p.currentHour = true
	if p.outDir == "" {
		p.outDir = filepath.Join(o.presyoHome, "grabcard", manila.Format("20060102-15"))
	}
	forcedFareOnly := p.noMaps
	res, code := grabcardRender(p)
	if code == grabcardNoRun && !forcedFareOnly {
		if min < grabTickFareOnlyAt {
			fmt.Fprintf(os.Stderr, "grabcard: auto: no fresh maps for this hour yet, waiting for the next tick\n")
			return grabAutoNoFreshExit
		}
		fmt.Fprintf(os.Stderr, "grabcard: auto: no fresh maps, falling back to the fare-only card\n")
		p.noMaps = true
		res, code = grabcardRender(p)
	}
	if code != 0 {
		if code == grabcardNoRun {
			fmt.Fprintf(os.Stderr, "grabcard: auto: no fare for this hour, skipping the hour\n")
		}
		return code
	}

	if o.dryRun {
		fmt.Printf("dry-run: would post grab %s run=%s fare_only=%v maps=%v png=%s\n", hourKey, res.RunTs, res.FareOnly, res.Maps, res.PNGPath)
		if o.simRecord {
			st.Posts = append(st.Posts, fbPostEntry{Region: grabRegion, AsOf: hourKey, PostID: "dry-run",
				PostedAt: manila.Format(time.RFC3339)})
			if err := saveFBState(stPath, st); err != nil {
				fmt.Fprintf(os.Stderr, "grabcard: error: %s\n", err.Error())
				return 1
			}
		}
		return 0
	}

	pageID, token, ok := loadFBCreds(o.fbEnvFile, o.presyoHome)
	if !ok {
		return 1
	}
	scrub := func(s string) string { return strings.ReplaceAll(s, token, "[redacted]") }
	postID, photoID, gerr := postPhoto(o.graphBase, pageID, token, res.Caption, res.PNGPath)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "grabcard: error: %s\n", scrub(gerr.Error()))
		return 1
	}
	st.Posts = append(st.Posts, fbPostEntry{
		Region: grabRegion, AsOf: hourKey, PostID: postID, PhotoID: photoID,
		PostedAt: manila.Format(time.RFC3339),
	})
	if err := saveFBState(stPath, st); err != nil {
		fmt.Fprintf(os.Stderr, "grabcard: error: %s\n", scrub(err.Error()))
		return 1
	}
	fmt.Printf("grabcard: posted grab=%s post_id=%s fare_only=%v maps=%v\n", hourKey, postID, res.FareOnly, res.Maps)
	return 0
}

// loadFBCreds reads PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN from the env file
// and the environment (environment wins). Never prints them.
func loadFBCreds(envFile, presyoHome string) (pageID, token string, ok bool) {
	envPath := envFile
	if envPath == "" {
		envPath = filepath.Join(presyoHome, ".config", "presyo", "fb.env")
	}
	if data, err := readEnvFile(envPath); err == nil {
		pageID, token = data["PRESYO_FB_PAGE_ID"], data["PRESYO_FB_PAGE_TOKEN"]
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "grabcard: error: cannot read the FB env file\n")
		return "", "", false
	}
	if v := os.Getenv("PRESYO_FB_PAGE_ID"); v != "" {
		pageID = v
	}
	if v := os.Getenv("PRESYO_FB_PAGE_TOKEN"); v != "" {
		token = v
	}
	if pageID == "" || token == "" {
		fmt.Fprintln(os.Stderr, "grabcard: error: missing PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN")
		return "", "", false
	}
	return pageID, token, true
}
