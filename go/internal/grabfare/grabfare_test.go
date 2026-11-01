package grabfare

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fptr(f float64) *float64 { v := f; return &v }
func sptr(s string) *string   { v := s; return &v }

func testNow() time.Time {
	t, _ := time.Parse(time.RFC3339, "2026-11-02T08:30:00+08:00")
	return t
}

func mkRun(captured, status string, min, max, std *float64, traffic *TrafficIn) Run {
	var fare *Fare
	if status != "pending" || min != nil || max != nil || std != nil {
		mt := &MinMaxOpt{Min: min, Max: max}
		var so *PriceOpt
		if std != nil {
			so = &PriceOpt{Price: std}
		}
		// For pending with no fares, leave fare nil.
		if min == nil && max == nil && std == nil && status == "pending" && traffic == nil {
			fare = nil
		} else {
			fare = &Fare{Currency: "PHP", Standard: so, MeteredTaxi: mt}
		}
		if min == nil && max == nil && std == nil && status != "pending" {
			// Keep fare object but unusable (tests null numbers).
			fare = &Fare{Currency: "PHP", Standard: so, MeteredTaxi: mt}
		}
	}
	return Run{Ts: captured, CapturedAt: captured, Status: status, Fare: fare, Traffic: traffic}
}

func mkDoc(runs []Run) []byte {
	doc := InputDoc{
		Route:    RouteIn{From: "Zapote Bridge", To: "Festival Mall", Via: "Alabang-Zapote Rd", Label: "x"},
		Currency: "PHP",
		Hours:    26,
		Count:    len(runs),
		Runs:     runs,
	}
	data, _ := json.Marshal(doc)
	return data
}

func parseAndBuild(t *testing.T, data []byte, now time.Time, stale time.Duration) *Output {
	t.Helper()
	doc, err := ParseInput(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Build(doc, now, stale)
}

func TestFreshComplete(t *testing.T) {
	traffic := &TrafficIn{Level: "heavy", Summary: "Heavy traffic", RedPct: fptr(35), GreenPct: fptr(40), YellowPct: fptr(25),
		Segments: []Segment{{Level: "heavy", RedSections: []string{"Alabang-Zapote Rd", "Zapote Rd"}, YellowSections: []string{"Madrigal Ave"}}}}
	data := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), fptr(300), traffic),
		mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(270), fptr(326), fptr(290), traffic),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if !out.Visible || out.State != "fresh" {
		t.Fatalf("visible=%v state=%q", out.Visible, out.State)
	}
	if out.Basis != "metered_taxi" {
		t.Fatalf("basis=%q", out.Basis)
	}
	if out.Fare == nil || out.Fare.Min != 282 || out.Fare.Max != 338 {
		t.Fatalf("fare=%+v", out.Fare)
	}
	if out.Traffic == nil || out.Traffic.Level != "heavy" {
		t.Fatalf("traffic=%+v", out.Traffic)
	}
	if len(out.Traffic.WorstSections) == 0 || len(out.Traffic.WorstSections) > 3 {
		t.Fatalf("worst=%v", out.Traffic.WorstSections)
	}
	if out.Trend.Direction != "up" {
		t.Fatalf("trend=%+v", out.Trend)
	}
	if out.Trend.DeltaPesos == nil || math.Abs(*out.Trend.DeltaPesos-12) > 0.001 {
		t.Fatalf("delta=%v", out.Trend.DeltaPesos)
	}
	if *out.AsOfHHMM != "08:00" {
		t.Fatalf("hhmm=%v", *out.AsOfHHMM)
	}
	if !strings.Contains(out.Text.En.Headline, "\u20b1282\u2013338") {
		t.Fatalf("headline=%q", out.Text.En.Headline)
	}
	if !strings.Contains(out.Text.En.Traffic, "35%") {
		t.Fatalf("traffic text=%q", out.Text.En.Traffic)
	}
	if out.ValidUntil == nil {
		t.Fatal("valid_until nil")
	}
	snip := RenderSnippet(out)
	if !strings.Contains(snip, `class="grab-fare"`) || !strings.Contains(snip, `data-valid-until=`) {
		t.Fatalf("snippet missing section: %s", snip[:200])
	}
	if strings.Count(snip, `class="gf-lang"`) != 3 {
		t.Fatalf("want 3 lang blocks: %s", snip)
	}
}

func TestTrendDirections(t *testing.T) {
	mk := func(curMin, curMax, prevMin, prevMax float64) TrendOut {
		data := mkDoc([]Run{
			mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(curMin), fptr(curMax), nil, nil),
			mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(prevMin), fptr(prevMax), nil, nil),
		})
		out := parseAndBuild(t, data, testNow(), 2*time.Hour)
		return out.Trend
	}
	up := mk(282, 338, 270, 326)
	if up.Direction != "up" {
		t.Fatalf("up=%+v", up)
	}
	down := mk(270, 326, 282, 338)
	if down.Direction != "down" {
		t.Fatalf("down=%+v", down)
	}
	flat := mk(300, 300, 300.4, 300.4)
	if flat.Direction != "flat" {
		t.Fatalf("flat=%+v", flat)
	}
	// none: single usable run
	data := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(300), fptr(300), nil, nil),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if out.Trend.Direction != "none" {
		t.Fatalf("none=%+v", out.Trend)
	}
	if !strings.Contains(out.Text.En.Trend, "No earlier reading yet") {
		t.Fatalf("none text=%q", out.Text.En.Trend)
	}
	// flat text has no peso amounts
	data2 := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(300), fptr(300), nil, nil),
		mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(300), fptr(300), nil, nil),
	})
	out2 := parseAndBuild(t, data2, testNow(), 2*time.Hour)
	if out2.Trend.Direction != "flat" {
		t.Fatalf("flat2=%+v", out2.Trend)
	}
	if strings.Contains(out2.Text.En.Trend, "\u20b1") {
		t.Fatalf("flat should carry no amounts: %q", out2.Text.En.Trend)
	}
}

func TestFallbackPending(t *testing.T) {
	data := mkDoc([]Run{
		{Ts: "x", CapturedAt: "2026-11-02T08:00:00+08:00", Status: "pending"},
		mkRun("2026-11-02T07:30:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
	})
	now, _ := time.Parse(time.RFC3339, "2026-11-02T08:00:00+08:00")
	out := parseAndBuild(t, data, now, 2*time.Hour)
	if !out.Visible || out.State != "fallback" || !out.Fallback {
		t.Fatalf("visible=%v state=%q fallback=%v", out.Visible, out.State, out.Fallback)
	}
	if *out.AsOfHHMM != "07:30" {
		t.Fatalf("asof=%v (must not be relabelled)", *out.AsOfHHMM)
	}
	if !strings.Contains(out.Text.En.Asof, "latest analysed reading") {
		t.Fatalf("asof text=%q", out.Text.En.Asof)
	}
}

func TestStaleHidden(t *testing.T) {
	data := mkDoc([]Run{
		mkRun("2026-11-02T05:00:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if out.Visible || out.State != "stale" {
		t.Fatalf("visible=%v state=%q", out.Visible, out.State)
	}
	if RenderSnippet(out) != "<!-- grab-fare: hidden (stale) -->\n" {
		t.Fatalf("snippet=%q", RenderSnippet(out))
	}
}

func TestOnlyPendingUnavailable(t *testing.T) {
	data := mkDoc([]Run{
		{Ts: "a", CapturedAt: "2026-11-02T08:00:00+08:00", Status: "pending"},
		{Ts: "b", CapturedAt: "2026-11-02T07:00:00+08:00", Status: "pending"},
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if out.Visible || out.State != "unavailable" {
		t.Fatalf("visible=%v state=%q", out.Visible, out.State)
	}
	if RenderSnippet(out) != "<!-- grab-fare: hidden (unavailable) -->\n" {
		t.Fatalf("snippet=%q", RenderSnippet(out))
	}
	if out.ValidUntil != nil {
		t.Fatalf("valid_until should be null: %v", *out.ValidUntil)
	}
}

func TestFareOnlyTrafficOmitted(t *testing.T) {
	data := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "fare_only", fptr(282), fptr(338), fptr(300), nil),
		mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(270), fptr(326), nil,
			&TrafficIn{Level: "heavy", Summary: "x"}),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if !out.Visible {
		t.Fatal("should be visible")
	}
	if out.Traffic != nil {
		t.Fatalf("traffic must be omitted (same run only): %+v", out.Traffic)
	}
	if out.Text.En.Traffic != "" || out.Text.Tl.Traffic != "" || out.Text.Ja.Traffic != "" {
		t.Fatalf("traffic texts must be empty: %+v", out.Text.En)
	}
}

func TestStandardBasis(t *testing.T) {
	fare := &Fare{Currency: "PHP", Standard: &PriceOpt{Price: fptr(250)}, MeteredTaxi: &MinMaxOpt{}}
	data := mkDoc([]Run{
		{Ts: "x", CapturedAt: "2026-11-02T08:00:00+08:00", Status: "complete", Fare: fare},
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if !out.Visible || out.Basis != "standard" {
		t.Fatalf("visible=%v basis=%q", out.Visible, out.Basis)
	}
	if out.Fare.Min != 250 || out.Fare.Max != 250 {
		t.Fatalf("fare=%+v", out.Fare)
	}
	if !strings.Contains(out.Text.En.Headline, "\u20b1250") || strings.Contains(out.Text.En.Headline, "\u2013") {
		t.Fatalf("headline=%q", out.Text.En.Headline)
	}
	// min-only uses it for both
	fare2 := &Fare{Currency: "PHP", MeteredTaxi: &MinMaxOpt{Min: fptr(282)}}
	data2 := mkDoc([]Run{
		{Ts: "x", CapturedAt: "2026-11-02T08:00:00+08:00", Status: "complete", Fare: fare2},
	})
	out2 := parseAndBuild(t, data2, testNow(), 2*time.Hour)
	if out2.Basis != "metered_taxi" || out2.Fare.Min != 282 || out2.Fare.Max != 282 {
		t.Fatalf("min-only=%+v basis=%q", out2.Fare, out2.Basis)
	}
}

func TestVsYesterday(t *testing.T) {
	data := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
		mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(270), fptr(326), nil, nil),
		mkRun("2026-11-01T08:10:00+08:00", "complete", fptr(250), fptr(290), nil, nil),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if out.VsYesterday == nil {
		t.Fatal("vs_yesterday nil")
	}
	if out.VsYesterday.Direction != "up" {
		t.Fatalf("vy=%+v", out.VsYesterday)
	}
	// far outside the window -> null
	data2 := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
		mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(270), fptr(326), nil, nil),
		mkRun("2026-11-01T02:00:00+08:00", "complete", fptr(250), fptr(290), nil, nil),
	})
	out2 := parseAndBuild(t, data2, testNow(), 2*time.Hour)
	if out2.VsYesterday != nil {
		t.Fatalf("vy should be null: %+v", out2.VsYesterday)
	}
}

func TestNullNumbers(t *testing.T) {
	// fare present but no usable numbers -> unavailable
	fare := &Fare{Currency: "PHP", Standard: &PriceOpt{}, MeteredTaxi: &MinMaxOpt{}}
	data := mkDoc([]Run{
		{Ts: "x", CapturedAt: "2026-11-02T08:00:00+08:00", Status: "complete", Fare: fare},
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	if out.Visible || out.State != "unavailable" {
		t.Fatalf("visible=%v state=%q", out.Visible, out.State)
	}
	// null traffic pcts omit the % clause
	traffic := &TrafficIn{Level: "heavy", Summary: "Heavy"}
	data2 := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), nil, traffic),
	})
	out2 := parseAndBuild(t, data2, testNow(), 2*time.Hour)
	if strings.Contains(out2.Text.En.Traffic, "%") {
		t.Fatalf("traffic text should omit %%: %q", out2.Text.En.Traffic)
	}
	if out2.Traffic.RedPct != nil {
		t.Fatalf("redpct should be null")
	}
}

func TestThousandsSeparator(t *testing.T) {
	if FormatRange(1234, 5678) != "\u20b11,234\u20135,678" {
		t.Fatalf("range=%q", FormatRange(1234, 5678))
	}
	if FormatRange(282, 282) != "\u20b1282" {
		t.Fatalf("single=%q", FormatRange(282, 282))
	}
}

func TestHTMLEscaping(t *testing.T) {
	doc := InputDoc{
		Route:    RouteIn{From: "<script>alert(1)</script>", To: "A&B", Via: `"><img src=x>`, Label: "x"},
		Currency: "PHP",
		Runs: []Run{
			mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
		},
	}
	data, _ := json.Marshal(doc)
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	snip := RenderSnippet(out)
	if strings.Contains(snip, "<script>alert") || strings.Contains(snip, `"><img`) {
		t.Fatalf("unescaped snippet: %s", snip)
	}
	if !strings.Contains(snip, "&lt;script&gt;") || !strings.Contains(snip, "A&amp;B") {
		t.Fatalf("missing escapes: %s", snip)
	}
	prev := RenderPreview(out, snip)
	if strings.Contains(prev, "<script>alert(1)") {
		t.Fatalf("unescaped preview")
	}
}

// TestNumbersBind checks every peso amount and % in each text derives
// from JSON values (rounded whole pesos / rounded pcts).
func TestNumbersBind(t *testing.T) {
	cases := []struct {
		name string
		runs []Run
	}{
		{"fresh", []Run{
			mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), fptr(300),
				&TrafficIn{Level: "heavy", Summary: "Heavy", RedPct: fptr(35), GreenPct: fptr(40), YellowPct: fptr(25)}),
			mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(270), fptr(326), fptr(290), nil),
			mkRun("2026-11-01T08:10:00+08:00", "complete", fptr(1250), fptr(1380), nil, nil),
		}},
		{"standard", []Run{
			{Ts: "x", CapturedAt: "2026-11-02T08:00:00+08:00", Status: "complete",
				Fare: &Fare{Currency: "PHP", Standard: &PriceOpt{Price: fptr(1250)}}},
			{Ts: "y", CapturedAt: "2026-11-02T07:00:00+08:00", Status: "complete",
				Fare: &Fare{Currency: "PHP", Standard: &PriceOpt{Price: fptr(1200)}}},
		}},
	}
	pesoRe := regexp.MustCompile("\u20b1([0-9,]+)")
	rangeRe := regexp.MustCompile("\u2013([0-9,]+)")
	pctRe := regexp.MustCompile(`([0-9]+)%`)
	hhmmRe := regexp.MustCompile(`([0-9]{2}:[0-9]{2})`)
	for _, c := range cases {
		out := parseAndBuild(t, mkDoc(c.runs), testNow(), 2*time.Hour)
		raw, _ := json.Marshal(out)
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		// Allowed peso ints: rounded fare min/max/standard/delta.
		allowedPeso := map[int64]bool{}
		addPeso := func(f float64) { allowedPeso[int64(math.Round(f))] = true }
		if out.Fare != nil {
			addPeso(out.Fare.Min)
			addPeso(out.Fare.Max)
			if out.Fare.Standard != nil {
				addPeso(*out.Fare.Standard)
			}
		}
		for _, tr := range []*TrendOut{&out.Trend, out.VsYesterday} {
			if tr == nil {
				continue
			}
			if tr.DeltaPesos != nil {
				allowedPeso[int64(math.Round(math.Abs(*tr.DeltaPesos)))] = true
			}
		}
		allowedPct := map[int64]bool{}
		addPct := func(f float64) { allowedPct[int64(math.Round(math.Abs(f)))] = true }
		if out.Traffic != nil {
			for _, p := range []*float64{out.Traffic.GreenPct, out.Traffic.YellowPct, out.Traffic.RedPct} {
				if p != nil {
					addPct(*p)
				}
			}
		}
		for _, tr := range []*TrendOut{&out.Trend, out.VsYesterday} {
			if tr == nil || tr.DeltaPct == nil {
				continue
			}
			addPct(*tr.DeltaPct)
		}
		allowedHHMM := map[string]bool{}
		if out.AsOfHHMM != nil {
			allowedHHMM[*out.AsOfHHMM] = true
		}
		for _, tr := range []*TrendOut{&out.Trend, out.VsYesterday} {
			if tr != nil && tr.PreviousHHMM != nil {
				allowedHHMM[*tr.PreviousHHMM] = true
			}
		}
		for lang, lt := range map[string]LangText{"tl": out.Text.Tl, "en": out.Text.En, "ja": out.Text.Ja} {
			joined := lt.Headline + " " + lt.Asof + " " + lt.Traffic + " " + lt.Trend + " " + lt.Note + " " + lt.Title
			for _, m := range pesoRe.FindAllStringSubmatch(joined, -1) {
				n, _ := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
				if !allowedPeso[n] {
					t.Fatalf("%s: peso %q not in JSON (allowed %v) text=%q", c.name+" "+lang, m[0], allowedPeso, joined)
				}
			}
			for _, m := range rangeRe.FindAllStringSubmatch(joined, -1) {
				n, _ := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
				if !allowedPeso[n] {
					t.Fatalf("%s: range second %q not in JSON (allowed %v) text=%q", c.name+" "+lang, m[0], allowedPeso, joined)
				}
			}
			for _, m := range pctRe.FindAllStringSubmatch(joined, -1) {
				n, _ := strconv.ParseInt(m[1], 10, 64)
				if !allowedPct[n] {
					t.Fatalf("%s: pct %q not in JSON (allowed %v) text=%q", c.name+" "+lang, m[0], allowedPct, joined)
				}
			}
			for _, m := range hhmmRe.FindAllStringSubmatch(joined, -1) {
				if !allowedHHMM[m[1]] {
					t.Fatalf("%s: hhmm %q not in JSON (allowed %v) text=%q", c.name+" "+lang, m[1], allowedHHMM, joined)
				}
			}
		}
		_ = raw
	}
}

func TestFetchBearer(t *testing.T) {
	var gotAuth, gotHours string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHours = r.URL.Query().Get("hours")
		if r.URL.Path != "/v1/grab/recent" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"route":{},"runs":[]}`)
	}))
	defer srv.Close()
	data, err := FetchGET(srv.URL, 26, "sekrit", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sekrit" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotHours != "26" {
		t.Fatalf("hours=%q", gotHours)
	}
	if len(data) == 0 {
		t.Fatal("empty")
	}
}

func TestExpirePaths(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-11-02T08:30:00+08:00")
	expired := `{"valid_until":"2026-11-02T07:00:00+08:00","route":{"from":"a","to":"b","via":"c"},"currency":"PHP"}`
	st := ExpireForFailure([]byte(expired), now)
	if st == nil || st.Visible || st.State != "stale" {
		t.Fatalf("expired should rewrite: %+v", st)
	}
	valid := `{"valid_until":"2026-11-02T10:00:00+08:00","route":{"from":"a","to":"b","via":"c"},"currency":"PHP"}`
	if st := ExpireForFailure([]byte(valid), now); st != nil {
		t.Fatalf("valid must be left untouched: %+v", st)
	}
	if st := ExpireForFailure([]byte(`{bad`), now); st != nil {
		t.Fatalf("malformed must be untouched")
	}
}

func TestScanNoSecrets(t *testing.T) {
	data := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), fptr(300),
			&TrafficIn{Level: "heavy", Summary: "Heavy", RedPct: fptr(35)}),
		mkRun("2026-11-02T07:00:00+08:00", "complete", fptr(270), fptr(326), nil, nil),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	files := map[string]string{
		"grab-fare.json": string(MarshalJSONDoc(out)),
		"grab-fare.html": RenderSnippet(out),
		"preview":        RenderPreview(out, RenderSnippet(out)),
	}
	allowed := []string{}
	for name, s := range files {
		for _, bad := range []string{"http", "ssh", "/home", "@"} {
			idx := strings.Index(s, bad)
			if idx < 0 {
				continue
			}
			ok := false
			for _, a := range allowed {
				if strings.Contains(s[max(0, idx-40):min(len(s), idx+40)], a) {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("%s contains %q: ...%s...", name, bad, s[max(0, idx-20):min(len(s), idx+20)])
			}
		}
	}
}

func TestPreviewToggleAndCSSVars(t *testing.T) {
	data := mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
	})
	out := parseAndBuild(t, data, testNow(), 2*time.Hour)
	snip := RenderSnippet(out)
	if !strings.Contains(snip, "hidden") {
		t.Fatalf("tl-only visible needs hidden on others")
	}
	if !strings.Contains(snip, "var(--card") {
		t.Fatalf("snippet CSS must use vars with fallbacks")
	}
	prev := RenderPreview(out, snip)
	for _, v := range []string{"--bg:#faf8f2", "--card:#fff", "--ink:#1c1a15", "--muted:#5c5648", "--line:#e2dccb", "--accent:#0b5d3b", "--up:#b3261e", "--down:#1e7e34"} {
		if !strings.Contains(prev, v) {
			t.Fatalf("preview missing %q", v)
		}
	}
	if !strings.Contains(prev, "data-gflang") || !strings.Contains(prev, ".gf-lang") {
		t.Fatalf("preview missing toggle")
	}
	if strings.Contains(strings.ToLower(prev), "http") {
		t.Fatalf("preview must make no external requests")
	}
}

func TestStaleAfterFlagHonored(t *testing.T) {
	data := mkDoc([]Run{
		mkRun("2026-11-02T05:30:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
	})
	// 3h window keeps a 3h-old reading fresh.
	out := parseAndBuild(t, data, testNow(), 3*time.Hour)
	if !out.Visible {
		t.Fatalf("3h window should be fresh: %+v", out)
	}
}

func TestWriteScanTmp(t *testing.T) {
	dir := t.TempDir()
	out := parseAndBuild(t, mkDoc([]Run{
		mkRun("2026-11-02T08:00:00+08:00", "complete", fptr(282), fptr(338), nil, nil),
	}), testNow(), 2*time.Hour)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]string{
		"grab-fare.json":         string(MarshalJSONDoc(out)),
		"grab-fare.html":         RenderSnippet(out),
		"grab-fare-preview.html": RenderPreview(out, RenderSnippet(out)),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImplausibleFareIsUnusable(t *testing.T) {
	cases := [][2]float64{{0, 300}, {400, 300}, {30800, 33800}, {100, 900}}
	for _, c := range cases {
		lo, hi := c[0], c[1]
		r := Run{Fare: &Fare{MeteredTaxi: &MinMaxOpt{Min: &lo, Max: &hi}}}
		if _, _, _, _, ok := usableHeadline(r); ok {
			t.Errorf("range %v-%v should be unusable", lo, hi)
		}
	}
	lo, hi := 308.0, 370.0
	r := Run{Fare: &Fare{MeteredTaxi: &MinMaxOpt{Min: &lo, Max: &hi}}}
	if _, _, _, _, ok := usableHeadline(r); !ok {
		t.Error("308-370 should be usable")
	}
}
