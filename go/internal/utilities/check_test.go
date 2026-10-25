package utilities

import (
	"strings"
	"testing"
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
	if problems := CheckPost(strings.Replace(text, "(DOE,", "(DOE (x,", 1), snap, "fuel"); len(problems) == 0 {
		t.Fatal("nested parens passed")
	}
	if problems := CheckPost(text+" NaN", snap, "fuel"); len(problems) == 0 {
		t.Fatal("NaN passed")
	}
	// Empty section.
	empty := strings.Replace(text, "Gasoline", "", 1)
	_ = empty
	blank := strings.Replace(text, "🇬🇧 Fuel prices — effective Oct 20–26, 2026\nGasoline −₱4.30/L · Diesel +₱2.60/L · Kerosene −₱1.00/L\nPer 10 L: gasoline −₱43.00, diesel +₱26.00\nNCR common pump prices before this change (DOE, Oct 13–19): RON 91 ₱87.90 · RON 95 ₱88.90 · Diesel ₱91.40", "  ", 1)
	if problems := CheckPost(blank, snap, "fuel"); len(problems) == 0 {
		t.Fatal("empty section passed")
	}
	if problems := CheckPost(text, snap, "bogus"); len(problems) == 0 {
		t.Fatal("bogus kind passed")
	}
}
