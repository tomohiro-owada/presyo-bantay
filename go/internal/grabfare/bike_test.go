package grabfare

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func bikeReading() BikeReading {
	return BikeReading{
		Ts:         "20261102-1229",
		CapturedAt: "2026-11-02T12:29:00+08:00",
		PriceMin:   147,
		PriceMax:   147,
		EtaMin:     fptr(5),
		Ok:         true,
	}
}

func bikeBlock() *BikeIn {
	r := bikeReading()
	return &BikeIn{
		App:     "moveit",
		Service: "Standard bike taxi",
		Latest:  &r,
		Series:  []BikeReading{r},
	}
}

func mkBikeDoc(runs []Run) []byte {
	return mkBikeDocWithBike(bikeBlock(), runs)
}

func mkBikeDocWithBike(bike *BikeIn, runs []Run) []byte {
	doc := InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Mall", Via: "Alabang-Zapote Rd", Label: "x"},
		Pickup:   pickupBlock(),
		Bike:     bike,
		Currency: "PHP",
		Hours:    26,
		Count:    len(runs),
		Runs:     runs,
	}
	data, _ := json.Marshal(doc)
	return data
}

func mkBikeDocNoPickup(bike *BikeIn, runs []Run) []byte {
	doc := InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Mall", Via: "Alabang-Zapote Rd", Label: "x"},
		Bike:     bike,
		Currency: "PHP",
		Hours:    26,
		Count:    len(runs),
		Runs:     runs,
	}
	data, _ := json.Marshal(doc)
	return data
}

func bikeRun() Run {
	return pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil)
}

func TestBikeFreshReading(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike == nil {
		t.Fatal("bike nil")
	}
	b := out.Bike
	if !b.Fresh {
		t.Fatalf("fresh=%v", b.Fresh)
	}
	if b.Min == nil || *b.Min != 147 || b.Max == nil || *b.Max != 147 {
		t.Fatalf("min/max=%v/%v", b.Min, b.Max)
	}
	if b.AsOfHHMM == nil || *b.AsOfHHMM != "12:29" {
		t.Fatalf("hhmm=%v", b.AsOfHHMM)
	}
	if b.AgeMinutes == nil || *b.AgeMinutes != 48 {
		t.Fatalf("age=%v", b.AgeMinutes)
	}
	if b.EtaMin == nil || *b.EtaMin != 5 {
		t.Fatalf("eta=%v", b.EtaMin)
	}
	if b.Label != "Motor taxi (Move It)" {
		t.Fatalf("label=%q", b.Label)
	}
	if b.Text.Tl.Headline != "Motor taxi (Move It): ₱147" {
		t.Fatalf("tl headline=%q", b.Text.Tl.Headline)
	}
	if b.Text.Tl.Asof != "Ayon sa Move It app noong 12:29 (oras sa Maynila)" {
		t.Fatalf("tl asof=%q", b.Text.Tl.Asof)
	}
	if b.Text.En.Asof != "As of 12:29 (Manila time) · Move It app quote" {
		t.Fatalf("en asof=%q", b.Text.En.Asof)
	}
	if b.Text.Ja.Asof != "12:29時点のMove Itアプリ表示（マニラ時間）" {
		t.Fatalf("ja asof=%q", b.Text.Ja.Asof)
	}
	if b.Hourly == nil || len(b.Hourly.Today) != 2 || b.Hourly.Today[0].H != 12 || b.Hourly.Today[0].Min != 147 || b.Hourly.Today[0].Max != 147 || b.Hourly.Today[0].Era != 0 || b.Hourly.Today[0].Carried {
		t.Fatalf("hourly=%+v", b.Hourly)
	}
	// hour 13 (now) is carried forward from the 12:29 reading
	if p := b.Hourly.Today[1]; p.H != 13 || !p.Carried || p.Min != 147 || p.Max != 147 {
		t.Fatalf("carried point=%+v", p)
	}
}

func TestBikeStaleReading(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T14:30:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike == nil {
		t.Fatal("bike nil")
	}
	b := out.Bike
	if b.Fresh {
		t.Fatalf("fresh=%v", b.Fresh)
	}
	// a quote older than 90 minutes is carried forward, not hidden or dashed
	if b.Min == nil || *b.Min != 147 || b.Max == nil || *b.Max != 147 {
		t.Fatalf("min/max=%v/%v", b.Min, b.Max)
	}
	if b.EtaMin != nil {
		t.Fatalf("eta=%v", b.EtaMin)
	}
	if b.Text.Tl.Headline != "Motor taxi (Move It): ₱147" {
		t.Fatalf("headline=%q", b.Text.Tl.Headline)
	}
	if b.Text.Tl.Asof != "Huling Move It quote: 12:29 (oras sa Maynila)" {
		t.Fatalf("tl asof=%q", b.Text.Tl.Asof)
	}
	if b.Text.En.Asof != "Last Move It quote: 12:29 (Manila time)" {
		t.Fatalf("en asof=%q", b.Text.En.Asof)
	}
	if b.Text.Ja.Asof != "最終Move It表示: 12:29（マニラ時間）" {
		t.Fatalf("ja asof=%q", b.Text.Ja.Asof)
	}
	for _, txt := range []string{b.Text.Tl.Headline, b.Text.Tl.Asof, b.Text.En.Asof, b.Text.Ja.Asof} {
		if strings.Contains(txt, "—") || strings.Contains(txt, "Walang bagong") {
			t.Fatalf("stale text must not be alarming: %q", txt)
		}
	}
	// hours 12..14 filled, only 12 is a real reading
	if b.Hourly == nil || len(b.Hourly.Today) != 3 {
		t.Fatalf("hourly=%+v", b.Hourly)
	}
	for i, p := range b.Hourly.Today {
		if p.H != 12+i || p.Min != 147 || p.Carried != (i > 0) {
			t.Fatalf("point %d=%+v", i, p)
		}
	}
}

func TestBikeFreshBoundary(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now90, _ := time.Parse(time.RFC3339, "2026-11-02T13:59:00+08:00")
	out90 := parseAndBuild(t, data, now90, 3*time.Hour)
	if out90.Bike == nil || !out90.Bike.Fresh {
		t.Fatalf("90min should be fresh: %+v", out90.Bike)
	}
	now91, _ := time.Parse(time.RFC3339, "2026-11-02T14:00:00+08:00")
	out91 := parseAndBuild(t, data, now91, 3*time.Hour)
	if out91.Bike == nil || out91.Bike.Fresh {
		t.Fatalf("91min should be stale: %+v", out91.Bike)
	}
}

func TestBikeNoValidReadings(t *testing.T) {
	invalid := BikeReading{Ts: "20261102-1229", CapturedAt: "2026-11-02T12:29:00+08:00", PriceMin: 147, PriceMax: 147, EtaMin: fptr(5), Ok: false}
	block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &invalid, Series: []BikeReading{invalid}}
	data := mkBikeDocWithBike(block, []Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike != nil {
		t.Fatalf("bike should be nil: %+v", out.Bike)
	}
}

func TestBikeYesterdayOnly(t *testing.T) {
	r := bikeReading()
	r.Ts = "20261101-1229"
	r.CapturedAt = "2026-11-01T12:29:00+08:00"
	block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &r, Series: []BikeReading{r}}
	data := mkBikeDocNoPickup(block, []Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	// no valid reading today on the current pickup -> the row is hidden
	if out.Bike != nil {
		t.Fatalf("yesterday-only reading should hide the row: %+v", out.Bike)
	}
}

func TestBikeOlderThanYesterday(t *testing.T) {
	r := bikeReading()
	r.Ts = "20261031-1229"
	r.CapturedAt = "2026-10-31T12:29:00+08:00"
	block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &r, Series: []BikeReading{r}}
	data := mkBikeDocNoPickup(block, []Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike != nil {
		t.Fatalf("bike should be nil: %+v", out.Bike)
	}
}

func TestBikePreChangeEraDropped(t *testing.T) {
	old := bikeReading()
	old.Ts = "20261102-1100"
	old.CapturedAt = "2026-11-02T11:00:00+08:00"
	block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &old, Series: []BikeReading{old, bikeReading()}}
	// put valid newest first in series too
	block.Series = []BikeReading{bikeReading(), old}
	data := mkBikeDocWithBike(block, []Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike == nil || out.Bike.AsOfHHMM == nil || *out.Bike.AsOfHHMM != "12:29" {
		t.Fatalf("newest should be 12:29: %+v", out.Bike)
	}
	if out.Bike.Hourly == nil {
		t.Fatal("hourly nil")
	}
	for _, hp := range out.Bike.Hourly.Today {
		if hp.H == 11 {
			t.Fatalf("pre-change reading not dropped: %+v", out.Bike.Hourly.Today)
		}
	}
}

func TestBikeInvalidReadingsDropped(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	invalids := []BikeReading{
		{Ts: "20261102-1200", CapturedAt: "2026-11-02T12:00:00+08:00", PriceMin: 147, PriceMax: 147, EtaMin: fptr(5), Ok: false},
		{Ts: "20261102-1201", CapturedAt: "2026-11-02T12:01:00+08:00", PriceMin: 0, PriceMax: 147, EtaMin: fptr(5), Ok: true},
		{Ts: "20261102-1202", CapturedAt: "2026-11-02T12:02:00+08:00", PriceMin: 200, PriceMax: 147, EtaMin: fptr(5), Ok: true},
		{Ts: "20261102-1203", CapturedAt: "2026-11-02T12:03:00+08:00", PriceMin: 147, PriceMax: 3001, EtaMin: fptr(5), Ok: true},
		{Ts: "20261102-1204", CapturedAt: "2026-11-02T12:04:00+08:00", PriceMin: 100, PriceMax: 400, EtaMin: fptr(5), Ok: true},
	}
	for _, inv := range invalids {
		block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &inv, Series: []BikeReading{inv}}
		data := mkBikeDocNoPickup(block, []Run{bikeRun()})
		out := parseAndBuild(t, data, now, 2*time.Hour)
		if out.Bike != nil {
			t.Fatalf("invalid reading should yield nil bike: %+v", inv)
		}
	}
	// mixed: valid plus all invalids -> still fresh 147
	series := []BikeReading{bikeReading()}
	for _, inv := range invalids {
		series = append(series, inv)
	}
	mixedBlock := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &series[0], Series: series}
	data := mkBikeDocNoPickup(mixedBlock, []Run{bikeRun()})
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike == nil || !out.Bike.Fresh || out.Bike.Min == nil || *out.Bike.Min != 147 {
		t.Fatalf("mixed bike wrong: %+v", out.Bike)
	}
	if out.Bike.Hourly == nil || len(out.Bike.Hourly.Today) != 2 || out.Bike.Hourly.Today[0].Carried || !out.Bike.Hourly.Today[1].Carried {
		t.Fatalf("invalid readings not dropped from hourly: %+v", out.Bike.Hourly)
	}
}

func TestBikeFutureReadingDropped(t *testing.T) {
	future := bikeReading()
	future.Ts = "20261102-1332"
	future.CapturedAt = "2026-11-02T13:32:00+08:00"
	block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &future, Series: []BikeReading{future}}
	data := mkBikeDocNoPickup(block, []Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike != nil {
		t.Fatalf("future reading should be dropped: %+v", out.Bike)
	}
}

func TestBikeNoBlock(t *testing.T) {
	doc := InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Mall", Via: "Alabang-Zapote Rd", Label: "x"},
		Pickup:   pickupBlock(),
		Currency: "PHP",
		Hours:    26,
		Count:    1,
		Runs:     []Run{bikeRun()},
	}
	data, _ := json.Marshal(doc)
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:10:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike != nil {
		t.Fatalf("bike should be nil: %+v", out.Bike)
	}
	b := MarshalJSONDoc(out)
	if strings.Contains(string(b), "\"bike\"") {
		t.Fatalf("bike key present: %s", b)
	}
}

func TestBikeHiddenOutputsNoBike(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	// stale hidden output
	nowStale, _ := time.Parse(time.RFC3339, "2026-11-02T16:00:00+08:00")
	outStale := parseAndBuild(t, data, nowStale, 2*time.Hour)
	if outStale.Visible {
		t.Fatal("expected hidden")
	}
	if outStale.Bike != nil {
		t.Fatalf("stale hidden bike not nil: %+v", outStale.Bike)
	}
	// BuildFromRun hidden/unavailable
	doc, _ := ParseInput(data)
	outBF := BuildFromRun(doc, nowStale, 2*time.Hour, "20261102-1200")
	if outBF.Bike != nil {
		t.Fatalf("BuildFromRun hidden bike not nil: %+v", outBF.Bike)
	}
}

func TestBikeCaptionFresh(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	c := RenderCaption(out)
	for _, want := range []string{
		"🏍️ Motor taxi (Move It): ₱147 · Ayon sa Move It app noong 12:29",
		"🏍️ Motor taxi (Move It): ₱147 · As of 12:29 · Move It app quote",
		"🏍️ Motor taxi (Move It): ₱147 · 12:29時点 · Move Itアプリ表示",
		" · Bike fare: Move It app quote",
	} {
		if !strings.Contains(c, want) {
			t.Fatalf("caption missing %q:\n%s", want, c)
		}
	}
	if probs := CheckCaption(c, out); len(probs) != 0 {
		t.Fatalf("problems: %v", probs)
	}
}

func TestBikeCaptionStale(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T14:30:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	c := RenderCaption(out)
	for _, want := range []string{
		"🏍️ Motor taxi (Move It): ₱147 · Huling Move It quote: 12:29",
		"🏍️ Motor taxi (Move It): ₱147 · Last Move It quote: 12:29",
		"🏍️ Motor taxi (Move It): ₱147 · 最終Move It表示: 12:29",
		"Bike fare: Move It app quote",
	} {
		if !strings.Contains(c, want) {
			t.Fatalf("stale caption missing %q:\n%s", want, c)
		}
	}
	if strings.Contains(c, "—") || strings.Contains(c, "Walang bagong") {
		t.Fatalf("stale caption must carry the price forward:\n%s", c)
	}
	if probs := CheckCaption(c, out); len(probs) != 0 {
		t.Fatalf("problems: %v", probs)
	}
}

func TestBikeCheckCaptionFlags(t *testing.T) {
	staleData := mkBikeDoc([]Run{bikeRun()})
	staleNow, _ := time.Parse(time.RFC3339, "2026-11-02T14:30:00+08:00")
	staleOut := parseAndBuild(t, staleData, staleNow, 2*time.Hour)
	staleCaption := RenderCaption(staleOut)
	if probs := CheckCaption(staleCaption, staleOut); len(probs) != 0 {
		t.Fatalf("carried-forward caption must pass: %v", probs)
	}
	if len(CheckCaption(strings.ReplaceAll(staleCaption, "Move It", ""), staleOut)) == 0 {
		t.Fatal("missing Move It not flagged for carried-forward bike")
	}
	noBike := parseAndBuild(t, mkBikeDocWithBike(nil, []Run{bikeRun()}), staleNow, 2*time.Hour)
	if noBike.Bike != nil {
		t.Fatalf("no bike block must give no bike: %+v", noBike.Bike)
	}
	if len(CheckCaption(RenderCaption(noBike)+"\nMove It", noBike)) == 0 {
		t.Fatal("Move It not flagged when there is no bike row")
	}

	freshData := mkBikeDoc([]Run{bikeRun()})
	freshNow, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	freshOut := parseAndBuild(t, freshData, freshNow, 2*time.Hour)
	freshCaption := RenderCaption(freshOut)
	noMoveIt := strings.ReplaceAll(freshCaption, "Move It", "")
	if len(CheckCaption(noMoveIt, freshOut)) == 0 {
		t.Fatal("missing Move It not flagged")
	}
	for _, bad := range []string{"gsm", "Green GSM", "moveit", "GSM", "MoveIt"} {
		if len(CheckCaption(freshCaption+"\n"+bad, freshOut)) == 0 {
			t.Fatalf("%q not flagged", bad)
		}
	}
}

func TestBikeMarshalledNoForbidden(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:17:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	b := MarshalJSONDoc(out)
	s := strings.ToLower(string(b))
	for _, bad := range []string{"gsm", "raw_text", "error"} {
		if strings.Contains(s, bad) {
			t.Fatalf("json contains %q", bad)
		}
	}
}

func TestBikeCarryForwardRealisticDay(t *testing.T) {
	mk := func(ts string, p float64, ok bool) BikeReading {
		return BikeReading{Ts: "20261102-" + ts, CapturedAt: "2026-11-02T" + ts[:2] + ":" + ts[2:] + ":00+08:00", PriceMin: p, PriceMax: p, Ok: ok}
	}
	series := []BikeReading{
		mk("1224", 0, false),
		mk("1229", 147, true),
		mk("1304", 0, false),
		mk("1404", 150, true),
		mk("1504", 147, true),
	}
	block := &BikeIn{App: "moveit", Service: "Standard bike taxi", Latest: &series[4], Series: series}
	data := mkBikeDocWithBike(block, []Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T16:12:00+08:00")
	out := parseAndBuild(t, data, now, 4*time.Hour)
	b := out.Bike
	if b == nil || b.Min == nil || *b.Min != 147 || b.AsOfHHMM == nil || *b.AsOfHHMM != "15:04" {
		t.Fatalf("bike=%+v", b)
	}
	if !b.Fresh {
		t.Fatalf("15:04 at 16:12 is 68 min old, fresh")
	}
	type want struct {
		h       int
		v       float64
		carried bool
	}
	wants := []want{{12, 147, false}, {13, 147, true}, {14, 150, false}, {15, 147, false}, {16, 147, true}}
	if b.Hourly == nil || len(b.Hourly.Today) != len(wants) {
		t.Fatalf("hourly=%+v", b.Hourly)
	}
	for i, w := range wants {
		p := b.Hourly.Today[i]
		if p.H != w.h || p.Min != w.v || p.Max != w.v || p.Carried != w.carried || p.Era != 0 {
			t.Fatalf("point %d=%+v want %+v", i, p, w)
		}
	}
	if len(b.Hourly.Yesterday) != 0 {
		t.Fatalf("yesterday must stay untouched: %+v", b.Hourly.Yesterday)
	}
}

func TestBikeCarryForwardNeverBeforeFirstReading(t *testing.T) {
	data := mkBikeDoc([]Run{bikeRun()})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T12:40:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Bike == nil || out.Bike.Hourly == nil || len(out.Bike.Hourly.Today) != 1 || out.Bike.Hourly.Today[0].Carried {
		t.Fatalf("no carry expected within the reading's hour: %+v", out.Bike)
	}
}

func TestCarryForwardTodayHelper(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-11-02T18:05:00+08:00")
	h := &HourlyOut{Today: []HourPoint{{H: 9, Min: 140, Max: 150}, {H: 12, Min: 160, Max: 170}}, Yesterday: []HourPoint{{H: 3, Min: 1, Max: 1}}}
	carryForwardToday(h, now)
	if len(h.Today) != 10 || len(h.Yesterday) != 1 {
		t.Fatalf("today=%d yesterday=%d", len(h.Today), len(h.Yesterday))
	}
	if p := h.Today[1]; p.H != 10 || !p.Carried || p.Min != 140 || p.Max != 150 {
		t.Fatalf("p=%+v", p)
	}
	if p := h.Today[9]; p.H != 18 || !p.Carried || p.Min != 160 {
		t.Fatalf("p=%+v", p)
	}
	carryForwardToday(nil, now)
	carryForwardToday(&HourlyOut{}, now)
}
