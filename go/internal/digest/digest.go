// Package digest builds the multi-region DA price digest (DIGEST_SPEC.md):
// ONE card + trilingual caption that replaces the five per-region Facebook
// posts. It computes, for the key staples, each region's median price on the
// latest report date D and its like-for-like change versus that region's
// previous report date, drops any region that fails its checks, and renders
// the caption. Image rendering lives in internal/fbchart (RenderDigest) so
// this package stays free of drawing code and import cycles.
package digest

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Tunables fixed by the spec.
const (
	// FlatBelowPct: |change| below this (unrounded %) is "flat".
	FlatBelowPct = 1.0
	// MaxPlausibleStepPct: a like-for-like change beyond this is treated as
	// a data glitch and the cell's trend is blanked (price stays).
	MaxPlausibleStepPct = 40.0
	// MedianBandLow/High: a region price outside this band of its own
	// 30-day median blanks the whole cell.
	MedianBandLow  = 0.4
	MedianBandHigh = 2.5
	// MinCellsPerRegion: a region with fewer valid key cells is dropped.
	MinCellsPerRegion = 5
	// MinRegions: no digest is built with fewer included regions.
	MinRegions = 3
	// MinMarketsPerCell: a cell needs this many markets reporting on D
	// (or all the region's reporting markets when it has fewer).
	MinMarketsPerCell = 1
	// PrevLookbackDays bounds the "previous report" search.
	PrevLookbackDays = 7
	// SeriesDays is the sanity-median window.
	SeriesDays = 30
)

// Direction values.
const (
	DirUp   = "up"
	DirDown = "down"
	DirFlat = "flat"
	DirNone = "none"
)

// Item is one key staple shown on the card.
type Item struct {
	// Canonical is the signals canonical commodity name.
	Canonical  string
	TL, EN, JA string
	// Piece is true for per-piece pricing (eggs).
	Piece bool
}

// KeyItems are the staples on the digest card, in card order.
var KeyItems = []Item{
	{"Well-milled Rice (Local)", "Bigas", "Rice", "米", false},
	{"Fresh Pork Liempo (Local)", "Liempo", "Pork belly", "豚バラ", false},
	{"Fresh Whole Chicken", "Manok", "Chicken", "鶏肉", false},
	{"Egg (Medium)", "Itlog", "Egg", "卵", true},
	{"Galunggong (Local)", "Galunggong", "Galunggong", "ガルンゴン", false},
	{"Tomato", "Kamatis", "Tomato", "トマト", false},
	{"Red Onion (Local)", "Sibuyas pula", "Red onion", "赤玉ねぎ", false},
	{"Sugar (Refined)", "Asukal", "Sugar", "砂糖", false},
}

// Cell is one (commodity, region-or-metro) value.
type Cell struct {
	Price   float64 `json:"price"`
	Pct     *int    `json:"pct,omitempty"` // rounded like-for-like change vs previous report
	Dir     string  `json:"dir"`
	Markets int     `json:"markets"`
}

// Row is one key commodity with its per-region cells and the metro cell.
type Row struct {
	Canonical string           `json:"canonical"`
	TL        string           `json:"tl"`
	EN        string           `json:"en"`
	JA        string           `json:"ja"`
	Piece     bool             `json:"piece"`
	Cells     map[string]*Cell `json:"cells"` // region id -> cell (missing = no valid cell)
	Metro     *Cell            `json:"metro,omitempty"`
}

// RegionStatus records whether a region made it into the digest.
type RegionStatus struct {
	ID       string `json:"id"`
	TitleTL  string `json:"title_tl"`
	TitleEN  string `json:"title_en"`
	TitleJA  string `json:"title_ja"`
	ShortTL  string `json:"short_tl"`
	Included bool   `json:"included"`
	Reason   string `json:"reason,omitempty"` // why dropped
	PrevAsOf string `json:"prev_as_of,omitempty"`
	Markets  int    `json:"markets"`
	Cells    int    `json:"cells"`
}

// BestDeal is the single best "cheaper than usual" deal across regions.
type BestDeal struct {
	Region string             `json:"region"`
	Deal   signals.RegionDeal `json:"deal"`
}

// Digest is the full digest document (digest.json).
type Digest struct {
	AsOf     string         `json:"as_of"`
	PrevAsOf string         `json:"prev_as_of,omitempty"` // set when all included regions share it
	Regions  []RegionStatus `json:"regions"`
	Rows     []Row          `json:"rows"`
	Best     *BestDeal      `json:"best,omitempty"`
}

// Options tunes Build.
type Options struct {
	// ForceFail forces the named region to fail its checks (tests).
	ForceFail func(regionID string) bool
}

// Included returns the ids of included regions in region order.
func (d *Digest) Included() []string {
	var out []string
	for _, r := range d.Regions {
		if r.Included {
			out = append(out, r.ID)
		}
	}
	return out
}

// Dropped returns the dropped region statuses in region order.
func (d *Digest) Dropped() []RegionStatus {
	var out []RegionStatus
	for _, r := range d.Regions {
		if !r.Included {
			out = append(out, r)
		}
	}
	return out
}

// obs is one market's price on one date for one key item.
type obs struct {
	region, market string
	date           string
	item           int
	price          float64
}

func roundInt(v float64) int { return int(math.Round(v)) }

// roundPrice rounds a displayed price: whole pesos from ₱100 up (a median
// of two market prices can end in .50), cents below.
func roundPrice(v float64) float64 {
	if v >= 100 {
		return math.Round(v)
	}
	return signals.Round2(v)
}

// Build computes the digest for data date asOf. It never fails because of
// one bad region; it errors only on DB problems. The returned digest may
// have fewer than MinRegions included regions (callers decide).
func Build(db *sql.DB, asOf string, opt Options) (*Digest, error) {
	asOfT, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil, fmt.Errorf("bad as-of %q", asOf)
	}
	winStart := asOfT.AddDate(0, 0, -(SeriesDays - 1)).Format("2006-01-02")

	itemIdx := map[string]int{}
	for i, it := range KeyItems {
		itemIdx[it.Canonical] = i
	}
	marketRegion := map[string]string{}
	for _, r := range regions.Regions {
		for _, m := range r.Markets {
			marketRegion[m.Name] = r.ID
		}
	}

	rows, err := db.Query(`SELECT p.date, m.name, c.name, p.price, p.price_low, p.price_high
		FROM prices p JOIN markets m ON m.id = p.market_id
		JOIN commodities c ON c.id = p.commodity_id
		WHERE p.date >= ? AND p.date <= ?`, winStart, asOf)
	if err != nil {
		return nil, fmt.Errorf("digest prices: %w", err)
	}
	// (region, market, item, date) -> values (several raw names can map to
	// one canonical item).
	type key struct {
		market string
		item   int
		date   string
	}
	vals := map[key][]float64{}
	regionOf := map[string]string{}
	for rows.Next() {
		var date, market, raw string
		var price, low, high sql.NullFloat64
		if err := rows.Scan(&date, &market, &raw, &price, &low, &high); err != nil {
			rows.Close()
			return nil, err
		}
		rid, ok := marketRegion[market]
		if !ok {
			continue
		}
		idx, ok := itemIdx[signals.CanonicalCommodity(raw)]
		if !ok {
			continue
		}
		v, ok := signals.EffectivePrice(price, low, high)
		if !ok || v <= 0 {
			continue
		}
		vals[key{market, idx, date}] = append(vals[key{market, idx, date}], v)
		regionOf[market] = rid
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var all []obs
	for k, v := range vals {
		all = append(all, obs{region: regionOf[k.market], market: k.market, date: k.date, item: k.item, price: signals.Median(v)})
	}
	// index: region -> item -> date -> market -> price
	idx := map[string]map[int]map[string]map[string]float64{}
	regionDates := map[string]map[string]map[string]bool{} // region -> date -> markets with any key price
	for _, o := range all {
		if idx[o.region] == nil {
			idx[o.region] = map[int]map[string]map[string]float64{}
		}
		if idx[o.region][o.item] == nil {
			idx[o.region][o.item] = map[string]map[string]float64{}
		}
		if idx[o.region][o.item][o.date] == nil {
			idx[o.region][o.item][o.date] = map[string]float64{}
		}
		idx[o.region][o.item][o.date][o.market] = o.price
		if regionDates[o.region] == nil {
			regionDates[o.region] = map[string]map[string]bool{}
		}
		if regionDates[o.region][o.date] == nil {
			regionDates[o.region][o.date] = map[string]bool{}
		}
		regionDates[o.region][o.date][o.market] = true
	}

	d := &Digest{AsOf: asOf}
	for _, it := range KeyItems {
		d.Rows = append(d.Rows, Row{
			Canonical: it.Canonical, TL: it.TL, EN: it.EN, JA: it.JA, Piece: it.Piece,
			Cells: map[string]*Cell{},
		})
	}

	// Metro pooling: per item, pairs (price on D, previous price) of
	// markets in included regions.
	type pair struct{ cur, prev float64 }
	metroCur := make([][]float64, len(KeyItems))
	metroPairs := make([][]pair, len(KeyItems))

	for _, r := range regions.Regions {
		st := RegionStatus{
			ID: r.ID, TitleTL: r.TitleTL, TitleEN: r.TitleEN, TitleJA: r.TitleJA,
			ShortTL: r.TitleTL,
		}
		drop := func(reason string) {
			st.Included = false
			st.Reason = reason
			d.Regions = append(d.Regions, st)
		}
		if len(regionDates[r.ID][asOf]) == 0 {
			drop("no data")
			continue
		}
		st.Markets = len(regionDates[r.ID][asOf])
		// Previous report date of this region: latest earlier date with a
		// reasonable number of reporting markets, within the lookback.
		prev := ""
		floor := asOfT.AddDate(0, 0, -PrevLookbackDays).Format("2006-01-02")
		for dt, ms := range regionDates[r.ID] {
			if dt < asOf && dt >= floor && len(ms) >= 2 && dt > prev {
				prev = dt
			}
		}
		st.PrevAsOf = prev

		// Existing per-region checker (same gate as the day pages).
		sig, serr := signals.ComputeRegion(db, r.ID, signals.RegionOptions{AsOf: asOf})
		if serr != nil {
			drop("signals: " + serr.Error())
			continue
		}
		if rep := fbregion.CheckPost(sig, fbregion.RenderPost(sig)); rep.Status != fbregion.StatusPass {
			drop("region checker failed")
			continue
		}
		if opt.ForceFail != nil && opt.ForceFail(r.ID) {
			drop("region checker failed (forced)")
			continue
		}

		type cellT struct {
			item  int
			cell  *Cell
			curs  []float64
			pairs []pair
		}
		var cells []cellT
		for i := range KeyItems {
			byDate := idx[r.ID][i]
			cur := byDate[asOf]
			if len(cur) < MinMarketsPerCell {
				continue
			}
			var curVals []float64
			for _, v := range cur {
				curVals = append(curVals, v)
			}
			price := roundPrice(signals.Median(curVals))
			// Plausibility vs the region's own 30-day median of daily medians.
			var series []float64
			for dt, ms := range byDate {
				if dt == asOf {
					continue
				}
				var vs []float64
				for _, v := range ms {
					vs = append(vs, v)
				}
				series = append(series, signals.Median(vs))
			}
			if len(series) >= 5 {
				med := signals.Median(series)
				if med > 0 && (price < med*MedianBandLow || price > med*MedianBandHigh) {
					continue
				}
			}
			c := &Cell{Price: price, Markets: len(cur), Dir: DirNone}
			var pairs []pair
			if prev != "" {
				for m, v := range cur {
					if pv, ok := byDate[prev][m]; ok {
						pairs = append(pairs, pair{v, pv})
					}
				}
				if len(pairs) >= 2 || (len(pairs) == 1 && len(cur) == 1) {
					var cs, ps []float64
					for _, p := range pairs {
						cs = append(cs, p.cur)
						ps = append(ps, p.prev)
					}
					pct := signals.PctChange(signals.Median(cs), signals.Median(ps))
					if !math.IsNaN(pct) && math.Abs(pct) <= MaxPlausibleStepPct {
						p := roundInt(pct)
						c.Pct = &p
						switch {
						case pct >= FlatBelowPct:
							c.Dir = DirUp
						case pct <= -FlatBelowPct:
							c.Dir = DirDown
						default:
							c.Dir = DirFlat
						}
					} else {
						pairs = nil
					}
				} else {
					pairs = nil
				}
			}
			cells = append(cells, cellT{item: i, cell: c, curs: curVals, pairs: pairs})
		}
		st.Cells = len(cells)
		if len(cells) < MinCellsPerRegion {
			drop(fmt.Sprintf("only %d of %d key prices valid", len(cells), len(KeyItems)))
			continue
		}
		st.Included = true
		for _, ct := range cells {
			d.Rows[ct.item].Cells[r.ID] = ct.cell
			metroCur[ct.item] = append(metroCur[ct.item], ct.curs...)
			metroPairs[ct.item] = append(metroPairs[ct.item], ct.pairs...)
		}
		// Best deal candidate: top deal of the region's signals.
		if len(sig.Deals) > 0 {
			dl := sig.Deals[0]
			if d.Best == nil || dl.PctRounded < d.Best.Deal.PctRounded {
				d.Best = &BestDeal{Region: r.ID, Deal: dl}
			}
		}
		d.Regions = append(d.Regions, st)
	}

	// Metro cells.
	for i := range d.Rows {
		if len(metroCur[i]) == 0 {
			continue
		}
		c := &Cell{Price: roundPrice(signals.Median(metroCur[i])), Markets: len(metroCur[i]), Dir: DirNone}
		if len(metroPairs[i]) >= 2 {
			var cs, ps []float64
			for _, p := range metroPairs[i] {
				cs = append(cs, p.cur)
				ps = append(ps, p.prev)
			}
			pct := signals.PctChange(signals.Median(cs), signals.Median(ps))
			if !math.IsNaN(pct) && math.Abs(pct) <= MaxPlausibleStepPct {
				p := roundInt(pct)
				c.Pct = &p
				switch {
				case pct >= FlatBelowPct:
					c.Dir = DirUp
				case pct <= -FlatBelowPct:
					c.Dir = DirDown
				default:
					c.Dir = DirFlat
				}
			}
		}
		d.Rows[i].Metro = c
	}

	// Common previous date label.
	common, mixed := "", false
	for _, r := range d.Regions {
		if !r.Included || r.PrevAsOf == "" {
			continue
		}
		if common == "" {
			common = r.PrevAsOf
		} else if common != r.PrevAsOf {
			mixed = true
		}
	}
	if !mixed {
		d.PrevAsOf = common
	}
	sort.SliceStable(d.Regions, func(i, j int) bool { return regionOrder(d.Regions[i].ID) < regionOrder(d.Regions[j].ID) })
	return d, nil
}

func regionOrder(id string) int {
	for _, r := range regions.Regions {
		if r.ID == id {
			return r.Order
		}
	}
	return 99
}
