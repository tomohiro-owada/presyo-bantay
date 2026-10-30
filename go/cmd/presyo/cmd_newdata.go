// Command newdata is the REGIONS_SPEC.md §9a new-data check: ONE conditional
// GET of the DA price-monitoring listing page (never PDFs; robots disallows
// *.pdf), parsed with the existing listing parser. If any listed report
// date is newer than MAX(prices.date) in the DB it prints the newest such
// date and exits 0; otherwise exits 10 (no new data); exit 1 on error.
//
// The parsed listed dates are stored in the state file on every 200, so a
// later 304 can still report new data when a previous run saw a new date
// but daily.sh failed to ingest it before the listing went quiet.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/listing"
)

// newdataState caches conditional-GET validators and the last parsed
// listed dates between runs.
type newdataState struct {
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	CheckedAt    string `json:"checked_at,omitempty"`
	// Dates holds the newest parsed listed report dates (YYYY-MM-DD,
	// newest first, capped) from the last 200 response.
	Dates []string `json:"dates,omitempty"`
}

// maxCachedDate returns the newest cached listed date, or "".
func maxCachedDate(dates []string) string {
	newest := ""
	for _, d := range dates {
		if d > newest {
			newest = d
		}
	}
	return newest
}

func loadNewdataState(path string) (*newdataState, error) {
	st := &newdataState{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	return st, nil
}

func saveNewdataState(path string, st *newdataState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".newdata-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
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

// dbMaxPriceDate returns MAX(prices.date), or "" when the DB has no prices.
func dbMaxPriceDate(dbPath string) (string, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var d *string
	if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil {
		return "", err
	}
	if d == nil {
		return "", nil
	}
	return *d, nil
}

func cmdNewdata(argv []string) int {
	fs := flag.NewFlagSet("newdata", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path (opened read-only)")
	statePath := fs.String("state", "", "conditional-GET state file (default $PRESYO_HOME/state/newdata.json)")
	listingURL := fs.String("listing-url", listing.ListingURL, "listing page URL (tests use httptest)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	presyoHome := defaultPresyoHome()
	stPath := *statePath
	if stPath == "" {
		stPath = filepath.Join(presyoHome, "state", "newdata.json")
	}

	st, err := loadNewdataState(stPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "newdata: error: %s\n", err.Error())
		return 1
	}

	// ONE GET of the listing HTML (never PDFs), conditional when cached.
	req, err := http.NewRequest("GET", *listingURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "newdata: error: %s\n", err.Error())
		return 1
	}
	req.Header.Set("User-Agent", UserAgent)
	if st.ETag != "" {
		req.Header.Set("If-None-Match", st.ETag)
	}
	if st.LastModified != "" {
		req.Header.Set("If-Modified-Since", st.LastModified)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "newdata: error: %s\n", err.Error())
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		// Compare the cached listed dates against the DB: a previous
		// 200 may have seen a new date that daily.sh failed to ingest,
		// in which case later runs must keep reporting it (exit 0)
		// instead of going quiet on 304.
		maxDB, err := dbMaxPriceDate(*dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "newdata: error: %s\n", err.Error())
			return 1
		}
		newest := maxCachedDate(st.Dates)
		st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		if serr := saveNewdataState(stPath, st); serr != nil {
			fmt.Fprintf(os.Stderr, "newdata: error: %s\n", serr.Error())
			return 1
		}
		if newest != "" && newest > maxDB {
			fmt.Println(newest)
			return 0
		}
		fmt.Fprintln(os.Stderr, "newdata: not modified, no new data")
		return 10
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "newdata: error: listing HTTP %d\n", resp.StatusCode)
		return 1
	}
	htmlData, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		fmt.Fprintf(os.Stderr, "newdata: error: %s\n", err.Error())
		return 1
	}

	links := listing.FindAllFromHTML(string(htmlData), *listingURL)
	newest := ""
	var dates []string
	for _, l := range links {
		iso := l.Date.ISODate()
		if iso > newest {
			newest = iso
		}
		dates = append(dates, iso)
	}
	// Newest first, capped, for the state file.
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) > 10 {
		dates = dates[:10]
	}

	// Only update ETag/Last-Modified state after a successful parse.
	st.ETag = resp.Header.Get("ETag")
	st.LastModified = resp.Header.Get("Last-Modified")
	st.Dates = dates
	st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	if serr := saveNewdataState(stPath, st); serr != nil {
		fmt.Fprintf(os.Stderr, "newdata: error: %s\n", serr.Error())
		return 1
	}

	maxDB, err := dbMaxPriceDate(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "newdata: error: %s\n", err.Error())
		return 1
	}
	if newest != "" && newest > maxDB {
		fmt.Println(newest)
		return 0
	}
	fmt.Fprintln(os.Stderr, "newdata: no new data")
	return 10
}
