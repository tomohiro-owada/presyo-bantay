package export

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fx"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

const (
	lasPinas = "New Las Piñas City Public Market"
	alabang  = "Alabang Central Market"
	laHuerta = "La Huerta Market/Parañaque"
	bicutan  = "Bicutan Market"
)

func seedDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	markets := map[string]int64{}
	for _, name := range []string{lasPinas, alabang, laHuerta, bicutan} {
		city := store.DeriveCity(name)
		var cityAny any
		if city != nil {
			cityAny = *city
		}
		res, err := db.Exec("INSERT INTO markets (name, city) VALUES (?, ?)", name, cityAny)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		markets[name] = id
	}
	commodities := map[string]int64{}
	for _, c := range [][4]string{
		{"Premium Rice (Local)", "rice", "Local", "per kg"},
		{"Premium Rice (Loca)", "rice", "Loca", "per kg"},
		{"Bangus", "fish", "", "per kg"},
		{"Sugar (Washed)", "sugar", "Washed", "per kg"},
	} {
		var spec any
		if c[2] != "" {
			spec = c[2]
		}
		res, err := db.Exec(
			"INSERT INTO commodities (name, category, spec, unit) VALUES (?, ?, ?, ?)",
			c[0], c[1], spec, c[3])
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		commodities[c[0]] = id
	}
	prices := []struct {
		day, market, commodity string
		price                  float64
	}{
		{"2026-10-09", lasPinas, "Premium Rice (Local)", 50.0},
		{"2026-10-09", alabang, "Premium Rice (Local)", 52.0},
		{"2026-10-16", lasPinas, "Premium Rice (Local)", 55.0},
		{"2026-10-16", alabang, "Premium Rice (Loca)", 54.0},
		{"2026-10-16", laHuerta, "Premium Rice (Local)", 53.456},
		{"2026-10-09", lasPinas, "Bangus", 240.0},
		{"2026-10-16", lasPinas, "Bangus", 270.0},
		{"2026-10-16", bicutan, "Bangus", 250.0},
		{"2026-10-16", lasPinas, "Sugar (Washed)", 80.0},
	}
	for _, p := range prices {
		if _, err := db.Exec(
			"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
			p.day, markets[p.market], commodities[p.commodity], p.price); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fx.UpsertRates(db, map[string]float64{"2026-10-16": 62.968},
		fx.FXBase, fx.FXQuote, fx.FXSource); err != nil {
		t.Fatal(err)
	}
	return db
}

func loadJSON(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestExportSiteWritesCompactCanonicalJSON(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.db")
	outDir := filepath.Join(dir, "site")
	db := seedDB(t, dbPath)
	db.Close()
	db2, err := store.Connect(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	written, err := ExportSite(db2, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) == 0 {
		t.Fatal("nothing written")
	}

	rawMeta, err := os.ReadFile(filepath.Join(outDir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Compact: no indentation.
	for _, b := range rawMeta {
		if b == '\n' {
			t.Fatal("meta.json is not compact")
		}
	}
	meta := loadJSON(t, filepath.Join(outDir, "meta.json")).(map[string]any)
	if meta["updated"] != "2026-10-16" {
		t.Fatalf("updated = %v", meta["updated"])
	}
	if meta["date_min"] != "2026-10-09" || meta["date_max"] != "2026-10-16" {
		t.Fatalf("range = %v..%v", meta["date_min"], meta["date_max"])
	}
	if _, ok := meta["generated_at"]; !ok {
		t.Fatal("generated_at missing")
	}
	commodities := meta["commodities"].([]any)
	names := map[string]map[string]any{}
	for _, c := range commodities {
		m := c.(map[string]any)
		names[m["name"].(string)] = m
	}
	if _, ok := names["Premium Rice (Local)"]; !ok {
		t.Fatalf("missing canonical rice: %v", names)
	}
	if _, ok := names["Premium Rice (Loca)"]; ok {
		t.Fatal("alias leaked into commodities")
	}
	if names["Sugar (Washed)"]["group"] != "sugar" {
		t.Fatalf("sugar group = %v", names["Sugar (Washed)"]["group"])
	}
	if names["Bangus"]["group"] != nil {
		t.Fatalf("bangus group = %v", names["Bangus"]["group"])
	}

	markets := meta["markets"].([]any)
	shorts := map[string]string{}
	byID := map[string]string{}
	for _, m := range markets {
		mm := m.(map[string]any)
		shorts[mm["name"].(string)] = mm["short"].(string)
		byID[jsonNum(mm["id"])] = mm["name"].(string)
	}
	wantShorts := map[string]string{
		lasPinas: "Las Piñas", alabang: "Alabang",
		laHuerta: "La Huerta", bicutan: "Bicutan",
	}
	for n, s := range wantShorts {
		if shorts[n] != s {
			t.Fatalf("short[%q] = %q, want %q", n, shorts[n], s)
		}
	}
	defs := meta["default_markets"].([]any)
	if len(defs) != 4 {
		t.Fatalf("default_markets = %v", defs)
	}
	if byID[jsonNum(defs[0])] != lasPinas {
		t.Fatalf("first default = %v", byID[jsonNum(defs[0])])
	}

	riceID := jsonNum(names["Premium Rice (Local)"]["id"])
	series := loadJSON(t, filepath.Join(outDir, "series", "c"+riceID+".json")).(map[string]any)
	if len(series["d"].([]any)) != 2 {
		t.Fatalf("series dates = %v", series["d"])
	}
	var lasID, albID, huertaID string
	for id, name := range byID {
		switch name {
		case lasPinas:
			lasID = id
		case alabang:
			albID = id
		case laHuerta:
			huertaID = id
		}
	}
	m := series["m"].(map[string]any)
	if !floatsEqual(m[lasID].([]any), []any{50.0, 55.0}) {
		t.Fatalf("las piñas series = %v", m[lasID])
	}
	if !floatsEqual(m[albID].([]any), []any{52.0, 54.0}) {
		t.Fatalf("alabang series = %v", m[albID])
	}
	if !floatsEqual(m[huertaID].([]any), []any{nil, 53.46}) {
		t.Fatalf("huerta series = %v", m[huertaID])
	}

	fxData := loadJSON(t, filepath.Join(outDir, "fx.json")).(map[string]any)
	if len(fxData["d"].([]any)) != 1 || fxData["r"].([]any)[0].(float64) != 62.968 {
		t.Fatalf("fx = %v", fxData)
	}

	week := loadJSON(t, filepath.Join(outDir, "week.json")).(map[string]any)
	if week["latest"] != "2026-10-16" || week["baseline"] != "2026-10-09" {
		t.Fatalf("week window = %v / %v", week["latest"], week["baseline"])
	}
	items := week["items"].([]any)
	if len(items) != len(commodities) {
		t.Fatalf("items = %d, commodities = %d", len(items), len(commodities))
	}
	var rice map[string]any
	for _, it := range items {
		mm := it.(map[string]any)
		if mm["name"] == "Premium Rice (Local)" {
			rice = mm
		}
	}
	if rice == nil {
		t.Fatal("rice week item missing")
	}
	got := rice["m"].(map[string]any)[lasID].(map[string]any)
	if got["p"] != 55.0 || got["b"] != 50.0 || got["c"] != 10.0 {
		t.Fatalf("rice las piñas week = %v", got)
	}
	cheapAll := rice["cheap_all"].(map[string]any)
	if cheapAll["p"] != 53.46 {
		t.Fatalf("cheap_all = %v", cheapAll)
	}
}

func jsonNum(v any) string {
	switch n := v.(type) {
	case float64:
		if n == float64(int64(n)) {
			return itoa(int64(n))
		}
		return ""
	case string:
		return n
	}
	return ""
}

func floatsEqual(got []any, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if want[i] == nil {
			if got[i] != nil {
				return false
			}
			continue
		}
		g, ok := got[i].(float64)
		if !ok {
			return false
		}
		if g != want[i].(float64) {
			return false
		}
	}
	return true
}

func TestExportSiteEmptyDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "empty.db")
	outDir := filepath.Join(dir, "site")
	db, err := store.Connect(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := ExportSite(db, outDir); err != nil {
		t.Fatal(err)
	}
	meta := loadJSON(t, filepath.Join(outDir, "meta.json")).(map[string]any)
	if meta["updated"] != nil {
		t.Fatalf("updated = %v", meta["updated"])
	}
	if len(meta["commodities"].([]any)) != 0 {
		t.Fatalf("commodities = %v", meta["commodities"])
	}
	fxData := loadJSON(t, filepath.Join(outDir, "fx.json")).(map[string]any)
	if len(fxData["d"].([]any)) != 0 || len(fxData["r"].([]any)) != 0 {
		t.Fatalf("fx = %v", fxData)
	}
	week := loadJSON(t, filepath.Join(outDir, "week.json")).(map[string]any)
	if len(week["items"].([]any)) != 0 {
		t.Fatalf("week = %v", week)
	}
}
