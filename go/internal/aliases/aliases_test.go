package aliases

import (
	"testing"
)

func TestCanonicalizeMerges(t *testing.T) {
	cases := map[string]string{
		"Premium Rice (Loca)":                "Premium Rice (Local)",
		"PREMIUM RICE (Imported, 5% broken)": "Premium Rice (Imported)",
		"PREMIUM RICE (Local, 5% broken)":    "Premium Rice (Local)",
		"premium rice (local)":               "Premium Rice (Local)",
		"  Bangus  ":                         "Bangus",
		"BANGUS":                             "Bangus",
		"Egg (Medium)*":                      "Egg (Medium)",
		"  tomato ":                          "Tomato",
	}
	for raw, want := range cases {
		got, _, _, _ := Canonicalize(raw)
		if got != want {
			t.Errorf("Canonicalize(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCanonicalizeKeepsDistinctSpecs(t *testing.T) {
	for _, raw := range []string{
		"Fresh Pork Kasim/Pigue",
		"Fresh Pork (Kasim) Local",
		"Fresh Pork Liempo",
		"Fresh Pork Liempo (Local)",
		"Sugar (Washed)",
		"Sugar (Refined)",
	} {
		if got, _, _, _ := Canonicalize(raw); got != raw {
			t.Errorf("Canonicalize(%q) = %q, want unchanged", raw, got)
		}
	}
}

func TestDifferentSpecsNotMergedButGrouped(t *testing.T) {
	a, _, _, _ := Canonicalize("Fresh Pork Kasim/Pigue")
	b, _, _, _ := Canonicalize("Fresh Pork (Kasim) Local")
	if a == b {
		t.Fatalf("kasim variants merged: %q", a)
	}
	c, _, _, _ := Canonicalize("Fresh Pork Liempo")
	d, _, _, _ := Canonicalize("Fresh Pork Liempo (Local)")
	if c == d {
		t.Fatalf("liempo variants merged: %q", c)
	}
	e, _, _, _ := Canonicalize("Sugar (Washed)")
	f, _, _, _ := Canonicalize("Sugar (Refined)")
	if e == f {
		t.Fatalf("sugar variants merged: %q", e)
	}
	groups := map[string]string{
		"Fresh Pork Kasim/Pigue":    "pork_kasim",
		"Fresh Pork (Kasim) Local":  "pork_kasim",
		"Fresh Pork Liempo":         "pork_liempo",
		"Fresh Pork Liempo (Local)": "pork_liempo",
		"Sugar (Washed)":            "sugar",
		"Sugar (Refined)":           "sugar",
	}
	for name, want := range groups {
		g := GroupFor(name)
		if g == nil || *g != want {
			t.Errorf("GroupFor(%q) = %v, want %q", name, g, want)
		}
	}
	if g := GroupFor("Bangus"); g != nil {
		t.Errorf("GroupFor(Bangus) = %q, want nil", *g)
	}
}

func TestCanonicalizeReturnsSpecUnitCategory(t *testing.T) {
	canonical, spec, unit, category := Canonicalize("Premium Rice (Loca)")
	if canonical != "Premium Rice (Local)" || spec == nil || *spec != "Local" ||
		unit != "per kg" || category != "rice" {
		t.Fatalf("got (%q, %v, %q, %q)", canonical, spec, unit, category)
	}
	canonical, spec, unit, category = Canonicalize("Egg (Medium)")
	if canonical != "Egg (Medium)" || spec == nil || *spec != "Medium" ||
		unit != "per piece" || category != "egg" {
		t.Fatalf("got (%q, %v, %q, %q)", canonical, spec, unit, category)
	}
	if _, _, _, category := Canonicalize("Bangus"); category != "fish" {
		t.Fatalf("Bangus category = %q", category)
	}
}

func TestAliasMapCoversKnownDBTypos(t *testing.T) {
	if _, ok := CanonicalAliases["Premium Rice (Local)"]; !ok {
		t.Fatal("missing Premium Rice (Local)")
	}
	if _, ok := CanonicalAliases["Premium Rice (Imported)"]; !ok {
		t.Fatal("missing Premium Rice (Imported)")
	}
}
