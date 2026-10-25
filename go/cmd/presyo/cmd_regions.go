package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
)

// cmdRegions lists every market in the DB with its region, last price
// date, and "UNMAPPED" when the market is in no region.
func cmdRegions(argv []string) int {
	fs := flag.NewFlagSet("regions", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	db, err := openReadOnly(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	rows, err := db.Query("SELECT name FROM markets ORDER BY name")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	var markets []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
		markets = append(markets, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	type row struct {
		market, region, last string
	}
	var out []row
	for _, m := range markets {
		region := "UNMAPPED"
		if r, _, ok := regions.RegionOf(m); ok {
			region = r.ID
		}
		var last any
		var lastStr string
		if err := db.QueryRow("SELECT MAX(date) FROM prices p JOIN markets m ON m.id = p.market_id WHERE m.name = ?", m).Scan(&last); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
		switch v := last.(type) {
		case string:
			lastStr = v
		case []byte:
			lastStr = string(v)
		default:
			lastStr = "-"
		}
		out = append(out, row{m, region, lastStr})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].market < out[j].market })
	for _, r := range out {
		fmt.Printf("%s\t%s\t%s\n", r.market, r.region, r.last)
	}
	return 0
}
