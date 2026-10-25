package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/utilities"
)

// cmdUtilities dispatches the household-utilities prototype commands
// (dry run only: fetch downloads public sources, render is offline;
// nothing ever posts).
func cmdUtilities(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: presyo utilities <fetch|render> [flags]")
		return 1
	}
	switch argv[0] {
	case "fetch":
		return cmdUtilitiesFetch(argv[1:])
	case "render":
		return cmdUtilitiesRender(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "error: unknown utilities command %q\n", argv[0])
		fmt.Fprintln(os.Stderr, "usage: presyo utilities <fetch|render> [flags]")
		return 1
	}
}

// cmdUtilitiesFetch is the ONLY network-touching utilities mode: it
// downloads the DOE/Meralco/Maynilad public pages politely and writes
// an indented snapshot JSON. Exit 0 even with partial source errors
// (printed to stderr), 1 if nothing parsed.
func cmdUtilitiesFetch(argv []string) int {
	fs := flag.NewFlagSet("utilities fetch", flag.ContinueOnError)
	cacheDir := fs.String("cache", "", "PDF cache directory (required)")
	outPath := fs.String("out", "", "snapshot JSON output file (required)")
	city := fs.String("city", "Las Piñas", "Maynilad city")
	maxOil := fs.Int("max-oil", 6, "max oil-monitor PDFs to parse")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *cacheDir == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo utilities fetch -cache DIR -out snapshot.json [-city \"Las Piñas\"] [-max-oil 6]")
		return 1
	}
	snap, err := utilities.Fetch(context.Background(), utilities.FetchOptions{
		Client:         &http.Client{Timeout: 60 * time.Second},
		CacheDir:       *cacheDir,
		MinDelay:       5 * time.Second,
		MaxOilMonitors: *maxOil,
		MaxRequests:    30,
		City:           *city,
		UserAgent:      UserAgent,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities fetch: error: %s\n", err.Error())
		return 1
	}
	for _, e := range snap.Errors {
		fmt.Fprintf(os.Stderr, "utilities fetch: WARNING %s\n", e)
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities fetch: error: %s\n", err.Error())
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "utilities fetch: error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(*outPath, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "utilities fetch: error: %s\n", err.Error())
		return 1
	}
	fmt.Println(*outPath)
	if snap.Empty() {
		fmt.Fprintln(os.Stderr, "utilities fetch: nothing parsed")
		return 1
	}
	return 0
}

// renderUtilitiesChart renders the post chart for kind.
func renderUtilitiesChart(kind string, snap *utilities.Snapshot) (*image.RGBA, error) {
	if kind == "fuel" {
		return fbchart.RenderFuelWeekly(snap)
	}
	return fbchart.RenderBills(snap)
}

// utilitiesCheckDoc is the <kind>.check.json document.
type utilitiesCheckDoc struct {
	OK       bool     `json:"ok"`
	Problems []string `json:"problems"`
}

// cmdUtilitiesRender is offline: it renders <kind>.txt, <kind>.png
// (compact) and <kind>.check.json from a snapshot file. Exit 3 when
// the checker fails.
func cmdUtilitiesRender(argv []string) int {
	fs := flag.NewFlagSet("utilities render", flag.ContinueOnError)
	snapPath := fs.String("snapshot", "", "snapshot JSON file (required)")
	kind := fs.String("kind", "", "post kind: fuel|bills (required)")
	outDir := fs.String("out-dir", "", "output directory (required)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *snapPath == "" || (*kind != "fuel" && *kind != "bills") || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo utilities render -snapshot FILE -kind fuel|bills -out-dir DIR")
		return 1
	}
	data, err := os.ReadFile(*snapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	var snap utilities.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	var text string
	if *kind == "fuel" {
		text, err = utilities.RenderFuelPost(&snap)
	} else {
		text, err = utilities.RenderBillsPost(&snap)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	chart, cerr := renderUtilitiesChart(*kind, &snap)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", cerr.Error())
		return 1
	}
	problems := utilities.CheckPost(text, &snap, *kind)
	doc := utilitiesCheckDoc{OK: len(problems) == 0, Problems: problems}
	if doc.Problems == nil {
		doc.Problems = []string{}
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(filepath.Join(*outDir, *kind+".txt"), []byte(text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	pngF, err := os.Create(filepath.Join(*outDir, *kind+".png"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	if err := fbchart.EncodeCompact(pngF, chart); err != nil {
		pngF.Close()
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	pngF.Close()
	checkData, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(filepath.Join(*outDir, *kind+".check.json"), append(checkData, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	fmt.Printf("rendered %s\n", *outDir)
	if !doc.OK {
		for _, p := range doc.Problems {
			fmt.Fprintf(os.Stderr, "utilities render: check: %s\n", p)
		}
		return 3
	}
	return 0
}
