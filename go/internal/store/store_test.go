package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fixturePath(name ...string) string {
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate test source")
	}
	parts := append([]string{filepath.Dir(src), "..", "..", "..",
		"tests", "fixtures"}, name...)
	return filepath.Clean(filepath.Join(parts...))
}

func realJSON() string {
	_, src, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(src), "..", "..", "..",
		"data", "json", "2026-10-23.json"))
}

func rowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM prices").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func openTmp(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Connect(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSchemaFileExists(t *testing.T) {
	if _, err := os.Stat("schema.sql"); err != nil {
		t.Fatalf("schema.sql missing: %v", err)
	}
	data, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "CREATE TABLE IF NOT EXISTS prices") {
		t.Fatal("schema.sql lacks prices table")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCityDerivation(t *testing.T) {
	cases := map[string]*string{
		"New Las Piñas City Public Market": strp("Las Piñas"),
		"Alabang Central Market":           strp("Muntinlupa"),
		"La Huerta Market/Parañaque":       strp("Parañaque"),
		"Bicutan Market":                   strp("Taguig"),
		"Agora Public Market/San Juan":     strp("San Juan"),
		"Balintawak (Cloverleaf) Market":   nil,
	}
	for in, want := range cases {
		got := DeriveCity(in)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("DeriveCity(%q) = %v, want %v", in, got, want)
		}
	}
}

func strp(s string) *string { return &s }

func TestCommodityCategories(t *testing.T) {
	cases := map[string]string{
		"Premium Rice (Imported)":  "rice",
		"Well-milled Rice (Local)": "rice",
		"Regular Milled (Local)":   "rice",
		"Bangus":                   "fish",
		"Egg (Medium)":             "egg",
		"Eggplant":                 "vegetables",
		"Tomato":                   "vegetables",
		"Red Onion (Local)":        "spices",
		"Sugar (Refined)":          "sugar",
		"Fresh Pork (Kasim) Local": "pork",
		"Fresh Whole Chicken":      "poultry",
	}
	for in, want := range cases {
		if got := CategorizeCommodity(in); got != want {
			t.Errorf("CategorizeCommodity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIdempotentIngestOverwrites(t *testing.T) {
	if _, err := os.Stat(realJSON()); err != nil {
		t.Skip("repo data/json not available")
	}
	db := openTmp(t)
	m, c, n, skipped, err := IngestFile(db, realJSON(), false)
	if err != nil {
		t.Fatal(err)
	}
	if skipped || m == 0 || c == 0 || n == 0 {
		t.Fatalf("ingest = (%d,%d,%d,%v)", m, c, n, skipped)
	}
	count1 := rowCount(t, db)
	if count1 == 0 {
		t.Fatal("no rows ingested")
	}
	if _, _, _, _, err := IngestFile(db, realJSON(), false); err != nil {
		t.Fatal(err)
	}
	if got := rowCount(t, db); got != count1 {
		t.Fatalf("re-ingest changed rows %d -> %d", count1, got)
	}
	data, err := os.ReadFile(realJSON())
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodePayload(data)
	if err != nil {
		t.Fatal(err)
	}
	const lasPinas = "New Las Piñas City Public Market"
	e := p.Markets[lasPinas]["Bangus"]
	v := 999.0
	e.Price = &v
	if _, _, _, err := IngestPayload(db, p, false); err != nil {
		t.Fatal(err)
	}
	if got := rowCount(t, db); got != count1 {
		t.Fatalf("overwrite changed rows %d -> %d", count1, got)
	}
	var price float64
	err = db.QueryRow(
		"SELECT p.price FROM prices p JOIN markets m ON m.id = p.market_id"+
			" JOIN commodities c ON c.id = p.commodity_id"+
			" WHERE m.name = ? AND c.name = ? AND p.date = ?",
		lasPinas, "Bangus", "2026-10-23").Scan(&price)
	if err != nil {
		t.Fatal(err)
	}
	if price != 999.0 {
		t.Fatalf("price = %v, want 999", price)
	}
}

func TestNullPriceStoredForNotAvailable(t *testing.T) {
	if _, err := os.Stat(realJSON()); err != nil {
		t.Skip("repo data/json not available")
	}
	db := openTmp(t)
	if _, _, _, _, err := IngestFile(db, realJSON(), false); err != nil {
		t.Fatal(err)
	}
	var nf sql.NullFloat64
	err := db.QueryRow(
		"SELECT p.price FROM prices p JOIN markets m ON m.id = p.market_id"+
			" JOIN commodities c ON c.id = p.commodity_id"+
			" WHERE m.name = ? AND c.name = ? AND p.date = ?",
		"La Huerta Market/Parañaque", "Well-milled Rice (Imported)", "2026-10-23").Scan(&nf)
	if err != nil {
		t.Fatal(err)
	}
	if nf.Valid {
		t.Fatalf("price = %v, want NULL", nf.Float64)
	}
	var city string
	if err := db.QueryRow("SELECT city FROM markets WHERE name = ?",
		"New Las Piñas City Public Market").Scan(&city); err != nil {
		t.Fatal(err)
	}
	if city != "Las Piñas" {
		t.Fatalf("city = %q", city)
	}
	var cat string
	if err := db.QueryRow("SELECT category FROM commodities WHERE name = ?",
		"Bangus").Scan(&cat); err != nil {
		t.Fatal(err)
	}
	if cat != "fish" {
		t.Fatalf("category = %q", cat)
	}
}

func TestSyntheticRefusedByDefault(t *testing.T) {
	db := openTmp(t)
	early := fixturePath("synthetic", "2026-10-16.json")
	m, c, n, skipped, err := IngestFile(db, early, false)
	if err != nil {
		t.Fatal(err)
	}
	if !skipped || m != 0 || c != 0 || n != 0 {
		t.Fatalf("synthetic not refused: (%d,%d,%d,%v)", m, c, n, skipped)
	}
	if got := rowCount(t, db); got != 0 {
		t.Fatalf("rows = %d, want 0", got)
	}
	if _, _, _, skipped, err := IngestFile(db, early, true); err != nil || skipped {
		t.Fatalf("allow-synthetic failed: %v %v", skipped, err)
	}
	if got := rowCount(t, db); got == 0 {
		t.Fatal("no rows after allow-synthetic")
	}
}

func TestIngestStoresCanonicalNames(t *testing.T) {
	db := openTmp(t)
	payload := `{"date":"2026-10-23","source_url":null,"markets":{"M1":{` +
		`"Premium Rice (Loca)":{"spec":"Loca","unit":"per kg","price":50.0},` +
		`"BANGUS":{"spec":null,"unit":"per kg","price":200.0}}}}`
	p, err := DecodePayload([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := IngestPayload(db, p, false); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("SELECT name FROM commodities")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names[n] = true
	}
	for _, want := range []string{"Premium Rice (Local)", "Bangus"} {
		if !names[want] {
			t.Fatalf("missing canonical %q (have %v)", want, names)
		}
	}
	for _, bad := range []string{"Premium Rice (Loca)", "BANGUS"} {
		if names[bad] {
			t.Fatalf("alias %q stored", bad)
		}
	}
	var spec string
	if err := db.QueryRow("SELECT spec FROM commodities WHERE name = 'Premium Rice (Local)'").Scan(&spec); err != nil {
		t.Fatal(err)
	}
	if spec != "Local" {
		t.Fatalf("spec = %q, want Local", spec)
	}
}
