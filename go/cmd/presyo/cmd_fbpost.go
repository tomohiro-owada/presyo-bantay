package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regionrender"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// manilaZone is Asia/Manila (UTC+8, no DST) as a fixed zone so the static
// binary needs no system tzdata.
var manilaZone = time.FixedZone("Asia/Manila", 8*3600)

// fbPostEntry is one recorded Facebook post in the state file.
type fbPostEntry struct {
	Region   string `json:"region"`
	AsOf     string `json:"as_of"`
	PostID   string `json:"post_id"`
	PhotoID  string `json:"photo_id"`
	PostedAt string `json:"posted_at"`
}

// fbPostState is the state file document.
type fbPostState struct {
	Posts []fbPostEntry `json:"posts"`
}

func loadFBState(path string) (*fbPostState, error) {
	st := &fbPostState{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	if st.Posts == nil {
		st.Posts = []fbPostEntry{}
	}
	return st, nil
}

// saveFBState writes the state file atomically (temp file + rename).
func saveFBState(path string, st *fbPostState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fbpost-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// readEnvFile parses a KEY=VALUE env file (comments/blank lines ignored).
func readEnvFile(path string) (map[string]string, error) {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if k != "" {
			out[k] = v
		}
	}
	return out, nil
}

func cmdFBPost(argv []string) int {
	fs := flag.NewFlagSet("fbpost", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	regionID := fs.String("region", "", "region id: south|manila|qc|north|east (required)")
	dateFlag := fs.String("date", "", "as-of date YYYY-MM-DD (default: latest price date)")
	outDir := fs.String("out-dir", "", "output directory (default $PRESYO_HOME/fbpost/<date>/ or ./fbpost/<date>)")
	dryRun := fs.Bool("dry-run", false, "render and check only; no network, token not needed")
	statePath := fs.String("state", "", "state file (default $PRESYO_HOME/state/fbpost.json)")
	envFile := fs.String("env-file", "", "env file with PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN (default $PRESYO_HOME/.config/presyo/fb.env)")
	maxAge := fs.Int("max-age-days", 2, "skip posting when as-of is older than this many days vs today in Asia/Manila")
	disableFile := fs.String("disable-file", "", "skip posting when this file exists (default $PRESYO_HOME/.config/presyo/fbpost.disabled)")
	graphBase := fs.String("graph-base", "https://graph.facebook.com/v21.0", "Graph API base URL (tests use httptest)")
	useLLM := fs.Bool("llm", false, "rewrite the template text with opencode (falls back to template when the check fails)")
	autoMode := fs.Bool("auto", false, "decide which region is due (at most one post per run)")
	windowStart := fs.String("window-start", "05:30", "auto posting window start HH:MM Asia/Manila")
	windowEnd := fs.String("window-end", "20:00", "auto posting window end HH:MM Asia/Manila (exclusive)")
	minGap := fs.Duration("min-gap", 2*time.Hour, "auto: skip when the last successful post is newer than this")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *regionID == "" && !*autoMode {
		fmt.Fprintln(os.Stderr, "usage: presyo fbpost -db DB -region south|manila|qc|north|east [-date ...] [-out-dir ...] [-dry-run]")
		fmt.Fprintln(os.Stderr, "   or: presyo fbpost -db DB -auto [-dry-run] [-now RFC3339] [-window-start HH:MM] [-window-end HH:MM] [-min-gap 2h]")
		return 1
	}
	presyoHome := os.Getenv("PRESYO_HOME")
	if presyoHome == "" {
		presyoHome = "$HOME"
	}

	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	defer db.Close()

	// -auto: decide which region is due (at most one post per run).
	if *autoMode {
		next, asOf, ok := autoSelect(db, autoOptions{
			nowFlag: *nowFlag, windowStart: *windowStart,
			windowEnd: *windowEnd, minGap: *minGap,
			statePath: *statePath, presyoHome: presyoHome,
		})
		if !ok {
			return 0 // skip reason already logged
		}
		if next == "" {
			return 0
		}
		*regionID = next
		*dateFlag = asOf
	}

	// 1. Region signals.
	sig, err := signals.ComputeRegion(db, *regionID, signals.RegionOptions{AsOf: *dateFlag})
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}

	// 2. Staleness: as-of older than max-age-days vs today in Manila.
	asOfT, err := time.Parse("2006-01-02", sig.AsOf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: bad as-of %q\n", sig.AsOf)
		return 1
	}
	today := time.Now().In(manilaZone).Truncate(24 * time.Hour)
	_ = today
	nowManila := time.Now().In(manilaZone)
	todayDate := time.Date(nowManila.Year(), nowManila.Month(), nowManila.Day(), 0, 0, 0, 0, manilaZone)
	ageDays := int(todayDate.Sub(asOfT).Hours() / 24)
	if ageDays > *maxAge {
		fmt.Fprintf(os.Stderr, "fbpost: stale data, skip (as-of %s, %d days old)\n", sig.AsOf, ageDays)
		return 0
	}

	// 3. Render text + PNG, run the checker (shared with daypages).
	text, img, rep, rerr := regionrender.Render(sig)
	if rerr != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", rerr.Error())
		return 1
	}
	if *useLLM {
		if rw, rerr := fbregion.RewriteWithLLM(text, advice.DefaultModel, "opencode", 10*time.Minute); rerr != nil {
			fmt.Fprintf(os.Stderr, "fbpost: WARNING llm rewrite failed, using template (%s)\n", rerr.Error())
		} else if rep2, cerr := checkRegionPost(sig, rw); cerr == nil && rep2.Status == fbregion.StatusPass {
			text = rw
			rep = rep2
		} else {
			fmt.Fprintf(os.Stderr, "fbpost: WARNING llm rewrite failed the check, using template\n")
		}
	}

	// 4. Write <region>.txt/.png/.signals.json/.check.json.
	out := *outDir
	if out == "" {
		if os.Getenv("PRESYO_HOME") == "" {
			out = filepath.Join("fbpost", sig.AsOf)
		} else {
			out = filepath.Join(presyoHome, "fbpost", sig.AsOf)
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	txtPath := filepath.Join(out, *regionID+".txt")
	pngPath := filepath.Join(out, *regionID+".png")
	sigPath := filepath.Join(out, *regionID+".signals.json")
	checkPath := filepath.Join(out, *regionID+".check.json")
	if err := os.WriteFile(txtPath, []byte(text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	pngF, err := os.Create(pngPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	if err := png.Encode(pngF, img); err != nil {
		pngF.Close()
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	pngF.Close()
	sigData, err := json.MarshalIndent(sig, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(sigPath, append(sigData, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	checkData, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(checkPath, append(checkData, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	if rep.Status != fbregion.StatusPass {
		fmt.Fprintf(os.Stderr, "fbpost: check %s (flagged=%d lint=%d), no post\n",
			rep.Status, len(rep.Flagged), len(rep.Lint))
		for _, f := range rep.Flagged {
			fmt.Fprintf(os.Stderr, "  flagged [line %d] %q reason=%s\n", f.Line, f.Snippet, f.Reason)
		}
		return 3
	}

	// 5. Dry run: no network, token not needed.
	if *dryRun {
		fmt.Printf("dry-run: %s %s %s %s\n", txtPath, pngPath, sigPath, checkPath)
		return 0
	}

	// 6. Disable file.
	disPath := *disableFile
	if disPath == "" {
		disPath = filepath.Join(presyoHome, ".config", "presyo", "fbpost.disabled")
	}
	if _, err := os.Stat(disPath); err == nil {
		fmt.Fprintf(os.Stderr, "fbpost: disabled (%s exists), skip\n", disPath)
		return 0
	}

	// 7. State dedupe.
	stPath := *statePath
	if stPath == "" {
		stPath = filepath.Join(presyoHome, "state", "fbpost.json")
	}
	st, err := loadFBState(stPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return 1
	}
	for _, p := range st.Posts {
		if p.Region == *regionID && p.AsOf == sig.AsOf {
			fmt.Fprintf(os.Stderr, "fbpost: already posted (region=%s as-of=%s post_id=%s), skip\n",
				p.Region, p.AsOf, p.PostID)
			return 0
		}
	}

	// 8. Credentials: env file then same-named env vars override.
	envPath := *envFile
	if envPath == "" {
		envPath = filepath.Join(presyoHome, ".config", "presyo", "fb.env")
	}
	pageID, token := "", ""
	if data, rerr := readEnvFile(envPath); rerr == nil {
		pageID, token = data["PRESYO_FB_PAGE_ID"], data["PRESYO_FB_PAGE_TOKEN"]
	} else if !os.IsNotExist(rerr) {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", rerr.Error())
		return 1
	}
	if v := os.Getenv("PRESYO_FB_PAGE_ID"); v != "" {
		pageID = v
	}
	if v := os.Getenv("PRESYO_FB_PAGE_TOKEN"); v != "" {
		token = v
	}
	if pageID == "" || token == "" {
		fmt.Fprintf(os.Stderr, "fbpost: error: missing PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN\n")
		return 1
	}
	scrub := func(s string) string {
		if token == "" {
			return s
		}
		return strings.ReplaceAll(s, token, "[redacted]")
	}

	// 9. POST multipart to {graph-base}/{page-id}/photos (one attempt).
	postID, photoID, gerr := postPhoto(*graphBase, pageID, token, text, pngPath)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", scrub(gerr.Error()))
		return 1
	}
	st.Posts = append(st.Posts, fbPostEntry{
		Region: *regionID, AsOf: sig.AsOf, PostID: postID, PhotoID: photoID,
		PostedAt: time.Now().In(manilaZone).Format(time.RFC3339),
	})
	if err := saveFBState(stPath, st); err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", scrub(err.Error()))
		return 1
	}
	fmt.Printf("fbpost: posted region=%s as-of=%s post_id=%s\n", *regionID, sig.AsOf, postID)
	return 0
}

// checkRegionPost adapts fbregion.CheckPost for the llm-rewrite gate.
func checkRegionPost(sig *signals.RegionSignals, text string) (*fbregion.Report, error) {
	return fbregion.CheckPost(sig, text), nil
}

// postPhoto uploads the PNG with the post text. The access token travels
// as a multipart form field, never in the URL. Exactly one attempt.
func postPhoto(graphBase, pageID, token, message, pngPath string) (postID, photoID string, err error) {
	pngData, err := os.ReadFile(pngPath)
	if err != nil {
		return "", "", err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="source"; filename="post.png"`},
		"Content-Type":        []string{"image/png"},
	})
	if err != nil {
		return "", "", err
	}
	if _, err := fw.Write(pngData); err != nil {
		return "", "", err
	}
	for k, v := range map[string]string{
		"message":      message,
		"access_token": token,
		"published":    "true",
	} {
		if err := w.WriteField(k, v); err != nil {
			return "", "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", "", err
	}
	url := strings.TrimSuffix(graphBase, "/") + "/" + pageID + "/photos"
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	respData, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, code := graphError(respData)
		return "", "", fmt.Errorf("graph error: status=%d code=%s message=%s",
			resp.StatusCode, code, msg)
	}
	var ok struct {
		ID     string `json:"id"`
		PostID string `json:"post_id"`
	}
	if err := json.Unmarshal(respData, &ok); err != nil {
		return "", "", fmt.Errorf("graph error: bad success payload: %s", string(respData))
	}
	return ok.PostID, ok.ID, nil
}

// autoOptions tunes autoSelect.
type autoOptions struct {
	nowFlag     string
	windowStart string
	windowEnd   string
	minGap      time.Duration
	statePath   string
	presyoHome  string
}

// parseHHMM parses an "HH:MM" wall time into minutes since midnight.
func parseHHMM(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, fmt.Errorf("bad HH:MM %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 || len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("bad HH:MM %q", s)
	}
	return h*60 + m, nil
}

// autoSelect implements `fbpost -auto`: it picks the next due region for
// the latest data date D. It returns ("", "", false) with a logged reason
// when nothing should be posted, or (region, D, true) when the caller
// should run the normal single-region flow once.
func autoSelect(db *sql.DB, opt autoOptions) (string, string, bool) {
	skip := func(format string, args ...any) (string, string, bool) {
		fmt.Fprintf(os.Stderr, "fbpost: auto skip: "+format+"\n", args...)
		return "", "", false
	}
	now := time.Now()
	if opt.nowFlag != "" {
		t, err := time.Parse(time.RFC3339, opt.nowFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fbpost: error: bad -now %q\n", opt.nowFlag)
			return "", "", false
		}
		now = t
	}
	manila := now.In(manilaZone)

	// D = latest data date in the DB. A newer date automatically takes
	// over: state is keyed by (region, date), so an older date's
	// unfinished regions are simply dropped.
	var d sql.NullString
	if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return "", "", false
	}
	if !d.Valid {
		return skip("no price dates in DB")
	}
	D := d.String
	dT, err := time.Parse("2006-01-02", D)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: bad data date %q\n", D)
		return "", "", false
	}
	today := time.Date(manila.Year(), manila.Month(), manila.Day(), 0, 0, 0, 0, manilaZone)
	if int(today.Sub(dT).Hours()/24) > 2 {
		return skip("data date %s older than 2 days", D)
	}

	// Posting window (start inclusive, end exclusive), Asia/Manila.
	start, err := parseHHMM(opt.windowStart)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return "", "", false
	}
	end, err := parseHHMM(opt.windowEnd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return "", "", false
	}
	tod := manila.Hour()*60 + manila.Minute()
	if !(start <= tod && tod < end) {
		return skip("outside posting window %s-%s (now %02d:%02d Manila)",
			opt.windowStart, opt.windowEnd, manila.Hour(), manila.Minute())
	}

	stPath := opt.statePath
	if stPath == "" {
		stPath = filepath.Join(opt.presyoHome, "state", "fbpost.json")
	}
	st, err := loadFBState(stPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fbpost: error: %s\n", err.Error())
		return "", "", false
	}
	posted := map[string]bool{}
	var latest time.Time
	for _, p := range st.Posts {
		if p.Region != "" && p.AsOf == D {
			posted[p.Region] = true
		}
		if t, err := time.Parse(time.RFC3339, p.PostedAt); err == nil {
			if t.After(latest) {
				latest = t
			}
		}
	}
	next := ""
	for _, id := range regions.IDs() {
		if !posted[id] {
			next = id
			break
		}
	}
	if next == "" {
		return skip("all regions already posted for %s", D)
	}

	// Spacing: at most one post per minGap, no exceptions.
	if !latest.IsZero() && manila.Sub(latest) < opt.minGap {
		return skip("last post %s ago (< %s)", manila.Sub(latest).Truncate(time.Second), opt.minGap)
	}

	fmt.Fprintf(os.Stderr, "fbpost: auto selected region=%s as-of=%s\n", next, D)
	return next, D, true
}

// graphError extracts the Graph API error message/code.
func graphError(data []byte) (message, code string) {
	var doc struct {
		Error struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t := strings.TrimSpace(string(data))
		if len(t) > 200 {
			t = t[:200]
		}
		return t, ""
	}
	if doc.Error.Message == "" {
		t := strings.TrimSpace(string(data))
		if len(t) > 200 {
			t = t[:200]
		}
		return t, ""
	}
	switch c := doc.Error.Code.(type) {
	case float64:
		code = fmt.Sprintf("%.0f", c)
	case string:
		code = c
	default:
		code = ""
	}
	return doc.Error.Message, code
}
