// Region signals (REGIONS_SPEC.md section 2): per-region deal numbers.
// Every market in the region contributes its own daily price series; there
// is no fallback between markets inside a region — a market without enough
// data is simply not a candidate for that commodity.

package signals

import (
	"database/sql"
	"fmt"
	"math"
	"sort"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
)

// Region deal/pricier selection thresholds (fixed by the spec).
const (
	RegionDealPct    = 8.0
	RegionMinDays    = 10
	RegionMaxTrend7d = 5.0
	RegionMaxDeals   = 5
	RegionMaxPricier = 3
	RegionWindowDays = 30
)

// RegionRef identifies the region in region signals.
type RegionRef struct {
	ID      string `json:"id"`
	TitleTL string `json:"title_tl"`
	TitleEN string `json:"title_en"`
	TitleJA string `json:"title_ja"`
}

// RegionMarketUsed is one mapped market that reported on as-of.
type RegionMarketUsed struct {
	Name              string `json:"name"`
	Short             string `json:"short"`
	PricedItemsOnAsOf int    `json:"priced_items_on_as_of"`
}

// RegionThresholds records the region deal-selection parameters.
type RegionThresholds struct {
	DealPct       float64 `json:"deal_pct"`
	MinDays       int     `json:"min_days"`
	MaxTrend7dPct float64 `json:"max_trend_7d_pct"`
}

// RegionDeal is one ranked regional deal.
type RegionDeal struct {
	Rank             int      `json:"rank"`
	Name             string   `json:"name"`
	Market           string   `json:"market"`
	MarketShort      string   `json:"market_short"`
	Unit             string   `json:"unit"`
	Price            float64  `json:"price"`
	Median30d        float64  `json:"median_30d"`
	PctVsMedian30d   float64  `json:"pct_vs_median_30d"`
	PctRounded       int      `json:"pct_rounded"`
	DiffVsMedian30d  float64  `json:"diff_vs_median_30d"`
	Trend7dPct       *float64 `json:"trend_7d_pct"`
	NMarketsWithDeal int      `json:"n_markets_with_deal"`
}

// RegionPricier is one ranked regional pricier entry (region medians).
type RegionPricier struct {
	Rank            int     `json:"rank"`
	Name            string  `json:"name"`
	Unit            string  `json:"unit"`
	PriceMedian     float64 `json:"price_median"`
	PctMedian       float64 `json:"pct_median"`
	PctRounded      int     `json:"pct_rounded"`
	NMarketsPricier int     `json:"n_markets_pricier"`
	NMarkets        int     `json:"n_markets"`
}

// RegionSignals is the top-level region-signals.json document.
type RegionSignals struct {
	AsOf           string             `json:"as_of"`
	Region         RegionRef          `json:"region"`
	MarketsUsed    []RegionMarketUsed `json:"markets_used"`
	MarketsMissing []string           `json:"markets_missing"`
	Thresholds     RegionThresholds   `json:"thresholds"`
	Deals          []RegionDeal       `json:"deals"`
	Pricier        []RegionPricier    `json:"pricier"`
	GeneratedBy    string             `json:"generated_by"`
}

// RegionOptions tunes ComputeRegion.
type RegionOptions struct {
	// AsOf is the as-of date (YYYY-MM-DD); empty means the latest price
	// date in the DB across the region's markets.
	AsOf string
}

// roundHalfAway rounds to the nearest int, half away from zero
// (-40.5 -> -41, 13.5 -> 14).
func roundHalfAway(v float64) int {
	return int(math.Round(v))
}

// regionMarketStats is the per (market, canonical commodity) basis.
type regionMarketStats struct {
	market string
	short  string
	unit   string
	price  float64
	median float64
	pct    float64
	diff   float64
	trend  *float64
	nDays  int
}

// ComputeRegion builds the region signals document for regionID.
func ComputeRegion(db *sql.DB, regionID string, opt RegionOptions) (*RegionSignals, error) {
	region, ok := regions.ByID(regionID)
	if !ok {
		return nil, fmt.Errorf("unknown region %q (want one of %v)", regionID, regions.IDs())
	}
	asOf := opt.AsOf
	if asOf == "" {
		var err error
		asOf, err = latestRegionDate(db, region)
		if err != nil {
			return nil, err
		}
	}
	asOfT, err := parseDate(asOf)
	if err != nil {
		return nil, fmt.Errorf("bad as-of date %q: %w", asOf, err)
	}
	winStart := fmtDate(asOfT.AddDate(0, 0, -(RegionWindowDays - 1)))

	// Commodity metadata (unit) by canonical name: prefer the raw name
	// equal to the canonical name, else the lowest-sorted raw name.
	unitByCanon := map[string]string{}
	{
		rows, err := db.Query("SELECT name, unit FROM commodities")
		if err != nil {
			return nil, fmt.Errorf("commodities: %w", err)
		}
		best := map[string]string{}
		for rows.Next() {
			var name string
			var unit sql.NullString
			if err := rows.Scan(&name, &unit); err != nil {
				rows.Close()
				return nil, err
			}
			c := CanonicalCommodity(name)
			if CanonicalCategory(name) == "other" {
				continue
			}
			if cur, ok := best[c]; !ok || name == c || (cur != c && name < cur) {
				best[c] = name
				unitByCanon[c] = unit.String
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Load prices in the window for the region's markets.
	type prow struct {
		date, market, raw string
		price, low, high  sql.NullFloat64
	}
	marketSet := map[string]bool{}
	placeholders := ""
	args := []any{}
	for _, m := range region.Markets {
		marketSet[m.Name] = true
	}
	i := 0
	for name := range marketSet {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, name)
		i++
	}
	var prows []prow
	{
		q := `SELECT p.date, m.name, c.name, p.price, p.price_low, p.price_high
			FROM prices p JOIN markets m ON m.id = p.market_id
			JOIN commodities c ON c.id = p.commodity_id
			WHERE p.date >= ? AND p.date <= ? AND m.name IN (` + placeholders + `)`
		qargs := append([]any{winStart, asOf}, args...)
		rows, err := db.Query(q, qargs...)
		if err != nil {
			return nil, fmt.Errorf("prices: %w", err)
		}
		for rows.Next() {
			var r prow
			if err := rows.Scan(&r.date, &r.market, &r.raw, &r.price, &r.low, &r.high); err != nil {
				rows.Close()
				return nil, err
			}
			prows = append(prows, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Group effective prices: market -> canonical -> date -> values.
	byMCD := map[string]map[string]map[string][]float64{}
	rawSeen := map[string]map[string]bool{} // canon -> raw -> true
	for _, r := range prows {
		v, ok := EffectivePrice(r.price, r.low, r.high)
		if !ok {
			continue
		}
		c := CanonicalCommodity(r.raw)
		if CanonicalCategory(r.raw) == "other" {
			continue
		}
		if byMCD[r.market] == nil {
			byMCD[r.market] = map[string]map[string][]float64{}
		}
		if byMCD[r.market][c] == nil {
			byMCD[r.market][c] = map[string][]float64{}
		}
		byMCD[r.market][c][r.date] = append(byMCD[r.market][c][r.date], v)
		if rawSeen[c] == nil {
			rawSeen[c] = map[string]bool{}
		}
		rawSeen[c][r.raw] = true
	}

	// Daily medians per (market, canonical).
	medByMC := map[string]map[string]map[string]float64{}
	pricedOnAsOf := map[string]map[string]bool{} // market -> canon priced on as-of
	for mkt, byC := range byMCD {
		for c, byD := range byC {
			for d, vals := range byD {
				if medByMC[mkt] == nil {
					medByMC[mkt] = map[string]map[string]float64{}
				}
				if medByMC[mkt][c] == nil {
					medByMC[mkt][c] = map[string]float64{}
				}
				medByMC[mkt][c][d] = Median(vals)
			}
		}
		if _, ok := byMCD[mkt]; ok {
			for c := range byMCD[mkt] {
				if _, ok := medByMC[mkt][c][asOf]; ok {
					if pricedOnAsOf[mkt] == nil {
						pricedOnAsOf[mkt] = map[string]bool{}
					}
					pricedOnAsOf[mkt][c] = true
				}
			}
		}
	}

	// Per (market, canonical) basis stats.
	stats := map[string]map[string]*regionMarketStats{} // market -> canon -> stats
	for _, m := range region.Markets {
		byC := medByMC[m.Name]
		if byC == nil {
			continue
		}
		for c, byD := range byC {
			price, ok := byD[asOf]
			if !ok {
				continue
			}
			if len(byD) < RegionMinDays {
				continue
			}
			var series []float64
			for _, v := range byD {
				series = append(series, v)
			}
			med := Median(series)
			if med == 0 || math.IsNaN(med) {
				continue
			}
			pct := Round1(PctChange(price, med))
			var trend *float64
			if _, bv, ok := trendBaseline(byD, asOfT, 7); ok {
				p := Round1(PctChange(price, bv))
				trend = &p
			}
			if stats[m.Name] == nil {
				stats[m.Name] = map[string]*regionMarketStats{}
			}
			unit := unitByCanon[c]
			stats[m.Name][c] = &regionMarketStats{
				market: m.Name, short: m.Short, unit: unit,
				price: Round2(price), median: Round2(med),
				pct: pct, diff: Round2(price - med),
				trend: trend, nDays: len(byD),
			}
		}
	}

	// Deals: candidates with pct <= -deal, n_days >= 10 (already
	// enforced), trend null or <= +maxTrend. De-duplicate per commodity:
	// lowest pct wins (tie: lower price, then market name).
	type cand struct {
		market, short, unit      string
		price, median, pct, diff float64
		trend                    *float64
	}
	byComm := map[string][]cand{}
	for mkt, byC := range stats {
		for c, s := range byC {
			if s.pct > -RegionDealPct || (s.trend != nil && *s.trend > RegionMaxTrend7d) {
				continue
			}
			byComm[c] = append(byComm[c], cand{
				market: mkt, short: s.short, unit: s.unit,
				price: s.price, median: s.median, pct: s.pct, diff: s.diff,
				trend: s.trend,
			})
		}
	}
	var deals []RegionDeal
	for c, cands := range byComm {
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].pct != cands[j].pct {
				return cands[i].pct < cands[j].pct
			}
			if cands[i].price != cands[j].price {
				return cands[i].price < cands[j].price
			}
			return cands[i].market < cands[j].market
		})
		best := cands[0]
		deals = append(deals, RegionDeal{
			Name: c, Market: best.market, MarketShort: best.short, Unit: best.unit,
			Price: best.price, Median30d: best.median,
			PctVsMedian30d: best.pct, PctRounded: roundHalfAway(best.pct),
			DiffVsMedian30d: best.diff, Trend7dPct: best.trend,
			NMarketsWithDeal: len(cands),
		})
	}
	sort.Slice(deals, func(i, j int) bool {
		if deals[i].PctVsMedian30d != deals[j].PctVsMedian30d {
			return deals[i].PctVsMedian30d < deals[j].PctVsMedian30d
		}
		return deals[i].Name < deals[j].Name
	})
	if len(deals) > RegionMaxDeals {
		deals = deals[:RegionMaxDeals]
	}
	for i := range deals {
		deals[i].Rank = i + 1
	}
	if deals == nil {
		deals = []RegionDeal{}
	}

	// Pricier: per commodity, among region markets with a valid basis, if
	// at least half of them (and >= 2 markets) have pct >= +deal.
	var pricier []RegionPricier
	for c := range unitAcross(stats) {
		var withBasis []*regionMarketStats
		for _, m := range region.Markets {
			if s := stats[m.Name][c]; s != nil {
				withBasis = append(withBasis, s)
			}
		}
		if len(withBasis) == 0 {
			continue
		}
		var up []*regionMarketStats
		for _, s := range withBasis {
			if s.pct >= RegionDealPct {
				up = append(up, s)
			}
		}
		if len(up) < 2 || len(up)*2 < len(withBasis) {
			continue
		}
		var prices, pcts []float64
		for _, s := range up {
			prices = append(prices, s.price)
			pcts = append(pcts, s.pct)
		}
		pm := Round2(Median(prices))
		qm := Round1(Median(pcts))
		pricier = append(pricier, RegionPricier{
			Name: c, Unit: up[0].unit,
			PriceMedian: pm, PctMedian: qm, PctRounded: roundHalfAway(qm),
			NMarketsPricier: len(up), NMarkets: len(withBasis),
		})
	}
	sort.Slice(pricier, func(i, j int) bool {
		if pricier[i].PctMedian != pricier[j].PctMedian {
			return pricier[i].PctMedian > pricier[j].PctMedian
		}
		return pricier[i].Name < pricier[j].Name
	})
	if len(pricier) > RegionMaxPricier {
		pricier = pricier[:RegionMaxPricier]
	}
	for i := range pricier {
		pricier[i].Rank = i + 1
	}
	if pricier == nil {
		pricier = []RegionPricier{}
	}

	// Markets used / missing.
	var used []RegionMarketUsed
	var missing []string
	for _, m := range region.Markets {
		n := len(pricedOnAsOf[m.Name])
		if n == 0 {
			missing = append(missing, m.Name)
			continue
		}
		used = append(used, RegionMarketUsed{Name: m.Name, Short: m.Short, PricedItemsOnAsOf: n})
	}
	if used == nil {
		used = []RegionMarketUsed{}
	}
	if missing == nil {
		missing = []string{}
	}

	return &RegionSignals{
		AsOf: asOf,
		Region: RegionRef{
			ID: region.ID, TitleTL: region.TitleTL,
			TitleEN: region.TitleEN, TitleJA: region.TitleJA,
		},
		MarketsUsed:    used,
		MarketsMissing: missing,
		Thresholds: RegionThresholds{
			DealPct: RegionDealPct, MinDays: RegionMinDays,
			MaxTrend7dPct: RegionMaxTrend7d,
		},
		Deals:       deals,
		Pricier:     pricier,
		GeneratedBy: "presyo region-signals",
	}, nil
}

// unitAcross returns the set of canonical commodities with any stats.
func unitAcross(stats map[string]map[string]*regionMarketStats) map[string]bool {
	out := map[string]bool{}
	for _, byC := range stats {
		for c := range byC {
			out[c] = true
		}
	}
	return out
}

// latestRegionDate returns the latest price date across the region's markets.
func latestRegionDate(db *sql.DB, region *regions.Region) (string, error) {
	placeholders := ""
	args := []any{}
	for i, m := range region.Markets {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, m.Name)
	}
	var d sql.NullString
	q := `SELECT MAX(p.date) FROM prices p JOIN markets m ON m.id = p.market_id WHERE m.name IN (` + placeholders + `)`
	if err := db.QueryRow(q, args...).Scan(&d); err != nil {
		return "", fmt.Errorf("latest region date: %w", err)
	}
	if !d.Valid {
		return "", fmt.Errorf("no price dates for region %q", region.ID)
	}
	return d.String, nil
}
