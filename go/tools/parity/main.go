// Command parity compares Go PDF parsing output against the Python
// reference dump (tools/parity/ref_dump.py) for every PDF in a raw dir.
//
// Usage:
//
//	go run ./tools/parity -raw ../data/raw -ref /workspace/presyo-scratch/ref.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/parse"
)

// RefRow is one reference (or Go) output row.
type RefRow struct {
	File      string   `json:"file"`
	Layout    string   `json:"layout"`
	Date      *string  `json:"date"`
	Market    string   `json:"market"`
	Commodity string   `json:"commodity"`
	Spec      *string  `json:"spec"`
	Unit      string   `json:"unit"`
	Price     *float64 `json:"price"`
	PriceLow  *float64 `json:"price_low"`
	PriceHigh *float64 `json:"price_high"`
	Error     string   `json:"error"`
}

func round2(x float64) float64 {
	return parse.Round2(x)
}

func sameFloat(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return round2(*a) == round2(*b)
}

func sameString(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func main() {
	raw := flag.String("raw", "../data/raw", "directory of *.pdf files")
	ref := flag.String("ref", "/workspace/presyo-scratch/ref.jsonl", "reference JSONL path")
	maxDiffs := flag.Int("max-diffs", 60, "max diffs to print in detail (0 = all)")
	flag.Parse()

	// Load reference.
	refRows := map[string][]RefRow{}
	refErr := map[string]string{}
	fh, err := os.Open(*ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open ref: %v\n", err)
		os.Exit(2)
	}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r RefRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			fmt.Fprintf(os.Stderr, "error: bad ref line: %v\n", err)
			os.Exit(2)
		}
		if r.Error != "" {
			refErr[r.File] = r.Error
		} else {
			refRows[r.File] = append(refRows[r.File], r)
		}
	}
	fh.Close()
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error: read ref: %v\n", err)
		os.Exit(2)
	}

	pdfs, err := filepath.Glob(filepath.Join(*raw, "*.pdf"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: glob: %v\n", err)
		os.Exit(2)
	}
	sort.Strings(pdfs)

	total, matched := 0, 0
	diffs := 0
	printed := 0
	show := func(format string, args ...any) {
		diffs++
		if *maxDiffs == 0 || printed < *maxDiffs {
			fmt.Printf(format, args...)
			printed++
		}
	}

	for _, pdfPath := range pdfs {
		file := filepath.Base(pdfPath)
		markets, hint, layout, gerr := parse.ParsePDFWithLayout(pdfPath)
		var grows []RefRow
		var gerrStr string
		if gerr != nil {
			gerrStr = gerr.Error()
		} else {
			date := parse.ReportDate(pdfPath, hint)
			var dateS *string
			if date != nil {
				s := date.String()
				dateS = &s
			}
			for _, m := range parse.SortedMarkets(markets) {
				comms := make([]string, 0, len(markets[m]))
				for c := range markets[m] {
					comms = append(comms, c)
				}
				sort.Strings(comms)
				for _, c := range comms {
					e := markets[m][c]
					grows = append(grows, RefRow{
						File: file, Layout: layout, Date: dateS,
						Market: m, Commodity: c,
						Spec: e.Spec, Unit: e.Unit,
						Price: e.Price, PriceLow: e.PriceLow, PriceHigh: e.PriceHigh,
					})
				}
			}
		}

		rrows := refRows[file]
		rerr, hasRErr := refErr[file]
		_, hasRef := refRows[file]
		if !hasRef && !hasRErr {
			total++
			show("DIFF %s: file not in reference (regenerate ref.jsonl)\n", file)
			continue
		}
		if gerr != nil || hasRErr {
			if gerr != nil && hasRErr {
				matched++ // both errored; count file as matched
				total++
				continue
			}
			total++
			if gerr != nil {
				show("DIFF %s: go error %q, ref has %d rows\n", file, gerrStr, len(rrows))
			} else {
				show("DIFF %s: ref error %q, go has %d rows\n", file, rerr, len(grows))
			}
			continue
		}

		// Compare row sets.
		key := func(r RefRow) string { return r.Market + "\x00" + r.Commodity }
		rmap := map[string]RefRow{}
		for _, r := range rrows {
			rmap[key(r)] = r
		}
		gmap := map[string]RefRow{}
		for _, r := range grows {
			gmap[key(r)] = r
		}
		fileTotal := 0
		fileMatch := 0
		for k, r := range rmap {
			fileTotal++
			g, ok := gmap[k]
			if !ok {
				show("DIFF %s: missing row market=%q commodity=%q\n", file, r.Market, r.Commodity)
				continue
			}
			var problems []string
			if g.Layout != r.Layout {
				problems = append(problems, fmt.Sprintf("layout %q != %q", g.Layout, r.Layout))
			}
			if !sameString(g.Date, r.Date) {
				problems = append(problems, fmt.Sprintf("date %v != %v", strp(g.Date), strp(r.Date)))
			}
			if !sameString(g.Spec, r.Spec) {
				problems = append(problems, fmt.Sprintf("spec %v != %v", strp(g.Spec), strp(r.Spec)))
			}
			if g.Unit != r.Unit {
				problems = append(problems, fmt.Sprintf("unit %q != %q", g.Unit, r.Unit))
			}
			if !sameFloat(g.Price, r.Price) {
				problems = append(problems, fmt.Sprintf("price %v != %v", fltp(g.Price), fltp(r.Price)))
			}
			if !sameFloat(g.PriceLow, r.PriceLow) {
				problems = append(problems, fmt.Sprintf("low %v != %v", fltp(g.PriceLow), fltp(r.PriceLow)))
			}
			if !sameFloat(g.PriceHigh, r.PriceHigh) {
				problems = append(problems, fmt.Sprintf("high %v != %v", fltp(g.PriceHigh), fltp(r.PriceHigh)))
			}
			if len(problems) > 0 {
				show("DIFF %s: market=%q commodity=%q: %s\n", file, r.Market, r.Commodity, join(problems))
			} else {
				fileMatch++
			}
		}
		for k, g := range gmap {
			if _, ok := rmap[k]; !ok {
				fileTotal++
				show("DIFF %s: extra row market=%q commodity=%q\n", file, g.Market, g.Commodity)
			}
		}
		total += fileTotal
		matched += fileMatch
	}

	rate := 100.0
	if total > 0 {
		rate = 100 * float64(matched) / float64(total)
	}
	fmt.Printf("parity: %d/%d rows match (%.2f%%), %d diffs\n", matched, total, rate, diffs)
	if diffs > printed {
		fmt.Printf("(%d further diffs omitted; use -max-diffs 0 to show all)\n", diffs-printed)
	}
	if diffs > 0 {
		os.Exit(1)
	}
}

func strp(s *string) string {
	if s == nil {
		return "<null>"
	}
	return fmt.Sprintf("%q", *s)
}

func fltp(f *float64) string {
	if f == nil {
		return "<null>"
	}
	return fmt.Sprintf("%v", *f)
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "; "
		}
		out += s
	}
	return out
}
