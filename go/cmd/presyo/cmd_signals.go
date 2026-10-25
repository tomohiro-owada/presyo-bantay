package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// defaultNearbyMarkets mirrors ADVICE_SPEC.md.
const defaultNearbyMarkets = "Alabang Central Market,La Huerta Market/Parañaque,Bicutan Market,Pasay City Market"

// defaultFocusMarket mirrors ADVICE_SPEC.md.
const defaultFocusMarket = "New Las Piñas City Public Market"

// openReadOnly opens a SQLite DB strictly read-only (never touches the
// live data/presyo.db write path; use /tmp/presyo-snap.db snapshots).
func openReadOnly(dbPath string) (*sql.DB, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func cmdSignals(argv []string) int {
	fs := flag.NewFlagSet("signals", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	dateFlag := fs.String("date", "", "as-of date YYYY-MM-DD (default: latest price date)")
	focus := fs.String("focus", defaultFocusMarket, "focus market name")
	nearbyFlag := fs.String("nearby", defaultNearbyMarkets, "comma-separated nearby market names")
	outPath := fs.String("out", "", "output file (default: stdout)")
	fxWindow := fs.Int("fx-window", 90, "accepted for compatibility; unused in v2 (fx is context only)")
	dealThreshold := fs.Float64("deal-threshold", signals.DefaultDealThreshold, "deal |pct_vs_median_30d| cutoff")
	maxDeals := fs.Int("max-deals", signals.DefaultMaxDeals, "max deals rows")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	var nearby []string
	for _, m := range strings.Split(*nearbyFlag, ",") {
		if m = strings.TrimSpace(m); m != "" {
			nearby = append(nearby, m)
		}
	}
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	s, err := signals.Compute(db, signals.Options{
		AsOf: *dateFlag, Focus: *focus, Nearby: nearby, FXWindow: *fxWindow,
		DealThreshold: *dealThreshold, MaxDeals: *maxDeals,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	data = append(data, '\n')
	if *outPath == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
		return 0
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	fmt.Println(*outPath)
	return 0
}
