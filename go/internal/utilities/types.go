// Package utilities is the household-utilities prototype (fuel / LPG /
// Meralco / Maynilad): fetch + parse official public sources, render
// sample Facebook-style post images + trilingual captions locally
// (dry run). No Facebook code, no posting, no cron/VPS changes.
//
// Every number in text/charts comes from parsed data or a documented
// derivation; missing fields omit their line.
package utilities

// Range is a signed pesos/L adjustment range. Lo is the smaller
// magnitude, Hi the larger magnitude (same sign); a single value has
// Lo == Hi.
type Range struct {
	Lo float64 `json:"lo"`
	Hi float64 `json:"hi"`
}

// OilAdjustment is one DOE Oil Monitor weekly price movement.
type OilAdjustment struct {
	AsOf          string `json:"as_of"`
	EffectiveFrom string `json:"effective_from"`
	EffectiveTo   string `json:"effective_to"`
	MovementNo    int    `json:"movement_no"`
	Gasoline      *Range `json:"gasoline,omitempty"`
	Diesel        *Range `json:"diesel,omitempty"`
	Kerosene      *Range `json:"kerosene,omitempty"`
	SourceURL     string `json:"source_url,omitempty"`
}

// PumpPrice is one prevailing NCR retail price row.
type PumpPrice struct {
	Product string  `json:"product"`
	Low     float64 `json:"low"`
	High    float64 `json:"high"`
	Common  float64 `json:"common"`
}

// CityPrice is one per-city product row ("#N/A" common → nil).
type CityPrice struct {
	Product string   `json:"product"`
	Low     float64  `json:"low"`
	High    float64  `json:"high"`
	Common  *float64 `json:"common,omitempty"`
}

// CityPump is the per-city table block for one monitored city.
type CityPump struct {
	City   string      `json:"city"`
	Prices []CityPrice `json:"prices"`
}

// PumpSummary is the NCR prevailing retail price table for one week.
type PumpSummary struct {
	WeekFrom  string      `json:"week_from"`
	WeekTo    string      `json:"week_to"`
	Prices    []PumpPrice `json:"prices"`
	Cities    []CityPump  `json:"cities,omitempty"`
	SourceURL string      `json:"source_url,omitempty"`
}

// LPGMonth is the NCR LPG monthly monitoring snapshot.
type LPGMonth struct {
	PeriodFrom string   `json:"period_from"`
	PeriodTo   string   `json:"period_to"`
	Low        float64  `json:"low"`
	High       float64  `json:"high"`
	Common     float64  `json:"common"`
	AutoLPG    float64  `json:"auto_lpg"`
	City       string   `json:"city"`
	CityLow    float64  `json:"city_low"`
	CityHigh   float64  `json:"city_high"`
	CityCommon *float64 `json:"city_common,omitempty"`
	SourceURL  string   `json:"source_url,omitempty"`
}

// MeralcoRate is one Meralco monthly rate release.
type MeralcoRate struct {
	Month      string  `json:"month"`
	Announced  string  `json:"announced"`
	Title      string  `json:"title"`
	Adjustment float64 `json:"adjustment"`
	Rate       float64 `json:"rate"`
	PrevRate   float64 `json:"prev_rate"`
	Impact200  int     `json:"impact_200"`
	SourceURL  string  `json:"source_url,omitempty"`
}

// Interruption is one Maynilad water-interruption table row.
type Interruption struct {
	Kind      string `json:"kind"`
	City      string `json:"city"`
	Barangays string `json:"barangays"`
	Area      string `json:"area"`
	From      string `json:"from"`
	FromRaw   string `json:"from_raw,omitempty"`
	To        string `json:"to,omitempty"`
	ToRaw     string `json:"to_raw,omitempty"`
	Time      string `json:"time"`
	Reason    string `json:"reason"`
}

// Maintenance is one Maynilad maintenance-activity table row.
type Maintenance struct {
	PostDate  string `json:"post_date"`
	PostURL   string `json:"post_url"`
	City      string `json:"city"`
	Barangays string `json:"barangays"`
	Schedule  string `json:"schedule"`
	Activity  string `json:"activity"`
	Location  string `json:"location"`
}

// WaterStatus is the Maynilad status for one city.
type WaterStatus struct {
	City        string         `json:"city"`
	CheckedAt   string         `json:"checked_at"`
	Scheduled   []Interruption `json:"scheduled"`
	Emergency   []Interruption `json:"emergency"`
	Rotational  []Interruption `json:"rotational"`
	Maintenance []Maintenance  `json:"maintenance"`
}

// FuelStatus groups the DOE fuel datasets (newest first).
type FuelStatus struct {
	Adjustments []OilAdjustment `json:"adjustments"`
	NCRPump     []PumpSummary   `json:"ncr_pump"`
}

// Source records one fetch attempt behind a snapshot field.
type Source struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	FetchedAt  string `json:"fetched_at"`
	HTTPStatus int    `json:"http_status"`
	Bytes      int    `json:"bytes"`
	SHA256     string `json:"sha256"`
	Err        string `json:"err,omitempty"`
}

// Snapshot is the offline bundle behind both post kinds.
type Snapshot struct {
	FetchedAt        string            `json:"fetched_at"`
	Fuel             FuelStatus        `json:"fuel"`
	LPG              []LPGMonth        `json:"lpg"`
	Meralco          []MeralcoRate     `json:"meralco"`
	Water            *WaterStatus      `json:"water,omitempty"`
	WaterRates       []WaterRate       `json:"water_rates,omitempty"`
	Alerts           []WaterAlert      `json:"alerts,omitempty"`
	SectionFetchedAt map[string]string `json:"section_fetched_at,omitempty"`
	Sources          []Source          `json:"sources"`
	Errors           []string          `json:"errors"`
}
