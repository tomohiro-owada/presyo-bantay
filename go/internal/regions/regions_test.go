package regions

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestEverySnapshotMarketMapped verifies that every market in the local
// snapshot maps to exactly one region.
func TestEverySnapshotMarketMapped(t *testing.T) {
	db, err := sql.Open("sqlite", "file:/tmp/presyo-snap.db?mode=ro")
	if err != nil {
		t.Skipf("no snapshot: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("no snapshot: %v", err)
	}
	rows, err := db.Query("SELECT name FROM markets ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		count++
		seen := 0
		for i := range Regions {
			for _, m := range Regions[i].Markets {
				if m.Name == name {
					seen++
				}
			}
		}
		if seen != 1 {
			t.Errorf("market %q maps to %d regions, want exactly 1", name, seen)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Error("snapshot has no markets")
	}
}

// TestRegionIDs checks ids, order and titles.
func TestRegionIDs(t *testing.T) {
	want := []Region{
		{ID: "south", Order: 1, TitleTL: "South Metro", TitleEN: "South Metro", TitleJA: "南部（ラス・ピニャス周辺）"},
		{ID: "manila", Order: 2, TitleTL: "Maynila at Pasay", TitleEN: "Manila & Pasay", TitleJA: "マニラ・パサイ"},
		{ID: "qc", Order: 3, TitleTL: "Quezon City", TitleEN: "Quezon City", TitleJA: "ケソン市"},
		{ID: "north", Order: 4, TitleTL: "CAMANAVA", TitleEN: "North (CAMANAVA)", TitleJA: "北部（カマナバ）"},
		{ID: "east", Order: 5, TitleTL: "Silangan", TitleEN: "East Metro", TitleJA: "東部"},
	}
	if len(Regions) != len(want) {
		t.Fatalf("got %d regions, want %d", len(Regions), len(want))
	}
	for i, w := range want {
		g := Regions[i]
		if g.ID != w.ID || g.Order != w.Order || g.TitleTL != w.TitleTL || g.TitleEN != w.TitleEN || g.TitleJA != w.TitleJA {
			t.Errorf("region %d = %+v, want %+v", i, g, w)
		}
	}
}

// TestShortNamesHaveNoParens guards the PNG "(<short>)" labels and the
// "· <short>" text lines against nested parentheses: no short display name
// may itself contain parentheses.
func TestShortNamesHaveNoParens(t *testing.T) {
	for _, r := range Regions {
		for _, m := range r.Markets {
			if strings.ContainsAny(m.Short, "()（）") {
				t.Errorf("region %s market %q short %q contains parentheses", r.ID, m.Name, m.Short)
			}
		}
	}
}

// TestRegionOf checks lookup behaviour including unknown markets.
func TestRegionOf(t *testing.T) {
	r, short, ok := RegionOf("Paco Market")
	if !ok || r.ID != "manila" || short != "Paco" {
		t.Errorf("Paco Market -> %v %q %v", r, short, ok)
	}
	if _, _, ok := RegionOf("No Such Market"); ok {
		t.Error("unknown market should not map")
	}
	if got := ShortOf("No Such Market"); got != "No Such Market" {
		t.Errorf("ShortOf unknown = %q", got)
	}
	if got := ShortOf("Farmers Market"); got != "Farmers, Cubao" {
		t.Errorf("ShortOf Farmers = %q", got)
	}
}
