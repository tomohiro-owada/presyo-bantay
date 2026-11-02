package grabfare

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func pickupBlock() *PickupIn {
	return &PickupIn{
		Current: "Old Zapote Public Market",
		SinceTs: "20261102-1220",
		Since:   "2026-11-02T12:20:00+08:00",
		History: []PickupEntry{
			{SinceTs: "19700101-0000", Pickup: "Zapote Bridge"},
			{SinceTs: "20261102-1220", Pickup: "Old Zapote Public Market"},
		},
	}
}

func mkPickupDoc(runs []Run) []byte {
	doc := InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Mall", Via: "Alabang-Zapote Rd", Label: "x"},
		Pickup:   pickupBlock(),
		Currency: "PHP",
		Hours:    26,
		Count:    len(runs),
		Runs:     runs,
	}
	data, _ := json.Marshal(doc)
	return data
}

func mkPickupDocWith(p *PickupIn, runs []Run) []byte {
	doc := InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Mall", Via: "Alabang-Zapote Rd", Label: "x"},
		Pickup:   p,
		Currency: "PHP",
		Hours:    26,
		Count:    len(runs),
		Runs:     runs,
	}
	data, _ := json.Marshal(doc)
	return data
}

func pickupRun(tsCompact, capturedRFC string, status string, min, max, std *float64, traffic *TrafficIn) Run {
	r := mkRun(capturedRFC, status, min, max, std, traffic)
	r.Ts = tsCompact
	return r
}

func TestPickupTrendBaseline(t *testing.T) {
	data := mkPickupDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
		pickupRun("20261102-1220", "2026-11-02T12:20:00+08:00", "complete", fptr(290), fptr(350), fptr(310), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:10:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Trend.PreviousHHMM == nil || *out.Trend.PreviousHHMM != "12:20" {
		t.Fatalf("previous hhmm=%v", out.Trend.PreviousHHMM)
	}

	data2 := mkPickupDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	out2 := parseAndBuild(t, data2, now, 2*time.Hour)
	if out2.Trend.Direction != "none" {
		t.Fatalf("trend=%+v", out2.Trend)
	}

	data3 := mkPickupDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
		pickupRun("20261102-1220", "2026-11-02T12:20:00+08:00", "complete", fptr(290), fptr(350), fptr(310), nil),
	})
	now3, _ := time.Parse(time.RFC3339, "2026-11-02T12:30:00+08:00")
	out3 := parseAndBuild(t, data3, now3, 2*time.Hour)
	if out3.Trend.Direction != "none" {
		t.Fatalf("trend=%+v", out3.Trend)
	}
}

func TestPickupHeadlineUnavailable(t *testing.T) {
	data := mkPickupDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
	})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T12:10:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if out.Visible || out.State != "unavailable" {
		t.Fatalf("visible=%v state=%q", out.Visible, out.State)
	}
}

func TestPickupVsYesterday(t *testing.T) {
	runs := []Run{
		pickupRun("20261103-1300", "2026-11-03T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(290), fptr(350), fptr(310), nil),
	}
	data := mkPickupDoc(runs)
	now, _ := time.Parse(time.RFC3339, "2026-11-03T13:05:00+08:00")
	out := parseAndBuild(t, data, now, 3*time.Hour)
	if out.VsYesterday == nil {
		t.Fatal("VsYesterday nil")
	}

	p := &PickupIn{
		Current: "Old Zapote Public Market",
		SinceTs: "20261103-1000",
		Since:   "2026-11-03T10:00:00+08:00",
		History: []PickupEntry{
			{SinceTs: "19700101-0000", Pickup: "Zapote Bridge"},
			{SinceTs: "20261103-1000", Pickup: "Old Zapote Public Market"},
		},
	}
	data2 := mkPickupDocWith(p, runs)
	out2 := parseAndBuild(t, data2, now, 3*time.Hour)
	if out2.VsYesterday != nil {
		t.Fatalf("VsYesterday=%+v", out2.VsYesterday)
	}
}

func TestPickupHourly(t *testing.T) {
	now := time.Date(2026, 11, 2, 14, 0, 0, 0, Manila)
	mk := func(day, hh, mm int, min, max float64, era int) usable {
		tm := time.Date(2026, 11, day, hh, mm, 0, 0, Manila)
		std := min + 3
		return usable{captured: tm, min: min, max: max, standard: &std, era: era}
	}
	us := []usable{
		mk(2, 11, 0, 280, 340, 1),
		mk(2, 12, 0, 290, 350, 1),
		mk(2, 12, 20, 295, 355, 0),
		mk(2, 13, 0, 300, 360, 0),
		mk(1, 10, 0, 270, 330, 1),
	}
	h := buildHourly(us, now)
	if len(h.Today) != 3 || h.Today[0].H != 11 || h.Today[1].H != 12 || h.Today[2].H != 13 {
		t.Fatalf("today hours=%v", h.Today)
	}
	if h.Today[0].Era != 1 || h.Today[1].Era != 0 || h.Today[2].Era != 0 {
		t.Fatalf("today eras=%d,%d,%d", h.Today[0].Era, h.Today[1].Era, h.Today[2].Era)
	}
	if h.Today[1].Min != 295 || h.Today[1].Max != 355 {
		t.Fatalf("hour12=%+v", h.Today[1])
	}
	if len(h.Yesterday) != 0 {
		t.Fatalf("yesterday=%v", h.Yesterday)
	}

	us2 := []usable{
		mk(2, 11, 0, 280, 340, 1),
		mk(2, 12, 20, 295, 355, 0),
		mk(2, 13, 0, 300, 360, 0),
		mk(1, 10, 0, 270, 330, 0),
	}
	h2 := buildHourly(us2, now)
	if len(h2.Yesterday) != 1 || h2.Yesterday[0].Era != 0 {
		t.Fatalf("yesterday2=%v", h2.Yesterday)
	}
}

func TestPickupNoBlock(t *testing.T) {
	data := mkDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	doc, _ := ParseInput(data)
	out := Build(doc, time.Date(2026, 11, 2, 13, 10, 0, 0, Manila), 2*time.Hour)
	if out.Pickup != nil {
		t.Fatal("pickup not nil")
	}
	if out.Route.From != "Zapote Bridge" {
		t.Fatalf("route.from=%q", out.Route.From)
	}
	b := MarshalJSONDoc(out)
	s := string(b)
	if strings.Contains(s, "pickup_note") || strings.Contains(s, "\"pickup\"") {
		t.Fatalf("pickup in json: %s", s)
	}
	if strings.Contains(s, "\"era\"") {
		t.Fatalf("era in json: %s", s)
	}
}

func TestPickupNote(t *testing.T) {
	data := mkPickupDoc([]Run{
		pickupRun("20261102-1220", "2026-11-02T12:20:00+08:00", "complete", fptr(290), fptr(350), fptr(310), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:10:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	noteTl := "Bagong sundo mula 12:20: Old Zapote Public Market. Hindi kasama sa paghahambing ang mga naunang presyo mula sa Zapote Bridge."
	noteEn := "Pickup changed at 12:20 to Old Zapote Public Market. Earlier fares from Zapote Bridge are not compared."
	noteJa := "12:20から乗車地点をOld Zapote Public Marketに変更。以前のZapote Bridgeの料金とは比較していません。"
	if out.Text.Tl.PickupNote != noteTl || out.Text.En.PickupNote != noteEn || out.Text.Ja.PickupNote != noteJa {
		t.Fatalf("notes tl=%q en=%q ja=%q", out.Text.Tl.PickupNote, out.Text.En.PickupNote, out.Text.Ja.PickupNote)
	}
	if out.Pickup == nil || out.Pickup.Name != "Old Zapote Public Market" || out.Pickup.Since != "2026-11-02T12:20:00+08:00" || out.Pickup.SinceHHMM != "12:20" || out.Pickup.Previous != "Zapote Bridge" {
		t.Fatalf("pickup=%+v", out.Pickup)
	}
	if out.Route.From != "Old Zapote Public Market" {
		t.Fatalf("route.from=%q", out.Route.From)
	}

	now2, _ := time.Parse(time.RFC3339, "2026-11-03T13:10:00+08:00")
	out2 := parseAndBuild(t, data, now2, 48*time.Hour)
	if out2.Text.En.PickupNote == "" {
		t.Fatal("note empty on 11-03")
	}

	now3, _ := time.Parse(time.RFC3339, "2026-11-04T13:10:00+08:00")
	out3 := parseAndBuild(t, data, now3, 48*time.Hour)
	if out3.Text.Tl.PickupNote != "" || out3.Text.En.PickupNote != "" || out3.Text.Ja.PickupNote != "" {
		t.Fatalf("note not empty on 11-04: %q", out3.Text.En.PickupNote)
	}

	oneHist := &PickupIn{
		Current: "Old Zapote Public Market",
		SinceTs: "20261102-1220",
		Since:   "2026-11-02T12:20:00+08:00",
		History: []PickupEntry{
			{SinceTs: "20261102-1220", Pickup: "Old Zapote Public Market"},
		},
	}
	dataOne := mkPickupDocWith(oneHist, []Run{
		pickupRun("20261102-1220", "2026-11-02T12:20:00+08:00", "complete", fptr(290), fptr(350), fptr(310), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	outOne := parseAndBuild(t, dataOne, now, 2*time.Hour)
	if outOne.Text.Tl.PickupNote != "" || outOne.Text.En.PickupNote != "" || outOne.Text.Ja.PickupNote != "" {
		t.Fatalf("note not empty with one history: %q", outOne.Text.En.PickupNote)
	}
}

func TestPickupSelectRuns(t *testing.T) {
	data := mkPickupDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	// add maps to era1 run by re-parsing? Use doc mutation.
	doc, _ := ParseInput(data)
	for i := range doc.Runs {
		doc.Runs[i].MapImages = []string{"map1", "map2"}
	}
	now := time.Date(2026, 11, 2, 13, 10, 0, 0, Manila)
	cands := SelectCardRun(doc, now, 3*time.Hour)
	for _, c := range cands {
		if doc.era(c.Ts) > 0 {
			t.Fatalf("era>0 candidate: %s", c.Ts)
		}
	}
	if len(SelectRuns(doc, now, 3*time.Hour, false)) == 0 {
		t.Fatal("no fare candidates")
	}
	// BuildFromRun on era1 ts -> unavailable
	out := BuildFromRun(doc, now, 2*time.Hour, "20261102-1200")
	if out.Visible || out.State != "unavailable" {
		t.Fatalf("visible=%v state=%q", out.Visible, out.State)
	}
}

func TestPickupCaption(t *testing.T) {
	// Build doc with route fields for caption shortening.
	doc := &InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Supermall Alabang", Via: "Alabang-Zapote Road", Label: "x"},
		Pickup:   pickupBlock(),
		Currency: "PHP",
		Hours:    26,
		Runs: []Run{
			pickupRun("20261102-1220", "2026-11-02T12:20:00+08:00", "complete", fptr(290), fptr(350), fptr(310), goodTraffic()),
			pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), goodTraffic()),
		},
	}
	now := time.Date(2026, 11, 2, 13, 10, 0, 0, Manila)
	out := Build(doc, now, 2*time.Hour)
	c := RenderCaption(out)
	noteEn := "Pickup changed at 12:20 to Old Zapote Public Market. Earlier fares from Zapote Bridge are not compared."
	noteTl := "Bagong sundo mula 12:20: Old Zapote Public Market. Hindi kasama sa paghahambing ang mga naunang presyo mula sa Zapote Bridge."
	noteJa := "12:20から乗車地点をOld Zapote Public Marketに変更。以前のZapote Bridgeの料金とは比較していません。"
	for _, note := range []string{noteEn, noteTl, noteJa} {
		if !strings.Contains(c, "📍 "+note) {
			t.Fatalf("caption missing 📍 %s", note)
		}
	}
	if !strings.Contains(c, "Old Zapote Public Market → Festival Mall (Alabang-Zapote Rd)") {
		t.Fatalf("route line missing: %s", c)
	}
	if probs := CheckCaption(c, out); len(probs) != 0 {
		t.Fatalf("problems: %v", probs)
	}
	if len(CheckCaption(c+"\nGreen GSM", out)) == 0 {
		t.Fatal("Green GSM not flagged")
	}
	if len(CheckCaption(c+"\nMove It", out)) == 0 {
		t.Fatal("Move It not flagged")
	}
}

func TestPickupJSONNoForbidden(t *testing.T) {
	data := mkPickupDoc([]Run{
		pickupRun("20261102-1200", "2026-11-02T12:00:00+08:00", "complete", fptr(280), fptr(340), fptr(300), nil),
		pickupRun("20261102-1220", "2026-11-02T12:20:00+08:00", "complete", fptr(290), fptr(350), fptr(310), nil),
		pickupRun("20261102-1300", "2026-11-02T13:00:00+08:00", "complete", fptr(300), fptr(360), fptr(320), nil),
	})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T13:10:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	b := MarshalJSONDoc(out)
	s := strings.ToLower(string(b))
	for _, bad := range []string{"gsm", "moveit", "apps"} {
		if strings.Contains(s, bad) {
			t.Fatalf("json contains %q", bad)
		}
	}
}

func TestCardSubtitle(t *testing.T) {
	r := RouteOut{From: "Old Zapote Public Market", To: "Festival Supermall Alabang", Via: "Alabang-Zapote Road"}
	if got := CardSubtitle(r); got != "Old Zapote Public Market → Festival Mall · Alabang-Zapote Rd" {
		t.Errorf("got %q", got)
	}
	if got := CardSubtitle(RouteOut{}); got != "" {
		t.Errorf("empty got %q", got)
	}
	r2 := RouteOut{From: "A", To: "Festival Supermall Alabang", Via: ""}
	if got := CardSubtitle(r2); got != "A → Festival Mall" {
		t.Errorf("got %q", got)
	}
}
