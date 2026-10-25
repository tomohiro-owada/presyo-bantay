package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/daypages"
)

// cmdDaypages publishes static daily report pages for one date or range.
func cmdDaypages(argv []string) int {
	fs := flag.NewFlagSet("daypages", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	sitePublic := fs.String("site-public", filepath.Join("site", "public"), "static site public directory")
	dateFlag := fs.String("date", "", "data date YYYY-MM-DD (default: latest price date)")
	fromFlag := fs.String("from", "", "backfill range start YYYY-MM-DD (inclusive, with -to)")
	toFlag := fs.String("to", "", "backfill range end YYYY-MM-DD (inclusive, with -from)")
	force := fs.Bool("force", false, "re-render dates that already have d/D/day.json")
	baseURL := fs.String("base-url", "https://abalol.xyz/presyo-bantay/", "site base URL (trailing slash)")
	htmlOnly := fs.Bool("html-only", false, "skip rendering; rebuild HTML+manifest from day.json files on disk")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daypages: error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	var dates []string
	switch {
	case *dateFlag != "":
		dates = []string{*dateFlag}
	case *fromFlag != "" || *toFlag != "":
		dates, err = daypages.DatesInRange(*fromFlag, *toFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "daypages: error: %s\n", err.Error())
			return 1
		}
	default:
		latest, err := daypages.LatestDate(db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "daypages: error: %s\n", err.Error())
			return 1
		}
		dates = []string{latest}
	}
	if err := daypages.Run(db, daypages.Config{
		SitePublic: *sitePublic,
		Dates:      dates,
		Force:      *force,
		HTMLOnly:   *htmlOnly,
		BaseURL:    *baseURL,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "daypages: error: %s\n", err.Error())
		return 1
	}
	return 0
}
