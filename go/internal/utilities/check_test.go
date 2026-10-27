package utilities

import (
	"strings"
	"testing"
	"time"
)

func TestCheckFuelPasses(t *testing.T) {
	snap := fixtureSnapshot(t)
	text, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckPost(text, snap, "fuel"); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
}

func TestCheckBillsPasses(t *testing.T) {
	snap := fixtureSnapshot(t)
	text, err := RenderBillsPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckPost(text, snap, "bills"); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
}

func TestCheckFuelTampered(t *testing.T) {
	snap := fixtureSnapshot(t)
	text, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	// Invented number.
	bad := strings.Replace(text, "−₱4.30/L", "−₱9.99/L", 1)
	if problems := CheckPost(bad, snap, "fuel"); len(problems) == 0 {
		t.Fatal("tampered number passed")
	}
	// Flipped sign.
	flipped := strings.Replace(text, "• Diesel: +₱2.60/L", "• Diesel: −₱2.60/L", 1)
	if problems := CheckPost(flipped, snap, "fuel"); len(problems) == 0 {
		t.Fatal("flipped sign passed")
	}
	// Swapped products (both numbers exist globally).
	swapped := strings.Replace(text, "• Gasolina: −₱4.30/L", "• Gasolina: +₱2.60/L", 1)
	swapped = strings.Replace(swapped, "• Diesel: +₱2.60/L", "• Diesel: −₱4.30/L", 1)
	if problems := CheckPost(swapped, snap, "fuel"); len(problems) == 0 {
		t.Fatal("swapped products passed")
	}
	// Swapped products on the combined EN line.
	swappedEN := strings.Replace(text,
		"Gasoline −₱4.30/L · Diesel +₱2.60/L",
		"Gasoline +₱2.60/L · Diesel −₱4.30/L", 1)
	if problems := CheckPost(swappedEN, snap, "fuel"); len(problems) == 0 {
		t.Fatal("swapped EN products passed")
	}
}

func TestCheckFuelTrendTampered(t *testing.T) {
	snap := fixtureSnapshot(t)
	adj13, err := ParseOilMonitor(mustLines(t, "oilmon_13oct.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	snap.Fuel.Adjustments = []OilAdjustment{snap.Fuel.Adjustments[0], adj13}
	text, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckPost(text, snap, "fuel"); len(problems) != 0 {
		t.Fatalf("trend caption fails: %v", problems)
	}
	// Swapped last-week products.
	swapped := strings.Replace(text,
		"gasolina +₱1.50 hanggang +₱1.56 · diesel −₱3.00 hanggang −₱3.02",
		"gasolina −₱3.00 hanggang −₱3.02 · diesel +₱1.50 hanggang +₱1.56", 1)
	if problems := CheckPost(swapped, snap, "fuel"); len(problems) == 0 {
		t.Fatal("swapped last-week products passed")
	}
	// Tampered cumulative sum.
	bad := strings.Replace(text, "gasolina −₱2.80 hanggang −₱2.74", "gasolina −₱9.80 hanggang −₱2.74", 1)
	if problems := CheckPost(bad, snap, "fuel"); len(problems) == 0 {
		t.Fatal("tampered cumulative passed")
	}
	// Tampered pump delta.
	bad = strings.Replace(text, "RON 91 ₱87.90 −₱0.40", "RON 91 ₱87.90 −₱0.90", 1)
	if problems := CheckPost(bad, snap, "fuel"); len(problems) == 0 {
		t.Fatal("tampered delta passed")
	}
	// Delta with flipped sign.
	flipped := strings.Replace(text, "RON 95 ₱88.90 +₱0.50", "RON 95 ₱88.90 −₱0.50", 1)
	if problems := CheckPost(flipped, snap, "fuel"); len(problems) == 0 {
		t.Fatal("flipped delta passed")
	}
	// Delta swapped across products (NCR RON91 gets RON95's delta).
	swappedPump := strings.Replace(text, "RON 91 ₱87.90 −₱0.40", "RON 91 ₱87.90 +₱0.50", 1)
	if problems := CheckPost(swappedPump, snap, "fuel"); len(problems) == 0 {
		t.Fatal("swapped pump delta passed")
	}
	// Delta swapped across cities (Parañaque gets Muntinlupa's delta).
	swappedCity := strings.Replace(text, "Parañaque: RON 91 ₱89.50 −₱0.80", "Parañaque: RON 91 ₱89.50 +₱2.30", 1)
	if problems := CheckPost(swappedCity, snap, "fuel"); len(problems) == 0 {
		t.Fatal("swapped city delta passed")
	}
}

func TestCheckBillsTampered(t *testing.T) {
	snap := fixtureSnapshot(t)
	text, err := RenderBillsPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	// Wrong Meralco rate.
	bad := strings.Replace(text, "₱15.0615/kWh", "₱15.9999/kWh", 1)
	if problems := CheckPost(bad, snap, "bills"); len(problems) == 0 {
		t.Fatal("tampered rate passed")
	}
	// Flipped LPG diff sign.
	flipped := strings.Replace(text, "+₱89 vs", "−₱89 vs", 1)
	if problems := CheckPost(flipped, snap, "bills"); len(problems) == 0 {
		t.Fatal("flipped diff passed")
	}
	// City range swapped with the NCR common price (TL city line).
	swapped := strings.Replace(text, "Sa Las Piñas: ₱1,368–₱1,569", "Sa Las Piñas: ₱1,430–₱1,569", 1)
	if problems := CheckPost(swapped, snap, "bills"); len(problems) == 0 {
		t.Fatal("swapped city range passed")
	}
	// Wrong previous LPG common.
	badPrev := strings.Replace(text, "₱1,341 → ₱1,430", "₱1,300 → ₱1,430", 1)
	if problems := CheckPost(badPrev, snap, "bills"); len(problems) == 0 {
		t.Fatal("tampered prev LPG passed")
	}
	// Wrong water rate.
	badWater := strings.Replace(text, "₱52.61 → ₱52.85/cu.m", "₱52.61 → ₱52.99/cu.m", 1)
	if problems := CheckPost(badWater, snap, "bills"); len(problems) == 0 {
		t.Fatal("tampered water rate passed")
	}
	// Wrong family total.
	badFam := strings.Replace(text, "₱5,341.68 → ₱5,499.30", "₱5,341.68 → ₱5,599.30", 1)
	if problems := CheckPost(badFam, snap, "bills"); len(problems) == 0 {
		t.Fatal("tampered family total passed")
	}
	// Wrong family component.
	badComp := strings.Replace(text, "kuryente +₱63.82", "kuryente +₱64.82", 1)
	if problems := CheckPost(badComp, snap, "bills"); len(problems) == 0 {
		t.Fatal("tampered family component passed")
	}
}

func TestCheckLint(t *testing.T) {
	snap := fixtureSnapshot(t)
	text, err := RenderFuelPost(snap)
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckPost(text+" $5", snap, "fuel"); len(problems) == 0 {
		t.Fatal("dollar sign passed")
	}
	if problems := CheckPost(strings.Replace(text, "（DOE、", "（DOE（x、", 1), snap, "fuel"); len(problems) == 0 {
		t.Fatal("nested parens passed")
	}
	if problems := CheckPost(text+" NaN", snap, "fuel"); len(problems) == 0 {
		t.Fatal("NaN passed")
	}
	// Empty section: blank the whole EN block.
	parts := strings.Split(text, Separator)
	parts[1] = "  \n"
	blank := strings.Join(parts, Separator)
	if problems := CheckPost(blank, snap, "fuel"); len(problems) == 0 {
		t.Fatal("empty section passed")
	}
	if problems := CheckPost(text, snap, "bogus"); len(problems) == 0 {
		t.Fatal("bogus kind passed")
	}
}

func waterSnap(t *testing.T) (*Snapshot, string) {
	t.Helper()
	b := readFixture(t, "maynilad_maintenance_posts.json")
	rows, err := ParseMaintenancePosts(b, "Las Pi")
	if err != nil {
		t.Fatal(err)
	}
	w := &WaterStatus{City: "Las Piñas", Maintenance: rows}
	alerts := BuildWaterAlerts(w)
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts", len(alerts))
	}
	snap := &Snapshot{Water: w, Alerts: alerts}
	return snap, RenderWaterAlertPost(alerts[0], false)
}

func TestCheckWaterPasses(t *testing.T) {
	snap, text := waterSnap(t)
	if problems := CheckPost(text, snap, "water"); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
}

func TestCheckWaterTampered(t *testing.T) {
	snap, text := waterSnap(t)
	// Wrong time.
	bad := strings.Replace(text, "10:00 PM", "11:00 PM", 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("tampered time passed")
	}
	// Wrong day.
	bad = strings.Replace(text, "Ago 26", "Ago 29", 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("tampered day passed")
	}
	// Swapped area.
	bad = strings.Replace(text, "Talon Singko", "Talon Sais", 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("tampered area passed")
	}
	// Wrong reason.
	bad = strings.Replace(text, "facility maintenance", "pipe repair", 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("tampered reason passed")
	}
	// No alert in snapshot.
	if problems := CheckPost(text, &Snapshot{}, "water"); len(problems) == 0 {
		t.Fatal("missing alert passed")
	}
}

func TestCheckWaterXLabels(t *testing.T) {
	s := time.Date(2026, 10, 22, 22, 0, 0, 0, manilaLoc)
	e := time.Date(2026, 10, 23, 6, 0, 0, 0, manilaLoc)
	a := WaterAlert{
		ID:         "xtest1234567",
		Kind:       "scheduled",
		City:       "Las Piñas",
		Areas:      []string{XAreasTL},
		Start:      s,
		End:        e,
		WindowText: "from 10:00 p.m. of October 22, 2026 until 6:00 a.m. of October 23, 2026",
		ReasonKey:  "audit",
		SourceURL:  "https://x.com/maynilad/status/2111361327537279200",
	}
	w := &WaterStatus{City: "Las Piñas", CheckedAt: "2026-10-17T10:00:00+08:00"}
	snap := &Snapshot{Water: w, Alerts: []WaterAlert{a}}
	text := RenderWaterAlertPost(a, false)
	if problems := CheckPost(text, snap, "water"); len(problems) != 0 {
		t.Fatalf("X alert problems: %v\n%s", problems, text)
	}
	if !strings.Contains(text, "📍 "+XAreasTL) {
		t.Fatalf("TL label missing:\n%s", text)
	}
	if !strings.Contains(text, "· "+XAreasEN+" ·") {
		t.Fatalf("EN label missing:\n%s", text)
	}
	if !strings.Contains(text, "・"+XAreasJA+"・") {
		t.Fatalf("JA label missing:\n%s", text)
	}
	// Swapping the JA label for the EN label must fail.
	bad := strings.Replace(text, XAreasJA, XAreasEN, 1)
	if problems := CheckPost(bad, snap, "water"); len(problems) == 0 {
		t.Fatal("swapped JA/EN label passed")
	}
}
