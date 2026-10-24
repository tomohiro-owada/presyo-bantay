// Package pyjson writes JSON byte-identical to Python's json module for
// the payloads this project produces.
//
// Two spots need exact compatibility with Python output:
//
//   - parse snapshots: json.dump(payload, indent=2, ensure_ascii=False)
//   - export snapshots: json.dumps(payload, ensure_ascii=False,
//     separators=(",", ":"))
//
// Go's encoding/json differs in two ways: it escapes <, >, & by default
// (disabled here via SetEscapeHTML(false)) and it formats integral floats
// as "45" where Python writes "45.0". Float handles that via PyFloat,
// which mirrors CPython's float_repr_style "short" formatting.
package pyjson

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Float formats like Python float.__repr__: shortest round-trip digits,
// with ".0" for integral values (45.0, not 45) and Python-style exponents.
type Float float64

// MarshalJSON implements json.Marshaler.
func (f Float) MarshalJSON() ([]byte, error) {
	return []byte(PyFloat(float64(f))), nil
}

// PyFloat formats f exactly like Python repr(f) for finite values.
func PyFloat(f float64) string {
	if math.IsNaN(f) {
		return "NaN" // unreachable for our data; mirrors allow_nan output
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	// Shortest digits + base-10 exponent, e.g. "8.53e+00", "1.5e-05".
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, ok := strings.Cut(s, "e")
	if !ok {
		return s
	}
	digits := strings.Replace(mant, ".", "", 1)
	neg := false
	if strings.HasPrefix(digits, "-") {
		neg = true
		digits = digits[1:]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0.0"
	}
	exp10, err := strconv.Atoi(exp)
	if err != nil {
		return s
	}
	// Go's %e prints mantissa in [1,10): value = 0.digits * 10^(e+1),
	// so the point sits after the first e+1 digits. Python (repr) uses
	// fixed notation when -4 < decpt <= 16, else exponential.
	point := exp10 + 1
	if point > -4 && point <= 16 {
		var b strings.Builder
		if neg {
			b.WriteByte('-')
		}
		if point <= 0 {
			b.WriteString("0." + strings.Repeat("0", -point) + digits)
		} else if point >= len(digits) {
			b.WriteString(digits + strings.Repeat("0", point-len(digits)) + ".0")
		} else {
			b.WriteString(digits[:point] + "." + digits[point:])
		}
		return b.String()
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteByte(digits[0])
	if len(digits) > 1 {
		b.WriteByte('.')
		b.WriteString(digits[1:])
	}
	b.WriteString("e")
	// Python prints the exponent with sign and no leading zeros
	// ("1e+16", "1e-05").
	e := exp10
	if e < 0 {
		b.WriteByte('-')
		e = -e
	} else {
		b.WriteByte('+')
	}
	// Python prints at least 2 exponent digits ("1e-05", "1e+16").
	es := strconv.Itoa(e)
	if len(es) < 2 {
		es = "0" + es
	}
	b.WriteString(es)
	return b.String()
}

// Obj is an ordered JSON object: keys marshal in slice order (unlike
// map[string]any, which encoding/json sorts). Values may be nested Obj,
// []any, string, bool, nil, Float/*Float or any other JSON-marshalable.
type Obj struct {
	Keys []string
	M    map[string]any
}

// NewObj builds an ordered object from key/value pairs.
func NewObj(pairs ...any) *Obj {
	o := &Obj{M: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		k, _ := pairs[i].(string)
		o.Keys = append(o.Keys, k)
		o.M[k] = pairs[i+1]
	}
	return o
}

// compact marshals one value without HTML escaping.
func compact(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// MarshalJSON implements json.Marshaler (compact form).
func (o *Obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := compact(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := compact(o.M[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Marshal renders compact JSON like
// json.dumps(payload, ensure_ascii=False, separators=(",", ":")).
func Marshal(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	out := b.Bytes()
	return bytes.TrimSuffix(out, []byte("\n"))
}

// MarshalIndent renders indented JSON like
// json.dump(payload, indent=2, ensure_ascii=False) (no trailing newline,
// which Python does not write either).
func MarshalIndent(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	out := b.Bytes()
	return bytes.TrimSuffix(out, []byte("\n"))
}
