package digest

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

const digestTestD = "2026-10-30"

// digestTestRaws are raw commodity names that canonicalize to KeyItems.
var digestTestRaws = []string{
	"Well-milled Rice (Local)",
	"Fresh Pork Liempo (Local)",
	"Fresh Whole Chicken",
	"Egg (Medium)",
	"Galunggong",
	"Tomato",
	"Red Onion (Local)",
	"Sugar (Refined)",
}

var digestTestBase = map[string]float64{
	"Well-milled Rice (Local)":  60,
	"Fresh Pork Liempo (Local)": 340,
	"Fresh Whole Chicken":       185,
	"Egg (Medium)":              7.5,
	"Galunggong":                220,
	"Tomato":                    95,
	"Red Onion (Local)":         160,
	"Sugar (Refined)":           92,
}

// buildDigestTestDB creates a temp DB with the first 3 markets of all 5
// regions, all 8 key items priced flat for 12 consecutive days up to D.
func buildDigestTestDB(t *testing.T) (*sql.DB, map[string]int64, map[string]int64) {
	t.Helper()
	db, err := store.Connect(filepath.Join(t.TempDir(), "digest.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mkIDs := map[string]int64{}
	for _, r := range regions.Regions {
		for _, m := range r.Markets[:3] {
			res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m.Name)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := res.LastInsertId()
			mkIDs[m.Name] = id
		}
	}
	commIDs := map[string]int64{}
	for _, raw := range digestTestRaws {
		unit := "per kg"
		if raw == "Egg (Medium)" {
			unit = "per piece"
		}
		res, err := db.Exec("INSERT INTO commodities (name, category, unit) VALUES (?, 'misc', ?)", raw, unit)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		commIDs[raw] = id
	}
	d, _ := time.Parse("2006-01-02", digestTestD)
	for ago := 11; ago >= 0; ago-- {
		ds := d.AddDate(0, 0, -ago).Format("2006-01-02")
		for _, mid := range mkIDs {
			for raw, cid := range commIDs {
				if _, err := db.Exec(
					"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
					ds, mid, cid, digestTestBase[raw]); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return db, mkIDs, commIDs
}

func regionMarkets(regionID string) []string {
	for _, r := range regions.Regions {
		if r.ID == regionID {
			var out []string
			for _, m := range r.Markets[:3] {
				out = append(out, m.Name)
			}
			return out
		}
	}
	return nil
}

func setPrice(t *testing.T, db *sql.DB, mkIDs, commIDs map[string]int64, date, region, raw string, v float64) {
	t.Helper()
	for _, m := range regionMarkets(region) {
		if _, err := db.Exec(
			"UPDATE prices SET price = ? WHERE date = ? AND market_id = ? AND commodity_id = ?",
			v, date, mkIDs[m], commIDs[raw]); err != nil {
			t.Fatal(err)
		}
	}
}

func statusOf(d *Digest, id string) RegionStatus {
	for _, r := range d.Regions {
		if r.ID == id {
			return r
		}
	}
	return RegionStatus{}
}

func intPtr(v int) *int { return &v }

func TestBuildTrend(t *testing.T) {
	db, mkIDs, commIDs := buildDigestTestDB(t)
	setPrice(t, db, mkIDs, commIDs, digestTestD, "south", "Well-milled Rice (Local)", 60*1.03)
	d, err := Build(db, digestTestD, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Included(); len(got) != 5 {
		t.Fatalf("included = %v, want all 5", got)
	}
	rice := &d.Rows[0]
	if rice.Canonical != "Well-milled Rice (Local)" {
		t.Fatalf("rows[0] = %q", rice.Canonical)
	}
	cell := rice.Cells["south"]
	if cell == nil {
		t.Fatal("south rice cell missing")
	}
	if cell.Dir != DirUp || cell.Pct == nil || *cell.Pct != 3 {
		t.Errorf("south rice = dir %q pct %v, want up 3", cell.Dir, cell.Pct)
	}
	for _, rid := range []string{"manila", "qc", "north", "east"} {
		c := rice.Cells[rid]
		if c == nil {
			t.Errorf("%s rice cell missing", rid)
			continue
		}
		if c.Dir != DirFlat || c.Pct == nil || *c.Pct != 0 {
			t.Errorf("%s rice = dir %q pct %v, want flat 0", rid, c.Dir, c.Pct)
		}
	}
	if rice.Metro == nil {
		t.Fatal("metro rice cell missing")
	} else if rice.Metro.Price != 60 || rice.Metro.Dir != DirFlat {
		t.Errorf("metro rice = %+v, want price 60 flat", rice.Metro)
	}
	if d.PrevAsOf != "2026-10-29" {
		t.Errorf("PrevAsOf = %q, want 2026-10-29", d.PrevAsOf)
	}
}

func TestLikeForLike(t *testing.T) {
	db, mkIDs, commIDs := buildDigestTestDB(t)
	// Two south markets lose D-1 for rice; a third price appears only on D.
	// Like-for-like must not invent a trend from the D-only markets.
	south := regionMarkets("south")
	for _, m := range south[1:] {
		if _, err := db.Exec("DELETE FROM prices WHERE date = ? AND market_id = ? AND commodity_id = ?",
			"2026-10-29", mkIDs[m], commIDs["Well-milled Rice (Local)"]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("UPDATE prices SET price = ? WHERE date = ? AND market_id = ? AND commodity_id = ?",
			61.8, digestTestD, mkIDs[m], commIDs["Well-milled Rice (Local)"]); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Build(db, digestTestD, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cell := d.Rows[0].Cells["south"]
	if cell == nil {
		t.Fatal("south rice cell missing")
	}
	if cell.Pct != nil || cell.Dir != DirNone {
		t.Errorf("D-only markets must not move the trend: pct %v dir %q", cell.Pct, cell.Dir)
	}
}

func TestRegionDroppedWhenTooFewCells(t *testing.T) {
	db, mkIDs, commIDs := buildDigestTestDB(t)
	for _, raw := range digestTestRaws[4:] {
		for _, m := range regionMarkets("east") {
			if _, err := db.Exec("DELETE FROM prices WHERE date = ? AND market_id = ? AND commodity_id = ?",
				digestTestD, mkIDs[m], commIDs[raw]); err != nil {
				t.Fatal(err)
			}
		}
	}
	d, err := Build(db, digestTestD, Options{})
	if err != nil {
		t.Fatal(err)
	}
	st := statusOf(d, "east")
	if st.Included {
		t.Error("east must be dropped with only 4 valid cells")
	}
	if !strings.Contains(st.Reason, "valid") {
		t.Errorf("drop reason = %q, want mention of valid", st.Reason)
	}
	if got := d.Included(); len(got) != 4 {
		t.Errorf("included = %v, want 4", got)
	}
	for _, id := range []string{"south", "manila", "qc", "north"} {
		if !statusOf(d, id).Included {
			t.Errorf("%s must stay included", id)
		}
	}
}

func TestForceFailDropsRegion(t *testing.T) {
	db, _, _ := buildDigestTestDB(t)
	d, err := Build(db, digestTestD, Options{ForceFail: func(id string) bool { return id == "north" }})
	if err != nil {
		t.Fatal(err)
	}
	if statusOf(d, "north").Included {
		t.Error("north must be dropped by ForceFail")
	}
	if got := d.Included(); len(got) != 4 {
		t.Errorf("included = %v, want 4", got)
	}
}

func TestImplausibleCellBlanked(t *testing.T) {
	db, mkIDs, commIDs := buildDigestTestDB(t)
	setPrice(t, db, mkIDs, commIDs, digestTestD, "south", "Tomato", 95*5)
	d, err := Build(db, digestTestD, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var tomato *Row
	for i := range d.Rows {
		if d.Rows[i].Canonical == "Tomato" {
			tomato = &d.Rows[i]
		}
	}
	if tomato == nil {
		t.Fatal("no tomato row")
	}
	if tomato.Cells["south"] != nil {
		t.Errorf("5x-median tomato cell must be blanked, got %+v", tomato.Cells["south"])
	}
	if !statusOf(d, "south").Included {
		t.Error("south must stay included with 7 valid cells")
	}
	for _, rid := range []string{"manila", "qc", "north", "east"} {
		if tomato.Cells[rid] == nil {
			t.Errorf("%s tomato cell must survive", rid)
		}
	}
}

func TestPrevAsOfSkipsGap(t *testing.T) {
	db, mkIDs, commIDs := buildDigestTestDB(t)
	for _, m := range regionMarkets("qc") {
		if _, err := db.Exec("DELETE FROM prices WHERE date = ? AND market_id = ?",
			"2026-10-29", mkIDs[m]); err != nil {
			t.Fatal(err)
		}
	}
	_ = commIDs
	d, err := Build(db, digestTestD, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := statusOf(d, "qc").PrevAsOf; got != "2026-10-28" {
		t.Errorf("qc PrevAsOf = %q, want 2026-10-28", got)
	}
	if got := statusOf(d, "south").PrevAsOf; got != "2026-10-29" {
		t.Errorf("south PrevAsOf = %q, want 2026-10-29", got)
	}
	if !statusOf(d, "qc").Included {
		t.Error("qc must stay included")
	}
}

func TestCaptionCheck(t *testing.T) {
	db, mkIDs, commIDs := buildDigestTestDB(t)
	setPrice(t, db, mkIDs, commIDs, digestTestD, "south", "Well-milled Rice (Local)", 60*1.03)
	d, err := Build(db, digestTestD, Options{})
	if err != nil {
		t.Fatal(err)
	}
	text := RenderCaption(d)
	if rep := CheckCaption(d, text); rep.Status != "pass" {
		t.Fatalf("caption must pass, flagged=%v", rep.Flagged)
	}
	badPeso := strings.Replace(text, "₱60/kg", "₱61/kg", 1)
	if badPeso == text {
		t.Fatal("peso fixture not found in caption")
	}
	if rep := CheckCaption(d, badPeso); rep.Status == "pass" {
		t.Error("mutated peso amount must fail the check")
	}
	badPct := strings.Replace(text, "→±0%", "→+1%", 1)
	if badPct == text {
		t.Fatal("pct fixture not found in caption")
	}
	if rep := CheckCaption(d, badPct); rep.Status == "pass" {
		t.Error("mutated pct must fail the check")
	}
	// Glyph mismatch against a hand-built digest containing -3%.
	d2 := &Digest{AsOf: digestTestD, Rows: []Row{{
		Canonical: "Tomato", TL: "Kamatis", EN: "Tomato", JA: "トマト",
		Cells: map[string]*Cell{},
		Metro: &Cell{Price: 100, Pct: intPtr(-3), Dir: DirDown, Markets: 3},
	}}}
	if rep := CheckCaption(d2, "x ₱100 ▲−3%"); rep.Status == "pass" {
		t.Error("▲ next to a negative percent must fail")
	} else {
		found := false
		for _, f := range rep.Flagged {
			if strings.Contains(f, "non-positive") {
				found = true
			}
		}
		if !found {
			t.Errorf("want glyph-mismatch flag, got %v", rep.Flagged)
		}
	}
}

func TestCaptionMentionsDropped(t *testing.T) {
	db, _, _ := buildDigestTestDB(t)
	d, err := Build(db, digestTestD, Options{ForceFail: func(id string) bool { return id == "east" }})
	if err != nil {
		t.Fatal(err)
	}
	text := RenderCaption(d)
	for _, want := range []string{"Silangan", "East Metro", "東部"} {
		if !strings.Contains(text, want) {
			t.Errorf("caption must name dropped region %q", want)
		}
	}
	for _, want := range []string{
		"🛒 Presyo sa Metro Manila",
		"🇬🇧 Metro Manila prices",
		"🇯🇵 メトロマニラの価格",
		DayURL(digestTestD),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("caption must contain %q", want)
		}
	}
}
