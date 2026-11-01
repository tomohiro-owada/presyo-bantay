package grabfare

import (
	"strings"
	"testing"
	"time"
)

func goodTraffic() *TrafficIn {
	return &TrafficIn{Level: "light", Summary: "Traffic is mostly free flowing along the road.",
		GreenPct: fptr(90), YellowPct: fptr(8), RedPct: fptr(2)}
}

func TestTrafficSane(t *testing.T) {
	mut := func(f func(*TrafficIn)) *TrafficIn { x := *goodTraffic(); f(&x); return &x }
	cases := []struct {
		name   string
		in     *TrafficIn
		ok     bool
		reason string
	}{
		{"good", goodTraffic(), true, ""},
		{"nil", nil, false, "empty"},
		{"blank summary", mut(func(x *TrafficIn) { x.Summary = "  " }), false, "empty"},
		{"notification overlay", mut(func(x *TrafficIn) {
			x.Summary = "The supplied screenshot is a Grab food-order notification card overlaid on the map"
		}), false, "overlay"},
		{"unusable", mut(func(x *TrafficIn) { x.Summary = "Unable to read the route line" }), false, "unusable"},
		{"bad level", mut(func(x *TrafficIn) { x.Level = "gridlock" }), false, "level"},
		{"missing pct", mut(func(x *TrafficIn) { x.RedPct = nil }), false, "pct"},
		{"sum off", mut(func(x *TrafficIn) { x.GreenPct = fptr(50) }), false, "pct"},
		{"pct range", mut(func(x *TrafficIn) { x.RedPct = fptr(-2); x.GreenPct = fptr(94) }), false, "pct"},
	}
	for _, c := range cases {
		ok, why := TrafficSane(c.in)
		if ok != c.ok || why != c.reason {
			t.Errorf("%s: got (%v,%q) want (%v,%q)", c.name, ok, why, c.ok, c.reason)
		}
	}
}

func TestBuildDropsInsaneTraffic(t *testing.T) {
	bad := goodTraffic()
	bad.Summary = "notification overlay hides the map"
	r := mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(300), fptr(360), fptr(305), bad)
	o := parseAndBuild(t, mkDoc([]Run{r}), testNow(), 2*time.Hour)
	if !o.Visible || o.Traffic != nil || o.Text.En.Traffic != "" {
		t.Errorf("insane traffic must be omitted: visible=%v traffic=%v", o.Visible, o.Traffic)
	}
}

func TestBuildFromRunUsesChosenRunAsNewest(t *testing.T) {
	newest := mkRun("2026-11-02T08:00:00+08:00", "fare_only", fptr(400), fptr(450), nil, nil)
	chosen := mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(300), fptr(360), fptr(305), goodTraffic())
	older := mkRun("2026-11-02T06:00:00+08:00", "fare_only", fptr(290), fptr(350), nil, nil)
	doc, _ := ParseInput(mkDoc([]Run{newest, chosen, older}))
	o := BuildFromRun(doc, testNow(), 3*time.Hour, chosen.Ts)
	if !o.Visible || o.AsOfHHMM == nil || *o.AsOfHHMM != "07:00" || o.Fare.Min != 300 {
		t.Fatalf("wrong run: %+v", o.AsOfHHMM)
	}
	if o.Trend.Direction != "up" || o.Trend.PreviousHHMM == nil || *o.Trend.PreviousHHMM != "06:00" {
		t.Errorf("trend must compare to the previous run, got %+v", o.Trend)
	}
	if got := BuildFromRun(doc, testNow(), 3*time.Hour, "nope"); got.Visible {
		t.Error("unknown ts must be hidden")
	}
}

func TestSelectCardRun(t *testing.T) {
	overlay := goodTraffic()
	overlay.Summary = "a notification card is overlaid on the map"
	withMaps := func(r Run, kinds ...string) Run { r.MapImages = kinds; return r }
	r1 := withMaps(mkRun("2026-11-02T08:00:00+08:00", "fare_only", fptr(300), fptr(360), nil, nil))            // no maps
	r2 := withMaps(mkRun("2026-11-02T07:30:00+08:00", "complete", fptr(300), fptr(360), nil, overlay), "map1") // overlay
	r3 := withMaps(mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(300), fptr(360), nil, goodTraffic()), "map1", "map2")
	r4 := withMaps(mkRun("2026-11-02T03:00:00+08:00", "complete", fptr(300), fptr(360), nil, goodTraffic()), "map1") // too old
	doc, _ := ParseInput(mkDoc([]Run{r1, r2, r3, r4}))
	got := SelectCardRun(doc, testNow(), 3*time.Hour)
	if len(got) != 1 || got[0].Ts != r3.Ts {
		t.Fatalf("candidates = %+v", got)
	}
	if fare := SelectRuns(doc, testNow(), 3*time.Hour, false); len(fare) != 3 || fare[0].Ts != r1.Ts {
		t.Errorf("fare-only candidates = %d", len(fare))
	}
}

func cardOutput(t *testing.T, traffic *TrafficIn, withYesterday bool) *Output {
	t.Helper()
	runs := []Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(320), fptr(385), fptr(325), traffic),
		mkRun("2026-11-02T07:00:00+08:00", "fare_only", fptr(308), fptr(370), fptr(309), nil),
	}
	if withYesterday {
		runs = append(runs, mkRun("2026-11-01T08:00:00+08:00", "fare_only", fptr(300), fptr(360), nil, nil))
	}
	return parseAndBuild(t, mkDoc(runs), testNow(), 3*time.Hour)
}

func TestCaptionOKAndShape(t *testing.T) {
	o := cardOutput(t, goodTraffic(), true)
	c := RenderCaption(o)
	if p := CheckCaption(c, o); len(p) != 0 {
		t.Fatalf("problems: %v\n%s", p, c)
	}
	if n := strings.Count(c, Separator); n != 3 {
		t.Errorf("separators = %d", n)
	}
	for _, want := range []string{"🚕", "🇬🇧", "🇯🇵", "🚦", "kumpara kahapon", "vs yesterday", "昨日", "Grab Standard ₱325", "Map: Grab app screenshot", "Hindi opisyal", "Not an official fare", "公式運賃ではありません"} {
		if !strings.Contains(c, want) {
			t.Errorf("caption lacks %q", want)
		}
	}
	// no traffic: no 🚦 line anywhere, still ok
	o2 := cardOutput(t, nil, false)
	c2 := RenderCaption(o2)
	if strings.Contains(c2, "🚦") {
		t.Error("traffic line without traffic")
	}
	if p := CheckCaption(c2, o2); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
}

func TestCaptionTrendAlwaysPresent(t *testing.T) {
	o := parseAndBuild(t, mkDoc([]Run{mkRun("2026-11-02T08:00:00+08:00", "fare_only", fptr(300), fptr(360), nil, nil)}), testNow(), 3*time.Hour)
	c := RenderCaption(o)
	for _, want := range []string{"Wala pang naunang reading", "No earlier reading yet", "以前の記録はまだありません"} {
		if !strings.Contains(c, want) {
			t.Errorf("no-trend caption lacks %q", want)
		}
	}
	if p := CheckCaption(c, o); len(p) != 0 {
		t.Errorf("problems: %v", p)
	}
}

func TestCheckCaptionCatchesProblems(t *testing.T) {
	o := cardOutput(t, goodTraffic(), true)
	c := RenderCaption(o)
	bad := map[string]string{
		"tampered peso":    strings.Replace(c, "₱320", "₱299", 1),
		"tampered pct":     strings.Replace(c, "2% red", "37% red", 1),
		"tampered time":    strings.Replace(c, "08:00", "09:45", 1),
		"missing disclaim": strings.ReplaceAll(c, "Not an official fare", "Official fare"),
		"no trend":         strings.ReplaceAll(strings.ReplaceAll(c, "▲", ""), "▼", ""),
		"ssh":              c + "\nssh box",
		"home path":        c + "\n/home/x",
		"email":            c + "\nme@example.com",
		"other url":        c + "\nhttps://abalol.blog/x",
		"dollar":           c + "\n$5",
		"block missing":    strings.Replace(c, Separator, "", 1),
	}
	for name, text := range bad {
		if len(CheckCaption(text, o)) == 0 {
			t.Errorf("%s: not flagged", name)
		}
	}
	// traffic mismatch both ways
	noTraffic := strings.ReplaceAll(c, "🚦", "•")
	if len(CheckCaption(noTraffic, o)) == 0 {
		t.Error("missing traffic line not flagged")
	}
	o2 := cardOutput(t, nil, false)
	if len(CheckCaption(RenderCaption(o)+"", o2)) == 0 {
		t.Error("traffic line without traffic data not flagged")
	}
}
