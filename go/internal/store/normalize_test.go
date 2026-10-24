package store

import (
	"path/filepath"
	"testing"
)

func aliasDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA busy_timeout = 30000"); err != nil {
		t.Fatal(err)
	}
	var market int64
	res, err := db.Exec("INSERT INTO markets (name, city) VALUES (?, ?)", "M1", nil)
	if err != nil {
		t.Fatal(err)
	}
	market, err = res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	// Portable path: insert then select.
	var canon, alias int64
	mustExec := func(query string, args ...any) int64 {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := db.QueryRow("SELECT id FROM commodities WHERE name = ?",
			args[0]).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	canon = mustExec(
		"INSERT OR IGNORE INTO commodities (name, category, spec, unit) VALUES (?, ?, ?, ?)",
		"Premium Rice (Local)", "rice", "Local", "per kg")
	alias = mustExec(
		"INSERT OR IGNORE INTO commodities (name, category, spec, unit) VALUES (?, ?, ?, ?)",
		"Premium Rice (Loca)", "rice", "Loca", "per kg")
	prices := []struct {
		date string
		cid  int64
		val  *float64
	}{
		{"2026-10-01", canon, nil},
		{"2026-10-01", alias, fptr(50.0)},
		{"2026-10-02", canon, fptr(52.0)},
		{"2026-10-02", alias, fptr(60.0)},
		{"2026-10-03", alias, fptr(51.0)},
	}
	for _, pr := range prices {
		var v any
		if pr.val != nil {
			v = *pr.val
		}
		if _, err := db.Exec(
			"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
			pr.date, market, pr.cid, v); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func fptr(f float64) *float64 { return &f }

func TestNormalizeMergeCorrectness(t *testing.T) {
	path := aliasDB(t)
	db, err := Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	merges, err := NormalizeCommodities(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(merges) != 1 || merges[0].Alias != "Premium Rice (Loca)" ||
		merges[0].Canonical != "Premium Rice (Local)" || merges[0].Moved != 3 {
		t.Fatalf("merges = %+v", merges)
	}
	rows, err := db.Query("SELECT name FROM commodities")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if len(names) != 1 || names[0] != "Premium Rice (Local)" {
		t.Fatalf("names = %v", names)
	}
	prows, err := db.Query(
		"SELECT p.date, p.price FROM prices p JOIN commodities c ON c.id = p.commodity_id" +
			" WHERE c.name = 'Premium Rice (Local)' ORDER BY p.date")
	if err != nil {
		t.Fatal(err)
	}
	defer prows.Close()
	type dp struct {
		date  string
		price float64
	}
	var got []dp
	for prows.Next() {
		var d string
		var p float64
		if err := prows.Scan(&d, &p); err != nil {
			t.Fatal(err)
		}
		got = append(got, dp{d, p})
	}
	want := []dp{{"2026-10-01", 50.0}, {"2026-10-02", 52.0}, {"2026-10-03", 51.0}}
	if len(got) != len(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %+v want %+v", got, want)
		}
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	path := aliasDB(t)
	db, err := Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m1, err := NormalizeCommodities(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(m1) == 0 {
		t.Fatal("first normalize merged nothing")
	}
	m2, err := NormalizeCommodities(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2) != 0 {
		t.Fatalf("second normalize merged %+v", m2)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM prices").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("prices = %d, want 3", n)
	}
}

func TestNormalizeDryRunWritesNothing(t *testing.T) {
	path := aliasDB(t)
	db, err := Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	merges, err := NormalizeCommodities(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(merges) != 1 {
		t.Fatalf("merges = %+v", merges)
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
	if !names["Premium Rice (Loca)"] {
		t.Fatalf("alias row deleted by dry run: %v", names)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM prices").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("prices = %d, want 5", count)
	}
}
