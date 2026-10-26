package utilities

import (
	"strings"
	"testing"
)

// fixtureSnapshot builds the 2026-10-20 snapshot from the saved
// fixtures (no network): oil monitors, both pump weeks, both LPG
// months, both Meralco releases, FCDA notices, empty Las Piñas water
// status.
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
	rates, err := ParseFCDAPosts(readFixture(t, "maynilad_fcda_posts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Snapshot{
		FetchedAt: "2026-10-26T20:00:00+08:00",
		Fuel: FuelStatus{
			Adjustments: []OilAdjustment{adj},
			NCRPump:     []PumpSummary{pump, pump2},
		},
		LPG:        []LPGMonth{lpg, lpg2},
		Meralco:    []MeralcoRate{merOct, merSep},
		WaterRates: rates,
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
	snap := fixtureSnapshot(t)
	adj13, err := ParseOilMonitor(mustLines(t, "oilmon_13oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	snap.Fuel.Adjustments = []OilAdjustment{snap.Fuel.Adjustments[0], adj13}
	got, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	want := `⛽ Presyo ng langis — epektibo Okt 20–26, 2026
• Gasolina: −₱4.30/L
• Diesel: +₱2.60/L
• Kerosene: −₱1.00/L
💡 Sa 10 litro: gasolina −₱43.00, diesel +₱26.00
Noong nakaraang linggo (Okt 13–19): gasolina +₱1.50 hanggang +₱1.56 · diesel −₱3.00 hanggang −₱3.02 · kerosene +₱1.70 hanggang +₱1.77
Kabuuan sa huling 2 na linggo (Okt 13–26): gasolina −₱2.80 hanggang −₱2.74 · diesel −₱0.42 hanggang −₱0.40
📍 Presyo sa bomba, DOE Okt 13–19 vs Okt 6–12, bago ang galaw ngayong linggo:
NCR common: RON 91 ₱87.90 −₱0.40 · RON 95 ₱88.90 +₱0.50 · Diesel ₱91.40 −₱2.30
Parañaque: RON 91 ₱89.50 −₱0.80 · RON 95 ₱90.50 −₱0.80 · Diesel ₱91.80 −₱3.00
Muntinlupa: RON 91 ₱91.00 +₱2.30 · RON 95 ₱91.20 +₱1.50 · Diesel ₱90.90 −₱3.00
Parañaque at Muntinlupa: malapit sa Las Piñas; wala sa monitoring ng DOE ang Las Piñas
━━━━━━━━━━
🇬🇧 Fuel prices — effective Oct 20–26, 2026
Gasoline −₱4.30/L · Diesel +₱2.60/L · Kerosene −₱1.00/L
Per 10 L: gasoline −₱43.00, diesel +₱26.00
Last week (Oct 13–19): gasoline +₱1.50 to +₱1.56 · diesel −₱3.00 to −₱3.02 · kerosene +₱1.70 to +₱1.77
Total over the last 2 weeks (Oct 13–26): gasoline −₱2.80 to −₱2.74 · diesel −₱0.42 to −₱0.40
📍 Pump prices, DOE Oct 13–19 vs Oct 6–12, before this week's change:
NCR common: RON 91 ₱87.90 −₱0.40 · RON 95 ₱88.90 +₱0.50 · Diesel ₱91.40 −₱2.30
Parañaque: RON 91 ₱89.50 −₱0.80 · RON 95 ₱90.50 −₱0.80 · Diesel ₱91.80 −₱3.00
Muntinlupa: RON 91 ₱91.00 +₱2.30 · RON 95 ₱91.20 +₱1.50 · Diesel ₱90.90 −₱3.00
Parañaque and Muntinlupa are nearby; Las Piñas is not in DOE's monitoring
━━━━━━━━━━
🇯🇵 燃料価格 — 10/20〜10/26適用
ガソリン −₱4.30/L・軽油 +₱2.60/L・灯油 −₱1.00/L
10Lあたり：ガソリン −₱43.00、軽油 +₱26.00
先週（10/13〜10/19）：ガソリン +₱1.50〜+₱1.56・軽油 −₱3.00〜−₱3.02・灯油 +₱1.70〜+₱1.77
直近2週間の合計（10/13〜10/26）：ガソリン −₱2.80〜−₱2.74・軽油 −₱0.42〜−₱0.40
📍 店頭価格（DOE、10/13〜10/19、前週10/6〜10/12比、今回の改定前）：
NCR一般：RON91 ₱87.90 −₱0.40・RON95 ₱88.90 +₱0.50・軽油 ₱91.40 −₱2.30
パラニャケ：RON91 ₱89.50 −₱0.80・RON95 ₱90.50 −₱0.80・軽油 ₱91.80 −₱3.00
ムンティンルパ：RON91 ₱91.00 +₱2.30・RON95 ₱91.20 +₱1.50・軽油 ₱90.90 −₱3.00
パラニャケ・ムンティンルパは近隣市。ラスピニャスはDOE調査対象外
━━━━━━━━━━
Source: DOE Oil Monitor & NCR pump price monitoring (doe.gov.ph) · Hindi opisyal na DOE page`
	if got != want {
		t.Fatalf("fuel post mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderFuelPostNoHistory(t *testing.T) {
	// Single adjustment, single pump week: previous-week say-so lines,
	// no deltas.
	snap := fixtureSnapshot(t)
	snap.Fuel.Adjustments = snap.Fuel.Adjustments[:1]
	snap.Fuel.NCRPump = snap.Fuel.NCRPump[:1]
	got, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Walang datos ng nakaraang linggo.",
		"Previous week not available.",
		"前回データなし。",
		"RON 91 ₱87.90 · RON 95",
		"Kabuuan sa huling 1 na linggo",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, " vs Okt") || strings.Contains(got, "hanggang +₱1.50") {
		t.Fatalf("unexpected trend content:\n%s", got)
	}
}

func TestRenderBillsPost(t *testing.T) {
	got, err := RenderBillsPost(fixtureSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := `🏠 Bayarin sa bahay — Oktubre 2026
⚡ Meralco: ₱14.7424 → ₱15.0615/kWh, +₱0.3191 vs Setyembre
Sa 200 kWh: mga +₱64 sa bill
🔥 LPG 11 kg NCR, karaniwang presyo (DOE): ₱1,341 → ₱1,430, +₱89 vs Set 1–7
Sa Las Piñas: ₱1,368–₱1,569, Set: ₱1,185–₱1,393
💧 Tubig Maynilad, average basic charge + FCDA: ₱52.61 → ₱52.85/cu.m, +₱0.24 mula Okt 1
💧 Maynilad, Las Piñas: walang naka-schedule na water interruption — tiningnan Okt 26
👪 Tantiyang buwanang gastos ng isang pamilya:
₱5,341.68 → ₱5,499.30, +₱157.62 vs Setyembre · kuryente +₱63.82, LPG +₱89.00, tubig +₱4.80
Palagay: 200 kWh kuryente sa typical-household rate ng Meralco · 1 tangke LPG 11 kg sa NCR common price ng DOE · 20 cu.m tubig sa average basic charge + FCDA ng Maynilad, hindi kasama ang ibang singil at VAT ng tubig · hindi kasama ang gasolina
━━━━━━━━━━
🇬🇧 Household bills — October 2026
⚡ Meralco: ₱14.7424 → ₱15.0615/kWh, +₱0.3191 vs September
At 200 kWh: about +₱64 on the bill
🔥 LPG 11 kg NCR, common price (DOE): ₱1,341 → ₱1,430, +₱89 vs Sep 1–7
Las Piñas: ₱1,368–₱1,569, Sep: ₱1,185–₱1,393
💧 Maynilad water, average basic charge + FCDA: ₱52.61 → ₱52.85/cu.m, +₱0.24 from Oct 1
💧 Maynilad, Las Piñas: no scheduled water interruptions — checked Oct 26
👪 Estimated monthly family spending:
₱5,341.68 → ₱5,499.30, +₱157.62 vs September · power +₱63.82, LPG +₱89.00, water +₱4.80
Assumptions: 200 kWh at Meralco's typical-household rate · one 11-kg LPG tank at DOE's NCR common price · 20 cu.m water at Maynilad's average basic charge + FCDA, excluding other water charges and VAT · fuel not included
━━━━━━━━━━
🇯🇵 家計の光熱費 — 2026年10月
⚡ Meralco：₱14.7424 → ₱15.0615/kWh、+₱0.3191（9月比）
200kWhで約+₱64
🔥 LPG 11kg・NCR一般価格（DOE）：₱1,341 → ₱1,430、+₱89（9/1〜9/7比）
ラスピニャス：₱1,368〜₱1,569、9月：₱1,185〜₱1,393
💧 Maynilad水道・基本料金＋FCDA：₱52.61 → ₱52.85/cu.m、+₱0.24（10/1から）
💧 Maynilad・ラスピニャス：計画断水なし（10/26確認）
👪 1か月の家計支出（目安）：
₱5,341.68 → ₱5,499.30、+₱157.62（9月比）・電気 +₱63.82・LPG +₱89.00・水道 +₱4.80
前提：200kWhをMeralco一般家庭料金で・11kg LPG 1本をDOE・NCR一般価格で・水20cu.mをMaynilad基本料金＋FCDAで（その他水道料金・VAT・燃料は含まず）
━━━━━━━━━━
Source: Meralco, DOE LPG monitoring, Maynilad advisories · Hindi opisyal na page`
	if got != want {
		t.Fatalf("bills post mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderBillsNoPrevTotal(t *testing.T) {
	// Missing previous LPG month and water rate: current total only.
	snap := fixtureSnapshot(t)
	snap.LPG = snap.LPG[:1]
	snap.WaterRates = snap.WaterRates[:1]
	got, err := RenderBillsPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"₱5,499.30\nWalang datos ng nakaraang kabuuan.",
		"Previous total not available.",
		"前回合計データなし。",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "+₱157.62") {
		t.Fatalf("change shown without previous:\n%s", got)
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
	// No pump predates the adjustment: both pump lines go away.
	snap.Fuel.NCRPump = nil
	got, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Presyo sa NCR") || strings.Contains(got, "Malapit sa Las") {
		t.Fatalf("pump lines should be omitted:\n%s", got)
	}
	// Total water failure (no status, no rates): no water lines at all.
	bsnap := fixtureSnapshot(t)
	bsnap.Water = nil
	bsnap.WaterRates = nil
	bgot, err := RenderBillsPost(bsnap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bgot, "Maynilad") && strings.Contains(bgot, "💧") {
		t.Fatalf("water lines should be omitted:\n%s", bgot)
	}
	if strings.Contains(bgot, "Tantiyang") {
		t.Fatalf("family block should be omitted without water rates:\n%s", bgot)
	}
	// Single LPG month: no month-on-month diff.
	bsnap.LPG = bsnap.LPG[:1]
	bgot, err = RenderBillsPost(bsnap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bgot, "+₱89 vs") || strings.Contains(bgot, "vs Set 1–7") {
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
