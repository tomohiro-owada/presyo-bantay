package daypages

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbregion"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/regions"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// fixtureDB builds a DB where the first four regions have 30 days of
// history (as-of -15%, a clean deal) and east has nothing.
func fixtureDB(t *testing.T, asOf string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "day.db")
	db, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	asOfT, _ := time.Parse("2006-01-02", asOf)
	res, err := db.Exec("INSERT INTO commodities (name, category, unit) VALUES ('Tilapia', 'fish', 'per kg')")
	if err != nil {
		t.Fatal(err)
	}
	commID, _ := res.LastInsertId()
	for i := 0; i < 4; i++ {
		mname := regions.Regions[i].Markets[0].Name
		res, err := db.Exec("INSERT INTO markets (name) VALUES (?)", mname)
		if err != nil {
			t.Fatal(err)
		}
		mid, _ := res.LastInsertId()
		for ago := 29; ago >= 0; ago-- {
			d := asOfT.AddDate(0, 0, -ago).Format("2006-01-02")
			v := 100.0
			if ago == 0 {
				v = 85
			}
			if _, err := db.Exec(
				"INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
				d, mid, commID, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

func openRO(t *testing.T, path string) *sql.DB {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+abs+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testConfig(site string, dates ...string) Config {
	return Config{
		SitePublic: site,
		Dates:      dates,
		BaseURL:    "https://abalol.xyz/presyo-bantay/",
		Now:        func() time.Time { return time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC) },
	}
}

func readDay(t *testing.T, site, date string) DayDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(site, "d", date, "day.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc DayDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestRenderDayJSON checks day.json shape: 5 regions in order, texts
// split per language, footer without the site link, no nested parens.
func TestRenderDayJSON(t *testing.T) {
	db := openRO(t, fixtureDB(t, "2026-10-23"))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	doc := readDay(t, site, "2026-10-23")
	if doc.Date != "2026-10-23" {
		t.Errorf("date = %q", doc.Date)
	}
	if len(doc.Regions) != 5 {
		t.Fatalf("regions = %d, want 5", len(doc.Regions))
	}
	wantIDs := []string{"south", "manila", "qc", "north", "east"}
	for i, id := range wantIDs {
		if doc.Regions[i].ID != id {
			t.Errorf("regions[%d].id = %q, want %q", i, doc.Regions[i].ID, id)
		}
	}
	// First four regions published, east has no data.
	for i := 0; i < 4; i++ {
		r := doc.Regions[i]
		if !r.HasData || r.Check != "pass" {
			t.Errorf("regions[%d]: has_data=%v check=%q", i, r.HasData, r.Check)
		}
		if r.Image == "" || r.ImageBytes == 0 {
			t.Errorf("regions[%d]: missing image", i)
		}
		if _, err := os.Stat(filepath.Join(site, "d", "2026-10-23", r.Image)); err != nil {
			t.Errorf("regions[%d]: image missing: %v", i, err)
		}
		for _, lang := range []string{"tl", "en", "ja"} {
			var text, summary string
			switch lang {
			case "en":
				text = r.Text.EN
				summary = r.Summary.EN
			case "ja":
				text = r.Text.JA
				summary = r.Summary.JA
			default:
				text = r.Text.TL
				summary = r.Summary.TL
			}
			if text == "" {
				t.Errorf("regions[%d].text.%s empty", i, lang)
			}
			for _, ln := range strings.Split(text, "\n") {
				if fbregion.HasNestedParens(ln) {
					t.Errorf("regions[%d].text.%s nested parens: %q", i, lang, ln)
				}
			}
			// Summary is the structured top deal only: no emoji, no
			// newlines, single item.
			if summary == "" {
				t.Errorf("regions[%d].summary.%s empty", i, lang)
			}
			if strings.Contains(summary, "✅") || strings.Contains(summary, "⚠️") ||
				strings.Contains(summary, "\n") || strings.Contains(summary, "🇬🇧") || strings.Contains(summary, "🇯🇵") {
				t.Errorf("regions[%d].summary.%s must be top deal only, no emoji: %q", i, lang, summary)
			}
		}
	}
	east := doc.Regions[4]
	if east.HasData || east.Image != "" {
		t.Errorf("east should have no data: %+v", east)
	}
	if east.Summary != (LangText{}) {
		t.Errorf("east summary must be empty: %+v", east.Summary)
	}
	// Footer: no site link line.
	for _, f := range []string{doc.Footer.TL, doc.Footer.EN, doc.Footer.JA} {
		if f == "" || strings.Contains(f, fbregion.Link) {
			t.Errorf("bad footer %q", f)
		}
	}
}

// TestDayHTML checks OG/Twitter/canonical/GA4/prev-next/regions/no-data.
func TestDayHTML(t *testing.T) {
	dbPath := fixtureDB(t, "2026-10-23")
	db := openRO(t, dbPath)
	site := t.TempDir()
	// Three consecutive renderable dates need history; render one real
	// date, then hand-write two more day.json docs for nav testing.
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	mkMini := func(date string) {
		doc := readDay(t, site, "2026-10-23")
		doc.Date = date
		var buf []byte
		buf, _ = json.MarshalIndent(doc, "", "  ")
		buf = append(buf, '\n')
		dir := filepath.Join(site, "d", date)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "day.json"), buf, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkMini("2026-10-22")
	mkMini("2026-10-24")
	if err := Rebuild(site, "https://abalol.xyz/presyo-bantay/"); err != nil {
		t.Fatal(err)
	}
	mid, err := os.ReadFile(filepath.Join(site, "d", "2026-10-23", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(mid)
	base := "https://abalol.xyz/presyo-bantay/d/2026-10-23/"
	for _, want := range []string{
		`og:type" content="article"`,
		`og:url" content="` + base + `"`,
		`og:image" content="https://abalol.xyz/presyo-bantay/d/2026-10-23/south.png"`,
		`og:image:width" content="1080"`,
		`og:image:height" content="1350"`,
		`og:locale" content="fil_PH"`,
		`twitter:card" content="summary_large_image"`,
		`<link rel="canonical" href="` + base + `"`,
		`article:published_time" content="2026-10-23"`,
		`G-VL047YCMK8`,
		`googletagmanager.com/gtag/js?id=G-VL047YCMK8`,
		`<html lang="fil">`,
		`data-lang="tl"`,
		`data-lang="en"`,
		`data-lang="ja"`,
		`pb-lang`,
		`href="../2026-10-22/"`, // older
		`href="../2026-10-24/"`, // newer
		`da.gov.ph/price-monitoring`,
		`width="1080" height="1350"`,
		`fetchpriority="high"`,
		`loading="lazy"`,
		`Sulit sa palengke · Okt 23, 2026 — Presyo Bantay`,
		`Sulit sa palengke`,
		`Market deals`,
		`市場のお買い得`,
		`aria-label="Wika / Language / 言語"`,
		`lang-label`,
		`white-space:nowrap`,
		`max-width:519px`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("day html missing %q", want)
		}
	}
	if strings.Contains(html, "Sulit ngayon") || strings.Contains(html, "Today’s deals") ||
		strings.Contains(html, "Today&rsquo;s deals") || strings.Contains(html, "今日のお買い得") {
		t.Error("day html keeps the old 'today' heading for a dated page")
	}
	// All 5 region titles in order.
	last := -1
	for _, title := range []string{"South Metro", "Maynila at Pasay", "Quezon City", "CAMANAVA", "Silangan"} {
		i := strings.Index(html, title)
		if i < 0 {
			t.Errorf("missing region title %q", title)
			continue
		}
		if i < last {
			t.Errorf("region %q out of order", title)
		}
		last = i
	}
	// No-data note for east.
	if !strings.Contains(html, "No data for") || !strings.Contains(html, "Walang data para sa") {
		t.Error("missing no-data note")
	}
	// First day has no older link, last day has no newer link.
	first, _ := os.ReadFile(filepath.Join(site, "d", "2026-10-22", "index.html"))
	if strings.Contains(string(first), "../2026-10-21/") {
		t.Error("first day must not link an older day")
	}
	if !strings.Contains(string(first), "../2026-10-23/") {
		t.Error("first day must link the newer day")
	}
	lastH, _ := os.ReadFile(filepath.Join(site, "d", "2026-10-24", "index.html"))
	if strings.Contains(string(lastH), "../2026-10-25/") {
		t.Error("last day must not link a newer day")
	}
	if !strings.Contains(string(lastH), "../2026-10-23/") {
		t.Error("last day must link the older day")
	}
	// days.json newest-first with 3 entries.
	daysData, err := os.ReadFile(filepath.Join(site, "data", "days.json"))
	if err != nil {
		t.Fatal(err)
	}
	var days DaysDoc
	if err := json.Unmarshal(daysData, &days); err != nil {
		t.Fatal(err)
	}
	if days.Latest != "2026-10-24" {
		t.Errorf("latest = %q", days.Latest)
	}
	if len(days.Days) != 3 || days.Days[0].Date != "2026-10-24" || days.Days[2].Date != "2026-10-22" {
		t.Errorf("days order wrong: %+v", days.Days)
	}
	for _, e := range days.Days {
		if e.Path != "d/"+e.Date+"/" {
			t.Errorf("bad path %q", e.Path)
		}
		if e.Label.TL == "" || e.Label.EN == "" || e.Label.JA == "" {
			t.Errorf("bad label %+v", e.Label)
		}
		if e.South.Title.TL == "" || e.South.Title.EN == "" || e.South.Title.JA == "" {
			t.Errorf("bad south title %+v", e.South.Title)
		}
	}
	// Archive page lists newest first with GA4 + canonical.
	arch, err := os.ReadFile(filepath.Join(site, "d", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	a := string(arch)
	if !strings.Contains(a, "G-VL047YCMK8") || !strings.Contains(a, `href="https://abalol.xyz/presyo-bantay/d/"`) {
		t.Error("archive head wrong")
	}
	i24, i23, i22 := strings.Index(a, "2026-10-24/"), strings.Index(a, "2026-10-23/"), strings.Index(a, "2026-10-22/")
	if i24 < 0 || !(i24 < i23 && i23 < i22) {
		t.Error("archive not newest-first")
	}
	// Month group headings in all three languages.
	for _, want := range []string{"Oktubre 2026", "October 2026", "2026年10月"} {
		if !strings.Contains(a, want) {
			t.Errorf("archive missing month heading %q", want)
		}
	}
	// Each summary line is prefixed with the featured region name,
	// language-specific.
	if !strings.Contains(a, "South Metro:") {
		t.Error("archive TL summary missing 'South Metro: ' prefix")
	}
	if !strings.Contains(a, "南部（ラス・ピニャス周辺）: ") {
		t.Error("archive JA summary missing region prefix")
	}
	for _, want := range []string{
		`aria-label="Wika / Language / 言語"`,
		`white-space:nowrap`,
		`max-width:519px`,
	} {
		if !strings.Contains(a, want) {
			t.Errorf("archive html missing %q", want)
		}
	}
}

// TestArchiveMonths groups days under month headings newest-first and
// prefixes each summary with the language-specific region name.
func TestArchiveMonths(t *testing.T) {
	mkEntry := func(date string) DaysEntry {
		dl := fbregion.FormatDateLong(date)
		return DaysEntry{
			Date: date, Path: "d/" + date + "/",
			Label:           LangText{TL: dl.TL, EN: dl.EN, JA: dl.JA},
			RegionsWithData: 5,
			South: DaysSouth{
				Text: LangText{
					TL: "🛒 Sulit\n✅ Mas mura kaysa usual:\n• Sayote ₱40/kg (−50%) · Alabang\n• Other ₱10/kg (−10%) · Elsewhere",
					EN: "🇬🇧 Deals\n✅ Chayote ₱40 (−50%), Other ₱10 (−10%)",
					JA: "🇯🇵 お買い得\n✅ サヨテ ₱40（−50%）、他 ₱10（−10%）",
				},
				Summary: LangText{
					TL: "Sayote ₱40/kg (−50%) · Alabang",
					EN: "Chayote ₱40 (−50%)",
					JA: "サヨテ ₱40（−50%）",
				},
				Image: "d/" + date + "/south.png",
				Title: LangText{TL: "South Metro", EN: "South Metro", JA: "南部（ラス・ピニャス周辺）"},
			},
		}
	}
	doc := DaysDoc{
		Latest: "2026-10-02",
		Days: []DaysEntry{
			mkEntry("2026-10-02"),
			mkEntry("2026-09-30"),
			mkEntry("2026-09-15"),
		},
	}
	html, err := renderArchivePage(doc, "https://abalol.xyz/presyo-bantay/")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Oktubre 2026", "October 2026", "2026年10月",
		"Setyembre 2026", "September 2026", "2026年9月",
		"South Metro: Sayote ₱40/kg",
		"South Metro: Chayote ₱40",
		"南部（ラス・ピニャス周辺）: サヨテ ₱40",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("archive months missing %q", want)
		}
	}
	iOct, iSep := strings.Index(html, "Oktubre 2026"), strings.Index(html, "Setyembre 2026")
	if iOct < 0 || iSep < 0 || !(iOct < iSep) {
		t.Error("months not newest-first")
	}
	i02, i30, i15 := strings.Index(html, "2026-10-02/"), strings.Index(html, "2026-09-30/"), strings.Index(html, "2026-09-15/")
	if !(i02 < i30 && i30 < i15) {
		t.Error("days not newest-first")
	}
}

// TestSouthDealPrefix checks the region-prefixed, language-specific deal line.
func TestSouthDealPrefix(t *testing.T) {
	e := DaysEntry{
		Date: "2026-10-23",
		South: DaysSouth{
			Text: LangText{
				TL: "🛒 Sulit\n✅ Mas mura kaysa usual:\n• Sayote ₱40/kg (−50%) · Alabang\n• Other ₱10/kg (−10%) · Elsewhere",
				EN: "🇬🇧 Deals\n✅ Chayote ₱40 (−50%), Other ₱10 (−10%)",
				JA: "🇯🇵 お買い得\n✅ サヨテ ₱40（−50%）、他 ₱10（−10%）",
			},
			Summary: LangText{
				TL: "Sayote ₱40/kg (−50%) · Alabang",
				EN: "Chayote ₱40 (−50%)",
				JA: "サヨテ ₱40（−50%）",
			},
			Title: LangText{TL: "South Metro", EN: "South Metro", JA: "南部（ラス・ピニャス周辺）"},
		},
	}
	if got := southDeal(e, "tl"); got != "South Metro: Sayote ₱40/kg (−50%) · Alabang" {
		t.Errorf("tl = %q", got)
	}
	if got := southDeal(e, "en"); got != "South Metro: Chayote ₱40 (−50%)" {
		t.Errorf("en = %q", got)
	}
	if got := southDeal(e, "ja"); got != "南部（ラス・ピニャス周辺）: サヨテ ₱40（−50%）" {
		t.Errorf("ja = %q", got)
	}
	for _, lang := range []string{"tl", "en", "ja"} {
		got := southDeal(e, lang)
		if strings.Contains(got, "✅") || strings.Contains(got, "Other") || strings.Contains(got, "他 ₱10") {
			t.Errorf("%s shows more than the top deal: %q", lang, got)
		}
	}
	empty := DaysEntry{Date: "2026-10-23", South: DaysSouth{Title: LangText{TL: "South Metro"}}}
	if got := southDeal(empty, "tl"); got != "" {
		t.Errorf("empty summary = %q, want empty", got)
	}
}

// TestPrivacyNotice checks the trilingual GA privacy notice is in the
// footer of every day page and the archive page.
func TestPrivacyNotice(t *testing.T) {
	db := openRO(t, fixtureDB(t, "2026-10-23"))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`data-lang="tl">Gumagamit ang site na ito ng Google Analytics para sa bilang ng bisita.`,
		`data-lang="en">This site uses Google Analytics to count visits.`,
		`data-lang="ja">アクセス解析にGoogleアナリティクスを使っています。`,
	}
	day, err := os.ReadFile(filepath.Join(site, "d", "2026-10-23", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if !strings.Contains(string(day), w) {
			t.Errorf("day html missing %q", w)
		}
	}
	arch, err := os.ReadFile(filepath.Join(site, "d", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if !strings.Contains(string(arch), w) {
			t.Errorf("archive html missing %q", w)
		}
	}
}

// TestBrandHeader checks the logo lockup header: circular basket mark
// (WebP srcset + PNG fallback), two-tone wordmark, trilingual tagline,
// favicons and theme-color on day and archive pages.
func TestBrandHeader(t *testing.T) {
	dbPath := fixtureDB(t, "2026-10-23")
	db := openRO(t, dbPath)
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	if err := Rebuild(site, "https://abalol.xyz/presyo-bantay/"); err != nil {
		t.Fatal(err)
	}
	day, err := os.ReadFile(filepath.Join(site, "d", "2026-10-23", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	arch, err := os.ReadFile(filepath.Join(site, "d", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	taglines := []string{
		`>Araw-araw na presyo sa palengke, mula sa DA<`,
		`>Daily market prices from DA Bantay Presyo<`,
		`>DAの市場価格を毎日お届け<`,
	}
	dayWants := []string{
		`<a class="brand home" href="../../">`,
		`srcset="../../brand/logo-mark-96.webp 2x, ../../brand/logo-mark-144.webp 3x"`,
		`<img class="brand-mark" src="../../brand/logo-mark-96.png"`,
		`alt="" aria-hidden="true"`,
		`<span class="brand-name">Presyo <span class="gold">Bantay</span></span>`,
		`a.brand:focus-visible`,
		`rel="icon" type="image/png" sizes="32x32" href="../../favicon-32.png"`,
		`rel="icon" type="image/png" sizes="16x16" href="../../favicon-16.png"`,
		`rel="apple-touch-icon" href="../../apple-touch-icon.png"`,
		`name="theme-color" content="#0b5d3b"`,
	}
	dayWants = append(dayWants, taglines...)
	for _, want := range dayWants {
		if !strings.Contains(string(day), want) {
			t.Errorf("day html missing brand header %q", want)
		}
	}
	archWants := []string{
		`<a class="brand home" href="../">`,
		`srcset="../brand/logo-mark-96.webp 2x, ../brand/logo-mark-144.webp 3x"`,
		`<img class="brand-mark" src="../brand/logo-mark-96.png"`,
		`rel="icon" type="image/png" sizes="32x32" href="../favicon-32.png"`,
		`rel="apple-touch-icon" href="../apple-touch-icon.png"`,
		`name="theme-color" content="#0b5d3b"`,
	}
	archWants = append(archWants, taglines...)
	for _, want := range archWants {
		if !strings.Contains(string(arch), want) {
			t.Errorf("archive html missing brand header %q", want)
		}
	}
}

// TestRerunDeterministic re-runs without -force and requires identical
// bytes for every generated file.
func TestRerunDeterministic(t *testing.T) {
	db := openRO(t, fixtureDB(t, "2026-10-23"))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	snap := map[string][]byte{}
	err := filepath.Walk(filepath.Join(site, "d"), func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		snap[p] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	daysBefore, err := os.ReadFile(filepath.Join(site, "data", "days.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	for p, before := range snap {
		after, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("missing after rerun: %s", p)
		}
		if string(after) != string(before) {
			t.Errorf("not deterministic: %s", p)
		}
	}
	daysAfter, _ := os.ReadFile(filepath.Join(site, "data", "days.json"))
	if string(daysAfter) != string(daysBefore) {
		t.Error("days.json not deterministic")
	}
}

// TestSkipFewRegions renders nothing when fewer than 3 regions report.
func TestSkipFewRegions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thin.db")
	db0, err := store.Connect(path)
	if err != nil {
		t.Fatal(err)
	}
	asOfT, _ := time.Parse("2006-01-02", "2026-10-23")
	res, _ := db0.Exec("INSERT INTO commodities (name, category, unit) VALUES ('Tilapia', 'fish', 'per kg')")
	commID, _ := res.LastInsertId()
	for i := 0; i < 2; i++ {
		res, _ := db0.Exec("INSERT INTO markets (name) VALUES (?)", regions.Regions[i].Markets[0].Name)
		mid, _ := res.LastInsertId()
		for ago := 29; ago >= 0; ago-- {
			d := asOfT.AddDate(0, 0, -ago).Format("2006-01-02")
			db0.Exec("INSERT INTO prices (date, market_id, commodity_id, price) VALUES (?, ?, ?, ?)",
				d, mid, commID, 100.0)
		}
	}
	db0.Close()
	db := openRO(t, path)
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(site, "d", "2026-10-23", "day.json")); !os.IsNotExist(err) {
		t.Error("day with 2/5 regions must be skipped")
	}
}

// TestHTMLOnly rebuilds HTML from existing day.json without a DB render.
func TestHTMLOnly(t *testing.T) {
	db := openRO(t, fixtureDB(t, "2026-10-23"))
	site := t.TempDir()
	if err := Run(db, testConfig(site, "2026-10-23")); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(site, "2026-10-23")
	cfg.HTMLOnly = true
	if err := os.Remove(filepath.Join(site, "d", "2026-10-23", "index.html")); err != nil {
		t.Fatal(err)
	}
	if err := Run(db, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(site, "d", "2026-10-23", "index.html")); err != nil {
		t.Error("html-only must rebuild index.html")
	}
}
