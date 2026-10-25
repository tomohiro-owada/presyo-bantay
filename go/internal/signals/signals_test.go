package signals

import (
	"database/sql"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

func fptr(v float64) *float64 { return &v }

func TestMedian(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("odd median = %v, want 2", got)
	}
	if got := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("even median = %v, want 2.5", got)
	}
	if got := Median(nil); !math.IsNaN(got) {
		t.Errorf("empty median = %v, want NaN", got)
	}
}

func TestEffectivePrice(t *testing.T) {
	null := sql.NullFloat64{}
	if v, ok := EffectivePrice(f(100), null, null); !ok || v != 100 {
		t.Errorf("price present: got %v,%v", v, ok)
	}
	if v, ok := EffectivePrice(null, f(90), f(110)); !ok || v != 100 {
		t.Errorf("midpoint: got %v,%v", v, ok)
	}
	if _, ok := EffectivePrice(null, f(90), null); ok {
		t.Error("half range should be skipped")
	}
	if _, ok := EffectivePrice(null, null, null); ok {
		t.Error("all NULL should be skipped")
	}
}

func f(v float64) sql.NullFloat64 { return sql.NullFloat64{Float64: v, Valid: true} }

func TestCanonicalCommodityMerge(t *testing.T) {
	if got := CanonicalCommodity("Galunggong"); got != "Galunggong (Local)" {
		t.Errorf("Galunggong merged = %q", got)
	}
	if got := CanonicalCommodity("Premium Rice (Loca)"); got != "Premium Rice (Local)" {
		t.Errorf("rice alias = %q", got)
	}
	if got := CanonicalCommodity("Tilapia"); got != "Tilapia" {
		t.Errorf("Tilapia = %q", got)
	}
}

// testSetup builds a scratch DB with the v2 market set. Commodities must
// be added per test with addComm.
type testSetup struct {
	db     *sql.DB
	market map[string]int64
	comm   map[string]int64
	asOf   string
}

func newTestSetup(t *testing.T) *testSetup {
	t.Helper()
	db, err := store.Connect(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &testSetup{db: db, market: map[string]int64{}, comm: map[string]int64{}, asOf: "2026-10-23"}
	for _, m := range []string{"Focus M", "Near N1", "Near N2", "Near N3", "Other O"} {
		res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", m)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		s.market[m] = id
	}
	return s
}

func (s *testSetup) addComm(t *testing.T, name, cat string) {
	t.Helper()
	res, err := s.db.Exec("INSERT INTO commodities (name, category, unit) VALUES (?, ?, 'per kg')", name, cat)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	s.comm[name] = id
}

func (s *testSetup) day(ago int) string {
	base, _ := time.Parse("2006-01-02", s.asOf)
	return base.AddDate(0, 0, -ago).Format("2006-01-02")
}

func (s *testSetup) price(t *testing.T, ago int, mkt, comm string, price, low, high *float64) {
	t.Helper()
	var p, l, h any
	if price != nil {
		p = *price
	}
	if low != nil {
		l = *low
	}
	if high != nil {
		h = *high
	}
	if _, err := s.db.Exec("INSERT INTO prices (date, market_id, commodity_id, price, price_low, price_high) VALUES (?, ?, ?, ?, ?, ?)",
		s.day(ago), s.market[mkt], s.comm[comm], p, l, h); err != nil {
		t.Fatalf("price: %v", err)
	}
}

func (s *testSetup) compute(t *testing.T) *Signals {
	t.Helper()
	sig, err := Compute(s.db, Options{
		AsOf: s.asOf, Focus: "Focus M",
		Nearby: []string{"Near N1", "Near N2", "Near N3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func findComm(t *testing.T, s *Signals, name string) Commodity {
	t.Helper()
	for _, c := range s.Commodities {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("commodity %q not in signals", name)
	return Commodity{}
}

func hasComm(s *Signals, name string) bool {
	for _, c := range s.Commodities {
		if c.Name == name {
			return true
		}
	}
	return false
}

// constantFocus fills `days` focus days (ago=days..1) at `v` plus the
// as-of focus price.
func (s *testSetup) constantFocus(t *testing.T, comm string, days int, v float64, asOfPrice float64) {
	t.Helper()
	for ago := days; ago >= 1; ago-- {
		s.price(t, ago, "Focus M", comm, fptr(v), nil, nil)
	}
	s.price(t, 0, "Focus M", comm, fptr(asOfPrice), nil, nil)
}

func TestBasisFocus(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Tilapia", "fish")
	s.constantFocus(t, "Tilapia", 29, 100, 92)
	// Other O must not affect the basis or market_prices.
	for ago := 29; ago >= 0; ago-- {
		s.price(t, ago, "Other O", "Tilapia", fptr(500), nil, nil)
	}
	sig := s.compute(t)
	c := findComm(t, sig, "Tilapia")
	if c.Basis != BasisLasPinas {
		t.Errorf("basis = %q, want las_pinas", c.Basis)
	}
	if c.BasisLabel != BasisLabelLasPinas {
		t.Errorf("basis_label = %q", c.BasisLabel)
	}
	if c.Price != 92 || c.Median30d != 100 || c.NDays30d != 30 {
		t.Errorf("price/median/days = %v %v %v, want 92 100 30", c.Price, c.Median30d, c.NDays30d)
	}
	if c.PctVsMedian30d != -8 {
		t.Errorf("pct = %v, want -8", c.PctVsMedian30d)
	}
	if c.DiffVsMedian30d != -8 {
		t.Errorf("diff = %v, want -8", c.DiffVsMedian30d)
	}
	if c.Trend7dPct == nil || *c.Trend7dPct != -8 || *c.Trend7dBaseDate != "2026-10-16" || *c.Trend7dBaseVal != 100 {
		t.Errorf("trend7 = %v %v %v, want -8 2026-10-16 100", c.Trend7dPct, c.Trend7dBaseDate, c.Trend7dBaseVal)
	}
	if len(c.MarketPrices) != 1 || c.MarketPrices[ShortLasPinas] != 92 {
		t.Errorf("market_prices = %v, want only Las Piñas 92", c.MarketPrices)
	}
	if c.CheapestNearby != nil {
		t.Errorf("cheapest_nearby should be null with 1 market, got %+v", c.CheapestNearby)
	}
}

func TestBasisFocusMidpointAndTrendNull(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Bangus", "fish")
	// Focus via NULL-price midpoint on as-of; gap around day(-7) so the
	// trend baseline is too old (null).
	for ago := 29; ago >= 1; ago-- {
		if ago >= 4 && ago <= 10 {
			continue
		}
		s.price(t, ago, "Focus M", "Bangus", fptr(100), nil, nil)
	}
	s.price(t, 0, "Focus M", "Bangus", nil, fptr(90), fptr(110)) // midpoint 100
	sig := s.compute(t)
	c := findComm(t, sig, "Bangus")
	if c.Basis != BasisLasPinas || c.Price != 100 {
		t.Errorf("midpoint basis/price = %q %v, want las_pinas 100", c.Basis, c.Price)
	}
	if c.Trend7dPct != nil {
		t.Errorf("trend7 should be null (baseline >3d from target), got %v", *c.Trend7dPct)
	}
}

func TestBasisFallbackNearby(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Chayote", "vegetables")
	// No focus prices at all; N1+N2 report every day at 100, as-of N1=90 N2=94.
	for ago := 29; ago >= 1; ago-- {
		s.price(t, ago, "Near N1", "Chayote", fptr(100), nil, nil)
		s.price(t, ago, "Near N2", "Chayote", fptr(100), nil, nil)
	}
	s.price(t, 0, "Near N1", "Chayote", fptr(90), nil, nil)
	s.price(t, 0, "Near N2", "Chayote", fptr(94), nil, nil)
	sig := s.compute(t)
	c := findComm(t, sig, "Chayote")
	if c.Basis != BasisNearbyMedian {
		t.Errorf("basis = %q, want nearby_median", c.Basis)
	}
	if c.BasisLabel != BasisLabelNearby {
		t.Errorf("basis_label = %q", c.BasisLabel)
	}
	if c.Price != 92 || c.Median30d != 100 || c.NDays30d != 30 {
		t.Errorf("price/median/days = %v %v %v, want 92 100 30", c.Price, c.Median30d, c.NDays30d)
	}
	if c.PctVsMedian30d != -8 {
		t.Errorf("pct = %v, want -8", c.PctVsMedian30d)
	}
}

func TestBasisThinFocusFallsBack(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Ampalaya", "vegetables")
	// Focus has as-of price but only 3 focus days (< 10); nearby N1+N2
	// report daily so the basis must fall back to nearby_median.
	for _, ago := range []int{2, 1} {
		s.price(t, ago, "Focus M", "Ampalaya", fptr(100), nil, nil)
	}
	s.price(t, 0, "Focus M", "Ampalaya", fptr(100), nil, nil)
	for ago := 29; ago >= 0; ago-- {
		s.price(t, ago, "Near N1", "Ampalaya", fptr(80), nil, nil)
		s.price(t, ago, "Near N2", "Ampalaya", fptr(80), nil, nil)
	}
	sig := s.compute(t)
	c := findComm(t, sig, "Ampalaya")
	if c.Basis != BasisNearbyMedian || c.Price != 80 {
		t.Errorf("thin focus should fall back: basis=%q price=%v", c.Basis, c.Price)
	}
}

func TestOmittedWithoutBasis(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Eggplant", "vegetables")
	// Focus missing; only ONE nearby market on as-of -> omitted.
	for ago := 29; ago >= 1; ago-- {
		s.price(t, ago, "Near N1", "Eggplant", fptr(80), nil, nil)
	}
	s.price(t, 0, "Near N1", "Eggplant", fptr(80), nil, nil)
	sig := s.compute(t)
	if hasComm(sig, "Eggplant") {
		t.Error("Eggplant with <2 nearby markets on as-of should be omitted")
	}
}

func TestNearbySeriesNeedsTwoMarketsDaily(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Sugar (Refined)", "sugar")
	// N1 reports all 30 days at 100; N2 only on as-of (110) and day(-1)
	// (110): only 2 series days qualify, n_days_30d = 2: day(-1) median
	// of (100,110) = 105 and as-of median of (110,110) = 110.
	for ago := 29; ago >= 1; ago-- {
		s.price(t, ago, "Near N1", "Sugar (Refined)", fptr(100), nil, nil)
	}
	s.price(t, 1, "Near N2", "Sugar (Refined)", fptr(110), nil, nil)
	s.price(t, 0, "Near N1", "Sugar (Refined)", fptr(110), nil, nil)
	s.price(t, 0, "Near N2", "Sugar (Refined)", fptr(110), nil, nil)
	sig := s.compute(t)
	c := findComm(t, sig, "Sugar (Refined)")
	if c.Basis != BasisNearbyMedian {
		t.Errorf("basis = %q", c.Basis)
	}
	if c.NDays30d != 2 || c.Median30d != 107.5 {
		t.Errorf("days/median = %v %v, want 2 107.5", c.NDays30d, c.Median30d)
	}
}

func TestCheapestNearby(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Tomato", "vegetables")
	s.constantFocus(t, "Tomato", 29, 100, 100)
	s.price(t, 0, "Near N1", "Tomato", fptr(90), nil, nil)
	s.price(t, 0, "Near N2", "Tomato", fptr(95), nil, nil)
	// Multi-day: candidate must also beat the basis on 2 of last 3 dates.
	s.price(t, 1, "Near N1", "Tomato", fptr(90), nil, nil)
	s.price(t, 1, "Near N2", "Tomato", fptr(95), nil, nil)
	s.price(t, 2, "Near N1", "Tomato", fptr(90), nil, nil)
	s.price(t, 2, "Near N2", "Tomato", fptr(95), nil, nil)
	sig := s.compute(t)
	c := findComm(t, sig, "Tomato")
	if len(c.MarketPrices) != 3 {
		t.Fatalf("market_prices = %v, want 3 entries", c.MarketPrices)
	}
	if c.CheapestNearby == nil {
		t.Fatal("cheapest_nearby should be present")
	}
	if c.CheapestNearby.Market != ShortAlabang || c.CheapestNearby.Price != 90 || c.CheapestNearby.DiffVsBasis != -10 {
		t.Errorf("cheapest = %+v, want {Alabang 90 -10}", c.CheapestNearby)
	}
	if c.CheapestNearby.DaysCheaper != 3 || c.CheapestNearby.DaysChecked != 3 {
		t.Errorf("days = %+v, want 3/3", c.CheapestNearby)
	}
}

func TestCheapestNearbyNeedsThreeMarkets(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "White Potato (Local)", "vegetables")
	s.constantFocus(t, "White Potato (Local)", 29, 100, 100)
	s.price(t, 0, "Near N1", "White Potato (Local)", fptr(90), nil, nil)
	sig := s.compute(t)
	c := findComm(t, sig, "White Potato (Local)")
	if c.CheapestNearby != nil {
		t.Errorf("cheapest with 2 markets should be null, got %+v", c.CheapestNearby)
	}
}

func TestCheapestNearbyNotCheaper(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Cabbage", "vegetables")
	// Basis (focus) is already the minimum: no cheapest_nearby.
	s.constantFocus(t, "Cabbage", 29, 100, 90)
	s.price(t, 0, "Near N1", "Cabbage", fptr(95), nil, nil)
	s.price(t, 0, "Near N2", "Cabbage", fptr(96), nil, nil)
	sig := s.compute(t)
	c := findComm(t, sig, "Cabbage")
	if c.CheapestNearby != nil {
		t.Errorf("cheapest should be null when basis is min, got %+v", c.CheapestNearby)
	}
}

func TestDealThresholdBoundary(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Tilapia", "fish") // -8.0 -> deal
	s.addComm(t, "Bangus", "fish")  // -7.9 -> neither
	s.constantFocus(t, "Tilapia", 29, 100, 92)
	s.constantFocus(t, "Bangus", 29, 100, 92.1)
	sig := s.compute(t)
	names := map[string]bool{}
	for _, d := range sig.Deals {
		names[d.Name] = true
	}
	if !names["Tilapia"] {
		t.Errorf("pct -8.0 must be a deal; deals=%+v", sig.Deals)
	}
	if names["Bangus"] {
		t.Errorf("pct -7.9 must not be a deal; deals=%+v", sig.Deals)
	}
	for _, p := range sig.Pricier {
		if p.Name == "Tilapia" || p.Name == "Bangus" {
			t.Errorf("negative pct in pricier: %+v", p)
		}
	}
	// Deal entry shape.
	for _, d := range sig.Deals {
		if d.Name == "Tilapia" {
			if d.Rank != 1 || d.Price != 92 || d.Median30d != 100 ||
				d.PctVsMedian30d != -8 || d.DiffVsMedian30d != -8 {
				t.Errorf("deal entry = %+v", d)
			}
		}
	}
}

func TestDealTrendFilter(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Cabbage", "vegetables")       // trend +6.1 -> excluded
	s.addComm(t, "Pechay Baguio", "vegetables") // trend +5.0 -> included
	// Cabbage: 28x115 + day(-7)=99 + as-of 105 -> pct -8.7, trend +6.1.
	for ago := 29; ago >= 1; ago-- {
		if ago == 7 {
			s.price(t, ago, "Focus M", "Cabbage", fptr(99), nil, nil)
			continue
		}
		s.price(t, ago, "Focus M", "Cabbage", fptr(115), nil, nil)
	}
	s.price(t, 0, "Focus M", "Cabbage", fptr(105), nil, nil)
	// Pechay: 28x115 + day(-7)=100 + as-of 105 -> pct -8.7, trend +5.0.
	for ago := 29; ago >= 1; ago-- {
		if ago == 7 {
			s.price(t, ago, "Focus M", "Pechay Baguio", fptr(100), nil, nil)
			continue
		}
		s.price(t, ago, "Focus M", "Pechay Baguio", fptr(115), nil, nil)
	}
	s.price(t, 0, "Focus M", "Pechay Baguio", fptr(105), nil, nil)
	sig := s.compute(t)
	names := map[string]bool{}
	for _, d := range sig.Deals {
		names[d.Name] = true
	}
	if names["Cabbage"] {
		t.Errorf("trend +6.1 deal must be filtered; deals=%+v", sig.Deals)
	}
	if !names["Pechay Baguio"] {
		t.Errorf("trend +5.0 deal must be kept; deals=%+v", sig.Deals)
	}
}

func TestDealRankingAndTopN(t *testing.T) {
	s := newTestSetup(t)
	for _, cm := range [][2]string{
		{"Tilapia", "fish"}, {"Bangus", "fish"}, {"Chayote", "vegetables"},
		{"Ampalaya", "vegetables"}, {"Eggplant", "vegetables"},
	} {
		s.addComm(t, cm[0], cm[1])
	}
	// pcts: Tilapia -12, Bangus -8, Chayote -12 (tie, alphabetical after
	// Tilapia? "Chayote" < "Tilapia" so Chayote first), Ampalaya -9,
	// Eggplant -15.
	s.constantFocus(t, "Tilapia", 29, 100, 88)
	s.constantFocus(t, "Bangus", 29, 100, 92)
	s.constantFocus(t, "Chayote", 29, 100, 88)
	s.constantFocus(t, "Ampalaya", 29, 100, 91)
	s.constantFocus(t, "Eggplant", 29, 100, 85)
	sig, err := Compute(s.db, Options{
		AsOf: s.asOf, Focus: "Focus M",
		Nearby: []string{"Near N1", "Near N2", "Near N3"}, MaxDeals: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sig.Deals) != 3 {
		t.Fatalf("deals = %+v, want top 3", sig.Deals)
	}
	// Eggplant -15, Chayote -12, Tilapia -12 (tie -> name order).
	want := []string{"Eggplant", "Chayote", "Tilapia"}
	for i, w := range want {
		if sig.Deals[i].Name != w || sig.Deals[i].Rank != i+1 {
			t.Errorf("deals[%d] = %+v, want %s rank %d", i, sig.Deals[i], w, i+1)
		}
	}
}

func TestPricierList(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Carrots (Local)", "vegetables") // +10
	s.addComm(t, "Red Onion (Local)", "spices")   // +8.0 boundary in
	s.addComm(t, "White Onion (Local)", "spices") // +7.9 out
	s.constantFocus(t, "Carrots (Local)", 29, 100, 110)
	s.constantFocus(t, "Red Onion (Local)", 29, 100, 108)
	s.constantFocus(t, "White Onion (Local)", 29, 100, 107.9)
	sig := s.compute(t)
	names := map[string]bool{}
	for _, p := range sig.Pricier {
		names[p.Name] = true
	}
	if !names["Carrots (Local)"] || !names["Red Onion (Local)"] {
		t.Errorf("pricier missing boundary items: %+v", sig.Pricier)
	}
	if names["White Onion (Local)"] {
		t.Errorf("+7.9 must not be pricier: %+v", sig.Pricier)
	}
	if len(sig.Pricier) >= 2 && sig.Pricier[0].Name != "Carrots (Local)" {
		t.Errorf("pricier not ranked desc: %+v", sig.Pricier)
	}
	for i, p := range sig.Pricier {
		if p.Rank != i+1 {
			t.Errorf("pricier rank = %d, want %d (%+v)", p.Rank, i+1, p)
		}
	}
}

func TestNoNCRFieldsEmitted(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Tilapia", "fish")
	s.constantFocus(t, "Tilapia", 29, 100, 92)
	sig := s.compute(t)
	data, err := json.Marshal(sig)
	if err != nil {
		t.Fatal(err)
	}
	lowered := strings.ToLower(string(data))
	for _, banned := range []string{
		"ncr", "focus_price", "nearby_min", "substitution", "focus_vs_ncr",
		"fx_corr", "trend_30d", `"flag"`, "summary", `"cheap"`, `"expensive"`,
	} {
		if strings.Contains(lowered, banned) {
			t.Errorf("v2 output must not contain %q: %s", banned, data)
		}
	}
	if sig.Thresholds.DealPct != DefaultDealThreshold || sig.Thresholds.MinDays != MinDays30d ||
		sig.Thresholds.MaxTrend7dPct != MaxTrend7dPct {
		t.Errorf("thresholds = %+v", sig.Thresholds)
	}
	if len(sig.NearbyMarkets) != 3 {
		t.Errorf("nearby_markets = %v", sig.NearbyMarkets)
	}
}

func TestCheapestNearbyMultiDay(t *testing.T) {
	// 2/3 cheaper (as-of + day1) passes; 1/3 fails.
	setup := func(t *testing.T, n1ago1, n1ago2 float64, haveAgo2 bool) *Signals {
		s := newTestSetup(t)
		s.addComm(t, "Tomato", "vegetables")
		s.constantFocus(t, "Tomato", 29, 100, 100)
		s.price(t, 0, "Near N1", "Tomato", fptr(90), nil, nil)
		s.price(t, 0, "Near N2", "Tomato", fptr(95), nil, nil)
		s.price(t, 0, "Near N3", "Tomato", fptr(96), nil, nil)
		s.price(t, 1, "Near N1", "Tomato", fptr(n1ago1), nil, nil)
		s.price(t, 1, "Near N2", "Tomato", fptr(95), nil, nil)
		s.price(t, 1, "Near N3", "Tomato", fptr(96), nil, nil)
		if haveAgo2 {
			s.price(t, 2, "Near N1", "Tomato", fptr(n1ago2), nil, nil)
			s.price(t, 2, "Near N2", "Tomato", fptr(95), nil, nil)
			s.price(t, 2, "Near N3", "Tomato", fptr(96), nil, nil)
		}
		return s.compute(t)
	}
	// 3/3 cheaper.
	sig := setup(t, 90, 90, true)
	c := findComm(t, sig, "Tomato")
	if c.CheapestNearby == nil || c.CheapestNearby.DaysCheaper != 3 || c.CheapestNearby.DaysChecked != 3 {
		t.Errorf("3/3 should pass with 3/3, got %+v", c.CheapestNearby)
	}
	// 2/3 cheaper (day2 at 110, not cheaper).
	sig = setup(t, 90, 110, true)
	c = findComm(t, sig, "Tomato")
	if c.CheapestNearby == nil || c.CheapestNearby.DaysCheaper != 2 || c.CheapestNearby.DaysChecked != 3 {
		t.Errorf("2/3 should pass with 2/3, got %+v", c.CheapestNearby)
	}
	// 1/3 cheaper (only as-of).
	sig = setup(t, 110, 110, true)
	c = findComm(t, sig, "Tomato")
	if c.CheapestNearby != nil {
		t.Errorf("1/3 should be rejected, got %+v", c.CheapestNearby)
	}
}

func TestCheapestNearbyOnlyOneDate(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Chayote", "vegetables")
	// Nearby basis with only as-of + 1 earlier date (2 markets each) so
	// days_checked would be 2, but make only 1 date total: only as-of.
	s.price(t, 0, "Near N1", "Chayote", fptr(90), nil, nil)
	s.price(t, 0, "Near N2", "Chayote", fptr(94), nil, nil)
	s.price(t, 0, "Near N3", "Chayote", fptr(96), nil, nil)
	sig := s.compute(t)
	c := findComm(t, sig, "Chayote")
	if c.CheapestNearby != nil {
		t.Errorf("only 1 basis date should reject cheapest, got %+v", c.CheapestNearby)
	}
}

func TestSharpFlags(t *testing.T) {
	cases := []struct {
		trend, pct float64
		hasTrend   bool
		wantDrop   bool
		wantRise   bool
	}{
		{-40.9, -40.9, true, true, false},
		{-25, -43.2, true, false, false}, // -25 > 0.8*-43.2 (-34.56)
		{-10, -12.5, true, true, false},  // boundary: -10<=-10 and <=-10
		{-9.9, -12.5, true, false, false},
		{60.7, 69.8, true, false, true}, // 60.7 >= 10 and >= 55.84
		{17.6, 25, true, false, false},  // 17.6 < 20
		{10, 12.5, true, false, true},   // boundary rise
		{9.9, 12.5, true, false, false},
		{0, -40.9, true, false, false},
	}
	for i, c := range cases {
		var tp *float64
		if c.hasTrend {
			v := c.trend
			tp = &v
		}
		if got := SharpDrop(tp, c.pct); got != c.wantDrop {
			t.Errorf("case %d SharpDrop(%v,%v)=%v want %v", i, c.trend, c.pct, got, c.wantDrop)
		}
		if got := SharpRise(tp, c.pct); got != c.wantRise {
			t.Errorf("case %d SharpRise(%v,%v)=%v want %v", i, c.trend, c.pct, got, c.wantRise)
		}
	}
	if SharpDrop(nil, -40) || SharpRise(nil, 40) {
		t.Error("nil trend must give false")
	}
}

func TestUsesNearbyBasis(t *testing.T) {
	// Deal on nearby basis -> true.
	s := newTestSetup(t)
	s.addComm(t, "Chayote", "vegetables")
	for ago := 29; ago >= 1; ago-- {
		s.price(t, ago, "Near N1", "Chayote", fptr(100), nil, nil)
		s.price(t, ago, "Near N2", "Chayote", fptr(100), nil, nil)
	}
	s.price(t, 0, "Near N1", "Chayote", fptr(60), nil, nil)
	s.price(t, 0, "Near N2", "Chayote", fptr(62), nil, nil)
	sig := s.compute(t)
	if !sig.UsesNearbyBasis {
		t.Error("uses_nearby_basis should be true with nearby deal")
	}
	found := false
	for _, d := range sig.Deals {
		if d.Name == "Chayote" {
			found = true
			if d.Basis != BasisNearbyMedian {
				t.Errorf("deal basis = %q want nearby_median", d.Basis)
			}
		}
	}
	if !found {
		t.Errorf("expected Chayote deal, got %+v", sig.Deals)
	}
	// Focus-only deals/pricier -> false.
	s2 := newTestSetup(t)
	s2.addComm(t, "Tilapia", "fish")
	s2.constantFocus(t, "Tilapia", 29, 100, 92)
	sig2 := s2.compute(t)
	if sig2.UsesNearbyBasis {
		t.Error("uses_nearby_basis should be false with focus-only deal")
	}
}

func TestPricierHasNoCheapest(t *testing.T) {
	s := newTestSetup(t)
	s.addComm(t, "Carrots (Local)", "vegetables")
	s.constantFocus(t, "Carrots (Local)", 29, 100, 110)
	// Even with 3 markets and a cheaper nearby on as-of + history,
	// the pricier entry must not carry cheapest_nearby.
	for _, ago := range []int{0, 1, 2} {
		s.price(t, ago, "Near N1", "Carrots (Local)", fptr(90), nil, nil)
		s.price(t, ago, "Near N2", "Carrots (Local)", fptr(95), nil, nil)
	}
	sig := s.compute(t)
	c := findComm(t, sig, "Carrots (Local)")
	if c.CheapestNearby == nil {
		t.Fatal("commodity should keep cheapest_nearby")
	}
	if len(sig.Pricier) == 0 {
		t.Fatal("expected pricier entry")
	}
	for _, p := range sig.Pricier {
		if p.CheapestNearby != nil {
			t.Errorf("pricier entry must not include cheapest_nearby, got %+v", p.CheapestNearby)
		}
		if p.Basis == "" {
			t.Error("pricier entry should carry basis")
		}
	}
}
