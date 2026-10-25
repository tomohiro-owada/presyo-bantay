package signals

import (
	"database/sql"
	"testing"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

var regionTestMarkets = []string{
	"New Las Piñas City Public Market",
	"Suki Market Moonwalk",
	"La Huerta Market/Parañaque",
	"Alabang Central Market",
	"Bicutan Market",
}

type regionTestSetup struct {
	db   *sql.DB
	mkt  map[string]int64
	comm map[string]int64
	asOf string
}

func newRegionTestSetup(t *testing.T) *regionTestSetup {
	t.Helper()
	db, err := store.Connect(t.TempDir() + "/rtest.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &regionTestSetup{db: db, mkt: map[string]int64{}, comm: map[string]int64{}, asOf: "2026-10-23"}
	for _, m := range regionTestMarkets {
		res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		s.mkt[m] = id
	}
	return s
}

func (s *regionTestSetup) addComm(t *testing.T, name string) {
	t.Helper()
	res, err := s.db.Exec("INSERT INTO commodities (name, category, unit) VALUES (?, 'fish', 'per kg')", name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	s.comm[name] = id
}

func (s *regionTestSetup) priceAt(t *testing.T, date, mkt, comm string, v float64) {
	t.Helper()
	if _, err := s.db.Exec("INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
		date, s.mkt[mkt], s.comm[comm], v); err != nil {
		t.Fatalf("price: %v", err)
	}
}

// base fills agos days-1..1 at v (excludes as-of); asof sets the as-of price.
func (s *regionTestSetup) base(t *testing.T, mkt, comm string, days int, v float64) {
	t.Helper()
	b, _ := parseDate(s.asOf)
	for ago := days - 1; ago >= 1; ago-- {
		s.priceAt(t, fmtDate(b.AddDate(0, 0, -ago)), mkt, comm, v)
	}
}

func (s *regionTestSetup) asof(t *testing.T, mkt, comm string, v float64) {
	t.Helper()
	s.priceAt(t, s.asOf, mkt, comm, v)
}

func computeSouth(t *testing.T, s *regionTestSetup) *RegionSignals {
	t.Helper()
	sig, err := ComputeRegion(s.db, "south", RegionOptions{AsOf: s.asOf})
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func TestRoundHalfAway(t *testing.T) {
	cases := map[float64]int{-40.5: -41, 13.5: 14, -40.4: -40, 2.5: 3, -2.5: -3, 0.5: 1, -0.5: -1}
	for in, want := range cases {
		if got := roundHalfAway(in); got != want {
			t.Errorf("roundHalfAway(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestRegionUnknownRegion(t *testing.T) {
	s := newRegionTestSetup(t)
	if _, err := ComputeRegion(s.db, "narnia", RegionOptions{}); err == nil {
		t.Error("unknown region should fail")
	}
}

// Two markets qualify for the same commodity: lowest pct wins, and
// n_markets_with_deal counts both.
func TestRegionDealsDedupe(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	a, b := regionTestMarkets[0], regionTestMarkets[1]
	s.base(t, a, "Tilapia", 30, 100)
	s.asof(t, a, "Tilapia", 90) // -10
	s.base(t, b, "Tilapia", 30, 100)
	s.asof(t, b, "Tilapia", 85) // -15 wins
	sig := computeSouth(t, s)
	if len(sig.Deals) != 1 {
		t.Fatalf("deals = %+v, want 1", sig.Deals)
	}
	d := sig.Deals[0]
	if d.Market != b || d.MarketShort != "Suki Moonwalk" {
		t.Errorf("winner = %q (%q), want %q", d.Market, d.MarketShort, b)
	}
	if d.PctVsMedian30d != -15 || d.PctRounded != -15 || d.Price != 85 || d.Median30d != 100 {
		t.Errorf("deal = %+v", d)
	}
	if d.NMarketsWithDeal != 2 {
		t.Errorf("n_markets_with_deal = %d, want 2", d.NMarketsWithDeal)
	}
	if d.Rank != 1 || d.Name != "Tilapia" || d.Unit != "per kg" {
		t.Errorf("deal meta = %+v", d)
	}
}

// Tie on pct: lower price wins; then market name.
func TestRegionDedupeTie(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	a, b := regionTestMarkets[0], regionTestMarkets[1]
	s.base(t, a, "Tilapia", 30, 200)
	s.asof(t, a, "Tilapia", 180) // -10
	s.base(t, b, "Tilapia", 30, 100)
	s.asof(t, b, "Tilapia", 90) // -10, cheaper wins
	sig := computeSouth(t, s)
	if len(sig.Deals) != 1 {
		t.Fatalf("deals = %+v", sig.Deals)
	}
	if sig.Deals[0].Market != b {
		t.Errorf("tie winner = %q, want %q", sig.Deals[0].Market, b)
	}
}

// Fewer than 10 priced days, or no price on as-of: not a candidate.
func TestRegionMinDaysAndAsOf(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	a, b := regionTestMarkets[0], regionTestMarkets[1]
	s.base(t, a, "Tilapia", 9, 100)
	s.asof(t, a, "Tilapia", 80)      // only 9 days -> out
	s.base(t, b, "Tilapia", 30, 100) // no as-of price -> out
	sig := computeSouth(t, s)
	for _, d := range sig.Deals {
		if d.Name == "Tilapia" {
			t.Errorf("Tilapia must not deal with thin/missing basis: %+v", d)
		}
	}
}

// Exactly 10 priced days qualifies.
func TestRegionMinDaysBoundary(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	a := regionTestMarkets[0]
	s.base(t, a, "Tilapia", 10, 100) // agos 9..1 = 9 days + as-of = 10
	s.asof(t, a, "Tilapia", 80)      // -20
	sig := computeSouth(t, s)
	if len(sig.Deals) != 1 || sig.Deals[0].PctVsMedian30d != -20 {
		t.Errorf("10-day basis should deal: %+v", sig.Deals)
	}
}

// Trend above +5 filters the deal out; trend exactly +5 keeps it.
func TestRegionTrendFilter(t *testing.T) {
	mk := func(t *testing.T, comm string, day7, asofV float64) *regionTestSetup {
		s := newRegionTestSetup(t)
		s.addComm(t, comm)
		a := regionTestMarkets[0]
		b, _ := parseDate(s.asOf)
		for ago := 29; ago >= 8; ago-- {
			s.priceAt(t, fmtDate(b.AddDate(0, 0, -ago)), a, comm, 115)
		}
		for ago := 7; ago >= 1; ago-- {
			v := 80.0
			if ago == 7 {
				v = day7
			}
			s.priceAt(t, fmtDate(b.AddDate(0, 0, -ago)), a, comm, v)
		}
		_ = asofV
		return s
	}
	_ = mk
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	s.addComm(t, "Bangus")
	a := regionTestMarkets[0]
	b, _ := parseDate(s.asOf)
	fill := func(comm string, day7 float64) {
		for ago := 29; ago >= 8; ago-- {
			s.priceAt(t, fmtDate(b.AddDate(0, 0, -ago)), a, comm, 115)
		}
		for ago := 7; ago >= 1; ago-- {
			v := 80.0
			if ago == 7 {
				v = day7
			}
			s.priceAt(t, fmtDate(b.AddDate(0, 0, -ago)), a, comm, v)
		}
		s.asof(t, a, comm, 105) // median 115 -> -8.7
	}
	fill("Tilapia", 80) // trend (105-80)/80 = +31.25 -> excluded
	fill("Bangus", 100) // trend (105-100)/100 = +5.0 -> kept
	sig := computeSouth(t, s)
	names := map[string]bool{}
	for _, d := range sig.Deals {
		names[d.Name] = true
	}
	if names["Tilapia"] {
		t.Errorf("trend +31.2 deal must be filtered: %+v", sig.Deals)
	}
	if !names["Bangus"] {
		t.Errorf("trend +5.0 deal must be kept: %+v", sig.Deals)
	}
}

// Ranking ascending, top 5, ranks sequential.
func TestRegionRankingTop5(t *testing.T) {
	s := newRegionTestSetup(t)
	comms := []string{"Tilapia", "Bangus", "Chayote", "Ampalaya", "Eggplant", "Tomato", "Cabbage"}
	for _, c := range comms {
		s.addComm(t, c)
	}
	a := regionTestMarkets[0]
	pcts := []float64{-30, -25, -20, -15, -12, -10, -9}
	for i, c := range comms {
		s.base(t, a, c, 30, 100)
		s.asof(t, a, c, 100+pcts[i])
	}
	sig := computeSouth(t, s)
	if len(sig.Deals) != 5 {
		t.Fatalf("deals = %+v, want top 5", sig.Deals)
	}
	for i, d := range sig.Deals {
		if d.Rank != i+1 {
			t.Errorf("deal rank = %d, want %d", d.Rank, i+1)
		}
	}
	if sig.Deals[0].PctVsMedian30d != -30 || sig.Deals[4].PctVsMedian30d != -12 {
		t.Errorf("ranking wrong: %+v", sig.Deals)
	}
}

// Pricier majority rule: 2 of 4 with pct >= +8 -> entry; 1 of 4 -> none.
func TestRegionPricierMajority(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	mk := regionTestMarkets[:4]
	for i, m := range mk {
		s.base(t, m, "Tilapia", 30, 100)
		if i < 2 {
			s.asof(t, m, "Tilapia", 110) // +10
		} else {
			s.asof(t, m, "Tilapia", 100) // 0
		}
	}
	sig := computeSouth(t, s)
	if len(sig.Pricier) != 1 {
		t.Fatalf("pricier = %+v, want 1", sig.Pricier)
	}
	p := sig.Pricier[0]
	if p.PriceMedian != 110 || p.PctMedian != 10 || p.PctRounded != 10 {
		t.Errorf("pricier = %+v", p)
	}
	if p.NMarketsPricier != 2 || p.NMarkets != 4 || p.Rank != 1 || p.Unit != "per kg" {
		t.Errorf("pricier meta = %+v", p)
	}

	s2 := newRegionTestSetup(t)
	s2.addComm(t, "Tilapia")
	for i, m := range mk {
		s2.base(t, m, "Tilapia", 30, 100)
		if i == 0 {
			s2.asof(t, m, "Tilapia", 110)
		} else {
			s2.asof(t, m, "Tilapia", 100)
		}
	}
	sig2 := computeSouth(t, s2)
	if len(sig2.Pricier) != 0 {
		t.Errorf("1 of 4 must not be pricier: %+v", sig2.Pricier)
	}
}

// Missing markets: mapped markets with no as-of price.
func TestRegionMissingMarkets(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	a := regionTestMarkets[0]
	s.base(t, a, "Tilapia", 30, 100)
	s.asof(t, a, "Tilapia", 90)
	sig := computeSouth(t, s)
	if len(sig.MarketsUsed) != 1 || sig.MarketsUsed[0].Name != a {
		t.Errorf("markets_used = %+v", sig.MarketsUsed)
	}
	if sig.MarketsUsed[0].PricedItemsOnAsOf != 1 || sig.MarketsUsed[0].Short != "Las Piñas" {
		t.Errorf("markets_used[0] = %+v", sig.MarketsUsed[0])
	}
	if len(sig.MarketsMissing) != 8 {
		t.Errorf("markets_missing = %v", sig.MarketsMissing)
	}
	if sig.Region.ID != "south" || sig.Region.TitleTL == "" {
		t.Errorf("region = %+v", sig.Region)
	}
	if sig.Thresholds.DealPct != 8 || sig.Thresholds.MinDays != 10 || sig.Thresholds.MaxTrend7dPct != 5 {
		t.Errorf("thresholds = %+v", sig.Thresholds)
	}
	if sig.GeneratedBy == "" || sig.AsOf != s.asOf {
		t.Errorf("meta = %+v", sig)
	}
}

// Default as-of: latest price date across the region's markets.
func TestRegionDefaultAsOf(t *testing.T) {
	s := newRegionTestSetup(t)
	s.addComm(t, "Tilapia")
	a := regionTestMarkets[0]
	s.base(t, a, "Tilapia", 30, 100)
	s.asof(t, a, "Tilapia", 90)
	sig, err := ComputeRegion(s.db, "south", RegionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sig.AsOf != "2026-10-23" {
		t.Errorf("as_of = %q", sig.AsOf)
	}
}
