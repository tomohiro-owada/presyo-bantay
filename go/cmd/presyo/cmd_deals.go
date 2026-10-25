package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
)

func dirOf(p string) string {
	d := filepath.Dir(p)
	if d == "" {
		return ""
	}
	return d
}

func cmdDeals(argv []string) int {
	fs := flag.NewFlagSet("deals", flag.ContinueOnError)
	signalsPath := fs.String("signals", "", "signals.json file (required)")
	adviceDir := fs.String("advice-dir", "", "advise out-dir to reuse llm texts from (optional)")
	outPath := fs.String("out", "", "output file (required)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *signalsPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo deals -signals FILE [-advice-dir DIR] -out FILE")
		return 1
	}
	signalsData, err := os.ReadFile(*signalsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	texts, textSource, model := advice.LoadTextsFromAdviceDir(signalsData, *adviceDir)
	data, err := advice.BuildDealsDoc(signalsData, texts, textSource, model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	// Companion fb_post.txt next to the -out file (advise writes it
	// into the out-dir instead).
	if dir := dirOf(*outPath); dir != "" {
		post, _ := advice.FBPostAndCheck(signalsData, texts)
		_ = os.WriteFile(dir+"/fb_post.txt", []byte(post+"\n"), 0o644)
	}
	fmt.Println(*outPath)
	return 0
}
