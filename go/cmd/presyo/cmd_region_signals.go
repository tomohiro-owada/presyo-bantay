package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

func cmdRegionSignals(argv []string) int {
	fs := flag.NewFlagSet("region-signals", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	region := fs.String("region", "", "region id: south|manila|qc|north|east (required)")
	dateFlag := fs.String("date", "", "as-of date YYYY-MM-DD (default: latest price date across the region's markets)")
	outPath := fs.String("out", "", "output file (default: stdout)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *region == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo region-signals -db DB -region south [-date YYYY-MM-DD] -out FILE")
		return 1
	}
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	s, err := signals.ComputeRegion(db, *region, signals.RegionOptions{AsOf: *dateFlag})
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
