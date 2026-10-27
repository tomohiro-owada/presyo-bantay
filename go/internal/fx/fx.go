// Package fx ports presyo_bantay/fx.py: USD/PHP reference rates via the
// Frankfurter API (ECB reference rates), cached in the fx_rates table.
package fx

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// FXBase, FXQuote and FXSource mirror fx.py constants.
const (
	FXBase   = "USD"
	FXQuote  = "PHP"
	FXSource = "ECB via Frankfurter"
)

// APIBase is the Frankfurter API root (overridable for httptest).
var APIBase = "https://api.frankfurter.dev/v1"

// UserAgent is sent on every Frankfurter request (same as the DA crawler).
const UserAgent = "PresyoBantay/0.1 (non-profit community price watch; https://github.com/tomohiro-owada/presyo-bantay)"

// RequestTimeout mirrors fx.REQUEST_TIMEOUT.
const RequestTimeout = 30 * time.Second

// ChunkDelay mirrors fx.CHUNK_DELAY_SECONDS (overridable in tests).
var ChunkDelay = 2 * time.Second

// Client is the HTTP client (overridable in tests).
var Client = &http.Client{Timeout: RequestTimeout}

// Sleep is the chunk-delay hook (overridable in tests).
var Sleep = time.Sleep

// ManilaToday mirrors fx.manila_today: today in Asia/Manila (UTC+8, no DST).
func ManilaToday() string {
	now := time.Now().UTC().Add(8 * time.Hour)
	return now.Format("2006-01-02")
}

// AddDays adds days to an ISO date.
func AddDays(dateStr string, days int) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	return t.AddDate(0, 0, days).Format("2006-01-02")
}

// LatestFXDate mirrors fx.latest_fx_date.
func LatestFXDate(db *sql.DB, base, quote string) (string, error) {
	var d sql.NullString
	err := db.QueryRow(
		"SELECT MAX(date) FROM fx_rates WHERE base = ? AND quote = ?",
		base, quote).Scan(&d)
	if err != nil {
		return "", err
	}
	if !d.Valid {
		return "", nil
	}
	return d.String, nil
}

// DefaultSince mirrors fx.default_since: day after the latest stored FX
// date, else the earliest price date, else "".
func DefaultSince(db *sql.DB) (string, error) {
	latest, err := LatestFXDate(db, FXBase, FXQuote)
	if err != nil {
		return "", err
	}
	if latest != "" {
		return AddDays(latest, 1), nil
	}
	var d sql.NullString
	if err := db.QueryRow("SELECT MIN(date) FROM prices").Scan(&d); err != nil {
		return "", err
	}
	if !d.Valid {
		return "", nil
	}
	return d.String, nil
}

// SplitYearly mirrors fx.split_yearly.
func SplitYearly(since, until string) [][2]string {
	var chunks [][2]string
	start, err1 := time.Parse("2006-01-02", since)
	end, err2 := time.Parse("2006-01-02", until)
	if err1 != nil || err2 != nil {
		return nil
	}
	for !start.After(end) {
		yearEnd := time.Date(start.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
		chunkEnd := yearEnd
		if end.Before(chunkEnd) {
			chunkEnd = end
		}
		chunks = append(chunks, [2]string{
			start.Format("2006-01-02"), chunkEnd.Format("2006-01-02"),
		})
		start = chunkEnd.AddDate(0, 0, 1)
	}
	return chunks
}

// FetchRates mirrors fx.fetch_rates: business-day rates for [since, until].
func FetchRates(since, until, base, quote string) (map[string]float64, error) {
	u, err := url.Parse(APIBase + "/" + since + ".." + until)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("from", base)
	q.Set("to", quote)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return map[string]float64{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("frankfurter HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Rates map[string]map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for day, per := range payload.Rates {
		if rate, ok := per[quote]; ok {
			out[day] = rate
		}
	}
	return out, nil
}

// UpsertRates mirrors fx.upsert_rates.
func UpsertRates(db *sql.DB, rates map[string]float64, base, quote, source string) (int, error) {
	days := make([]string, 0, len(rates))
	for day := range rates {
		days = append(days, day)
	}
	sortStrings(days)
	count := 0
	for _, day := range days {
		if _, err := db.Exec(
			"INSERT INTO fx_rates (date, base, quote, rate, source) VALUES (?, ?, ?, ?, ?)"+
				" ON CONFLICT (date, base, quote, source) DO UPDATE SET rate = excluded.rate",
			day, base, quote, rates[day], source); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// FXAsOf mirrors fx.fx_asof: latest stored rate on or before date.
func FXAsOf(db *sql.DB, date, base, quote string) (*float64, error) {
	var rate sql.NullFloat64
	err := db.QueryRow(
		"SELECT rate FROM fx_rates WHERE base = ? AND quote = ? AND date <= ? ORDER BY date DESC LIMIT 1",
		base, quote, date).Scan(&rate)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !rate.Valid {
		return nil, nil
	}
	f := rate.Float64
	return &f, nil
}

// FXRate is one (date, rate) row.
type FXRate struct {
	Date string
	Rate float64
}

// Series returns the (date, rate) series ascending.
func Series(db *sql.DB, base, quote string) ([]FXRate, error) {
	rows, err := db.Query(
		"SELECT date, rate FROM fx_rates WHERE base = ? AND quote = ? ORDER BY date",
		base, quote)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FXRate
	for rows.Next() {
		var r FXRate
		if err := rows.Scan(&r.Date, &r.Rate); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
