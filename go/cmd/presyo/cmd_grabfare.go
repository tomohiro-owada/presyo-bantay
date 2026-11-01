package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/grabfare"
)

func cmdGrabfare(argv []string) int {
	fs := flag.NewFlagSet("grabfare", flag.ContinueOnError)
	outDir := fs.String("out-dir", "", "output directory (required)")
	apiURLFlag := fs.String("api-url", "", "API base URL (or PRESYO_GRAB_API_URL)")
	tokenEnv := fs.String("token-env", "PRESYO_GRAB_API_TOKEN", "env var name holding the API token")
	envFile := fs.String("env-file", "", "KEY=VALUE env file (no default path)")
	hours := fs.Int("hours", 50, "hours window for the recent-runs request (covers yesterday for the hourly chart)")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	inputFile := fs.String("input-file", "", "read API JSON from disk instead of fetching")
	staleAfterFlag := fs.String("stale-after", "2h", "freshness window (Go duration)")
	timeoutFlag := fs.String("timeout", "20s", "fetch timeout (Go duration)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo grabfare -out-dir DIR [-api-url URL] [-token-env NAME] [-env-file FILE] [-hours 26] [-now RFC3339] [-input-file FILE] [-stale-after 2h] [-timeout 20s]")
		return 1
	}
	staleAfter, err := time.ParseDuration(*staleAfterFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grabfare: error: bad -stale-after")
		return 1
	}
	timeout, err := time.ParseDuration(*timeoutFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grabfare: error: bad -timeout")
		return 1
	}
	now := time.Now()
	if *nowFlag != "" {
		t, err := time.Parse(time.RFC3339, *nowFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "grabfare: error: bad -now")
			return 1
		}
		now = t
	}
	fail := func() int {
		// On fetch/parse failure never leave an old card looking
		// current: when the existing card is expired, rewrite it as
		// the hidden/stale state.
		if data, rerr := os.ReadFile(filepath.Join(*outDir, "grab-fare.json")); rerr == nil {
			if st := grabfare.ExpireForFailure(data, now); st != nil {
				_ = writeGrabfareOutputs(*outDir, st)
			}
		}
		return 1
	}

	var payload []byte
	if *inputFile != "" {
		data, err := os.ReadFile(*inputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "grabfare: error: cannot read input")
			return fail()
		}
		payload = data
	} else {
		// Config comes only from flags/env/env-file; never hardcoded.
		envMap := map[string]string{}
		if *envFile != "" {
			m, err := readEnvFile(*envFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "grabfare: error: cannot read config")
				return fail()
			}
			envMap = m
		}
		apiURL := *apiURLFlag
		if apiURL == "" {
			if v, ok := envMap["PRESYO_GRAB_API_URL"]; ok && v != "" {
				apiURL = v
			} else {
				apiURL = os.Getenv("PRESYO_GRAB_API_URL")
			}
		}
		name := *tokenEnv
		if name == "" {
			name = "PRESYO_GRAB_API_TOKEN"
		}
		token := ""
		if v, ok := envMap[name]; ok && v != "" {
			token = v
		} else {
			token = os.Getenv(name)
		}
		if apiURL == "" || token == "" {
			fmt.Fprintln(os.Stderr, "grabfare: error: missing configuration")
			return fail()
		}
		fmt.Fprintln(os.Stderr, "fetching grab fare data")
		data, err := grabfare.FetchGET(apiURL, *hours, token, timeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, "grabfare: error: fetch failed")
			return fail()
		}
		payload = data
	}

	doc, err := grabfare.ParseInput(payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "grabfare: error: bad payload")
		return fail()
	}
	out := grabfare.Build(doc, now, staleAfter)
	if err := writeGrabfareOutputs(*outDir, out); err != nil {
		fmt.Fprintln(os.Stderr, "grabfare: error: cannot write output")
		return 1
	}
	fmt.Printf("grabfare: wrote %s\n", filepath.Join(*outDir, "grab-fare.json"))
	return 0
}

// writeGrabfareOutputs writes grab-fare.json, grab-fare.html and
// grab-fare-preview.html atomically (temp file + rename).
func writeGrabfareOutputs(dir string, out *grabfare.Output) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	jsonData := grabfare.MarshalJSONDoc(out)
	snippet := grabfare.RenderSnippet(out)
	preview := grabfare.RenderPreview(out, snippet)
	files := map[string][]byte{
		"grab-fare.json":         jsonData,
		"grab-fare.html":         []byte(snippet),
		"grab-fare-preview.html": []byte(preview),
	}
	for name, data := range files {
		dst := filepath.Join(dir, name)
		tmp, err := os.CreateTemp(dir, ".grabfare-*.tmp")
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
		if err := os.Rename(tmpName, dst); err != nil {
			os.Remove(tmpName)
			return err
		}
	}
	return nil
}
