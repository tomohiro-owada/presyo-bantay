// Command presyo is the Go port of the presyo_bantay daily pipeline:
// parse, ingest, ingest-json, fx, export and version subcommands.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/export"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/fx"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/parse"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/pyjson"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/store"
)

// version is the binary version (presyo version). It is a var (not const)
// so deploy.sh can stamp it at build time:
// go build -ldflags "-X main.version=<git describe>".
var version = "0.1.0"

// UserAgent is sent on every DA and Frankfurter request.
const UserAgent = "PresyoBantay/0.1 (non-profit community price watch; https://github.com/tomohiro-owada/presyo-bantay)"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		usage()
		return 1
	}
	switch argv[0] {
	case "parse":
		return cmdParse(argv[1:])
	case "ingest":
		g, code := newIngesterFromFlags(argv[1:])
		if code != 0 {
			return code
		}
		return g.Run()
	case "ingest-json":
		return cmdIngestJSON(argv[1:])
	case "fx":
		return cmdFx(argv[1:])
	case "newdata":
		return cmdNewdata(argv[1:])
	case "export":
		return cmdExport(argv[1:])
	case "signals":
		return cmdSignals(argv[1:])
	case "region-signals":
		return cmdRegionSignals(argv[1:])
	case "regions":
		return cmdRegions(argv[1:])
	case "chart":
		return cmdChart(argv[1:])
	case "fbpost":
		return cmdFBPost(argv[1:])
	case "fbweekly":
		return cmdFBWeekly(argv[1:])
	case "advise":
		return cmdAdvise(argv[1:])
	case "advice-check":
		return cmdAdviceCheck(argv[1:])
	case "deals":
		return cmdDeals(argv[1:])
	case "daypages":
		return cmdDaypages(argv[1:])
	case "weekpages":
		return cmdWeekpages(argv[1:])
	case "version":
		fmt.Println("presyo " + version)
		return 0
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", argv[0])
		usage()
		return 1
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: presyo <parse|ingest|ingest-json|fx|newdata|export|signals|region-signals|regions|chart|fbpost|fbweekly|advise|advice-check|deals|daypages|weekpages|version> [flags]")
}

// readMetaSourceURL mirrors cli._read_meta_source_url.
func readMetaSourceURL(rawDir, pdfPath string) *string {
	filename := filepath.Base(pdfPath)
	for _, candidate := range []string{
		filepath.Join(rawDir, filename+".meta.json"),
		pdfPath + ".meta.json",
	} {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		// Lenient: ignore malformed meta files like Python does.
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if u, ok := m["source_url"].(string); ok && u != "" {
			s := u
			return &s
		}
	}
	return nil
}

func cmdParse(argv []string) int {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	outDir := fs.String("out-dir", filepath.Join("data", "json"), "JSON output directory")
	rawDir := fs.String("raw", filepath.Join("data", "raw"), "raw PDF directory (meta.json lookup)")
	sourceURL := fs.String("source-url", "", "source URL of the PDF")
	moi := fs.Bool("markets-of-interest", false, "only include the 4 markets of interest")
	// Accept the PDF operand before or after flags (unlike Go's flag
	// package, which stops at the first positional).
	argv, positional := splitPositional(argv, 1)
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "usage: presyo parse PDF [-out-dir data/json]")
		return 1
	}
	return parsePDFToJSON(positional[0], *outDir, *rawDir, *sourceURL, *moi)
}

// splitPositional pulls the single file operand out of argv so commands
// accept both `cmd PDF -flag x` and `cmd -flag x PDF`. If the first token
// is a non-flag it is the operand; otherwise the last non-flag token is.
func splitPositional(argv []string, n int) ([]string, []string) {
	_ = n
	if len(argv) == 0 {
		return argv, nil
	}
	if !strings.HasPrefix(argv[0], "-") {
		return argv[1:], argv[:1]
	}
	for i := len(argv) - 1; i >= 0; i-- {
		if !strings.HasPrefix(argv[i], "-") {
			out := make([]string, 0, len(argv)-1)
			out = append(out, argv[:i]...)
			out = append(out, argv[i+1:]...)
			return out, []string{argv[i]}
		}
	}
	return argv, nil
}

// parsePDFToJSON mirrors cli.cmd_parse: strict sanity, exit 2 on layout
// change, identical JSON output.
func parsePDFToJSON(pdfPath, outDir, rawDir, sourceURL string, moi bool) int {
	if _, err := os.Stat(pdfPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: file not found: %s\n", pdfPath)
		return 1
	}
	markets, textHint, layout, marketOrder, commodityOrder, err :=
		parse.ParsePDFOrdered(pdfPath)
	if err != nil {
		if _, ok := err.(*parse.LayoutChangedError); ok {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 2
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	date := parse.ParseDateFromFilename(pdfPath)
	if date == nil {
		date = textHint
	}
	if date == nil {
		fmt.Fprintln(os.Stderr, "error: Layout may have changed: could not determine report date from filename or PDF text")
		return 2
	}
	if err := parse.RunSanityChecks(markets, layout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 2
	}
	if moi {
		selected := map[string]map[string]parse.Entry{}
		var selOrder []string
		for _, wanted := range parse.MarketsOfInterest {
			if actual := parse.MatchMarket(markets, wanted); actual != "" {
				selected[actual] = markets[actual]
				selOrder = append(selOrder, actual)
			}
		}
		markets = selected
		marketOrder = selOrder
	}
	var src *string
	if sourceURL != "" {
		src = &sourceURL
	} else {
		src = readMetaSourceURL(rawDir, pdfPath)
	}
	payload := snapshotJSON(date.String(), src, layout, markets, marketOrder, commodityOrder)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	outPath := filepath.Join(outDir, date.String()+".json")
	if err := os.WriteFile(outPath, pyjson.MarshalIndent(payload), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	fmt.Println(outPath)
	return 0
}

// snapshotJSON builds the ordered cmd_parse payload object.
func snapshotJSON(date string, sourceURL *string, layout string,
	markets map[string]map[string]parse.Entry,
	marketOrder []string, commodityOrder map[string][]string) *pyjson.Obj {
	marketsObj := &pyjson.Obj{M: map[string]any{}}
	for _, market := range marketOrder {
		items, ok := markets[market]
		if !ok {
			continue
		}
		comms := &pyjson.Obj{M: map[string]any{}}
		for _, commodity := range commodityOrder[market] {
			e, ok := items[commodity]
			if !ok {
				continue
			}
			comms.Keys = append(comms.Keys, commodity)
			comms.M[commodity] = pyjson.NewObj(
				"spec", strOrNil(e.Spec),
				"unit", e.Unit,
				"price", floatOrNil(e.Price),
				"price_low", floatOrNil(e.PriceLow),
				"price_high", floatOrNil(e.PriceHigh),
			)
		}
		marketsObj.Keys = append(marketsObj.Keys, market)
		marketsObj.M[market] = comms
	}
	var src any
	if sourceURL != nil {
		src = *sourceURL
	}
	return pyjson.NewObj(
		"date", date,
		"source_url", src,
		"layout", layout,
		"markets", marketsObj,
	)
}

func strOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func floatOrNil(f *float64) any {
	if f == nil {
		return nil
	}
	return pyjson.Float(*f)
}

func cmdIngestJSON(argv []string) int {
	fs := flag.NewFlagSet("ingest-json", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path")
	allowSynthetic := fs.Bool("allow-synthetic", false, "allow synthetic payloads")
	argv, positional := splitPositional(argv, 1)
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "usage: presyo ingest-json PATH [-db ...]")
		return 1
	}
	src := positional[0]
	var files []string
	fi, err := os.Stat(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: file not found: %s\n", src)
		return 1
	}
	if fi.IsDir() {
		entries, err := os.ReadDir(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				files = append(files, filepath.Join(src, e.Name()))
			}
		}
		sort.Strings(files)
	} else {
		files = []string{src}
	}
	db, err := store.Connect(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	var nFiles, tMarkets, tCommodities, tPrices int
	var skipped []string
	for _, path := range files {
		m, c, n, wasSkipped, err := store.IngestFile(db, path, *allowSynthetic)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", path, err.Error())
			return 1
		}
		if wasSkipped {
			skipped = append(skipped, filepath.Base(path))
			fmt.Fprintf(os.Stderr, "warning: skipping synthetic file %s (use --allow-synthetic to ingest)\n", path)
			continue
		}
		nFiles++
		tMarkets += m
		tCommodities += c
		tPrices += n
	}
	fmt.Printf("ingested %d file(s) into %s: %d market rows, %d commodity rows, %d price rows",
		nFiles, *dbPath, tMarkets, tCommodities, tPrices)
	if len(skipped) > 0 {
		fmt.Printf(" (skipped synthetic: %s)", strings.Join(skipped, ", "))
	}
	fmt.Println()
	return 0
}

func cmdFx(argv []string) int {
	fs := flag.NewFlagSet("fx", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path")
	sinceFlag := fs.String("since", "", "start date YYYY-MM-DD")
	untilFlag := fs.String("until", "", "end date YYYY-MM-DD")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	db, err := store.Connect(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	since := *sinceFlag
	if since == "" {
		since, err = fx.DefaultSince(db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
	}
	until := *untilFlag
	if until == "" {
		until = fx.ManilaToday()
	}
	if since == "" {
		fmt.Println("fx: no price dates in DB; nothing to do")
		return 0
	}
	if since > until {
		fmt.Printf("fx: since %s > until %s; nothing to do\n", since, until)
		return 0
	}
	total := 0
	for i, chunk := range fx.SplitYearly(since, until) {
		if i > 0 {
			fx.Sleep(fx.ChunkDelay)
		}
		rates, err := fx.FetchRates(chunk[0], chunk[1], fx.FXBase, fx.FXQuote)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
		n, err := fx.UpsertRates(db, rates, fx.FXBase, fx.FXQuote, fx.FXSource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
		total += n
	}
	fmt.Printf("fx: %d rate(s) for %s..%s\n", total, since, until)
	return 0
}

func cmdExport(argv []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	dbPath := fs.String("db", filepath.Join("data", "presyo.db"), "SQLite database path")
	outDir := fs.String("out", filepath.Join("site", "public", "data"), "site data output directory")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	db, err := store.Connect(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer db.Close()
	written, err := export.ExportSite(db, *outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	var total int64
	for _, p := range written {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	fmt.Printf("export: wrote %d file(s) to %s (%d bytes)\n", len(written), *outDir, total)
	return 0
}
