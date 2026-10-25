// Package signals computes deterministic "deal" numbers from the DA
// market-price SQLite store and emits them as signals.json (advice v2).
//
// Every peso amount / percentage an LLM later writes must come from (or be
// derivable from) this output; the LLM only interprets the numbers. The
// document contains ONLY local-basis numbers (Las Piñas market price or a
// nearby-markets median) so the LLM cannot mix in NCR-wide bases.
//
// Canonicalization: raw commodity names are merged with
// aliases.Canonicalize, plus one explicit merge the aliases table does not
// do: bare "Galunggong" -> "Galunggong (Local)" (same fish, DA qualifier
// drift). Commodities whose canonical category is "other" (cabbage
// varieties, corn, regular milled) are skipped.
package signals

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/aliases"
)

// GalunggongMerge documents the one explicit alias merge done here instead
// of aliases.Canonicalize (which keeps bare "Galunggong" separate from
// "Galunggong (Local)").
const GalunggongMerge = `"Galunggong" is merged into "Galunggong (Local)"`

// CanonicalCommodity maps a raw DB commodity name to its canonical signals
// name: aliases.Canonicalize plus the explicit Galunggong merge.
func CanonicalCommodity(raw string) string {
	canonical, _, _, _ := aliases.Canonicalize(raw)
	if canonical == "Galunggong" {
		return "Galunggong (Local)"
	}
	return canonical
}

// CanonicalCategory returns the canonical category for a raw DB commodity
// name (recomputed from the canonical name).
func CanonicalCategory(raw string) string {
	return aliases.CategorizeCommodity(CanonicalCommodity(raw))
}

// Basis values for Commodity.Basis.
const (
	BasisLasPinas     = "las_pinas"
	BasisNearbyMedian = "nearby_median"
)

// Basis labels (the only basis wording emitted).
const (
	BasisLabelLasPinas = "Las Piñas"
	BasisLabelNearby   = "nearby markets (Alabang, La Huerta, Bicutan, Pasay)"
)

// Short market names used in market_prices / cheapest_nearby and the
// top-level nearby_markets list.
const (
	ShortLasPinas = "Las Piñas"
	ShortAlabang  = "Alabang"
	ShortLaHuerta = "La Huerta"
	ShortBicutan  = "Bicutan"
	ShortPasay    = "Pasay"
)

// Options tunes Compute.
type Options struct {
	// AsOf is the as-of date (YYYY-MM-DD); empty means latest price date.
	AsOf string
	// Focus is the focus market name.
	Focus string
	// Nearby lists nearby market names.
	Nearby []string
	// DealThreshold is the |pct_vs_median_30d| cutoff for deals/pricier.
	DealThreshold float64
	// MaxDeals caps the deals list.
	MaxDeals int
	// FXWindow is accepted for CLI compatibility but unused in v2 (the fx
	// object is context only; there are no per-commodity correlations).
	FXWindow int
}

// CheapestNearby is the minimum of market_prices, present only when at
// least 3 of the 5 markets reported AND it is strictly cheaper than the
// basis price on as-of AND strictly cheaper than the basis price on at
// least 2 of the last 3 basis report dates (as-of counts).
// DiffVsBasis = price - basis price (negative). DaysCheaper/DaysChecked
// record the multi-day check.
type CheapestNearby struct {
	Market      string  `json:"market"`
	Price       float64 `json:"price"`
	DiffVsBasis float64 `json:"diff_vs_basis"`
	DaysCheaper int     `json:"days_cheaper"`
	DaysChecked int     `json:"days_checked"`
}

// Commodity holds one canonical commodity's local-basis signals.
type Commodity struct {
	Name            string             `json:"name"`
	Category        string             `json:"category"`
	Spec            *string            `json:"spec"`
	Unit            string             `json:"unit"`
	Basis           string             `json:"basis"`
	BasisLabel      string             `json:"basis_label"`
	Price           float64            `json:"price"`
	Median30d       float64            `json:"median_30d"`
	NDays30d        int                `json:"n_days_30d"`
	PctVsMedian30d  float64            `json:"pct_vs_median_30d"`
	DiffVsMedian30d float64            `json:"diff_vs_median_30d"`
	Trend7dPct      *float64           `json:"trend_7d_pct"`
	Trend7dBaseDate *string            `json:"trend_7d_base_date"`
	Trend7dBaseVal  *float64           `json:"trend_7d_base_value"`
	MarketPrices    map[string]float64 `json:"market_prices"`
	CheapestNearby  *CheapestNearby    `json:"cheapest_nearby"`
	// SharpDropThisWeek is true when trend_7d_pct <= -10 AND
	// trend_7d_pct <= 0.8 * pct_vs_median_30d.
	SharpDropThisWeek bool `json:"sharp_drop_this_week"`
	// SharpRiseThisWeek is true when trend_7d_pct >= +10 AND
	// trend_7d_pct >= 0.8 * pct_vs_median_30d.
	SharpRiseThisWeek bool `json:"sharp_rise_this_week"`
}

// DealEntry is one row of the deals / pricier lists. Pricier entries
// never carry CheapestNearby (nil).
type DealEntry struct {
	Rank              int             `json:"rank"`
	Name              string          `json:"name"`
	Basis             string          `json:"basis"`
	BasisLabel        string          `json:"basis_label"`
	Unit              string          `json:"unit"`
	Price             float64         `json:"price"`
	Median30d         float64         `json:"median_30d"`
	PctVsMedian30d    float64         `json:"pct_vs_median_30d"`
	DiffVsMedian30d   float64         `json:"diff_vs_median_30d"`
	Trend7dPct        *float64        `json:"trend_7d_pct"`
	CheapestNearby    *CheapestNearby `json:"cheapest_nearby"`
	SharpDropThisWeek bool            `json:"sharp_drop_this_week"`
	SharpRiseThisWeek bool            `json:"sharp_rise_this_week"`
}

// Thresholds records the deal-selection parameters used.
type Thresholds struct {
	DealPct       float64 `json:"deal_pct"`
	MinDays       int     `json:"min_days"`
	MaxTrend7dPct float64 `json:"max_trend_7d_pct"`
}

// FX holds the USD/PHP summary as context only. Correlation is not
// causation (see Note).
type FX struct {
	LatestDate     string   `json:"latest_date"`
	LatestRate     *float64 `json:"latest_rate"`
	Rate30dAgoDate *string  `json:"rate_30d_ago_date"`
	Rate30dAgo     *float64 `json:"rate_30d_ago"`
	Change30dPct   *float64 `json:"fx_change_30d_pct"`
	Note           string   `json:"note"`
}

// Signals is the top-level signals.json document (v2: local basis only).
type Signals struct {
	AsOf            string      `json:"as_of"`
	GeneratedBy     string      `json:"generated_by"`
	FocusMarket     string      `json:"focus_market"`
	NearbyMarkets   []string    `json:"nearby_markets"`
	Thresholds      Thresholds  `json:"thresholds"`
	Deals           []DealEntry `json:"deals"`
	Pricier         []DealEntry `json:"pricier"`
	Commodities     []Commodity `json:"commodities"`
	FX              FX          `json:"fx"`
	UsesNearbyBasis bool        `json:"uses_nearby_basis"`
}

// FXNote is the correlation-is-not-causation disclaimer in FX.Note.
const FXNote = "Correlation is not causation: an FX-price correlation only means the two series moved together, not that exchange-rate moves caused the price moves."

// Deal-selection defaults.
const (
	DefaultDealThreshold = 8.0
	DefaultMaxDeals      = 8
	MaxPricier           = 5
	MinDays30d           = 10
	MaxTrend7dPct        = 5.0
)

// Median returns the median of vals (average of the two middle values for
// even n). Returns NaN for empty input.
func Median(vals []float64) float64 {
	if len(vals) == 0 {
		return math.NaN()
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// Round2 rounds money values to 2 decimals; Round1 rounds percentages to 1.
func Round2(v float64) float64 { return math.Round(v*100) / 100 }
func Round1(v float64) float64 { return math.Round(v*10) / 10 }

// PctChange returns (latest-base)/base*100, or NaN when base is 0.
func PctChange(latest, base float64) float64 {
	if base == 0 {
		return math.NaN()
	}
	return (latest - base) / base * 100
}

// EffectivePrice mirrors the DB fact: use price when present, else the
// (low+high)/2 midpoint when both are present, else skip (ok=false).
func EffectivePrice(price, low, high sql.NullFloat64) (float64, bool) {
	if price.Valid {
		return price.Float64, true
	}
	if low.Valid && high.Valid {
		return (low.Float64 + high.Float64) / 2, true
	}
	return 0, false
}

func parseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

func fmtDate(t time.Time) string { return t.Format("2006-01-02") }

// shortMarketName maps a full market name to its short display name.
func shortMarketName(full, focus string, nearby []string) string {
	if full == focus {
		return ShortLasPinas
	}
	l := strings.ToLower(full)
	switch {
	case strings.Contains(l, "alabang"):
		return ShortAlabang
	case strings.Contains(l, "huerta"):
		return ShortLaHuerta
	case strings.Contains(l, "bicutan"):
		return ShortBicutan
	case strings.Contains(l, "pasay"):
		return ShortPasay
	}
	// Fallback: position in the nearby list.
	for i, n := range nearby {
		if n == full {
			switch i {
			case 0:
				return ShortAlabang
			case 1:
				return ShortLaHuerta
			case 2:
				return ShortBicutan
			case 3:
				return ShortPasay
			}
		}
	}
	return full
}

// dateMarket keys the per-market price buckets.
type dateMarket struct {
	date, market string
}

// priceRow is one raw price row joined with market/commodity names.
type priceRow struct {
	date             string
	market           string
	rawCommodity     string
	price, low, high sql.NullFloat64
}

// Compute builds the full signals document from db (opened by the caller;
// the subcommand opens it read-only).
func Compute(db *sql.DB, opt Options) (*Signals, error) {
	asOf := opt.AsOf
	if asOf == "" {
		var d sql.NullString
		if err := db.QueryRow("SELECT MAX(date) FROM prices").Scan(&d); err != nil {
			return nil, fmt.Errorf("latest date: %w", err)
		}
		if !d.Valid {
			return nil, fmt.Errorf("no price dates in DB")
		}
		asOf = d.String
	}
	asOfT, err := parseDate(asOf)
	if err != nil {
		return nil, fmt.Errorf("bad as-of date %q: %w", asOf, err)
	}
	deal := opt.DealThreshold
	if deal == 0 {
		deal = DefaultDealThreshold
	}
	maxDeals := opt.MaxDeals
	if maxDeals <= 0 {
		maxDeals = DefaultMaxDeals
	}
	winStart := fmtDate(asOfT.AddDate(0, 0, -29)) // 30 calendar days incl. as-of

	// Load commodity metadata for spec/unit lookup by raw name.
	type commMeta struct {
		spec     *string
		unit     string
		category string
	}
	metaByRaw := map[string]commMeta{}
	{
		rows, err := db.Query("SELECT name, category, spec, unit FROM commodities")
		if err != nil {
			return nil, fmt.Errorf("commodities: %w", err)
		}
		for rows.Next() {
			var name string
			var category, spec, unit sql.NullString
			if err := rows.Scan(&name, &category, &spec, &unit); err != nil {
				rows.Close()
				return nil, err
			}
			m := commMeta{category: category.String}
			if spec.Valid {
				s := spec.String
				m.spec = &s
			}
			m.unit = unit.String
			metaByRaw[name] = m
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	// Load prices in the 30-day window joined with names.
	var prows []priceRow
	{
		rows, err := db.Query(`SELECT p.date, m.name, c.name, p.price, p.price_low, p.price_high
			FROM prices p JOIN markets m ON m.id = p.market_id
			JOIN commodities c ON c.id = p.commodity_id
			WHERE p.date >= ? AND p.date <= ?`, winStart, asOf)
		if err != nil {
			return nil, fmt.Errorf("prices: %w", err)
		}
		for rows.Next() {
			var r priceRow
			if err := rows.Scan(&r.date, &r.market, &r.rawCommodity, &r.price, &r.low, &r.high); err != nil {
				rows.Close()
				return nil, err
			}
			prows = append(prows, r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	// Group effective prices: canonical -> date -> market -> values.
	byCanonDateMarket := map[string]map[dateMarket][]float64{}
	for _, r := range prows {
		v, ok := EffectivePrice(r.price, r.low, r.high)
		if !ok {
			continue
		}
		c := CanonicalCommodity(r.rawCommodity)
		if CanonicalCategory(r.rawCommodity) == "other" {
			continue
		}
		if byCanonDateMarket[c] == nil {
			byCanonDateMarket[c] = map[dateMarket][]float64{}
		}
		k := dateMarket{r.date, r.market}
		byCanonDateMarket[c][k] = append(byCanonDateMarket[c][k], v)
	}

	// Median per canonical/date/market (usually a single row).
	medByCDM := map[string]map[string]map[string]float64{}
	for c, m := range byCanonDateMarket {
		medByCDM[c] = map[string]map[string]float64{}
		for k, vals := range m {
			if medByCDM[c][k.date] == nil {
				medByCDM[c][k.date] = map[string]float64{}
			}
			medByCDM[c][k.date][k.market] = Median(vals)
		}
	}

	nearbyList := append([]string(nil), opt.Nearby...)
	fiveMarkets := append([]string{opt.Focus}, nearbyList...)

	commodities := make([]Commodity, 0, len(medByCDM))
	for c, perDate := range medByCDM {
		// Focus daily series over the window; nearby daily series needs
		// >= 2 nearby markets reporting that day.
		var focusSeries []float64
		nearbySeries := map[string]float64{} // date -> median
		for d, byMkt := range perDate {
			if v, ok := byMkt[opt.Focus]; ok {
				focusSeries = append(focusSeries, v)
			}
			var nb []float64
			for _, mkt := range nearbyList {
				if v, ok := byMkt[mkt]; ok {
					nb = append(nb, v)
				}
			}
			if len(nb) >= 2 {
				nearbySeries[d] = Median(nb)
			}
		}
		focusLatest, hasFocusLatest := perDate[asOf][opt.Focus]
		var nearbyLatestVals []float64
		if byMkt, ok := perDate[asOf]; ok {
			for _, mkt := range nearbyList {
				if v, ok := byMkt[mkt]; ok {
					nearbyLatestVals = append(nearbyLatestVals, v)
				}
			}
		}

		var basis, basisLabel string
		var price float64
		var series []float64
		switch {
		case hasFocusLatest && len(focusSeries) >= 10:
			basis, basisLabel = BasisLasPinas, BasisLabelLasPinas
			price = focusLatest
			for _, byMkt := range perDate {
				if v, ok := byMkt[opt.Focus]; ok {
					series = append(series, v)
				}
			}
		case len(nearbyLatestVals) >= 2:
			basis, basisLabel = BasisNearbyMedian, BasisLabelNearby
			price = Median(nearbyLatestVals)
			for _, v := range nearbySeries {
				series = append(series, v)
			}
		default:
			continue // omitted: no usable local basis
		}
		if len(series) == 0 {
			continue
		}
		med := Median(series)
		if med == 0 || math.IsNaN(med) {
			continue
		}
		nDays := len(series)
		pct := Round1(PctChange(price, med))
		diff := Round2(price - med)

		// Trend: baseline = latest series date <= as-of-7; null when
		// farther than 3 days from the target.
		seriesByDate := map[string]float64{}
		if basis == BasisLasPinas {
			for d, byMkt := range perDate {
				if v, ok := byMkt[opt.Focus]; ok {
					seriesByDate[d] = v
				}
			}
		} else {
			seriesByDate = nearbySeries
		}
		var tPct *float64
		var tDate *string
		var tVal *float64
		if bd, bv, ok := trendBaseline(seriesByDate, asOfT, 7); ok {
			// Trend compares the basis price on as-of against the
			// basis series baseline (== series value that day when
			// present; for the nearby basis the as-of median).
			p := Round1(PctChange(price, bv))
			rv := Round2(bv)
			tPct = &p
			tDate = &bd
			tVal = &rv
		}

		// market_prices for the 5 markets that reported on as-of.
		mp := map[string]float64{}
		if byMkt, ok := perDate[asOf]; ok {
			for _, mkt := range fiveMarkets {
				if v, ok := byMkt[mkt]; ok {
					mp[shortMarketName(mkt, opt.Focus, nearbyList)] = Round2(v)
				}
			}
		}
		var cheap *CheapestNearby
		if len(mp) >= 3 {
			best := math.Inf(1)
			bestMkt := ""
			for mkt, v := range mp {
				if v < best {
					best, bestMkt = v, mkt
				}
			}
			if !math.IsInf(best, 1) && best < Round2(price) {
				// Multi-day check: candidate must also beat the basis
				// on >= 2 of the last 3 basis report dates.
				if dc, checked, ok := cheapMultiDay(perDate, seriesByDate, asOf, fiveMarkets, opt.Focus, nearbyList, bestMkt); ok {
					d := Round2(best - Round2(price))
					cheap = &CheapestNearby{Market: bestMkt, Price: best, DiffVsBasis: d, DaysCheaper: dc, DaysChecked: checked}
				}
			}
		}

		com := Commodity{
			Name: c, Basis: basis, BasisLabel: basisLabel,
			Price: Round2(price), Median30d: Round2(med), NDays30d: nDays,
			PctVsMedian30d: pct, DiffVsMedian30d: diff,
			Trend7dPct: tPct, Trend7dBaseDate: tDate, Trend7dBaseVal: tVal,
			MarketPrices: mp, CheapestNearby: cheap,
			SharpDropThisWeek: SharpDrop(tPct, pct),
			SharpRiseThisWeek: SharpRise(tPct, pct),
		}
		if m, ok := metaByRaw[c]; ok {
			com.Category = m.category
			if com.Category == "" || com.Category == "other" {
				com.Category = CanonicalCategory(c)
			}
			com.Spec = m.spec
			com.Unit = m.unit
		} else {
			_, spec, unit, _ := aliases.Canonicalize(c)
			com.Category = CanonicalCategory(c)
			com.Spec = spec
			com.Unit = unit
		}
		if com.Category == "" {
			com.Category = CanonicalCategory(c)
		}
		commodities = append(commodities, com)
	}
	sort.Slice(commodities, func(i, j int) bool {
		if commodities[i].Category != commodities[j].Category {
			return commodities[i].Category < commodities[j].Category
		}
		return commodities[i].Name < commodities[j].Name
	})

	// Deals / pricier.
	var deals, pricier []DealEntry
	for _, c := range commodities {
		entry := DealEntry{
			Name: c.Name, Basis: c.Basis, BasisLabel: c.BasisLabel, Unit: c.Unit,
			Price: c.Price, Median30d: c.Median30d,
			PctVsMedian30d: c.PctVsMedian30d, DiffVsMedian30d: c.DiffVsMedian30d,
			Trend7dPct: c.Trend7dPct, CheapestNearby: c.CheapestNearby,
			SharpDropThisWeek: c.SharpDropThisWeek,
			SharpRiseThisWeek: c.SharpRiseThisWeek,
		}
		if c.PctVsMedian30d <= -deal && c.NDays30d >= MinDays30d &&
			(c.Trend7dPct == nil || *c.Trend7dPct <= MaxTrend7dPct) {
			deals = append(deals, entry)
		}
		if c.PctVsMedian30d >= deal {
			// Pricier entries never carry cheapest_nearby.
			noCheap := entry
			noCheap.CheapestNearby = nil
			pricier = append(pricier, noCheap)
		}
	}
	sort.Slice(deals, func(i, j int) bool {
		if deals[i].PctVsMedian30d != deals[j].PctVsMedian30d {
			return deals[i].PctVsMedian30d < deals[j].PctVsMedian30d
		}
		return deals[i].Name < deals[j].Name
	})
	if len(deals) > maxDeals {
		deals = deals[:maxDeals]
	}
	for i := range deals {
		deals[i].Rank = i + 1
	}
	sort.Slice(pricier, func(i, j int) bool {
		if pricier[i].PctVsMedian30d != pricier[j].PctVsMedian30d {
			return pricier[i].PctVsMedian30d > pricier[j].PctVsMedian30d
		}
		return pricier[i].Name < pricier[j].Name
	})
	if len(pricier) > MaxPricier {
		pricier = pricier[:MaxPricier]
	}
	for i := range pricier {
		pricier[i].Rank = i + 1
	}
	if deals == nil {
		deals = []DealEntry{}
	}
	if pricier == nil {
		pricier = []DealEntry{}
	}
	if commodities == nil {
		commodities = []Commodity{}
	}

	// FX context only.
	fxByDate := map[string]float64{}
	{
		rows, err := db.Query(`SELECT date, rate FROM fx_rates WHERE base = 'USD' AND quote = 'PHP' AND date <= ? ORDER BY date`, asOf)
		if err != nil {
			return nil, fmt.Errorf("fx_rates: %w", err)
		}
		for rows.Next() {
			var d string
			var rate float64
			if err := rows.Scan(&d, &rate); err != nil {
				rows.Close()
				return nil, err
			}
			fxByDate[d] = rate
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	fx := buildFX(fxByDate, asOf, asOfT)

	shortNearby := make([]string, 0, len(nearbyList))
	for _, mkt := range nearbyList {
		shortNearby = append(shortNearby, shortMarketName(mkt, opt.Focus, nearbyList))
	}

	// Displayed items only: all deals (already capped to maxDeals)
	// plus the first 3 pricier lines shown in texts (pricier #4/#5
	// are never displayed, so they must not trigger the footnote).
	usesNearby := false
	for _, d := range deals {
		if d.Basis == BasisNearbyMedian {
			usesNearby = true
			break
		}
	}
	if !usesNearby {
		shown := pricier
		if len(shown) > 3 {
			shown = shown[:3]
		}
		for _, p := range shown {
			if p.Basis == BasisNearbyMedian {
				usesNearby = true
				break
			}
		}
	}

	return &Signals{
		AsOf:            asOf,
		GeneratedBy:     "presyo signals",
		FocusMarket:     opt.Focus,
		NearbyMarkets:   shortNearby,
		Thresholds:      Thresholds{DealPct: deal, MinDays: MinDays30d, MaxTrend7dPct: MaxTrend7dPct},
		Deals:           deals,
		Pricier:         pricier,
		Commodities:     commodities,
		FX:              fx,
		UsesNearbyBasis: usesNearby,
	}, nil
}

// SharpDrop reports whether the 7-day drop carries most of the fall below
// usual: pct negative, trend <= -10 AND trend <= 0.8 * pct.
func SharpDrop(trend *float64, pct float64) bool {
	if trend == nil || pct >= 0 {
		return false
	}
	t := *trend
	return t <= -10 && t <= 0.8*pct
}

// SharpRise reports the pricier analog: pct positive, trend >= +10 AND
// trend >= 0.8 * pct.
func SharpRise(trend *float64, pct float64) bool {
	if trend == nil || pct <= 0 {
		return false
	}
	t := *trend
	return t >= 10 && t >= 0.8*pct
}

// cheapMultiDay implements the v3 multi-day cheapest_nearby check. dates are
// the last 3 basis report dates (<= asOf, most recent first). The candidate
// (short name) must beat the basis on at least 2 of them (as-of counts).
// Returns daysCheaper, daysChecked (=len(dates), 3 or fewer) and ok=false
// when daysChecked < 2 or daysCheaper < 2.
func cheapMultiDay(perDate map[string]map[string]float64, seriesByDate map[string]float64, asOf string, fiveMarkets []string, focus string, nearby []string, candidateShort string) (daysCheaper, daysChecked int, ok bool) {
	// Last 3 basis report dates.
	var dates []string
	for d := range seriesByDate {
		if d <= asOf {
			dates = append(dates, d)
		}
	}
	sort.Strings(dates)
	if len(dates) > 3 {
		dates = dates[len(dates)-3:]
	}
	daysChecked = len(dates)
	if daysChecked < 2 {
		return 0, daysChecked, false
	}
	// Full market name of the candidate.
	candidateFull := candidateShort
	for _, mkt := range fiveMarkets {
		if shortMarketName(mkt, focus, nearby) == candidateShort {
			candidateFull = mkt
			break
		}
	}
	cheaper := 0
	for _, d := range dates {
		bv, ok := seriesByDate[d]
		if !ok {
			continue
		}
		byMkt, ok := perDate[d]
		if !ok {
			continue
		}
		cv, ok := byMkt[candidateFull]
		if !ok {
			continue
		}
		if cv < bv {
			cheaper++
		}
	}
	if cheaper < 2 {
		return cheaper, daysChecked, false
	}
	return cheaper, daysChecked, true
}

// trendBaseline finds the latest series date <= asOf-daysAgo; ok=false when
// no such date exists within 3 days of the target.
func trendBaseline(series map[string]float64, asOf time.Time, daysAgo int) (date string, val float64, ok bool) {
	target := fmtDate(asOf.AddDate(0, 0, -daysAgo))
	floor := fmtDate(asOf.AddDate(0, 0, -daysAgo-3))
	best := ""
	for d := range series {
		if d <= target && d > best {
			best = d
		}
	}
	if best == "" || best < floor {
		return "", 0, false
	}
	return best, series[best], true
}

// fxOnOrBefore returns the latest rate on or before date.
func fxOnOrBefore(fx map[string]float64, date string) (day string, rate float64, ok bool) {
	best := ""
	for d := range fx {
		if d <= date && d > best {
			best = d
		}
	}
	if best == "" {
		return "", 0, false
	}
	return best, fx[best], true
}

func buildFX(fx map[string]float64, asOf string, asOfT time.Time) FX {
	f := FX{LatestDate: asOf, Note: FXNote}
	if d, r, ok := fxOnOrBefore(fx, asOf); ok {
		rv := Round2(r)
		f.LatestDate = d
		f.LatestRate = &rv
	}
	ago := fmtDate(asOfT.AddDate(0, 0, -30))
	if d, r, ok := fxOnOrBefore(fx, ago); ok {
		dc := d
		rv := Round2(r)
		f.Rate30dAgoDate = &dc
		f.Rate30dAgo = &rv
	}
	if f.LatestRate != nil && f.Rate30dAgo != nil && *f.Rate30dAgo != 0 {
		p := Round1(PctChange(*f.LatestRate, *f.Rate30dAgo))
		f.Change30dPct = &p
	}
	return f
}
