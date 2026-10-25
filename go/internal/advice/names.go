package advice

import (
	"strings"
)

// NameEntry is one row of the single display-name table shared by the
// template, the prompt hints and the checker aliases.
type NameEntry struct {
	Canonical string
	TL        string // Taglish display name
	JA        string // Japanese display name
}

// NameTable lists every canonical commodity name the pipeline knows.
// TL follows common Manila names; JA follows the v2 prompt list.
var NameTable = []NameEntry{
	{"Pechay Baguio", "Pechay Baguio", "ペチャイ・バギオ（白菜の一種）"},
	{"Chayote", "Sayote", "サヨテ（ハヤトウリ）"},
	{"Tomato", "Kamatis", "トマト"},
	{"Eggplant", "Talong", "ナス"},
	{"Red Chili (Local)", "Pulang sili", "赤唐辛子"},
	{"Chili (Green)", "Siling berde", "青唐辛子"},
	{"White Potato (Local)", "Patatas", "じゃがいも"},
	{"Carrots (Local)", "Karot", "にんじん"},
	{"Red Onion (Local)", "Sibuyas na pula", "赤玉ねぎ"},
	{"White Onion (Local)", "Sibuyas na puti", "白玉ねぎ"},
	{"Fresh Whole Chicken", "Manok", "丸鶏"},
	{"Egg (Medium)", "Itlog", "卵"},
	{"Premium Rice (Imported)", "Bigas", "米"},
	{"Premium Rice (Local)", "Bigas", "米"},
	{"Well-milled Rice (Imported)", "Bigas", "米"},
	{"Well-milled Rice (Local)", "Bigas", "米"},
	{"Regular Milled (Local)", "Bigas", "米"},
	{"Sugar (Refined)", "Asukal", "砂糖"},
	{"Sugar (Washed)", "Asukal", "砂糖"},
	{"Calamansi", "Kalamansi", "カラマンシー"},
	{"Ampalaya", "Ampalaya", "ゴーヤ"},
	{"Cabbage", "Repolyo", "キャベツ"},
	{"Galunggong (Local)", "Galunggong", "ガルンゴン（アジの一種）"},
	{"Bangus", "Bangus", "バングス（ミルクフィッシュ）"},
	{"Tilapia", "Tilapia", "ティラピア"},
	{"Fresh Pork (Kasim) Imported", "Kasim", "豚肩ロース（カシム）"},
	{"Fresh Pork (Kasim) Local", "Kasim", "豚肩ロース（カシム）"},
	{"Fresh Pork Kasim/Pigue", "Kasim", "豚肩ロース（カシム）"},
	{"Fresh Pork Liempo", "Liempo", "豚バラ（リエンポ）"},
	{"Fresh Pork Liempo (Imported)", "Liempo", "豚バラ（リエンポ）"},
	{"Fresh Pork Liempo (Local)", "Liempo", "豚バラ（リエンポ）"},
}

var nameByCanonical = func() map[string]NameEntry {
	m := map[string]NameEntry{}
	for _, e := range NameTable {
		m[e.Canonical] = e
	}
	return m
}()

// DisplayTL returns the Taglish display name for a canonical commodity.
func DisplayTL(canonical string) string {
	if e, ok := nameByCanonical[canonical]; ok {
		return e.TL
	}
	return stripParen(canonical)
}

// DisplayJA returns the Japanese display name for a canonical commodity.
func DisplayJA(canonical string) string {
	if e, ok := nameByCanonical[canonical]; ok {
		return e.JA
	}
	return stripParen(canonical)
}

// DisplayEN is the canonical name without "(Local)".
func DisplayEN(canonical string) string {
	if strings.Contains(canonical, "(Local)") {
		s := strings.Replace(canonical, " (Local)", "", 1)
		s = strings.Replace(s, "(Local)", "", 1)
		return strings.Join(strings.Fields(s), " ")
	}
	return canonical
}

// MarketJA maps short market names to katakana.
var MarketJA = map[string]string{
	"Las Piñas": "ラス・ピニャス",
	"Alabang":   "アラバン",
	"La Huerta": "ラ・ウエルタ",
	"Bicutan":   "ビクタン",
	"Pasay":     "パサイ",
}

// MarketDisplayJA returns the katakana market name.
func MarketDisplayJA(short string) string {
	if j, ok := MarketJA[short]; ok {
		return j
	}
	return short
}

// nameHintLines renders the name table for prompt hints.
func nameHintLines() string {
	var b strings.Builder
	for _, e := range NameTable {
		b.WriteString(e.Canonical + " -> TL: " + e.TL + " / EN: " + DisplayEN(e.Canonical) + " / JA: " + e.JA + "\n")
	}
	return b.String()
}

// tableVariants returns extra alias variants derived from the name table
// (TL + stripped EN forms). Used by the checker so template display names
// always bind.
type tableVariant struct {
	variant string
	latin   bool
	target  string
}

func tableVariants() []tableVariant {
	var out []tableVariant
	for _, e := range NameTable {
		out = append(out, tableVariant{e.TL, true, e.Canonical})
		en := stripParen(e.Canonical)
		if !strings.EqualFold(en, e.Canonical) {
			out = append(out, tableVariant{en, true, e.Canonical})
		}
		if e.JA != "" {
			out = append(out, tableVariant{e.JA, false, e.Canonical})
		}
	}
	return out
}
