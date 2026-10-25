// Package weekly computes the Sunday weekly report signals
// (WEEKLY_SPEC.md section 2): Metro Manila-wide week-over-week numbers per
// commodity plus best area deals per region. All numbers are deterministic
// and JSON-serialisable; text rendering lives in text.go and validation in
// check.go.
package weekly

import (
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// Thresholds fixed by the spec.
const (
	MetroDropPct   = -5
	MetroRisePct   = 5
	MetroMinDates  = 3
	DealPct        = -10
	DealMinDates   = 2
	RegionMaxDeals = 2
	MaxDrops       = 5
	MaxRises       = 3
	RegionWindow   = 30
	RegionMinDays  = 10
)

// weekIDRe validates ISO week ids like "2026-W43".
var weekIDRe = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// ParseWeekID splits "YYYY-Www" into year and ISO week numbers.
func ParseWeekID(id string) (year, week int, err error) {
	m := weekIDRe.FindStringSubmatch(id)
	if m == nil {
		return 0, 0, fmt.Errorf("bad week id %q (want YYYY-Www)", id)
	}
	fmt.Sscanf(m[1], "%d", &year)
	fmt.Sscanf(m[2], "%d", &week)
	if week < 1 || week > 53 {
		return 0, 0, fmt.Errorf("bad week id %q (week out of range)", id)
	}
	// Verify the week exists: its Monday must round-trip to the same id.
	mon := weekMonday(year, week)
	if WeekIDOf(mon) != id {
		return 0, 0, fmt.Errorf("bad week id %q (no such ISO week)", id)
	}
	return year, week, nil
}

// weekMonday returns the Monday of ISO week (year, week) in UTC.
func weekMonday(year, week int) time.Time {
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	wd := (int(jan4.Weekday()) + 6) % 7 // Mon=0
	mon1 := jan4.AddDate(0, 0, -wd)
	return mon1.AddDate(0, 0, (week-1)*7)
}

// WeekIDOf returns the ISO week id of t (YYYY-Www).
func WeekIDOf(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// WeekSaturday returns the Saturday date (YYYY-MM-DD) of the ISO week.
func WeekSaturday(year, week int) string {
	return weekMonday(year, week).AddDate(0, 0, 5).Format("2006-01-02")
}

// WeekSunday returns the Sunday date (YYYY-MM-DD) of the ISO week.
func WeekSunday(year, week int) string {
	return weekMonday(year, week).AddDate(0, 0, 6).Format("2006-01-02")
}

// MonSat returns the calendar Mon..Sat dates (YYYY-MM-DD) of the week.
func MonSat(year, week int) []string {
	mon := weekMonday(year, week)
	out := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		out = append(out, mon.AddDate(0, 0, i).Format("2006-01-02"))
	}
	return out
}

// PrevWeekID returns the ISO week id before id.
func PrevWeekID(id string) (string, error) {
	year, week, err := ParseWeekID(id)
	if err != nil {
		return "", err
	}
	return WeekIDOf(weekMonday(year, week).AddDate(0, 0, -7)), nil
}

// mappedMarkets returns the exact mapped market names plus a market->region
// region-id index.
func mappedMarkets() (names []string, regionOf map[string]string) {
	regionOf = map[string]string{}
	for i := range regions.Regions {
		r := &regions.Regions[i]
		for _, m := range r.Markets {
			names = append(names, m.Name)
			regionOf[m.Name] = r.ID
		}
	}
	return names, regionOf
}

// priceDatesIn returns the DISTINCT price dates in [lo, hi] that have at
// least one mapped-market price, oldest first.
func priceDatesIn(db *sql.DB, names []string, lo, hi string) ([]string, error) {
	ph := make([]string, 0, len(names)+2)
	args := make([]any, 0, len(names)+2)
	for _, n := range names {
		ph = append(ph, "?")
		args = append(args, n)
	}
	args = append(args, lo, hi)
	q := `SELECT DISTINCT p.date FROM prices p JOIN markets m ON m.id = p.market_id
		WHERE m.name IN (` + strings.Join(ph, ",") + `) AND p.date >= ? AND p.date <= ?
		ORDER BY p.date`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CoveredDates returns the report (Mon..Sat, Sunday excluded) dates of the
// week present in the DB.
func CoveredDates(db *sql.DB, weekID string) ([]string, error) {
	year, week, err := ParseWeekID(weekID)
	if err != nil {
		return nil, err
	}
	days := MonSat(year, week)
	names, _ := mappedMarkets()
	got, err := priceDatesIn(db, names, days[0], days[5])
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, d := range got {
		have[d] = true
	}
	var out []string
	for _, d := range days {
		if have[d] {
			out = append(out, d)
		}
	}
	return out, nil
}

// MissingDates returns the calendar Mon..Sat dates of the week with no
// mapped-market price in the DB.
func MissingDates(db *sql.DB, weekID string) ([]string, error) {
	year, week, err := ParseWeekID(weekID)
	if err != nil {
		return nil, err
	}
	days := MonSat(year, week)
	covered, err := CoveredDates(db, weekID)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, d := range covered {
		have[d] = true
	}
	var out []string
	for _, d := range days {
		if !have[d] {
			out = append(out, d)
		}
	}
	return out, nil
}

// Reportable reports whether the week has >= 3 covered dates and the
// previous week has >= 3.
func Reportable(db *sql.DB, weekID string) (bool, error) {
	covered, err := CoveredDates(db, weekID)
	if err != nil {
		return false, err
	}
	if len(covered) < MetroMinDates {
		return false, nil
	}
	prev, err := PrevWeekID(weekID)
	if err != nil {
		return false, err
	}
	prevCovered, err := CoveredDates(db, prev)
	if err != nil {
		return false, err
	}
	return len(prevCovered) >= MetroMinDates, nil
}

// MetroEntry is one Metro Manila-wide week-over-week commodity row.
type MetroEntry struct {
	Name         string  `json:"name"`
	Unit         string  `json:"unit"`
	AvgThis      float64 `json:"avg_this"`
	AvgPrev      float64 `json:"avg_prev"`
	Pct          int     `json:"pct"`
	DisplayPrice float64 `json:"display_price"`
	NThis        int     `json:"n_this"`
	NPrev        int     `json:"n_prev"`
}

// DealEntry is one best area deal (one market, one commodity).
type DealEntry struct {
	Name         string  `json:"name"`
	Unit         string  `json:"unit"`
	Market       string  `json:"market"`
	MarketShort  string  `json:"market_short"`
	Pct          int     `json:"pct"`
	DisplayPrice float64 `json:"display_price"`
	NDates       int     `json:"n_dates"`
}

// RegionDeals holds the best deals of one region (best first).
type RegionDeals struct {
	Region  string      `json:"region"`
	TitleTL string      `json:"title_tl"`
	TitleEN string      `json:"title_en"`
	TitleJA string      `json:"title_ja"`
	ShortJA string      `json:"short_ja"`
	Deals   []DealEntry `json:"deals"`
}

// Signals is the top-level weekly signals document.
type Signals struct {
	Week          string        `json:"week"`
	Covered       []string      `json:"covered_dates"`
	PrevCovered   []string      `json:"prev_covered_dates"`
	Missing       []string      `json:"missing_dates"`
	Reportable    bool          `json:"reportable"`
	Metro         []MetroEntry  `json:"metro"`
	Drops         []MetroEntry  `json:"drops"`
	Rises         []MetroEntry  `json:"rises"`
	DealsByRegion []RegionDeals `json:"deals_by_region"`
	GeneratedBy   string        `json:"generated_by"`
}

// roundInt rounds half away from zero (math.Round).
func roundInt(v float64) int {
	return int(math.Round(v))
}

// unitByCanon maps canonical commodity names to units (same rule as the
// daily region signals: raw name equal to canonical wins, else lowest raw).
func unitByCanon(db *sql.DB) (map[string]string, error) {
	out := map[string]string{}
	rows, err := db.Query("SELECT name, unit FROM commodities")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	best := map[string]string{}
	for rows.Next() {
		var name string
		var unit sql.NullString
		if err := rows.Scan(&name, &unit); err != nil {
			return nil, err
		}
		c := signals.CanonicalCommodity(name)
		if signals.CanonicalCategory(name) == "other" {
			continue
		}
		if cur, ok := best[c]; !ok || name == c || (cur != c && name < cur) {
			best[c] = name
			out[c] = unit.String
		}
	}
	return out, rows.Err()
}

// dailyMedians loads medByMarket[market][canon][date] = median effective
// price for mapped markets over [lo, hi].
func dailyMedians(db *sql.DB, names []string, lo, hi string) (map[string]map[string]map[string]float64, error) {
	ph := make([]string, 0, len(names))
	args := make([]any, 0, len(names)+2)
	for _, n := range names {
		ph = append(ph, "?")
		args = append(args, n)
	}
	args = append(args, lo, hi)
	q := `SELECT p.date, m.name, c.name, p.price, p.price_low, p.price_high
		FROM prices p JOIN markets m ON m.id = p.market_id
		JOIN commodities c ON c.id = p.commodity_id
		WHERE m.name IN (` + strings.Join(ph, ",") + `) AND p.date >= ? AND p.date <= ?`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byMCD := map[string]map[string]map[string][]float64{}
	for rows.Next() {
		var date, market, raw string
		var price, low, high sql.NullFloat64
		if err := rows.Scan(&date, &market, &raw, &price, &low, &high); err != nil {
			return nil, err
		}
		v, ok := signals.EffectivePrice(price, low, high)
		if !ok {
			continue
		}
		c := signals.CanonicalCommodity(raw)
		if signals.CanonicalCategory(raw) == "other" {
			continue
		}
		if byMCD[market] == nil {
			byMCD[market] = map[string]map[string][]float64{}
		}
		if byMCD[market][c] == nil {
			byMCD[market][c] = map[string][]float64{}
		}
		byMCD[market][c][date] = append(byMCD[market][c][date], v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]map[string]map[string]float64{}
	for mkt, byC := range byMCD {
		for c, byD := range byC {
			for d, vals := range byD {
				if out[mkt] == nil {
					out[mkt] = map[string]map[string]float64{}
				}
				if out[mkt][c] == nil {
					out[mkt][c] = map[string]float64{}
				}
				out[mkt][c][d] = signals.Median(vals)
			}
		}
	}
	return out, nil
}

// mean returns the arithmetic mean.
func mean(vals []float64) float64 {
	s := 0.0
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}

// Compute builds the weekly signals document for weekID.
func Compute(db *sql.DB, weekID string) (*Signals, error) {
	year, week, err := ParseWeekID(weekID)
	if err != nil {
		return nil, err
	}
	_ = year
	_ = week
	prevID, err := PrevWeekID(weekID)
	if err != nil {
		return nil, err
	}
	covered, err := CoveredDates(db, weekID)
	if err != nil {
		return nil, err
	}
	prevCovered, err := CoveredDates(db, prevID)
	if err != nil {
		return nil, err
	}
	missing, err := MissingDates(db, weekID)
	if err != nil {
		return nil, err
	}
	sig := &Signals{
		Week: weekID, Covered: covered, PrevCovered: prevCovered,
		Missing:     missing,
		Reportable:  len(covered) >= MetroMinDates && len(prevCovered) >= MetroMinDates,
		GeneratedBy: "presyo fbweekly",
	}
	if sig.Covered == nil {
		sig.Covered = []string{}
	}
	if sig.PrevCovered == nil {
		sig.PrevCovered = []string{}
	}
	if sig.Missing == nil {
		sig.Missing = []string{}
	}
	names, regionOf := mappedMarkets()
	units, err := unitByCanon(db)
	if err != nil {
		return nil, err
	}

	// Metro Manila-wide: daily medians across mapped markets per date.
	allDates := append(append([]string{}, covered...), prevCovered...)
	if len(allDates) > 0 {
		lo, hi := allDates[0], allDates[0]
		for _, d := range allDates[1:] {
			if d < lo {
				lo = d
			}
			if d > hi {
				hi = d
			}
		}
		med, err := dailyMedians(db, names, lo, hi)
		if err != nil {
			return nil, err
		}
		// canon -> date -> daily metro median.
		canons := map[string]bool{}
		for _, byC := range med {
			for c := range byC {
				canons[c] = true
			}
		}
		for c := range canons {
			var thisVals, prevVals []float64
			for _, d := range covered {
				var perMkt []float64
				for _, m := range names {
					if v, ok := med[m][c][d]; ok {
						perMkt = append(perMkt, v)
					}
				}
				if len(perMkt) > 0 {
					thisVals = append(thisVals, signals.Median(perMkt))
				}
			}
			for _, d := range prevCovered {
				var perMkt []float64
				for _, m := range names {
					if v, ok := med[m][c][d]; ok {
						perMkt = append(perMkt, v)
					}
				}
				if len(perMkt) > 0 {
					prevVals = append(prevVals, signals.Median(perMkt))
				}
			}
			if len(thisVals) < MetroMinDates || len(prevVals) < MetroMinDates {
				continue
			}
			avgThis, avgPrev := mean(thisVals), mean(prevVals)
			if avgPrev == 0 || math.IsNaN(avgThis) || math.IsNaN(avgPrev) {
				continue
			}
			pct := roundInt(avgThis/avgPrev*100 - 100)
			sig.Metro = append(sig.Metro, MetroEntry{
				Name: c, Unit: units[c],
				AvgThis: signals.Round2(avgThis), AvgPrev: signals.Round2(avgPrev),
				Pct: pct, DisplayPrice: math.Round(avgThis),
				NThis: len(thisVals), NPrev: len(prevVals),
			})
		}
		sort.Slice(sig.Metro, func(i, j int) bool { return sig.Metro[i].Name < sig.Metro[j].Name })
		for _, m := range sig.Metro {
			if m.Pct <= MetroDropPct {
				sig.Drops = append(sig.Drops, m)
			}
			if m.Pct >= MetroRisePct {
				sig.Rises = append(sig.Rises, m)
			}
		}
		sort.Slice(sig.Drops, func(i, j int) bool {
			if sig.Drops[i].Pct != sig.Drops[j].Pct {
				return sig.Drops[i].Pct < sig.Drops[j].Pct
			}
			return sig.Drops[i].Name < sig.Drops[j].Name
		})
		sort.Slice(sig.Rises, func(i, j int) bool {
			if sig.Rises[i].Pct != sig.Rises[j].Pct {
				return sig.Rises[i].Pct > sig.Rises[j].Pct
			}
			return sig.Rises[i].Name < sig.Rises[j].Name
		})
		if len(sig.Drops) > MaxDrops {
			sig.Drops = sig.Drops[:MaxDrops]
		}
		if len(sig.Rises) > MaxRises {
			sig.Rises = sig.Rises[:MaxRises]
		}
	}
	if sig.Metro == nil {
		sig.Metro = []MetroEntry{}
	}
	if sig.Drops == nil {
		sig.Drops = []MetroEntry{}
	}
	if sig.Rises == nil {
		sig.Rises = []MetroEntry{}
	}

	// Best deals per region: daily pct vs the market's own 30-day median.
	if len(covered) > 0 {
		earliest := covered[0]
		et, _ := time.Parse("2006-01-02", earliest)
		lo := et.AddDate(0, 0, -(RegionWindow - 1)).Format("2006-01-02")
		med, err := dailyMedians(db, names, lo, covered[len(covered)-1])
		if err != nil {
			return nil, err
		}
		type key struct{ market, canon string }
		type acc struct {
			pcts   []float64
			prices []float64
		}
		byKey := map[key]*acc{}
		get := func(m, c string) *acc {
			k := key{m, c}
			a := byKey[k]
			if a == nil {
				a = &acc{}
				byKey[k] = a
			}
			return a
		}
		for _, d := range covered {
			dT, _ := time.Parse("2006-01-02", d)
			winLo := dT.AddDate(0, 0, -(RegionWindow - 1)).Format("2006-01-02")
			for _, m := range names {
				for c, byD := range med[m] {
					price, ok := byD[d]
					if !ok {
						continue
					}
					var series []float64
					for dd, v := range byD {
						if dd >= winLo && dd <= d {
							series = append(series, v)
						}
					}
					if len(series) < RegionMinDays {
						continue
					}
					md := signals.Median(series)
					if md == 0 || math.IsNaN(md) {
						continue
					}
					pct := signals.Round1(signals.PctChange(price, md))
					a := get(m, c)
					a.pcts = append(a.pcts, pct)
					a.prices = append(a.prices, price)
				}
			}
		}
		for i := range regions.Regions {
			r := &regions.Regions[i]
			type cand struct {
				canon, market, short, unit string
				pct                        int
				price                      float64
				n                          int
			}
			var cands []cand
			for _, m := range r.Markets {
				for c := range units {
					_ = c
				}
				// Iterate canons seen for this market.
				for c := range med[m.Name] {
					a := byKey[key{m.Name, c}]
					if a == nil || len(a.pcts) < DealMinDates {
						continue
					}
					pct := roundInt(mean(a.pcts))
					if pct > DealPct {
						continue
					}
					cands = append(cands, cand{
						canon: c, market: m.Name, short: m.Short, unit: units[c],
						pct: pct, price: math.Round(mean(a.prices)), n: len(a.pcts),
					})
				}
			}
			sort.Slice(cands, func(a, b int) bool {
				if cands[a].pct != cands[b].pct {
					return cands[a].pct < cands[b].pct
				}
				if cands[a].canon != cands[b].canon {
					return cands[a].canon < cands[b].canon
				}
				return cands[a].market < cands[b].market
			})
			rd := RegionDeals{
				Region: r.ID, TitleTL: r.TitleTL, TitleEN: r.TitleEN,
				TitleJA: r.TitleJA, ShortJA: shortJA(r.ID),
			}
			usedCanon := map[string]bool{}
			for _, c := range cands {
				if usedCanon[c.canon] {
					continue
				}
				usedCanon[c.canon] = true
				rd.Deals = append(rd.Deals, DealEntry{
					Name: c.canon, Unit: c.unit, Market: c.market,
					MarketShort: c.short, Pct: c.pct,
					DisplayPrice: c.price, NDates: c.n,
				})
				if len(rd.Deals) >= RegionMaxDeals {
					break
				}
			}
			if rd.Deals == nil {
				rd.Deals = []DealEntry{}
			}
			sig.DealsByRegion = append(sig.DealsByRegion, rd)
		}
		_ = regionOf
	} else {
		for i := range regions.Regions {
			r := &regions.Regions[i]
			sig.DealsByRegion = append(sig.DealsByRegion, RegionDeals{
				Region: r.ID, TitleTL: r.TitleTL, TitleEN: r.TitleEN,
				TitleJA: r.TitleJA, ShortJA: shortJA(r.ID), Deals: []DealEntry{},
			})
		}
	}
	return sig, nil
}

// shortJA returns the compact Japanese region label (no parens).
func shortJA(id string) string {
	switch id {
	case "south":
		return "南部"
	case "manila":
		return "マニラ・パサイ"
	case "qc":
		return "ケソン市"
	case "north":
		return "北部"
	case "east":
		return "東部"
	}
	return id
}
