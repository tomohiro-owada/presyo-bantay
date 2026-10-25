package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/weekpages"
)

// cmdWeekpages publishes static Sunday weekly report pages.
func cmdWeekpages(argv []string) int {
	fs := flag.NewFlagSet("weekpages", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	sitePublic := fs.String("site-public", filepath.Join("site", "public"), "static site public directory")
	weekFlag := fs.String("week", "", "ISO week id YYYY-Www (default: latest reportable week)")
	fromWeek := fs.String("from-week", "", "backfill range start YYYY-Www (inclusive, with -to-week)")
	toWeek := fs.String("to-week", "", "backfill range end YYYY-Www (inclusive, with -from-week)")
	force := fs.Bool("force", false, "re-render weeks that already have w/<week>/week.json")
	baseURL := fs.String("base-url", "https://abalol.xyz/presyo-bantay/", "site base URL (trailing slash)")
	htmlOnly := fs.Bool("html-only", false, "skip rendering; rebuild HTML+manifest from week.json files on disk")
	nowFlag := fs.String("now", "", "RFC3339 now override (tests)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "weekpages: error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	cfg := weekpages.Config{
		SitePublic: *sitePublic,
		Force:      *force,
		HTMLOnly:   *htmlOnly,
		BaseURL:    *baseURL,
	}
	if *weekFlag != "" {
		cfg.Weeks = []string{*weekFlag}
	}
	cfg.FromWeek, cfg.ToWeek = *fromWeek, *toWeek
	if *nowFlag != "" {
		cfg.Now = parseNowFlag(*nowFlag)
	}
	if err := weekpages.Run(db, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "weekpages: error: %s\n", err.Error())
		return 1
	}
	return 0
}
