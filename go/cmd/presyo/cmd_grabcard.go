package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

// grabcard exit codes.
const (
	grabcardCheckFailed = 3
	grabcardNoRun       = 4
)

type grabcardDropped struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// cmdGrabcard renders the Grab fare Facebook card + caption for review.
// DRY RUN ONLY: there is no posting code, no state file and no FB token.
func cmdGrabcard(argv []string) int {
	fs := flag.NewFlagSet("grabcard", flag.ContinueOnError)
	outDir := fs.String("out-dir", "", "output directory (required)")
	apiURLFlag := fs.String("api-url", "", "API base URL (or PRESYO_GRAB_API_URL)")
	tokenEnv := fs.String("token-env", "PRESYO_GRAB_API_TOKEN", "env var name holding the API token")
	envFile := fs.String("env-file", "", "KEY=VALUE env file (no default path)")
	hours := fs.Int("hours", 50, "hours window for the recent-runs request (covers yesterday for the hourly chart)")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	inputFile := fs.String("input-file", "", "read API JSON from disk instead of fetching")
	mapsDir := fs.String("maps-dir", "", "with -input-file: directory with {ts}_{kind}.webp files")
	maxAgeFlag := fs.String("max-age", "3h", "max age of the run behind the card")
	staleAfterFlag := fs.String("stale-after", "3h", "freshness window")
	timeoutFlag := fs.String("timeout", "20s", "fetch timeout")
	sample := fs.Bool("sample", false, "draw the SAMPLE (trial data) banner")
	noMaps := fs.Bool("no-maps", false, "fare-only card: ignore map images")
	auto := fs.Bool("auto", false, "hourly auto mode: gates, window, tick stage, gaps, post (or -dry-run)")
	dryRun := fs.Bool("dry-run", false, "auto: render and decide only; never posts, never writes the post state")
	simRecord := fs.Bool("sim-record", false, "auto+dry-run: record a dry-run entry in -state (simulation only; use a scratch state file)")
	statePath := fs.String("state", "", "auto: post state file (default $PRESYO_HOME/state/fbpost.json)")
	fbEnvFile := fs.String("fb-env-file", "", "auto: env file with PRESYO_FB_PAGE_ID/PRESYO_FB_PAGE_TOKEN (default $PRESYO_HOME/.config/presyo/fb.env)")
	disableFile := fs.String("disable-file", "", "auto: skip when this file exists (default $PRESYO_HOME/.config/presyo/fbpost.disabled)")
	graphBase := fs.String("graph-base", "https://graph.facebook.com/v21.0", "auto: Graph API base URL (tests use httptest)")
	window := fs.String("window", "", "auto: posting window HH:MM-HH:MM Manila (default $PRESYO_GRAB_POST_WINDOW or 05:00-22:00)")
	minGrabGap := fs.Duration("min-grab-gap", 50*time.Minute, "auto: minimum spacing between two Grab posts")
	otherGap := fs.Duration("other-gap", 15*time.Minute, "auto: skip the hour when any other post is newer than this")
	gapGrace := fs.Duration("gap-grace", 5*time.Minute, "auto: min-grab-gap skips only when since < gap - grace")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *outDir == "" && !*auto {
		fmt.Fprintln(os.Stderr, "usage: presyo grabcard -out-dir DIR [-api-url URL] [-token-env NAME] [-env-file FILE] [-hours 50] [-now RFC3339] [-input-file FILE -maps-dir DIR] [-max-age 3h] [-stale-after 3h] [-sample] [-no-maps]")
		return 1
	}
	fail := func(msg string) int {
		fmt.Fprintln(os.Stderr, "grabcard: error: "+msg)
		return 1
	}
	maxAge, err := time.ParseDuration(*maxAgeFlag)
	if err != nil {
		return fail("bad -max-age")
	}
	staleAfter, err := time.ParseDuration(*staleAfterFlag)
	if err != nil {
		return fail("bad -stale-after")
	}
	timeout, err := time.ParseDuration(*timeoutFlag)
	if err != nil {
		return fail("bad -timeout")
	}
	now := time.Now()
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			return fail("bad -now")
		}
		now = t
	}
	p := grabcardParams{
		outDir: *outDir, apiURL: *apiURLFlag, tokenEnv: *tokenEnv, envFile: *envFile,
		hours: *hours, now: now, inputFile: *inputFile, mapsDir: *mapsDir,
		maxAge: maxAge, staleAfter: staleAfter, timeout: timeout,
		sample: *sample, noMaps: *noMaps,
	}
	if *auto {
		return grabAuto(p, grabAutoOptions{
			dryRun: *dryRun, simRecord: *simRecord, statePath: *statePath,
			fbEnvFile: *fbEnvFile, disableFile: *disableFile, graphBase: *graphBase,
			window: *window, minGrabGap: *minGrabGap, otherGap: *otherGap, gapGrace: *gapGrace,
			presyoHome: defaultPresyoHome(),
		})
	}
	_, code := grabcardRender(p)
	return code
}

// grabcardParams configures one render.
type grabcardParams struct {
	outDir, apiURL, tokenEnv, envFile string
	hours                             int
	now                               time.Time
	inputFile, mapsDir                string
	maxAge, staleAfter, timeout       time.Duration
	sample, noMaps                    bool
	// currentHour: only a run captured in the current Manila hour may be used
	// (auto mode: never post an older reading as this hour's).
	currentHour bool
}

// grabcardResult describes a rendered card.
type grabcardResult struct {
	RunTs    string
	Maps     []string
	Dropped  []grabcardDropped
	Caption  string
	PNGPath  string
	TxtPath  string
	FareOnly bool
	Focus    bool
}

// grabcardRender fetches the data, picks the run and writes the card files.
// Exit codes: 0 ok, 1 error, 3 check failed, 4 no eligible run.
func grabcardRender(p grabcardParams) (*grabcardResult, int) {
	fail := func(msg string) (*grabcardResult, int) {
		fmt.Fprintln(os.Stderr, "grabcard: error: "+msg)
		return nil, 1
	}
	now, maxAge, staleAfter, timeout := p.now, p.maxAge, p.staleAfter, p.timeout
	// Source of the data and of the images.
	var payload []byte
	var fetchImage func(ts, kind string) ([]byte, error)
	if p.inputFile != "" {
		data, err := os.ReadFile(p.inputFile)
		if err != nil {
			return fail("cannot read input")
		}
		payload = data
		fetchImage = func(ts, kind string) ([]byte, error) {
			if p.mapsDir == "" {
				return nil, fmt.Errorf("no maps dir")
			}
			return os.ReadFile(filepath.Join(p.mapsDir, ts+"_"+kind+".webp"))
		}
	} else {
		cfg, err := loadGrabConfig(p.apiURL, p.tokenEnv, p.envFile)
		if err != nil {
			return fail("missing configuration")
		}
		fmt.Fprintln(os.Stderr, "fetching grab fare data")
		data, err := grabfare.FetchGET(cfg.url, p.hours, cfg.token, timeout)
		if err != nil {
			return fail("fetch failed")
		}
		payload = data
		client := &http.Client{Timeout: timeout}
		fetchImage = func(ts, kind string) ([]byte, error) {
			u := strings.TrimSuffix(cfg.url, "/") + "/v1/grab/map/" + ts + "/" + kind + ".webp"
			req, err := http.NewRequest("GET", u, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+cfg.token)
			req.Header.Set("User-Agent", grabfare.UserAgent)
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("image fetch failed")
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("image status %d", resp.StatusCode)
			}
			return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		}
	}
	doc, err := grabfare.ParseInput(payload)
	if err != nil {
		return fail("bad payload")
	}

	// Choose the run (newest first) and its clean map images.
	var (
		chosen  *grabfare.Candidate
		kinds   []string
		images  []image.Image
		dropped []grabcardDropped
		focus   image.Image
	)
	for _, cand := range grabfare.SelectRuns(doc, now, maxAge, !p.noMaps) {
		if p.currentHour && !sameManilaHour(cand.Captured, now) {
			continue
		}
		if p.noMaps {
			c := cand
			chosen = &c
			break
		}
		clean := map[string]image.Image{}
		var drop []grabcardDropped
		var focusImg image.Image
		for _, kind := range cand.Run.MapImages {
			raw, err := fetchImage(cand.Ts, kind)
			if err != nil {
				drop = append(drop, grabcardDropped{kind, "fetch-failed"})
				continue
			}
			img, err := grabfare.DecodeWebp(raw)
			if err != nil {
				drop = append(drop, grabcardDropped{kind, "decode-failed"})
				continue
			}
			if grabfare.HasDeviceMarker(img) {
				drop = append(drop, grabcardDropped{kind, "device-marker"})
				continue
			}
			if kind == "focus" {
				// the wide Colours/SM Southmall band: already cropped
				// (no top cut), shown as the main image
				focusImg = img
				continue
			}
			clean[kind] = img
		}
		picked := grabfare.PickMaps(clean)
		if len(picked) < 2 && focusImg == nil {
			continue
		}
		c := cand
		chosen, kinds, dropped, focus = &c, picked, drop, focusImg
		for _, k := range picked {
			images = append(images, clean[k])
		}
		break
	}
	if chosen == nil {
		fmt.Fprintln(os.Stderr, "grabcard: no eligible run")
		return nil, grabcardNoRun
	}

	o := grabfare.BuildFromRun(doc, now, staleAfter, chosen.Ts)
	o.Focus = focus != nil
	img, err := fbchart.RenderGrabFareFocus(o, images, focus, p.sample)
	if err != nil {
		return fail("render failed")
	}
	caption := grabfare.RenderCaption(o)
	problems := grabfare.CheckCaption(caption, o)
	if problems == nil {
		problems = []string{}
	}
	if err := os.MkdirAll(p.outDir, 0o755); err != nil {
		return fail("cannot write output")
	}
	writeJSON := func(name string, v any) error {
		data, _ := json.MarshalIndent(v, "", "  ")
		return writeFileAtomic(filepath.Join(p.outDir, name), append(data, '\n'))
	}
	if err := writeJSON("grab-card.check.json", map[string]any{"ok": len(problems) == 0, "problems": problems}); err != nil {
		return fail("cannot write output")
	}
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "grabcard: check failed")
		return nil, grabcardCheckFailed
	}
	// Full-colour PNG (not the palette-quantised EncodeCompact): the real
	// map screenshots must keep their colours.
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return fail("encode failed")
	}
	if err := writeFileAtomic(filepath.Join(p.outDir, "grab-card.png"), buf.Bytes()); err != nil {
		return fail("cannot write output")
	}
	if err := writeFileAtomic(filepath.Join(p.outDir, "grab-card.txt"), []byte(caption+"\n")); err != nil {
		return fail("cannot write output")
	}

	trafficReason := "absent"
	if chosen.Run.Traffic != nil {
		if ok, why := grabfare.TrafficSane(chosen.Run.Traffic); ok {
			trafficReason = "ok"
		} else {
			trafficReason = why
		}
	}
	if kinds == nil {
		kinds = []string{}
	}
	if dropped == nil {
		dropped = []grabcardDropped{}
	}
	asOf := ""
	if o.AsOf != nil {
		asOf = *o.AsOf
	}
	meta := map[string]any{
		"run_ts":       chosen.Ts,
		"as_of":        asOf,
		"sample":       p.sample,
		"maps":         kinds,
		"focus":        focus != nil,
		"dropped_maps": dropped,
		"traffic":      map[string]any{"included": o.Traffic != nil, "reason": trafficReason},
	}
	if err := writeJSON("grab-card.json", meta); err != nil {
		return fail("cannot write output")
	}
	fmt.Printf("grabcard: wrote %s\n", filepath.Join(p.outDir, "grab-card.png"))
	res := &grabcardResult{
		RunTs: chosen.Ts, Maps: kinds, Dropped: dropped, Caption: caption,
		PNGPath: filepath.Join(p.outDir, "grab-card.png"), TxtPath: filepath.Join(p.outDir, "grab-card.txt"),
		FareOnly: p.noMaps,
		Focus:    focus != nil,
	}
	return res, 0
}

type grabConfig struct{ url, token string }

// loadGrabConfig resolves the API URL and token from flags, the env file or
// the environment (never hardcoded, never printed).
func loadGrabConfig(urlFlag, tokenEnv, envFile string) (grabConfig, error) {
	envMap := map[string]string{}
	if envFile != "" {
		m, err := readEnvFile(envFile)
		if err != nil {
			return grabConfig{}, err
		}
		envMap = m
	}
	pick := func(key string) string {
		if v := envMap[key]; v != "" {
			return v
		}
		return os.Getenv(key)
	}
	url := urlFlag
	if url == "" {
		url = pick("PRESYO_GRAB_API_URL")
	}
	if tokenEnv == "" {
		tokenEnv = "PRESYO_GRAB_API_TOKEN"
	}
	token := pick(tokenEnv)
	if url == "" || token == "" {
		return grabConfig{}, fmt.Errorf("missing configuration")
	}
	return grabConfig{url: url, token: token}, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".grabcard-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func sameManilaHour(a, b time.Time) bool {
	a, b = a.In(manilaZone), b.In(manilaZone)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay() && a.Hour() == b.Hour()
}
