// Package regions maps every DA market name to exactly one Facebook-post
// region (REGIONS_SPEC.md section 1). The table is explicit: unknown or
// unmapped market names are reported by `presyo regions` and ignored by
// region signals.
package regions

// Market is one mapped market: its exact `markets`-table name plus the
// short display name used in posts and charts.
type Market struct {
	// Name is the exact market name in the markets table.
	Name string
	// Short is the short display name for posts (e.g. "Las Piñas").
	Short string
}

// Region is one posting region.
type Region struct {
	// ID is the region id used on the command line
	// (south|manila|qc|north|east).
	ID string
	// Order is the daily posting order (1-5).
	Order int
	// TitleTL is the Tagalog title ("South Metro").
	TitleTL string
	// TitleEN is the English title ("South Metro").
	TitleEN string
	// TitleJA is the Japanese title ("南部（ラス・ピニャス周辺）").
	TitleJA string
	// Markets lists the region's markets in posting order.
	Markets []Market
}

// Regions is the full region table in posting order.
var Regions = []Region{}

func init() {
	Regions = []Region{
		{
			ID:      "south",
			Order:   1,
			TitleTL: "South Metro",
			TitleEN: "South Metro",
			TitleJA: "南部（ラス・ピニャス周辺）",
			Markets: []Market{
				{"New Las Piñas City Public Market", "Las Piñas"},
				{"Suki Market Moonwalk", "Suki Moonwalk"},
				{"La Huerta Market/Parañaque", "La Huerta"},
				{"Alabang Central Market", "Alabang"},
				{"Pamilihang Lungsod ng Muntinlupa", "Muntinlupa"},
				{"Bicutan Market", "Bicutan"},
				{"Taguig People's Market", "Taguig People's"},
				{"Grace Marketplace", "Grace, Pateros"},
				{"Pateros Market", "Pateros"},
			},
		},
		{
			ID:      "manila",
			Order:   2,
			TitleTL: "Maynila at Pasay",
			TitleEN: "Manila & Pasay",
			TitleJA: "マニラ・パサイ",
			Markets: []Market{
				{"Paco Market", "Paco"},
				{"Quinta Market/Manila", "Quinta"},
				{"Pritil Market/Manila", "Pritil"},
				{"San Andres Market/Manila", "San Andres"},
				{"Trabajo Market", "Trabajo"},
				{"Dagonoy Market", "Dagonoy"},
				{"Blumentritt Market", "Blumentritt"},
				{"Obrero Market", "Obrero"},
				{"Pasay City Market", "Pasay"},
				{"Cartimar Market", "Cartimar"},
			},
		},
		{
			ID:      "qc",
			Order:   3,
			TitleTL: "Quezon City",
			TitleEN: "Quezon City",
			TitleJA: "ケソン市",
			Markets: []Market{
				{"Commonwealth Market/Quezon City", "Commonwealth"},
				{"Muñoz Market/Quezon City", "Muñoz"},
				{"Mega Q-mart/Quezon City", "Mega Q-mart"},
				{"Farmers Market", "Farmers, Cubao"},
				{"Balintawak (Cloverleaf) Market", "Balintawak"},
				{"Murphy Public Market", "Murphy"},
				{"North Diversion Market", "North Diversion"},
				{"Kamuning Public Market", "Kamuning"},
			},
		},
		{
			ID:      "north",
			Order:   4,
			TitleTL: "CAMANAVA",
			TitleEN: "North (CAMANAVA)",
			TitleJA: "北部（カマナバ）",
			Markets: []Market{
				{"Maypajo Public Market/Caloocan", "Maypajo"},
				{"Malabon Central Market", "Malabon Central"},
				{"Hulong Duhat Public Market", "Hulong Duhat"},
				{"Navotas Agora Market", "Navotas Agora"},
				{"KLMV New Karuhatan Market", "Karuhatan"},
				{"New Marulas Public Market/Valenzuela", "Marulas"},
			},
		},
		{
			ID:      "east",
			Order:   5,
			TitleTL: "Silangan",
			TitleEN: "East Metro",
			TitleJA: "東部",
			Markets: []Market{
				{"Mutya ng Pasig Mega Market", "Mutya ng Pasig"},
				{"Pasig City Mega Market", "Pasig Mega"},
				{"Marikina Public Market", "Marikina"},
				{"New Parang Market", "Parang"},
				{"Mandaluyong Public Market", "Mandaluyong"},
				{"Mandaluyong Public Market I", "Mandaluyong I"},
				{"Mandaluyong Public Market II", "Mandaluyong II"},
				{"Agora Public Market/San Juan", "San Juan Agora"},
				{"Guadalupe Public Market/Makati", "Guadalupe"},
				{"Poblacion Public Market", "Poblacion"},
			},
		},
	}
}

// byID indexes Regions by id.
func byID() map[string]*Region {
	m := map[string]*Region{}
	for i := range Regions {
		m[Regions[i].ID] = &Regions[i]
	}
	return m
}

// ByID returns the region with the given id.
func ByID(id string) (*Region, bool) {
	r, ok := byID()[id]
	return r, ok
}

// marketIndex maps exact market names to (region, market) pairs.
func marketIndex() map[string]struct {
	region *Region
	market *Market
} {
	m := map[string]struct {
		region *Region
		market *Market
	}{}
	for i := range Regions {
		for j := range Regions[i].Markets {
			m[Regions[i].Markets[j].Name] = struct {
				region *Region
				market *Market
			}{&Regions[i], &Regions[i].Markets[j]}
		}
	}
	return m
}

// RegionOf returns the region and short display name for an exact market
// name, or ok=false when the market is unmapped.
func RegionOf(market string) (region *Region, short string, ok bool) {
	e, ok := marketIndex()[market]
	if !ok {
		return nil, "", false
	}
	return e.region, e.market.Short, true
}

// ShortOf returns the short display name for an exact market name, or the
// full name when the market is unmapped.
func ShortOf(market string) string {
	if _, s, ok := RegionOf(market); ok {
		return s
	}
	return market
}

// IDs returns the region ids in posting order.
func IDs() []string {
	out := make([]string, 0, len(Regions))
	for i := range Regions {
		out = append(out, Regions[i].ID)
	}
	return out
}
