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
	"strings"
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
// downloads the DOE/Meralco/Maynilad public pages politely and merges
// the requested sections into the -out snapshot (sections not fetched
// this run keep their old data; per-section fetched_at recorded).
// Exit 0 even with partial source errors (printed to stderr), 1 if
// nothing is parsed at all.
func cmdUtilitiesFetch(argv []string) int {
	fs := flag.NewFlagSet("utilities fetch", flag.ContinueOnError)
	cacheDir := fs.String("cache", "", "PDF cache directory (required)")
	outPath := fs.String("out", "", "snapshot JSON output file (required)")
	city := fs.String("city", "Las Piñas", "Maynilad city")
	maxOil := fs.Int("max-oil", 6, "max oil-monitor PDFs to parse")
	sections := fs.String("sections", "", "comma-separated sections to fetch (default all: fuel,pump,lpg,meralco,water,fcda)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *cacheDir == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo utilities fetch -cache DIR -out snapshot.json [-city \"Las Piñas\"] [-max-oil 6] [-sections fuel,pump,lpg,meralco,water,fcda]")
		return 1
	}
	var wanted []string
	if strings.TrimSpace(*sections) == "" {
		wanted = append([]string(nil), utilities.FetchSections...)
	} else {
		valid := map[string]bool{}
		for _, s := range utilities.FetchSections {
			valid[s] = true
		}
		for _, s := range strings.Split(*sections, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !valid[s] {
				fmt.Fprintf(os.Stderr, "utilities fetch: error: unknown section %q (want one of fuel,pump,lpg,meralco,water,fcda)\n", s)
				return 1
			}
			wanted = append(wanted, s)
		}
		if len(wanted) == 0 {
			fmt.Fprintln(os.Stderr, "utilities fetch: error: no sections selected")
			return 1
		}
	}
	snap, err := utilities.Fetch(context.Background(), utilities.FetchOptions{
		Client:         &http.Client{Timeout: 60 * time.Second},
		CacheDir:       *cacheDir,
		MinDelay:       5 * time.Second,
		MaxOilMonitors: *maxOil,
		MaxRequests:    30,
		City:           *city,
		UserAgent:      UserAgent,
		Sections:       wanted,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "utilities fetch: error: %s\n", err.Error())
		return 1
	}
	for _, e := range snap.Errors {
		fmt.Fprintf(os.Stderr, "utilities fetch: WARNING %s\n", e)
	}
	merged := snap
	if data, rerr := os.ReadFile(*outPath); rerr == nil {
		var base utilities.Snapshot
		if jerr := json.Unmarshal(data, &base); jerr != nil {
			fmt.Fprintf(os.Stderr, "utilities fetch: error: bad existing snapshot: %s\n", jerr.Error())
			return 1
		}
		merged = utilities.MergeSnapshots(&base, snap, wanted)
	}
	data, err := json.MarshalIndent(merged, "", "  ")
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
	if merged.Empty() && len(merged.WaterRates) == 0 {
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
// (compact) and <kind>.check.json. Exit 3 when the checker fails.
// kind water renders one alert: -alert-id selects it (default first);
// -maintenance-json plus -city builds a water-only snapshot offline
// from a WP posts JSON file, and -past-sample marks sample output.
func cmdUtilitiesRender(argv []string) int {
	fs := flag.NewFlagSet("utilities render", flag.ContinueOnError)
	snapPath := fs.String("snapshot", "", "snapshot JSON file (required unless -maintenance-json)")
	kind := fs.String("kind", "", "post kind: fuel|bills|water (required)")
	outDir := fs.String("out-dir", "", "output directory (required)")
	alertID := fs.String("alert-id", "", "water alert ID (default first)")
	maintJSON := fs.String("maintenance-json", "", "WP posts JSON file for an offline water-only snapshot")
	city := fs.String("city", "Las Piñas", "city for -maintenance-json")
	pastSample := fs.Bool("past-sample", false, "mark output as a past-data sample")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if (*kind != "fuel" && *kind != "bills" && *kind != "water") || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo utilities render -snapshot FILE -kind fuel|bills|water -out-dir DIR [-alert-id ID] [-maintenance-json FILE -city C -past-sample]")
		return 1
	}
	if *kind == "water" {
		return cmdUtilitiesRenderWater(*snapPath, *outDir, *alertID, *maintJSON, *city, *pastSample)
	}
	if *snapPath == "" {
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
	return writeUtilitiesRender(*outDir, *kind, text, chart, utilities.CheckPost(text, &snap, *kind))
}

// cmdUtilitiesRenderWater renders one water alert offline.
func cmdUtilitiesRenderWater(snapPath, outDir, alertID, maintJSON, city string, pastSample bool) int {
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "utilities render: error: "+format+"\n", args...)
		return 1
	}
	var snap utilities.Snapshot
	if maintJSON != "" {
		data, err := os.ReadFile(maintJSON)
		if err != nil {
			return fail("%s", err.Error())
		}
		rows, err := utilities.ParseMaintenancePosts(data, city)
		if err != nil {
			return fail("%s", err.Error())
		}
		snap.Water = &utilities.WaterStatus{City: city, Maintenance: rows}
		snap.Alerts = utilities.BuildWaterAlerts(snap.Water)
	} else {
		if snapPath == "" {
			return fail("need -snapshot FILE or -maintenance-json FILE")
		}
		data, err := os.ReadFile(snapPath)
		if err != nil {
			return fail("%s", err.Error())
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			return fail("%s", err.Error())
		}
		if len(snap.Alerts) == 0 {
			snap.Alerts = utilities.BuildWaterAlerts(snap.Water)
		}
	}
	if len(snap.Alerts) == 0 {
		return fail("no water alerts")
	}
	alert := snap.Alerts[0]
	if alertID != "" {
		found := false
		for _, a := range snap.Alerts {
			if a.ID == alertID {
				alert, found = a, true
				break
			}
		}
		if !found {
			return fail("unknown alert id %q", alertID)
		}
	}
	text := utilities.RenderWaterAlertPost(alert, pastSample)
	chart, err := fbchart.RenderWaterAlert(&alert, pastSample)
	if err != nil {
		return fail("%s", err.Error())
	}
	prefix := "water-" + alert.ID
	if pastSample {
		prefix = fmt.Sprintf("water-alert_PAST-SAMPLE_%s", alert.Start.Format("2006-01-02"))
	}
	checkSnap := &utilities.Snapshot{Alerts: []utilities.WaterAlert{alert}}
	return writeUtilitiesRenderPrefixed(outDir, prefix, text, chart, utilities.CheckPost(text, checkSnap, "water"))
}

// writeUtilitiesRender writes <kind>.txt/.png/.check.json.
func writeUtilitiesRender(outDir, kind, text string, chart *image.RGBA, problems []string) int {
	return writeUtilitiesRenderPrefixed(outDir, kind, text, chart, problems)
}

// writeUtilitiesRenderPrefixed writes <prefix>.txt/.png/.check.json.
func writeUtilitiesRenderPrefixed(outDir, prefix, text string, chart *image.RGBA, problems []string) int {
	doc := utilitiesCheckDoc{OK: len(problems) == 0, Problems: problems}
	if doc.Problems == nil {
		doc.Problems = []string{}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(filepath.Join(outDir, prefix+".txt"), []byte(text+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	pngF, err := os.Create(filepath.Join(outDir, prefix+".png"))
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
	if err := os.WriteFile(filepath.Join(outDir, prefix+".check.json"), append(checkData, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "utilities render: error: %s\n", err.Error())
		return 1
	}
	fmt.Printf("rendered %s\n", outDir)
	if !doc.OK {
		for _, p := range doc.Problems {
			fmt.Fprintf(os.Stderr, "utilities render: check: %s\n", p)
		}
		return 3
	}
	return 0
}
