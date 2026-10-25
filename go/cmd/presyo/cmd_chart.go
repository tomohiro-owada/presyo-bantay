package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"os"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/fbchart"
	"github.com/tomohiro-owada/presyo-bantay/go/internal/signals"
)

// cmdChart renders the 1080x1350 regional PNG from a region-signals file.
func cmdChart(argv []string) int {
	fs := flag.NewFlagSet("chart", flag.ContinueOnError)
	signalsPath := fs.String("signals", "", "region-signals JSON file (required)")
	outPath := fs.String("out", "", "output PNG file (required)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *signalsPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo chart -signals region.json -out file.png")
		return 1
	}
	data, err := os.ReadFile(*signalsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	var sig signals.RegionSignals
	if err := json.Unmarshal(data, &sig); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	img, err := fbchart.Render(&sig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	out, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	fmt.Println(*outPath)
	return 0
}
