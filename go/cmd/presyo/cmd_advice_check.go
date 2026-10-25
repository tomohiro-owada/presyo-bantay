package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
)

func cmdAdviceCheck(argv []string) int {
	fs := flag.NewFlagSet("advice-check", flag.ContinueOnError)
	signalsPath := fs.String("signals", "", "signals.json file (required)")
	advicePath := fs.String("advice", "", "advice markdown file (required)")
	outPath := fs.String("out", "", "check.json output file (default: stdout)")
	if err := fs.Parse(argv); err != nil {
		return 1
	}
	if *signalsPath == "" || *advicePath == "" {
		fmt.Fprintln(os.Stderr, "usage: presyo advice-check -signals FILE -advice FILE [-out check.json]")
		return 1
	}
	signalsData, err := os.ReadFile(*signalsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	adviceText, err := os.ReadFile(*advicePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	rep, err := advice.Check(signalsData, string(adviceText))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	// v3 jargon lint shares check.json under "lint" and fails the run.
	// The nearby-footnote check needs signals context (displayed items:
	// all deals + first 3 pricier).
	usesNearby := true
	var doc struct {
		Deals []struct {
			Basis string `json:"basis"`
		} `json:"deals"`
		Pricier []struct {
			Basis string `json:"basis"`
		} `json:"pricier"`
		UsesNearbyBasis *bool `json:"uses_nearby_basis"`
	}
	if jerr := json.Unmarshal(signalsData, &doc); jerr == nil {
		if doc.Deals == nil && doc.Pricier == nil {
			if doc.UsesNearbyBasis != nil {
				usesNearby = *doc.UsesNearbyBasis
			}
		} else {
			usesNearby = false
			for _, d := range doc.Deals {
				if d.Basis == "nearby_median" {
					usesNearby = true
					break
				}
			}
			if !usesNearby {
				shown := doc.Pricier
				if len(shown) > 3 {
					shown = shown[:3]
				}
				for _, p := range shown {
					if p.Basis == "nearby_median" {
						usesNearby = true
						break
					}
				}
			}
		}
	}
	advice.ApplyLint(rep, advice.LintAdviceWithBasis(string(adviceText), usesNearby))
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	data = append(data, '\n')
	if *outPath == "" {
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return 1
		}
	} else if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		return 1
	}
	fmt.Fprintf(os.Stderr, "check: %s (total=%d ok=%d derived=%d unbound=%d flagged=%d lint=%d)\n",
		rep.Status, rep.Total, rep.OK, rep.Derived, rep.Unbound, len(rep.Flagged), len(rep.Lint.Flags))
	for _, f := range rep.Flagged {
		fmt.Fprintf(os.Stderr, "  flagged [%s:%d] %q (%v %s) reason=%s commodities=%v\n",
			f.Section, f.Line, f.Snippet, f.Value, f.Kind, f.Reason, f.Commodities)
	}
	if rep.Status != advice.StatusPass {
		return 3
	}
	return 0
}
