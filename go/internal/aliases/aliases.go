// Package aliases ports presyo_bantay/aliases.py: canonical commodity
// names (typo/qualifier/case/whitespace drift collapsed to one series).
//
// Only names that clearly mean the same item *and spec* are merged; related
// specs stay separate but share a group id for the site to show together.
package aliases

import (
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/parse"
)

// CanonicalAliases mirrors CANONICAL_ALIASES.
var CanonicalAliases = map[string][]string{
	"Premium Rice (Local)": {
		"Premium Rice (Loca)",             // typo seen 2026-08-18/19
		"PREMIUM RICE (Local, 5% broken)", // DA premium rice = 5% broken
		"Premium Rice (Local, 5% broken)",
	},
	"Premium Rice (Imported)": {
		"PREMIUM RICE (Imported, 5% broken)",
		"Premium Rice (Imported, 5% broken)",
	},
}

// KnownCanonicals mirrors KNOWN_CANONICALS.
var KnownCanonicals = []string{
	"Ampalaya",
	"Bangus",
	"Cabbage",
	"Calamansi",
	"Carrots (Local)",
	"Chayote",
	"Chili (Green)",
	"Egg (Medium)",
	"Eggplant",
	"Fresh Pork (Kasim) Imported",
	"Fresh Pork (Kasim) Local",
	"Fresh Pork Kasim/Pigue",
	"Fresh Pork Liempo",
	"Fresh Pork Liempo (Imported)",
	"Fresh Pork Liempo (Local)",
	"Fresh Whole Chicken",
	"Galunggong (Local)",
	"Pechay Baguio",
	"Premium Rice (Imported)",
	"Premium Rice (Local)",
	"Red Chili (Local)",
	"Red Onion (Local)",
	"Regular Milled (Local)",
	"Sugar (Refined)",
	"Sugar (Washed)",
	"Tilapia",
	"Tomato",
	"Well-milled Rice (Imported)",
	"Well-milled Rice (Local)",
	"White Onion (Local)",
	"White Potato (Local)",
}

// CommodityGroups mirrors COMMODITY_GROUPS.
var CommodityGroups = map[string]string{
	"Fresh Pork (Kasim) Local":     "pork_kasim",
	"Fresh Pork (Kasim) Imported":  "pork_kasim",
	"Fresh Pork Kasim/Pigue":       "pork_kasim",
	"Fresh Pork Liempo":            "pork_liempo",
	"Fresh Pork Liempo (Local)":    "pork_liempo",
	"Fresh Pork Liempo (Imported)": "pork_liempo",
	"Sugar (Washed)":               "sugar",
	"Sugar (Refined)":              "sugar",
}

// normKey mirrors _norm_key: case/whitespace/asterisk-insensitive match key.
func normKey(name string) string {
	cleaned := strings.ReplaceAll(name, "*", " ")
	return strings.Join(strings.Fields(strings.ToLower(cleaned)), " ")
}

var aliasToCanonical = map[string]string{}

func init() {
	for canonical, aliases := range CanonicalAliases {
		aliasToCanonical[normKey(canonical)] = canonical
		for _, alias := range aliases {
			if _, ok := aliasToCanonical[normKey(alias)]; !ok {
				aliasToCanonical[normKey(alias)] = canonical
			}
		}
	}
	for _, known := range KnownCanonicals {
		if _, ok := aliasToCanonical[normKey(known)]; !ok {
			aliasToCanonical[normKey(known)] = known
		}
	}
}

func tidy(name string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(name, "*", " ")), " ")
}

// well-known market -> city mapping lives in store; category logic mirrors
// db.categorize_commodity (also re-exported by store to avoid an import
// cycle: store -> aliases -> parse).
var fish = map[string]bool{"bangus": true, "tilapia": true, "galunggong (local)": true}

var vegetables = map[string]bool{
	"ampalaya": true, "eggplant": true, "tomato": true, "cabbage": true,
	"carrots (local)": true, "chayote": true, "pechay baguio": true,
	"white potato (local)": true,
}

var spices = map[string]bool{
	"red onion (local)": true, "white onion (local)": true,
	"chili (green)": true, "red chili (local)": true, "calamansi": true,
}

// CategorizeCommodity mirrors db.categorize_commodity.
func CategorizeCommodity(name string) string {
	lowered := strings.Join(strings.Fields(strings.ToLower(name)), " ")
	if strings.Contains(lowered, "rice") || strings.Contains(lowered, "milled") {
		return "rice"
	}
	if strings.Contains(lowered, "egg") && !strings.Contains(lowered, "eggplant") {
		return "egg"
	}
	if fish[lowered] || strings.Contains(lowered, "bangus") ||
		strings.Contains(lowered, "tilapia") || strings.Contains(lowered, "galunggong") {
		return "fish"
	}
	if vegetables[lowered] {
		return "vegetables"
	}
	if spices[lowered] || strings.Contains(lowered, "onion") ||
		strings.Contains(lowered, "chili") || strings.Contains(lowered, "calamansi") {
		return "spices"
	}
	if strings.Contains(lowered, "sugar") {
		return "sugar"
	}
	if strings.Contains(lowered, "pork") {
		return "pork"
	}
	if strings.Contains(lowered, "chicken") {
		return "poultry"
	}
	return "other"
}

// Canonicalize mirrors aliases.canonicalize: map a raw commodity name to
// (canonical, spec, unit, category).
func Canonicalize(name string) (canonical string, spec *string, unit string, category string) {
	canonical, ok := aliasToCanonical[normKey(name)]
	if !ok {
		canonical = tidy(name)
	}
	spec = parse.InferSpec(canonical)
	unit = parse.InferUnit(canonical)
	return canonical, spec, unit, CategorizeCommodity(canonical)
}

// GroupFor mirrors aliases.group_for.
func GroupFor(canonicalName string) *string {
	if g, ok := CommodityGroups[canonicalName]; ok {
		s := g
		return &s
	}
	canonical, _, _, _ := Canonicalize(canonicalName)
	if g, ok := CommodityGroups[canonical]; ok {
		s := g
		return &s
	}
	return nil
}
