// Package export ports presyo_bantay/export_site.py byte-for-byte:
// compact JSON snapshots of prices + FX + week summary, with alias
// canonicalization applied at export time.
package export

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/aliases"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fx"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/parse"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/pyjson"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// FXSource and PriceSource mirror export_site constants.
const (
	FXSource    = "USD/PHP: European Central Bank reference rates via Frankfurter (frankfurter.dev)"
	PriceSource = "Philippine Department of Agriculture Price Monitoring (da.gov.ph)"
)

// generatedAt returns the meta.json timestamp ("...Z"). The
// PRESYO_GENERATED_AT env var pins it (test hook so byte-identical output
// can be verified against the Python exporter).
func generatedAt() string {
	if v := os.Getenv("PRESYO_GENERATED_AT"); v != "" {
		return v
	}
	return time.Now().UTC().Format("2006-01-02T15:04:05") + "Z"
}

func round2(v *float64) *pyjson.Float {
	if v == nil {
		return nil
	}
	f := parse.RoundN(*v, 2)
	r := pyjson.Float(f)
	return &r
}

func round4(v float64) pyjson.Float {
	return pyjson.Float(parse.RoundN(v, 4))
}

// norm mirrors summary/sanity normalization.
func norm(name string) string {
	lower := ""
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		lower += string(r)
	}
	fields := splitFields(lower)
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += " "
		}
		out += f
	}
	return out
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// shortMarket mirrors render.short_market.
func shortMarket(market string) string {
	lowered := norm(market)
	if contains(lowered, "las pi") {
		return "Las Piñas"
	}
	if contains(lowered, "alabang") {
		return "Alabang"
	}
	if contains(lowered, "huerta") {
		return "La Huerta"
	}
	if contains(lowered, "bicutan") {
		return "Bicutan"
	}
	return market
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// marketsOfInterest mirrors summary.MARKETS_OF_INTEREST_ORDERED.
var marketsOfInterest = []string{
	"New Las Piñas City Public Market",
	"Alabang Central Market",
	"La Huerta Market/Parañaque",
	"Bicutan Market",
}

// matchMarket mirrors sanity.match_market over plain name lists.
func matchMarket(names []string, wanted string) string {
	needle := norm(wanted)
	for _, actual := range names {
		hay := norm(actual)
		if contains(hay, needle) || contains(needle, hay) {
			return actual
		}
	}
	return ""
}

// resolveMarkets mirrors summary.resolve_markets.
func resolveMarkets(markets map[string]map[string]struct{}) []string {
	var names []string
	for n := range markets {
		names = append(names, n)
	}
	var resolved []string
	for _, wanted := range marketsOfInterest {
		if actual := matchMarket(names, wanted); actual != "" {
			dup := false
			for _, r := range resolved {
				if r == actual {
					dup = true
				}
			}
			if !dup {
				resolved = append(resolved, actual)
			}
		}
	}
	return resolved
}

// selectBaseline mirrors summary.select_snapshots over plain date lists.
func selectBaseline(dates []string, weekEnding string) (latest, baseline string) {
	var eligible []string
	for _, d := range dates {
		if d <= weekEnding {
			eligible = append(eligible, d)
		}
	}
	if len(eligible) == 0 {
		return "", ""
	}
	latest = eligible[0]
	for _, d := range eligible[1:] {
		if d > latest {
			latest = d
		}
	}
	type cand struct {
		dist int
		date string
	}
	var cands []cand
	for _, d := range eligible {
		if d == latest {
			continue
		}
		diff := daysBetween(latest, d)
		if diff >= 5 && diff <= 10 {
			ad := diff - 7
			if ad < 0 {
				ad = -ad
			}
			cands = append(cands, cand{ad, d})
		}
	}
	if len(cands) == 0 {
		return latest, ""
	}
	best := cands[0].dist
	for _, c := range cands[1:] {
		if c.dist < best {
			best = c.dist
		}
	}
	baseline = ""
	for _, c := range cands {
		if c.dist == best && c.date > baseline {
			baseline = c.date
		}
	}
	return latest, baseline
}

func daysBetween(a, b string) int {
	ta, err1 := time.Parse("2006-01-02", a)
	tb, err2 := time.Parse("2006-01-02", b)
	if err1 != nil || err2 != nil {
		return 0
	}
	d := ta.Sub(tb)
	if d < 0 {
		d = -d
	}
	return int(d / (24 * time.Hour))
}

// pctChange mirrors summary.pct_change.
func pctChange(latest, baseline *float64) *pyjson.Float {
	if latest == nil || baseline == nil || *baseline == 0 {
		return nil
	}
	f := parse.RoundN((*latest-*baseline) / *baseline * 100.0, 1)
	r := pyjson.Float(f)
	return &r
}

// cheapestResult mirrors summary.CheapestResult.
type cheapestResult struct {
	price   *float64
	markets []string
}

// computeCheapest mirrors summary.compute_cheapest (input prices already
// rounded; ties listed, NULLs skipped).
func computeCheapest(latest map[string]*float64, markets []string) cheapestResult {
	var best *float64
	var winners []string
	for _, m := range markets {
		price := latest[m]
		if price == nil {
			continue
		}
		if best == nil || *price < *best {
			best = price
			winners = []string{m}
		} else if *price == *best {
			dup := false
			for _, w := range winners {
				if w == m {
					dup = true
				}
			}
			if !dup {
				winners = append(winners, m)
			}
		}
	}
	if winners == nil {
		winners = []string{}
	}
	return cheapestResult{price: best, markets: winners}
}

type marketRec struct {
	id    int64
	name  string
	city  sql.NullString
	short string
}

type commodityInfo struct {
	id       int64
	name     string
	unit     string
	category string
	group    *string
}

// canonicalGroups mirrors export_site._canonical_groups.
func canonicalGroups(db *sql.DB) (map[string]*commodityInfo, []string, map[int64][2]any, error) {
	rows, err := db.Query("SELECT id, name, category, spec, unit FROM commodities")
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	type member struct {
		id        int64
		name      string
		category  sql.NullString
		spec      sql.NullString
		unit      sql.NullString
		canonical bool
	}
	groups := map[string][]member{}
	orderSeen := map[string]bool{}
	var orderKeys []string
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.id, &m.name, &m.category, &m.spec, &m.unit); err != nil {
			return nil, nil, nil, err
		}
		canonical, _, _, _ := aliases.Canonicalize(m.name)
		m.canonical = canonical == m.name
		if !orderSeen[canonical] {
			orderSeen[canonical] = true
			orderKeys = append(orderKeys, canonical)
		}
		groups[canonical] = append(groups[canonical], m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	info := map[string]*commodityInfo{}
	memberCanonical := map[int64][2]any{}
	for canonical, members := range groups {
		sort.Slice(members, func(i, j int) bool {
			if members[i].canonical != members[j].canonical {
				return members[i].canonical && !members[j].canonical
			}
			return members[i].id < members[j].id
		})
		first := members[0]
		_, _, unit, category := aliases.Canonicalize(canonical)
		// Python: first["unit"] or unit (empty string falls back).
		u := unit
		if first.unit.Valid && first.unit.String != "" {
			u = first.unit.String
		}
		c := category
		if first.category.Valid && first.category.String != "" {
			c = first.category.String
		}
		info[canonical] = &commodityInfo{
			id: first.id, name: canonical, unit: u, category: c,
			group: aliases.GroupFor(canonical),
		}
		for _, m := range members {
			memberCanonical[m.id] = [2]any{canonical, m.canonical}
		}
	}
	_ = orderKeys
	// order: canonicals sorted by export id.
	order := make([]string, 0, len(info))
	for canonical := range info {
		order = append(order, canonical)
	}
	sort.Slice(order, func(i, j int) bool { return info[order[i]].id < info[order[j]].id })
	return info, order, memberCanonical, nil
}

func strOrNull(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

// ExportSite mirrors export_site.export_site; returns written paths.
func ExportSite(db *sql.DB, outDir string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	seriesDir := filepath.Join(outDir, "series")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		return nil, err
	}

	dates, err := store.ListDates(db)
	if err != nil {
		return nil, err
	}
	if dates == nil {
		dates = []string{}
	}
	var latestDate *string
	if len(dates) > 0 {
		mx := dates[0]
		for _, d := range dates[1:] {
			if d > mx {
				mx = d
			}
		}
		latestDate = &mx
	}

	mrows, err := db.Query("SELECT id, name, city FROM markets ORDER BY id")
	if err != nil {
		return nil, err
	}
	var markets []marketRec
	for mrows.Next() {
		var m marketRec
		if err := mrows.Scan(&m.id, &m.name, &m.city); err != nil {
			mrows.Close()
			return nil, err
		}
		m.short = shortMarket(m.name)
		markets = append(markets, m)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	info, order, memberCanonical, err := canonicalGroups(db)
	if err != nil {
		return nil, err
	}

	resolved, err := store.MatchNames(db, "markets", "")
	if err != nil {
		return nil, err
	}
	byName := map[string]int64{}
	for _, m := range markets {
		byName[m.name] = m.id
	}
	marketNames := map[string]map[string]struct{}{}
	for _, n := range resolved {
		marketNames[n] = map[string]struct{}{}
	}
	defaultNames := resolveMarkets(marketNames)
	var defaultIDs []int64
	for _, n := range defaultNames {
		if id, ok := byName[n]; ok {
			defaultIDs = append(defaultIDs, id)
		}
	}
	if defaultIDs == nil {
		defaultIDs = []int64{}
	}

	// Merged price lookup (canonical wins, alias fills gaps).
	prows, err := db.Query("SELECT date, market_id, commodity_id, price FROM prices ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	type priceRow struct {
		day         string
		marketID    int64
		commodityID int64
		price       *float64
	}
	var priceRows []priceRow
	for prows.Next() {
		var pr priceRow
		var price sql.NullFloat64
		if err := prows.Scan(&pr.day, &pr.marketID, &pr.commodityID, &price); err != nil {
			prows.Close()
			return nil, err
		}
		if price.Valid {
			f := price.Float64
			pr.price = &f
		}
		priceRows = append(priceRows, pr)
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}
	type mkey struct {
		day       string
		marketID  int64
		canonical string
	}
	merged := map[mkey]*float64{}
	for _, pr := range priceRows {
		mapping, ok := memberCanonical[pr.commodityID]
		if !ok {
			continue
		}
		canonical, _ := mapping[0].(string)
		isCanonical, _ := mapping[1].(bool)
		if isCanonical {
			merged[mkey{pr.day, pr.marketID, canonical}] = pr.price
		}
	}
	for _, pr := range priceRows {
		mapping, ok := memberCanonical[pr.commodityID]
		if !ok {
			continue
		}
		canonical, _ := mapping[0].(string)
		isCanonical, _ := mapping[1].(bool)
		if isCanonical {
			continue
		}
		k := mkey{pr.day, pr.marketID, canonical}
		if cur, ok := merged[k]; !ok || (cur == nil && pr.price != nil) {
			merged[k] = pr.price
		}
	}

	var marketIDs []int64
	for _, m := range markets {
		marketIDs = append(marketIDs, m.id)
	}

	write := func(path string, v any) error {
		return os.WriteFile(path, pyjson.Marshal(v), 0o644)
	}

	for _, canonical := range order {
		entry := info[canonical]
		columns := pyjson.NewObj()
		for _, mid := range marketIDs {
			col := make([]any, 0, len(dates))
			for _, day := range dates {
				col = append(col, round2(merged[mkey{day, mid, canonical}]))
			}
			columns.Keys = append(columns.Keys, itoa(mid))
			columns.M[itoa(mid)] = col
		}
		d := make([]any, 0, len(dates))
		for _, day := range dates {
			d = append(d, day)
		}
		obj := pyjson.NewObj("d", d, "m", columns)
		if err := write(filepath.Join(seriesDir, "c"+itoa(entry.id)+".json"), obj); err != nil {
			return nil, err
		}
	}

	fxRows, err := fx.Series(db, fx.FXBase, fx.FXQuote)
	if err != nil {
		return nil, err
	}
	fxd := make([]any, 0, len(fxRows))
	fxr := make([]any, 0, len(fxRows))
	for _, r := range fxRows {
		fxd = append(fxd, r.Date)
		fxr = append(fxr, round4(r.Rate))
	}
	if err := write(filepath.Join(outDir, "fx.json"),
		pyjson.NewObj("d", fxd, "r", fxr)); err != nil {
		return nil, err
	}

	var weekItems []any
	var baselineDate *string
	if latestDate != nil {
		_, baseline := selectBaseline(dates, *latestDate)
		if baseline != "" {
			baselineDate = &baseline
		}
		latestBy := map[string]map[string]*float64{}
		baselineBy := map[string]map[string]*float64{}
		for _, canonical := range order {
			pl := map[string]*float64{}
			pb := map[string]*float64{}
			for _, m := range markets {
				key := itoa(m.id)
				pl[key] = round2toF(merged[mkey{*latestDate, m.id, canonical}])
				if baselineDate != nil {
					pb[key] = round2toF(merged[mkey{*baselineDate, m.id, canonical}])
				} else {
					pb[key] = nil
				}
			}
			latestBy[canonical] = pl
			baselineBy[canonical] = pb
		}
		for _, canonical := range order {
			entry := info[canonical]
			perMarket := pyjson.NewObj()
			for _, m := range markets {
				mid := itoa(m.id)
				newV := latestBy[canonical][mid]
				oldV := baselineBy[canonical][mid]
				perMarket.Keys = append(perMarket.Keys, mid)
				perMarket.M[mid] = pyjson.NewObj(
					"p", round2(newV), "b", round2(oldV), "c", pctChange(newV, oldV))
			}
			itemLatest := map[string]*float64{}
			for _, m := range markets {
				itemLatest[itoa(m.id)] = latestBy[canonical][itoa(m.id)]
			}
			cheapAll := computeCheapest(itemLatest, idStrs(marketIDs))
			var cheapDefault cheapestResult
			{
				dl := map[string]*float64{}
				for _, id := range defaultIDs {
					dl[itoa(id)] = latestBy[canonical][itoa(id)]
				}
				cheapDefault = computeCheapest(dl, idStrs(defaultIDs))
			}
			cheapAllM := make([]any, 0, len(cheapAll.markets))
			for _, mm := range cheapAll.markets {
				cheapAllM = append(cheapAllM, mm)
			}
			cheapDefM := make([]any, 0, len(cheapDefault.markets))
			for _, mm := range cheapDefault.markets {
				cheapDefM = append(cheapDefM, mm)
			}
			weekItems = append(weekItems, pyjson.NewObj(
				"id", entry.id,
				"name", canonical,
				"unit", entry.unit,
				"group", groupOrNull(entry.group),
				"m", perMarket,
				"cheap4", pyjson.NewObj("p", round2(cheapDefault.price), "m", cheapDefM),
				"cheap_all", pyjson.NewObj("p", round2(cheapAll.price), "m", cheapAllM),
			))
		}
	}
	if weekItems == nil {
		weekItems = []any{}
	}
	var latestAny, baselineAny any
	if latestDate != nil {
		latestAny = *latestDate
	}
	if baselineDate != nil {
		baselineAny = *baselineDate
	}
	if err := write(filepath.Join(outDir, "week.json"), pyjson.NewObj(
		"latest", latestAny, "baseline", baselineAny, "items", weekItems)); err != nil {
		return nil, err
	}

	metaMarkets := make([]any, 0, len(markets))
	for _, m := range markets {
		var city any
		if m.city.Valid {
			city = m.city.String
		}
		metaMarkets = append(metaMarkets, pyjson.NewObj(
			"id", m.id, "name", m.name, "city", city, "short", m.short))
	}
	metaCommodities := make([]any, 0, len(order))
	for _, canonical := range order {
		e := info[canonical]
		metaCommodities = append(metaCommodities, pyjson.NewObj(
			"id", e.id, "name", canonical, "unit", e.unit,
			"category", e.category, "group", groupOrNull(e.group)))
	}
	defIDs := make([]any, 0, len(defaultIDs))
	for _, id := range defaultIDs {
		defIDs = append(defIDs, id)
	}
	var updAny, minAny, maxAny any
	if latestDate != nil {
		updAny = *latestDate
		maxAny = *latestDate
	}
	if len(dates) > 0 {
		mn := dates[0]
		for _, d := range dates[1:] {
			if d < mn {
				mn = d
			}
		}
		minAny = mn
	}
	if err := write(filepath.Join(outDir, "meta.json"), pyjson.NewObj(
		"updated", updAny,
		"generated_at", generatedAt(),
		"date_min", minAny,
		"date_max", maxAny,
		"markets", metaMarkets,
		"commodities", metaCommodities,
		"default_markets", defIDs,
		"fx_source", FXSource,
		"price_source", PriceSource,
	)); err != nil {
		return nil, err
	}

	written := []string{"meta.json", "fx.json", "week.json"}
	for _, canonical := range order {
		written = append(written, "series/c"+itoa(info[canonical].id)+".json")
	}
	paths := make([]string, 0, len(written))
	for _, w := range written {
		paths = append(paths, filepath.Join(outDir, w))
	}
	return paths, nil
}

func groupOrNull(g *string) any {
	if g == nil {
		return nil
	}
	return *g
}

// round2toF rounds a *float64 to 2dp (nil stays nil).
func round2toF(v *float64) *float64 {
	if v == nil {
		return nil
	}
	f := parse.RoundN(*v, 2)
	return &f
}

func idStrs(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, itoa(id))
	}
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
