package pyjson

import (
	"encoding/json"
	"strings"
	"testing"
)

// Expected values are Python repr() outputs (verified with the repo's
// .venv/bin/python).
func TestPyFloatMatchesPythonRepr(t *testing.T) {
	cases := map[float64]string{
		45.0:        "45.0",
		52.0:        "52.0",
		240.0:       "240.0",
		8.5:         "8.5",
		53.456:      "53.456",
		53.46:       "53.46",
		62.968:      "62.968",
		62.799:      "62.799",
		10.0:        "10.0",
		0.0:         "0.0",
		0.1:         "0.1",
		0.0001:      "0.0001",
		0.375:       "0.375",
		2.675:       "2.675",
		8.53:        "8.53",
		270.0:       "270.0",
		310.0:       "310.0",
		307.5:       "307.5",
		47.5:        "47.5",
		100.0:       "100.0",
		-3.25:       "-3.25",
		123456.0:    "123456.0",
		1e16:        "1e+16",
		1.5e-05:     "1.5e-05",
		1e-05:       "1e-05",
		123456789.0: "123456789.0",
	}
	for f, want := range cases {
		if got := PyFloat(f); got != want {
			t.Errorf("PyFloat(%v) = %q, want %q", f, got, want)
		}
	}
}

func TestMarshalIndentSeparators(t *testing.T) {
	v := NewObj("a", Float(45.0), "b", nil, "c", "Piñas")
	got := string(MarshalIndent(v))
	want := "{\n  \"a\": 45.0,\n  \"b\": null,\n  \"c\": \"Piñas\"\n}"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestMarshalCompact(t *testing.T) {
	v := NewObj("d", []any{"2026-10-09"}, "m", NewObj("1", []any{Float(50.0), nil}))
	got := string(Marshal(v))
	want := `{"d":["2026-10-09"],"m":{"1":[50.0,null]}}`
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	// No HTML escaping (ensure_ascii=False + no <>& escaping).
	v2 := NewObj("u", "a<b&c>")
	if got := string(Marshal(v2)); got != `{"u":"a<b&c>"}` {
		t.Fatalf("got %q", got)
	}
	if !json.Valid(Marshal(v)) {
		t.Fatal("invalid JSON")
	}
	_ = strings.TrimSpace
}
