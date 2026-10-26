package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilpages"
)

// cmdUtilpages publishes static household-utilities pages.
func cmdUtilpages(argv []string) int {
	fs := flag.NewFlagSet("utilpages", flag.ContinueOnError)
	snapPath := fs.String("snapshot", "", "snapshot JSON file (required)")
	sitePublic := fs.String("site-public", filepath.Join("site", "public"), "static site public directory")
	force := fs.Bool("force", false, "re-render posts that already have u/<kind>/<id>/post.json")
	baseURL := fs.String("base-url", "https://abalol.xyz/presyo-bantay/", "site base URL (trailing slash)")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *snapPath == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo utilpages -snapshot FILE -site-public DIR [-force] [-base-url URL] [-now RFC3339]")
		return 1
	}
	data, err := os.ReadFile(*snapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilpages: error: %s\n", err.Error())
		return 1
	}
	var snap utilities.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		fmt.Fprintf(os.Stderr, "utilpages: error: %s\n", err.Error())
		return 1
	}
	cfg := utilpages.Config{
		SitePublic: *sitePublic,
		Force:      *force,
		BaseURL:    *baseURL,
	}
	if *nowFlag != "" {
		cfg.Now = parseNowFlag(*nowFlag)
	}
	if err := utilpages.Run(&snap, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "utilpages: error: %s\n", err.Error())
		return 1
	}
	return 0
}
