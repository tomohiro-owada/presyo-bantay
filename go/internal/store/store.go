// Package store ports presyo_bantay/db.py: the SQLite store
// (modernc.org/sqlite, pure Go, via database/sql), payload ingestion with
// alias canonicalization, and idempotent upserts.
package store

import (
	"bytes"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/aliases"
)

//go:embed schema.sql
var schemaSQL string

// knownCities mirrors db._KNOWN_CITIES.
var knownCities = map[string]string{
	"new las piñas city public market": "Las Piñas",
	"alabang central market":           "Muntinlupa",
	"bicutan market":                   "Taguig",
}

// DeriveCity mirrors db.derive_city.
func DeriveCity(marketName string) *string {
	name := strings.TrimSpace(marketName)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		hint := strings.Trim(name[i+1:], "() ")
		if hint != "" {
			return &hint
		}
	}
	if known, ok := knownCities[strings.Join(strings.Fields(strings.ToLower(name)), " ")]; ok {
		return &known
	}
	return nil
}

// CategorizeCommodity mirrors db.categorize_commodity.
func CategorizeCommodity(commodityName string) string {
	return aliases.CategorizeCommodity(commodityName)
}

// nullString converts *string for database/sql.
func nullString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// Connect mirrors db.connect: open dbPath (creating parents), apply the
// schema, and run the price_low/price_high column migration.
func Connect(dbPath string) (*sql.DB, error) {
	parent := filepath.Dir(dbPath)
	if abs, err := filepath.Abs(parent); err == nil {
		parent = abs
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Strip "--" line comments (the header comment contains ";" which
	// would break naive statement splitting), then run each statement.
	var body []string
	for _, line := range strings.Split(schemaSQL, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		body = append(body, line)
	}
	for _, stmt := range strings.Split(strings.Join(body, "\n"), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("schema: %w", err)
		}
	}
	if err := ensurePriceRangeColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// ensurePriceRangeColumns mirrors _ensure_price_range_columns.
func ensurePriceRangeColumns(db *sql.DB) error {
	for _, column := range []string{"price_low", "price_high"} {
		if _, err := db.Exec("ALTER TABLE prices ADD COLUMN " + column + " REAL"); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				return err
			}
		}
	}
	return nil
}

// PayloadEntry is one {spec, unit, price, ...} entry. HasLow/HasHigh record
// whether the keys were present (older JSON lacks them and falls back to
// the midpoint price, like Python).
type PayloadEntry struct {
	Spec      *string
	Unit      *string
	Price     *float64
	PriceLow  *float64
	HasLow    bool
	PriceHigh *float64
	HasHigh   bool
}

func floatPtr(v any) *float64 {
	switch n := v.(type) {
	case float64:
		f := n
		return &f
	case nil:
		return nil
	default:
		return nil
	}
}

// entryFromMap converts a decoded JSON object (nil when not an object,
// mirroring Python's isinstance(entry, dict) guards).
func entryFromMap(m map[string]any) *PayloadEntry {
	if m == nil {
		return &PayloadEntry{}
	}
	e := &PayloadEntry{}
	if v, ok := m["spec"]; ok {
		if s, ok := v.(string); ok {
			e.Spec = &s
		}
	}
	if v, ok := m["unit"]; ok {
		if s, ok := v.(string); ok {
			e.Unit = &s
		}
	}
	if v, ok := m["price"]; ok {
		e.Price = floatPtr(v)
	}
	if v, ok := m["price_low"]; ok {
		e.PriceLow = floatPtr(v)
		e.HasLow = true
	}
	if v, ok := m["price_high"]; ok {
		e.PriceHigh = floatPtr(v)
		e.HasHigh = true
	}
	return e
}

// Payload is one snapshot payload with insertion order preserved
// (market/commodity row IDs depend on it, exactly like Python).
type Payload struct {
	Date           string
	SourceURL      *string
	Synthetic      bool
	Markets        map[string]map[string]*PayloadEntry
	MarketOrder    []string
	CommodityOrder map[string][]string
}

// DecodePayload parses snapshot JSON preserving market/commodity order.
func DecodePayload(data []byte) (*Payload, error) {
	var top map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	_ = dec
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	p := &Payload{
		Markets:        map[string]map[string]*PayloadEntry{},
		CommodityOrder: map[string][]string{},
	}
	if v, ok := top["date"]; ok {
		_ = json.Unmarshal(v, &p.Date)
	}
	if v, ok := top["source_url"]; ok {
		var s *string
		if err := json.Unmarshal(v, &s); err == nil {
			p.SourceURL = s
		}
	}
	if v, ok := top["synthetic"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			p.Synthetic = b
		}
	}
	// Order-preserving walk of markets -> commodities.
	dec2 := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec2.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("payload must be an object")
	}
	rawMarkets := map[string]json.RawMessage{}
	if v, ok := top["markets"]; ok {
		if err := json.Unmarshal(v, &rawMarkets); err != nil {
			return nil, err
		}
	}
	for dec2.More() {
		kt, err := dec2.Token()
		if err != nil {
			return nil, err
		}
		key, _ := kt.(string)
		if key != "markets" {
			var skip any
			if err := dec2.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}
		// Markets object: record order.
		tok, err := dec2.Token()
		if err != nil || tok != json.Delim('{') {
			return nil, fmt.Errorf("markets must be an object")
		}
		for dec2.More() {
			mt, err := dec2.Token()
			if err != nil {
				return nil, err
			}
			market, _ := mt.(string)
			p.MarketOrder = append(p.MarketOrder, market)
			p.Markets[market] = map[string]*PayloadEntry{}
			// Commodities object: record order.
			tok, err := dec2.Token()
			if err != nil || tok != json.Delim('{') {
				return nil, fmt.Errorf("market must be an object")
			}
			for dec2.More() {
				ct, err := dec2.Token()
				if err != nil {
					return nil, err
				}
				commodity, _ := ct.(string)
				p.CommodityOrder[market] = append(p.CommodityOrder[market], commodity)
				var raw json.RawMessage
				if err := dec2.Decode(&raw); err != nil {
					return nil, err
				}
				var m map[string]any
				if err := json.Unmarshal(raw, &m); err != nil {
					// Non-object entry (e.g. a bare number): all-nil entry.
					p.Markets[market][commodity] = &PayloadEntry{}
					continue
				}
				p.Markets[market][commodity] = entryFromMap(m)
			}
			if _, err := dec2.Token(); err != nil { // '}'
				return nil, err
			}
		}
		if _, err := dec2.Token(); err != nil { // '}'
			return nil, err
		}
	}
	return p, nil
}

// execer abstracts *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func getOrCreateMarket(db execer, name string) (int64, error) {
	var id int64
	err := db.QueryRow("SELECT id FROM markets WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := db.Exec("INSERT INTO markets (name, city) VALUES (?, ?)", name, nullString(DeriveCity(name)))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func getOrCreateCommodity(db execer, name string, spec, unit *string) (int64, error) {
	var id int64
	err := db.QueryRow("SELECT id FROM commodities WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := db.Exec(
		"INSERT INTO commodities (name, category, spec, unit) VALUES (?, ?, ?, ?)",
		name, CategorizeCommodity(name), nullString(spec), nullString(unit))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// IngestPayload mirrors db.ingest_payload: upsert one snapshot payload;
// returns (markets, commodities, prices). Refused synthetic payloads (like
// Python when allow_synthetic is false) yield (0, 0, 0).
func IngestPayload(db *sql.DB, p *Payload, allowSynthetic bool) (int, int, int, error) {
	if p.Synthetic && !allowSynthetic {
		return 0, 0, 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, 0, err
	}
	// Roll back on failure; commit at the end (one transaction per file,
	// like Python's per-payload conn.commit()).
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	if p.Synthetic {
		if _, err := tx.Exec("INSERT OR IGNORE INTO synthetic_dates (date) VALUES (?)", p.Date); err != nil {
			return 0, 0, 0, err
		}
	}
	nMarkets, nCommodities, nPrices := 0, 0, 0
	for _, marketName := range p.MarketOrder {
		items := p.Markets[marketName]
		marketID, err := getOrCreateMarket(tx, marketName)
		if err != nil {
			return nMarkets, nCommodities, nPrices, err
		}
		nMarkets++
		for _, commodityName := range p.CommodityOrder[marketName] {
			entry := items[commodityName]
			var spec, unit *string
			if entry != nil {
				spec, unit = entry.Spec, entry.Unit
			}
			canonical, cspec, cunit, _ := aliases.Canonicalize(commodityName)
			cunitPtr := &cunit
			if canonical != commodityName {
				spec, unit = cspec, cunitPtr
			} else {
				if spec == nil {
					spec = cspec
				}
				if unit == nil {
					unit = cunitPtr
				}
			}
			var price, low, high *float64
			if entry != nil {
				price = entry.Price
				if entry.HasLow {
					low = entry.PriceLow
				} else {
					low = price
				}
				if entry.HasHigh {
					high = entry.PriceHigh
				} else {
					high = price
				}
			}
			commodityID, err := getOrCreateCommodity(tx, canonical, spec, unit)
			if err != nil {
				return nMarkets, nCommodities, nPrices, err
			}
			nCommodities++
			if _, err := tx.Exec(
				"INSERT INTO prices (date, market_id, commodity_id, price, price_low, price_high, source_url)"+
					" VALUES (?, ?, ?, ?, ?, ?, ?)"+
					" ON CONFLICT (date, market_id, commodity_id) DO UPDATE SET"+
					" price = excluded.price, price_low = excluded.price_low,"+
					" price_high = excluded.price_high, source_url = excluded.source_url",
				p.Date, marketID, commodityID,
				nullFloat(price), nullFloat(low), nullFloat(high), nullString(p.SourceURL)); err != nil {
				return nMarkets, nCommodities, nPrices, err
			}
			nPrices++
		}
	}
	if err := tx.Commit(); err != nil {
		return nMarkets, nCommodities, nPrices, err
	}
	committed = true
	return nMarkets, nCommodities, nPrices, nil
}

// IngestFile mirrors db.ingest_file: ingest one JSON file;
// returns (markets, commodities, prices, skipped).
func IngestFile(db *sql.DB, path string, allowSynthetic bool) (int, int, int, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, false, err
	}
	p, err := DecodePayload(data)
	if err != nil {
		return 0, 0, 0, false, err
	}
	if p.Synthetic && !allowSynthetic {
		return 0, 0, 0, true, nil
	}
	m, c, n, err := IngestPayload(db, p, allowSynthetic)
	if err != nil {
		return 0, 0, 0, false, err
	}
	return m, c, n, false, nil
}

// ListDates mirrors db.list_dates.
func ListDates(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT DISTINCT date FROM prices ORDER BY date")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LatestDate mirrors db.latest_date.
func LatestDate(db *sql.DB) (string, error) {
	var d sql.NullString
	if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil {
		return "", err
	}
	if !d.Valid {
		return "", nil
	}
	return d.String, nil
}

// MatchNames mirrors db.match_names.
func MatchNames(db *sql.DB, table, needle string) ([]string, error) {
	norm := strings.Join(strings.Fields(strings.ToLower(needle)), " ")
	query := "SELECT name FROM commodities ORDER BY name"
	if table == "markets" {
		query = "SELECT name FROM markets ORDER BY name"
	}
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if strings.Contains(strings.Join(strings.Fields(strings.ToLower(name)), " "), norm) {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

// AliasRow is one (id, raw name, canonical name) non-canonical commodity.
type AliasRow struct {
	ID        int64
	Name      string
	Canonical string
}

// FindAliasRows mirrors aliases.find_alias_rows.
func FindAliasRows(db *sql.DB) ([]AliasRow, error) {
	rows, err := db.Query("SELECT id, name FROM commodities ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AliasRow
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		canonical, _, _, _ := aliases.Canonicalize(name)
		if canonical != name {
			out = append(out, AliasRow{ID: id, Name: name, Canonical: canonical})
		}
	}
	return out, rows.Err()
}

// Merge is one (alias, canonical, moved-prices) normalization result.
type Merge struct {
	Alias     string
	Canonical string
	Moved     int
}

// NormalizeCommodities mirrors aliases.normalize_commodities.
func NormalizeCommodities(db *sql.DB, dryRun bool) ([]Merge, error) {
	if _, err := db.Exec("PRAGMA busy_timeout = 30000"); err != nil {
		return nil, err
	}
	rows, err := FindAliasRows(db)
	if err != nil {
		return nil, err
	}
	var merges []Merge
	for _, row := range rows {
		canonical, spec, unit, category := aliases.Canonicalize(row.Canonical)
		if canonical != row.Canonical {
			return nil, fmt.Errorf("canonicalize(%q) = %q", row.Canonical, canonical)
		}
		var canonicalID int64
		err := db.QueryRow("SELECT id FROM commodities WHERE name = ?", row.Canonical).Scan(&canonicalID)
		if err == sql.ErrNoRows {
			if dryRun {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM prices WHERE commodity_id = ?", row.ID).Scan(&count); err != nil {
					return nil, err
				}
				merges = append(merges, Merge{row.Name, row.Canonical, count})
				continue
			}
			res, err := db.Exec(
				"INSERT INTO commodities (name, category, spec, unit) VALUES (?, ?, ?, ?)",
				row.Canonical, category, nullString(spec), unit)
			if err != nil {
				return nil, err
			}
			canonicalID, err = res.LastInsertId()
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		prices, err := queryPrices(db, row.ID)
		if err != nil {
			return nil, err
		}
		moved := 0
		if !dryRun {
			for _, pr := range prices {
				var existingPrice sql.NullFloat64
				var existingURL sql.NullString
				err := db.QueryRow(
					"SELECT price, source_url FROM prices WHERE date = ? AND market_id = ? AND commodity_id = ?",
					pr.Date, pr.MarketID, canonicalID).Scan(&existingPrice, &existingURL)
				if err == sql.ErrNoRows {
					if _, err := db.Exec(
						"UPDATE prices SET commodity_id = ? WHERE date = ? AND market_id = ? AND commodity_id = ?",
						canonicalID, pr.Date, pr.MarketID, row.ID); err != nil {
						return nil, err
					}
					moved++
				} else if err != nil {
					return nil, err
				} else {
					if !existingPrice.Valid && pr.Price != nil {
						if _, err := db.Exec(
							"UPDATE prices SET price = ?, price_low = ?, price_high = ? WHERE date = ? AND market_id = ? AND commodity_id = ?",
							nullFloat(pr.Price), nullFloat(pr.Low), nullFloat(pr.High),
							pr.Date, pr.MarketID, canonicalID); err != nil {
							return nil, err
						}
					}
					if !existingURL.Valid && pr.SourceURL != nil {
						if _, err := db.Exec(
							"UPDATE prices SET source_url = ? WHERE date = ? AND market_id = ? AND commodity_id = ?",
							*pr.SourceURL, pr.Date, pr.MarketID, canonicalID); err != nil {
							return nil, err
						}
					}
					if _, err := db.Exec(
						"DELETE FROM prices WHERE date = ? AND market_id = ? AND commodity_id = ?",
						pr.Date, pr.MarketID, row.ID); err != nil {
						return nil, err
					}
					moved++
				}
			}
			if _, err := db.Exec("DELETE FROM commodities WHERE id = ?", row.ID); err != nil {
				return nil, err
			}
		} else {
			moved = len(prices)
		}
		merges = append(merges, Merge{row.Name, row.Canonical, moved})
	}
	return merges, nil
}

type priceRow struct {
	Date      string
	MarketID  int64
	Price     *float64
	Low       *float64
	High      *float64
	SourceURL *string
}

func queryPrices(db *sql.DB, commodityID int64) ([]priceRow, error) {
	rows, err := db.Query(
		"SELECT date, market_id, price, price_low, price_high, source_url FROM prices WHERE commodity_id = ?",
		commodityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []priceRow
	for rows.Next() {
		var pr priceRow
		var price, low, high sql.NullFloat64
		var url sql.NullString
		if err := rows.Scan(&pr.Date, &pr.MarketID, &price, &low, &high, &url); err != nil {
			return nil, err
		}
		if price.Valid {
			f := price.Float64
			pr.Price = &f
		}
		if low.Valid {
			f := low.Float64
			pr.Low = &f
		}
		if high.Valid {
			f := high.Float64
			pr.High = &f
		}
		if url.Valid {
			s := url.String
			pr.SourceURL = &s
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}
