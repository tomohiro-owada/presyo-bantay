package grabfare

import (
	"math"
	"sort"
	"time"
)

// BikeFreshWindow is the freshness window for a Move It bike reading.
const BikeFreshWindow = 90 * time.Minute

// BikeIn mirrors the optional top-level "bike" block.
type BikeIn struct {
	App     string        `json:"app"`
	Service string        `json:"service"`
	Latest  *BikeReading  `json:"latest"`
	Series  []BikeReading `json:"series"`
}

// BikeReading is one Move It bike quote reading.
type BikeReading struct {
	Ts         string   `json:"ts"`
	CapturedAt string   `json:"captured_at"`
	PriceMin   float64  `json:"price_min"`
	PriceMax   float64  `json:"price_max"`
	EtaMin     *float64 `json:"eta_min"`
	Ok         bool     `json:"ok"`
}

// BikeOut is the public bike object.
type BikeOut struct {
	Label      string     `json:"label"`
	Fresh      bool       `json:"fresh"`
	Min        *float64   `json:"min"`
	Max        *float64   `json:"max"`
	AsOf       *string    `json:"as_of"`
	AsOfHHMM   *string    `json:"as_of_hhmm"`
	AgeMinutes *int       `json:"age_minutes"`
	EtaMin     *int       `json:"eta_min"`
	Hourly     *HourlyOut `json:"hourly"`
	Text       BikeTexts  `json:"text"`
}

// BikeTexts holds tl/en/ja bike strings.
type BikeTexts struct {
	Tl BikeText `json:"tl"`
	En BikeText `json:"en"`
	Ja BikeText `json:"ja"`
}

// BikeText holds one language's bike strings.
type BikeText struct {
	Headline string `json:"headline"`
	Asof     string `json:"asof"`
}

func parseBikeCaptured(r BikeReading) (time.Time, bool) {
	if r.CapturedAt != "" {
		if t, err := time.Parse(time.RFC3339, r.CapturedAt); err == nil {
			return t, true
		}
	}
	if t, err := time.Parse("20060102-1504", r.Ts); err == nil {
		return t.In(Manila), true
	}
	return time.Time{}, false
}

func validBikeReading(doc *InputDoc, r BikeReading, now time.Time) (time.Time, bool) {
	if !r.Ok {
		return time.Time{}, false
	}
	if !(r.PriceMin > 0 && r.PriceMax >= r.PriceMin && r.PriceMax <= 3000 && r.PriceMax <= 3*r.PriceMin) {
		return time.Time{}, false
	}
	t, ok := parseBikeCaptured(r)
	if !ok {
		return time.Time{}, false
	}
	if t.After(now.Add(10 * time.Minute)) {
		return time.Time{}, false
	}
	if doc != nil && doc.era(r.Ts) != 0 {
		return time.Time{}, false
	}
	return t, true
}

func collectBikeReadings(doc *InputDoc) []BikeReading {
	if doc == nil || doc.Bike == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []BikeReading
	add := func(r BikeReading) {
		if seen[r.Ts] {
			return
		}
		seen[r.Ts] = true
		out = append(out, r)
	}
	if doc.Bike.Latest != nil {
		add(*doc.Bike.Latest)
	}
	for _, r := range doc.Bike.Series {
		add(r)
	}
	return out
}

func buildBike(doc *InputDoc, now time.Time) *BikeOut {
	if doc == nil || doc.Bike == nil {
		return nil
	}
	raws := collectBikeReadings(doc)
	type validR struct {
		r BikeReading
		t time.Time
	}
	var valids []validR
	for _, r := range raws {
		if t, ok := validBikeReading(doc, r, now); ok {
			valids = append(valids, validR{r: r, t: t})
		}
	}
	if len(valids) == 0 {
		return nil
	}
	sort.Slice(valids, func(i, j int) bool { return valids[i].t.After(valids[j].t) })

	// The row needs a valid reading today on the current pickup; the newest
	// of today's readings is carried forward when nothing newer arrives.
	today := now.In(Manila).Format("2006-01-02")
	var newest *validR
	for i := range valids {
		if valids[i].t.In(Manila).Format("2006-01-02") == today {
			newest = &valids[i]
			break
		}
	}
	if newest == nil {
		return nil
	}
	fresh := now.Sub(newest.t) <= BikeFreshWindow

	age := now.Sub(newest.t)
	ageMin := int(math.Floor(age.Minutes()))
	if ageMin < 0 {
		ageMin = 0
	}

	var usables []usable
	for _, v := range valids {
		usables = append(usables, usable{captured: v.t, min: v.r.PriceMin, max: v.r.PriceMax, era: 0})
	}
	hourly := buildHourly(usables, now)
	carryForwardToday(hourly, now)

	label := "Motor taxi (Move It)"
	hhmm := newest.t.In(Manila).Format("15:04")
	headline := label + ": " + FormatRange(newest.r.PriceMin, newest.r.PriceMax)
	var asofTl, asofEn, asofJa string
	if fresh {
		asofTl = "Ayon sa Move It app noong " + hhmm + " (oras sa Maynila)"
		asofEn = "As of " + hhmm + " (Manila time) · Move It app quote"
		asofJa = hhmm + "時点のMove Itアプリ表示（マニラ時間）"
	} else {
		asofTl = "Huling Move It quote: " + hhmm + " (oras sa Maynila)"
		asofEn = "Last Move It quote: " + hhmm + " (Manila time)"
		asofJa = "最終Move It表示: " + hhmm + "（マニラ時間）"
	}
	texts := BikeTexts{
		Tl: BikeText{Headline: headline, Asof: asofTl},
		En: BikeText{Headline: headline, Asof: asofEn},
		Ja: BikeText{Headline: headline, Asof: asofJa},
	}

	out := &BikeOut{
		Label:      label,
		Fresh:      fresh,
		AsOf:       strPtr(rfc3339Manila(newest.t)),
		AsOfHHMM:   strPtr(hhmm),
		AgeMinutes: intPtr(ageMin),
		Hourly:     hourly,
		Text:       texts,
	}
	out.Min = f64Ptr(newest.r.PriceMin)
	out.Max = f64Ptr(newest.r.PriceMax)
	if fresh && newest.r.EtaMin != nil {
		out.EtaMin = intPtr(int(math.Round(*newest.r.EtaMin)))
	}
	return out
}

// carryForwardToday fills today's missing hours, from the first reading's hour
// up to the current Manila hour, with the previous known value. Filled points
// are marked Carried. Yesterday is left untouched.
func carryForwardToday(h *HourlyOut, now time.Time) {
	if h == nil || len(h.Today) == 0 {
		return
	}
	sort.Slice(h.Today, func(i, j int) bool { return h.Today[i].H < h.Today[j].H })
	last := now.In(Manila).Hour()
	byHour := map[int]HourPoint{}
	for _, p := range h.Today {
		byHour[p.H] = p
	}
	first := h.Today[0].H
	if first > last {
		return
	}
	var out []HourPoint
	var prev HourPoint
	for hr := first; hr <= last; hr++ {
		if p, ok := byHour[hr]; ok {
			out = append(out, p)
			prev = p
			continue
		}
		out = append(out, HourPoint{H: hr, Min: prev.Min, Max: prev.Max, Carried: true})
	}
	for _, p := range h.Today {
		if p.H > last {
			out = append(out, p)
		}
	}
	h.Today = out
}
