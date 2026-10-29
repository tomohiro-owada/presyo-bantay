package utilpages

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

func testSnapshot(t *testing.T) *utilities.Snapshot {
	t.Helper()
	fix := func(name string) string {
		p := filepath.Join("..", "utilities", "testdata", name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("fixture %s missing", name)
		}
		abs, _ := filepath.Abs(p)
		return abs
	}
	var adjs []utilities.OilAdjustment
	for _, f := range []string{"oilmon_20oct.pdf", "oilmon_13oct.pdf", "oilmon_06oct.pdf", "oilmon_29sep.pdf", "oilmon_22sep.pdf", "oilmon_15sep.pdf"} {
		lines, err := utilities.PDFLines(fix(f))
		if err != nil {
			t.Fatal(err)
		}
		a, err := utilities.ParseOilMonitor(lines)
		if err != nil {
			t.Fatal(err)
		}
		adjs = append(adjs, a)
	}
	pump := func(f string) utilities.PumpSummary {
		t.Helper()
		lines, err := utilities.PDFLines(fix(f))
		if err != nil {
			t.Fatal(err)
		}
		s, err := utilities.ParseNCRPumpSummary(lines)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	lpg := func(f string) utilities.LPGMonth {
		t.Helper()
		lines, err := utilities.PDFLines(fix(f))
		if err != nil {
			t.Fatal(err)
		}
		m, err := utilities.ParseNCRLPG(lines)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	mer := func(f string) utilities.MeralcoRate {
		t.Helper()
		b, err := os.ReadFile(fix(f))
		if err != nil {
			t.Fatal(err)
		}
		m, err := utilities.ParseMeralcoRelease(b)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	fcdaB, err := os.ReadFile(fix("maynilad_fcda_posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	rates, err := utilities.ParseFCDAPosts(fcdaB)
	if err != nil {
		t.Fatal(err)
	}
	maintB, err := os.ReadFile(fix("maynilad_maintenance_posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	maint, err := utilities.ParseMaintenancePosts(maintB, "Las Piñas")
	if err != nil {
		t.Fatal(err)
	}
	return &utilities.Snapshot{
		FetchedAt: "2026-10-26T20:00:00+08:00",
		Fuel: utilities.FuelStatus{
			Adjustments: adjs,
			NCRPump:     []utilities.PumpSummary{pump("ncr_pump_1013.pdf"), pump("ncr_pump_1006.pdf")},
		},
		LPG:        []utilities.LPGMonth{lpg("ncr_lpg_oct.pdf"), lpg("ncr_lpg_sep.pdf")},
		Meralco:    []utilities.MeralcoRate{mer("meralco_2026_10.html"), mer("meralco_2026_09.html")},
		WaterRates: rates,
		Water: &utilities.WaterStatus{
			City: "Las Piñas", CheckedAt: "2026-10-26T20:00:00+08:00",
			Maintenance: maint,
		},
	}
}

func testConfig(site string) Config {
	return Config{
		SitePublic: site,
		BaseURL:    "https://abalol.xyz/presyo-bantay/",
		Now:        func() time.Time { return time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC) },
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing %s", path)
	}
	if len(b) == 0 {
		t.Fatalf("empty %s", path)
	}
	return b
}

func TestUtilpagesWritesAll(t *testing.T) {
	site := t.TempDir()
	snap := testSnapshot(t)
	if err := Run(snap, testConfig(site)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"u/index.html",
		"u/fuel/2026-10-20/index.html", "u/fuel/2026-10-20/post.json", "u/fuel/2026-10-20/fuel.png",
		"u/bills/2026-10/index.html", "u/bills/2026-10/post.json", "u/bills/2026-10/bills.png",
		"data/utilities.json",
	} {
		mustRead(t, filepath.Join(site, p))
	}
	// Water alert page (id from the frozen doc).
	var waters []string
	matches, _ := filepath.Glob(filepath.Join(site, "u", "water", "*", "post.json"))
	if len(matches) != 1 {
		t.Fatalf("water pages = %v", matches)
	}
	waters = matches
	_ = waters
	wdoc := mustRead(t, matches[0])
	var wpost PostDoc
	if err := json.Unmarshal(wdoc, &wpost); err != nil {
		t.Fatal(err)
	}
	if wpost.Kind != "water" || wpost.Check != "pass" || wpost.Text == "" {
		t.Fatalf("water post.json = %+v", wpost)
	}
	mustRead(t, filepath.Join(site, "u", "water", wpost.AsOf, "index.html"))
	mustRead(t, filepath.Join(site, "u", "water", wpost.AsOf, "water.png"))

	// FB == site: frozen text/PNG equal the render pipeline output.
	var fdoc PostDoc
	if err := json.Unmarshal(mustRead(t, filepath.Join(site, "u", "fuel", "2026-10-20", "post.json")), &fdoc); err != nil {
		t.Fatal(err)
	}
	wantText, err := utilities.RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if fdoc.Text != wantText {
		t.Fatal("fuel post.json text != RenderFuelPost")
	}
	img, err := fbchart.RenderFuelWeekly(snap)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := fbchart.EncodeCompact(&buf, img); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), mustRead(t, filepath.Join(site, "u", "fuel", "2026-10-20", "fuel.png"))) {
		t.Fatal("fuel.png != EncodeCompact(RenderFuelWeekly)")
	}

	// Index carries the template chrome + content.
	idx := string(mustRead(t, filepath.Join(site, "u", "index.html")))
	for _, want := range []string{
		"G-VL047YCMK8",
		`<link rel="canonical" href="https://abalol.xyz/presyo-bantay/u/">`,
		`<meta property="og:image" content="https://abalol.xyz/presyo-bantay/u/fuel/2026-10-20/fuel.png">`,
		"data-setlang",
		"Google Analytics",
		"logo-mark",
		"87.90",    // pump table value
		"5,499.30", // family total
		`href="fuel/2026-10-20/"`,
		`href="bills/2026-10/"`,
		`href="water/` + wpost.AsOf + `/"`,
		"Almanza Uno",
	} {
		if !strings.Contains(idx, want) {
			t.Fatalf("index.html missing %q", want)
		}
	}

	// Manifest: latest ids + pinned generated_at.
	var manifest UtilitiesDoc
	if err := json.Unmarshal(mustRead(t, filepath.Join(site, "data", "utilities.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.GeneratedAt != "2026-10-26T20:00:00+08:00" {
		t.Fatalf("generated_at = %q", manifest.GeneratedAt)
	}
	if manifest.Fuel == nil || manifest.Fuel.ID != "2026-10-20" || manifest.Fuel.Path != "u/fuel/2026-10-20/" {
		t.Fatalf("fuel manifest = %+v", manifest.Fuel)
	}
	if manifest.Bills == nil || manifest.Bills.ID != "2026-10" {
		t.Fatalf("bills manifest = %+v", manifest.Bills)
	}
	if manifest.Water == nil || manifest.Water.ID != wpost.AsOf {
		t.Fatalf("water manifest = %+v", manifest.Water)
	}

	// Water page: own wording + advisory link, no copied sentences.
	whtml := string(mustRead(t, filepath.Join(site, "u", "water", wpost.AsOf, "index.html")))
	for _, want := range []string{"maintenance ng pasilidad", "mayniladwater.com.ph/maintenance-activities-for-the-upcoming-week"} {
		if !strings.Contains(whtml, want) {
			t.Fatalf("water page missing %q", want)
		}
	}
	for _, bad := range []string{"no over or under recovery", "granted Maynilad a Foreign"} {
		if strings.Contains(whtml, bad) {
			t.Fatalf("water page copies source text %q", bad)
		}
	}
}

func TestUtilpagesFrozen(t *testing.T) {
	site := t.TempDir()
	snap := testSnapshot(t)
	if err := Run(snap, testConfig(site)); err != nil {
		t.Fatal(err)
	}
	fuelDoc := filepath.Join(site, "u", "fuel", "2026-10-20", "post.json")
	before := mustRead(t, fuelDoc)
	// Corrupt the frozen doc: a second run must NOT rewrite it.
	if err := os.WriteFile(fuelDoc, []byte(`{"text":"x","kind":"fuel","as_of":"2026-10-20","generated_at":"old","check":"pass"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	idxBefore := mustRead(t, filepath.Join(site, "u", "index.html"))
	if err := Run(snap, testConfig(site)); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, fuelDoc); string(got) != `{"text":"x","kind":"fuel","as_of":"2026-10-20","generated_at":"old","check":"pass"}` {
		t.Fatal("frozen post.json was re-rendered without -force")
	}
	// Index still rebuilds (links intact).
	idxAfter := mustRead(t, filepath.Join(site, "u", "index.html"))
	if !strings.Contains(string(idxAfter), "u/fuel/2026-10-20/") {
		t.Fatal("index lost frozen link")
	}
	_ = idxBefore
	_ = before
	// With -force the frozen doc is fixed.
	cfg := testConfig(site)
	cfg.Force = true
	if err := Run(snap, cfg); err != nil {
		t.Fatal(err)
	}
	var fixed PostDoc
	if err := json.Unmarshal(mustRead(t, fuelDoc), &fixed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fixed.Text, "Presyo ng langis") {
		t.Fatal("force run did not re-render frozen post")
	}
}
