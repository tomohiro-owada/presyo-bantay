package utilities

import (
	"strings"
	"testing"
)

// fixtureSnapshot builds the 2026-10-20 snapshot from the saved
// fixtures (no network): oil monitors, both pump weeks, both LPG
// months, both Meralco releases, empty Las Piñas water status.
func fixtureSnapshot(t *testing.T) *Snapshot {
	t.Helper()
	adjLines := mustLines(t, "oilmon_20oct.pdf")
	adj, err := ParseOilMonitor(adjLines)
	if err != nil {
		t.Fatal(err)
	}
	pumpLines := mustLines(t, "ncr_pump_1013.pdf")
	pump, err := ParseNCRPumpSummary(pumpLines)
	if err != nil {
		t.Fatal(err)
	}
	pump2Lines := mustLines(t, "ncr_pump_1006.pdf")
	pump2, err := ParseNCRPumpSummary(pump2Lines)
	if err != nil {
		t.Fatal(err)
	}
	lpgLines := mustLines(t, "ncr_lpg_oct.pdf")
	lpg, err := ParseNCRLPG(lpgLines)
	if err != nil {
		t.Fatal(err)
	}
	lpg2Lines := mustLines(t, "ncr_lpg_sep.pdf")
	lpg2, err := ParseNCRLPG(lpg2Lines)
	if err != nil {
		t.Fatal(err)
	}
	merOct, err := ParseMeralcoRelease(readFixture(t, "meralco_2026_10.html"))
	if err != nil {
		t.Fatal(err)
	}
	merSep, err := ParseMeralcoRelease(readFixture(t, "meralco_2026_09.html"))
	if err != nil {
		t.Fatal(err)
	}
	return &Snapshot{
		FetchedAt: "2026-10-26T20:00:00+08:00",
		Fuel: FuelStatus{
			Adjustments: []OilAdjustment{adj},
			NCRPump:     []PumpSummary{pump, pump2},
		},
		LPG:     []LPGMonth{lpg, lpg2},
		Meralco: []MeralcoRate{merOct, merSep},
		Water: &WaterStatus{
			City:        "Las Piñas",
			CheckedAt:   "2026-10-26T20:00:00+08:00",
			Scheduled:   []Interruption{},
			Emergency:   []Interruption{},
			Rotational:  []Interruption{},
			Maintenance: []Maintenance{},
		},
	}
}

func TestRenderFuelPost(t *testing.T) {
	got, err := RenderFuelPost(fixtureSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := `⛽ Presyo ng langis — epektibo Okt 20–26, 2026
• Gasolina: −₱4.30/L
• Diesel: +₱2.60/L
• Kerosene: −₱1.00/L
💡 Sa 10 litro: gasolina −₱43.00, diesel +₱26.00
📍 Karaniwang presyo sa NCR bago ang galaw na ito (DOE, Okt 13–19): RON 91 ₱87.90 · RON 95 ₱88.90 · Diesel ₱91.40
━━━━━━━━━━
🇬🇧 Fuel prices — effective Oct 20–26, 2026
Gasoline −₱4.30/L · Diesel +₱2.60/L · Kerosene −₱1.00/L
Per 10 L: gasoline −₱43.00, diesel +₱26.00
NCR common pump prices before this change (DOE, Oct 13–19): RON 91 ₱87.90 · RON 95 ₱88.90 · Diesel ₱91.40
━━━━━━━━━━
🇯🇵 燃料価格 — 10/20〜10/26適用
ガソリン −₱4.30/L・軽油 +₱2.60/L・灯油 −₱1.00/L
10Lあたり：ガソリン −₱43.00、軽油 +₱26.00
今回の改定前のNCR一般価格（DOE、10/13〜10/19）：RON91 ₱87.90・RON95 ₱88.90・軽油 ₱91.40
━━━━━━━━━━
Source: DOE Oil Monitor & NCR pump price monitoring (doe.gov.ph) · Hindi opisyal na DOE page`
	if got != want {
		t.Fatalf("fuel post mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderBillsPost(t *testing.T) {
	got, err := RenderBillsPost(fixtureSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := `🏠 Bayarin sa bahay — Oktubre 2026
⚡ Meralco: +₱0.3191/kWh → ₱15.0615/kWh mula ₱14.7424
   Sa 200 kWh: mga +₱64 sa bill
🔥 LPG 11 kg, karaniwang presyo sa NCR (DOE, Okt 1–8): ₱1,430, +₱89 vs Set 1–7
   Sa Las Piñas: ₱1,368–₱1,569
💧 Maynilad, Las Piñas: walang naka-schedule na water interruption — tiningnan Okt 26
━━━━━━━━━━
🇬🇧 Household bills — October 2026
⚡ Meralco: +₱0.3191/kWh → ₱15.0615/kWh, from ₱14.7424. At 200 kWh: about +₱64 on the bill
🔥 LPG 11 kg, NCR common price (DOE, Oct 1–8): ₱1,430, +₱89 vs Sep 1–7. Las Piñas: ₱1,368–₱1,569
💧 Maynilad, Las Piñas: no scheduled water interruptions — checked Oct 26
━━━━━━━━━━
🇯🇵 家計の光熱費 — 2026年10月
⚡ Meralco：+₱0.3191/kWh → ₱15.0615/kWh（前月 ₱14.7424）。200kWhで約+₱64
🔥 LPG 11kg・NCR一般価格（DOE、10/1〜10/8）：₱1,430、9/1〜9/7比 +₱89。ラスピニャス：₱1,368〜₱1,569
💧 Maynilad・ラスピニャス：計画断水なし（10/26確認）
━━━━━━━━━━
Source: Meralco, DOE LPG monitoring, Maynilad advisories · Hindi opisyal na page`
	if got != want {
		t.Fatalf("bills post mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderFuelRanges(t *testing.T) {
	snap := fixtureSnapshot(t)
	adjLines := mustLines(t, "oilmon_13oct.pdf")
	adj, err := ParseOilMonitor(adjLines)
	if err != nil {
		t.Fatal(err)
	}
	snap.Fuel.Adjustments = []OilAdjustment{adj}
	got, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"• Gasolina: +₱1.50 hanggang +₱1.56/L",
		"Gasoline +₱1.50 to +₱1.56/L",
		"ガソリン +₱1.50〜+₱1.56/L",
		"gasolina +₱15.00 hanggang +₱15.60, diesel −₱30.00 hanggang −₱30.20",
		"epektibo Okt 13–19, 2026",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderOmitsMissingFields(t *testing.T) {
	snap := fixtureSnapshot(t)
	// No pump predates the adjustment: the 📍 line goes away.
	snap.Fuel.NCRPump = nil
	got, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Karaniwang presyo") {
		t.Fatalf("pump line should be omitted:\n%s", got)
	}
	// Water fetch failed: no water line at all.
	bsnap := fixtureSnapshot(t)
	bsnap.Water = nil
	bgot, err := RenderBillsPost(bsnap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bgot, "Maynilad") && strings.Contains(bgot, "💧") {
		t.Fatalf("water line should be omitted:\n%s", bgot)
	}
	// Single LPG month: no month-on-month diff.
	bsnap.LPG = bsnap.LPG[:1]
	bgot, err = RenderBillsPost(bsnap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bgot, "vs Set") {
		t.Fatalf("diff should be omitted:\n%s", bgot)
	}
}

func TestRenderWaterRows(t *testing.T) {
	snap := fixtureSnapshot(t)
	snap.Water.Scheduled = []Interruption{{
		Kind: "scheduled", City: "Las Piñas",
		Barangays: "Brgy. Almanza Uno", Area: "at Station",
		From: "2026-10-27", FromRaw: "October 27, 2026",
		To: "2026-10-28", ToRaw: "October 28, 2026",
		Time: "10:00 PM to 6:00 AM", Reason: "Network Maintenance",
	}}
	got, err := RenderBillsPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "💧 Water interruption, Las Piñas: Brgy. Almanza Uno, Okt 27–Okt 28, 10:00 PM to 6:00 AM, Network Maintenance") {
		t.Fatalf("water row missing:\n%s", got)
	}
	if strings.Contains(got, "walang naka-schedule") {
		t.Fatalf("empty-water line should be gone:\n%s", got)
	}
}
