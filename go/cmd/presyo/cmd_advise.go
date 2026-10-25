package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
)

func cmdAdvise(argv []string) int {
	fs := flag.NewFlagSet("advise", flag.ContinueOnError)
	signalsPath := fs.String("signals", "", "signals.json file (required)")
	outDir := fs.String("out-dir", "", "output directory (required)")
	model := fs.String("model", advice.DefaultModel, "opencode model")
	opencodeBin := fs.String("opencode", "opencode", "opencode binary")
	timeout := fs.String("timeout", "10m", "opencode call timeout (Go duration)")
	retries := fs.Int("retries", 1, "extra attempts if the call fails or sections are missing")
	noLLM := fs.Bool("no-llm", false, "skip the opencode call and use the template directly")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	dur, err := time.ParseDuration(*timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "advise: bad -timeout: %s\n", *timeout)
		return 1
	}
	return advice.Run(advice.Options{
		SignalsPath: *signalsPath,
		OutDir:      *outDir,
		Model:       *model,
		OpencodeBin: *opencodeBin,
		Timeout:     dur,
		Retries:     *retries,
		NoLLM:       *noLLM,
	})
}
