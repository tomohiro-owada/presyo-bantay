package advice

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

//go:embed prompt.tmpl
var promptTmpl string

// DefaultModel mirrors ADVICE_SPEC.md.
const DefaultModel = "opencode-go/muse-spark-1.3-contributor"

// RequiredHeaders are the exact section headers the model must emit.
var RequiredHeaders = []string{"## Taglish", "## English", "## 日本語"}

// insertHeaderNewlines inserts a newline before each required header when
// it appears glued mid-line (e.g. "...JSON.## Taglish"). Headers already
// at a line start are left alone, so line numbers are otherwise stable.
func insertHeaderNewlines(text string) string {
	for _, h := range RequiredHeaders {
		var b strings.Builder
		idx := 0
		for {
			pos := strings.Index(text[idx:], h)
			if pos < 0 {
				b.WriteString(text[idx:])
				break
			}
			abs := idx + pos
			if abs > 0 && text[abs-1] != '\n' {
				b.WriteString(text[idx:abs])
				b.WriteString("\n")
				b.WriteString(h)
				idx = abs + len(h)
			} else {
				b.WriteString(text[idx : abs+len(h)])
				idx = abs + len(h)
			}
		}
		text = b.String()
	}
	return text
}

// NormalizeAdvice splits glued headers onto their own lines (see
// insertHeaderNewlines) and strips everything before the first section
// header, so a preamble glued to "## Taglish" cannot pollute the advice.
func NormalizeAdvice(text string) string {
	text = insertHeaderNewlines(text)
	lines := strings.Split(text, "\n")
	first := -1
	for i, ln := range lines {
		t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		for _, h := range RequiredHeaders {
			if t == h {
				first = i
				break
			}
		}
		if first >= 0 {
			break
		}
	}
	if first < 0 {
		return text
	}
	return strings.Join(lines[first:], "\n")
}

// Options tunes Run.
type Options struct {
	SignalsPath string
	OutDir      string
	Model       string
	OpencodeBin string
	Timeout     time.Duration
	// Retries is the number of extra attempts, taken only when the call
	// fails or required sections are missing.
	Retries int
	// NoLLM skips the opencode call and uses the template directly (v3).
	NoLLM bool
}

// Tokens sums part.tokens from step_finish events.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// RunRecord is the run.json document.
type RunRecord struct {
	Model          string  `json:"model"`
	StartedAt      string  `json:"started_at"`
	DurationMs     int64   `json:"duration_ms"`
	Calls          int     `json:"calls"`
	Tokens         Tokens  `json:"tokens"`
	Cost           float64 `json:"cost"`
	ExitStatus     int     `json:"exit_status"`
	TextSource     string  `json:"text_source"`
	FallbackReason string  `json:"fallback_reason,omitempty"`
}

// BuildPrompt renders prompt.tmpl with the signals JSON inlined plus the
// deterministic template skeleton and the shared name hints (v3). The
// skeleton shows the exact FB-ready line structure the model must keep
// (rewording connecting words only).
func BuildPrompt(signalsJSON string) (string, error) {
	t, err := template.New("prompt").Parse(promptTmpl)
	if err != nil {
		return "", err
	}
	skelTL, skelEN, skelJA := "", "", ""
	usesNearby := false
	if txts, err := RenderTemplateJSON([]byte(signalsJSON)); err == nil {
		skelTL, skelEN, skelJA = txts.TL, txts.EN, txts.JA
		usesNearby = displayedUsesNearbyFromData([]byte(signalsJSON))
	}
	var b strings.Builder
	data := map[string]any{
		"Signals":    signalsJSON,
		"SkeletonTL": skelTL,
		"SkeletonEN": skelEN,
		"SkeletonJA": skelJA,
		"NameHints":  nameHintLines(),
		"UsesNearby": usesNearby,
	}
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// HasRequiredSections reports whether text has all three section headers.
// Glued headers ("...JSON.## Taglish") count: each required header is
// recognised even when not at a line start.
func HasRequiredSections(text string) bool {
	text = insertHeaderNewlines(text)
	seen := map[string]bool{}
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		for _, h := range RequiredHeaders {
			if t == h {
				seen[h] = true
			}
		}
	}
	return len(seen) == len(RequiredHeaders)
}

func num64(v any) int64 {
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return 0
}

// ParseEvents consumes opencode --format json JSON-lines output:
// concatenate part.text of "text" events; sum part.tokens and part.cost
// from "step_finish" events. Unknown shapes are ignored.
func ParseEvents(data []byte) (text string, tokens Tokens, cost float64) {
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		typ, _ := e["type"].(string)
		part, _ := e["part"].(map[string]any)
		if part == nil {
			continue
		}
		switch typ {
		case "text":
			if s, ok := part["text"].(string); ok {
				text += s
			}
		case "step_finish":
			tok, _ := part["tokens"].(map[string]any)
			if tok == nil {
				tok, _ = e["tokens"].(map[string]any)
			}
			if tok != nil {
				tokens.Input += num64(tok["input"])
				tokens.Output += num64(tok["output"])
				tokens.Reasoning += num64(tok["reasoning"])
				if cache, ok := tok["cache"].(map[string]any); ok {
					tokens.CacheRead += num64(cache["read"])
					tokens.CacheWrite += num64(cache["write"])
				}
				tokens.CacheRead += num64(tok["cache.read"])
				tokens.CacheWrite += num64(tok["cache.write"])
			}
			if c, ok := part["cost"].(float64); ok {
				cost += c
			} else if c, ok := e["cost"].(float64); ok {
				cost += c
			}
		}
	}
	return text, tokens, cost
}

// Run executes the full advise flow (v3) and returns the process exit code:
// 0 ok (llm or template fallback — fallback is not an error, just a
// warning), 1 only on I/O errors.
func Run(opt Options) int {
	if opt.Model == "" {
		opt.Model = DefaultModel
	}
	if opt.OpencodeBin == "" {
		opt.OpencodeBin = "opencode"
	}
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "advise: "+format+"\n", args...)
		return 1
	}
	if opt.SignalsPath == "" || opt.OutDir == "" {
		return fail("usage: presyo advise -signals FILE -out-dir DIR")
	}
	signalsData, err := os.ReadFile(opt.SignalsPath)
	if err != nil {
		return fail("%s", err.Error())
	}
	if !json.Valid(signalsData) {
		return fail("signals file is not valid JSON: %s", opt.SignalsPath)
	}
	usesNearby := usesNearbyFromSignals(signalsData)
	prompt, err := BuildPrompt(string(signalsData))
	if err != nil {
		return fail("prompt: %s", err.Error())
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return fail("%s", err.Error())
	}
	if err := os.WriteFile(filepath.Join(opt.OutDir, "prompt.txt"), []byte(prompt), 0o644); err != nil {
		return fail("%s", err.Error())
	}

	tmplTexts, terr := RenderTemplateJSON(signalsData)
	if terr != nil {
		return fail("template: %s", terr.Error())
	}

	started := time.Now()
	rec := RunRecord{Model: opt.Model, StartedAt: started.Format(time.RFC3339)}

	// -no-llm: skip the call entirely.
	if opt.NoLLM {
		adviceText := tmplTexts.CombinedAdvice()
		rec.DurationMs = time.Since(started).Milliseconds()
		rec.TextSource = "template"
		rec.FallbackReason = "no-llm flag"
		if werr := writeAdviseOutputs(opt.OutDir, signalsData, adviceText, tmplTexts, usesNearby, rec); werr != nil {
			return fail("%s", werr.Error())
		}
		fmt.Fprintf(os.Stderr, "advise: using template fallback (no-llm flag)\n")
		return 0
	}

	// Fresh empty temp dir so the agent has no repo files.
	workDir, err := os.MkdirTemp("", "presyo-advise-*")
	if err != nil {
		return fail("%s", err.Error())
	}
	defer os.RemoveAll(workDir)

	var adviceText string
	exitStatus := 1
	maxAttempts := 1 + opt.Retries
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rec.Calls = attempt
		ctx, cancel := context.WithTimeout(context.Background(), opt.Timeout)
		cmd := exec.CommandContext(ctx, opt.OpencodeBin,
			"run", "-m", opt.Model, "--format", "json", prompt)
		cmd.Dir = workDir
		out, err := cmd.Output()
		cancel()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				exitStatus = ee.ExitCode()
				out = append(out, ee.Stderr...)
			} else {
				fmt.Fprintf(os.Stderr, "advise: attempt %d: %s\n", attempt, err.Error())
				continue
			}
		} else {
			exitStatus = 0
		}
		text, toks, cost := ParseEvents(out)
		rec.Tokens.Input += toks.Input
		rec.Tokens.Output += toks.Output
		rec.Tokens.Reasoning += toks.Reasoning
		rec.Tokens.CacheRead += toks.CacheRead
		rec.Tokens.CacheWrite += toks.CacheWrite
		rec.Cost += cost
		// Strip any preamble glued to the first header and split glued
		// headers onto their own lines before checking sections.
		// Missing any of the three sections counts as a failed attempt
		// (retried once by default via opt.Retries).
		text = NormalizeAdvice(text)
		if exitStatus == 0 && HasRequiredSections(text) {
			adviceText = text
			break
		}
		adviceText = text
		if attempt < maxAttempts {
			fmt.Fprintf(os.Stderr, "advise: attempt %d missing sections or failed; retrying\n", attempt)
		}
	}
	rec.DurationMs = time.Since(started).Milliseconds()
	rec.ExitStatus = exitStatus

	// LLM path: success + sections + check (incl. lint) pass => llm.
	if exitStatus == 0 && HasRequiredSections(adviceText) {
		rep, cerr := Check(signalsData, adviceText)
		if cerr != nil {
			return fail("check: %s", cerr.Error())
		}
		ApplyLint(rep, LintAdviceWithBasis(adviceText, usesNearby))
		if rep.Status == StatusPass {
			rec.TextSource = "llm"
			tl, en, ja, ok := SplitSections(adviceText)
			var texts Texts
			if ok {
				texts = Texts{TL: tl, EN: en, JA: ja}
			} else {
				texts = tmplTexts
			}
			if werr := writeAdviseOutputsWithCheck(opt.OutDir, signalsData, adviceText, texts, usesNearby, rec, rep); werr != nil {
				return fail("%s", werr.Error())
			}
			fmt.Fprintf(os.Stderr, "advise: check %s (total=%d ok=%d derived=%d unbound=%d flagged=%d lint=%d, calls=%d)\n",
				rep.Status, rep.Total, rep.OK, rep.Derived, rep.Unbound, len(rep.Flagged), len(rep.Lint.Flags), rec.Calls)
			return 0
		}
		// Check failed => template fallback (still exit 0).
		rec.TextSource = "template"
		rec.FallbackReason = fmt.Sprintf("check failed: %d flagged, %d lint", len(rep.Flagged), len(rep.Lint.Flags))
		attachFBPost(signalsData, tmplTexts, rep)
		checkData, _ := json.MarshalIndent(rep, "", "  ")
		checkData = append(checkData, '\n')
		_ = os.WriteFile(filepath.Join(opt.OutDir, "advice.md"), []byte(adviceText), 0o644)
		_ = os.WriteFile(filepath.Join(opt.OutDir, "check.json"), checkData, 0o644)
		if werr := writeDealsOnly(opt.OutDir, signalsData, tmplTexts, "template", ""); werr != nil {
			return fail("%s", werr.Error())
		}
		if werr := writeFBPost(opt.OutDir, signalsData, tmplTexts); werr != nil {
			return fail("%s", werr.Error())
		}
		writeRunRecord(opt.OutDir, rec)
		fmt.Fprintf(os.Stderr, "advise: WARNING using template fallback (%s); check %s (flagged=%d lint=%d)\n",
			rec.FallbackReason, rep.Status, len(rep.Flagged), len(rep.Lint.Flags))
		return 0
	}
	// Call failed or sections missing => template fallback.
	rec.TextSource = "template"
	if exitStatus != 0 {
		rec.FallbackReason = fmt.Sprintf("opencode call failed (exit %d)", exitStatus)
	} else {
		rec.FallbackReason = fmt.Sprintf("advice missing required sections after %d call(s)", rec.Calls)
	}
	fallbackText := tmplTexts.CombinedAdvice()
	if werr := writeAdviseOutputs(opt.OutDir, signalsData, fallbackText, tmplTexts, usesNearby, rec); werr != nil {
		return fail("%s", werr.Error())
	}
	fmt.Fprintf(os.Stderr, "advise: WARNING using template fallback (%s)\n", rec.FallbackReason)
	return 0
}

func usesNearbyFromSignals(signalsData []byte) bool {
	return displayedUsesNearbyFromData(signalsData)
}

// displayedUsesNearbyFromData mirrors the renderer's footnote decision:
// true iff any displayed item (all deals, first 3 pricier) uses the
// nearby basis. Falls back to the signals-level flag when deals/pricier
// are absent, and to true for old signals without the field (don't lint
// the footnote).
func displayedUsesNearbyFromData(signalsData []byte) bool {
	var doc struct {
		Deals []struct {
			Basis string `json:"basis"`
		} `json:"deals"`
		Pricier []struct {
			Basis string `json:"basis"`
		} `json:"pricier"`
		UsesNearbyBasis *bool `json:"uses_nearby_basis"`
	}
	if err := json.Unmarshal(signalsData, &doc); err != nil {
		return true
	}
	// Old signals without deals/pricier lists: fall back to the flag.
	if doc.Deals == nil && doc.Pricier == nil {
		if doc.UsesNearbyBasis == nil {
			return true
		}
		return *doc.UsesNearbyBasis
	}
	for _, d := range doc.Deals {
		if d.Basis == "nearby_median" {
			return true
		}
	}
	shown := doc.Pricier
	if len(shown) > 3 {
		shown = shown[:3]
	}
	for _, p := range shown {
		if p.Basis == "nearby_median" {
			return true
		}
	}
	return false
}

func writeAdviseOutputs(outDir string, signalsData []byte, adviceText string, texts Texts, usesNearby bool, rec RunRecord) error {
	if err := os.WriteFile(filepath.Join(outDir, "advice.md"), []byte(adviceText), 0o644); err != nil {
		return err
	}
	rep, err := Check(signalsData, adviceText)
	if err != nil {
		return err
	}
	ApplyLint(rep, LintAdviceWithBasis(adviceText, usesNearby))
	attachFBPost(signalsData, texts, rep)
	checkData, _ := json.MarshalIndent(rep, "", "  ")
	checkData = append(checkData, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "check.json"), checkData, 0o644); err != nil {
		return err
	}
	if err := writeDealsOnly(outDir, signalsData, texts, rec.TextSource, rec.Model); err != nil {
		return err
	}
	if err := writeFBPost(outDir, signalsData, texts); err != nil {
		return err
	}
	writeRunRecord(outDir, rec)
	fmt.Fprintf(os.Stderr, "advise: check %s (total=%d ok=%d derived=%d unbound=%d flagged=%d lint=%d, calls=%d)\n",
		rep.Status, rep.Total, rep.OK, rep.Derived, rep.Unbound, len(rep.Flagged), len(rep.Lint.Flags), rec.Calls)
	return nil
}

func writeAdviseOutputsWithCheck(outDir string, signalsData []byte, adviceText string, texts Texts, usesNearby bool, rec RunRecord, rep *Report) error {
	if err := os.WriteFile(filepath.Join(outDir, "advice.md"), []byte(adviceText), 0o644); err != nil {
		return err
	}
	attachFBPost(signalsData, texts, rep)
	checkData, _ := json.MarshalIndent(rep, "", "  ")
	checkData = append(checkData, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "check.json"), checkData, 0o644); err != nil {
		return err
	}
	if err := writeDealsOnly(outDir, signalsData, texts, rec.TextSource, rec.Model); err != nil {
		return err
	}
	if err := writeFBPost(outDir, signalsData, texts); err != nil {
		return err
	}
	writeRunRecord(outDir, rec)
	return nil
}

// attachFBPost checks the combined FB post for texts and records the
// result under check.json's fb_post (nil when the check errors).
func attachFBPost(signalsData []byte, texts Texts, rep *Report) {
	_, fbRep := FBPostAndCheck(signalsData, texts)
	rep.FBPost = fbRep
}

func writeFBPost(outDir string, signalsData []byte, texts Texts) error {
	post, _ := FBPostAndCheck(signalsData, texts)
	return os.WriteFile(filepath.Join(outDir, "fb_post.txt"), []byte(post+"\n"), 0o644)
}

func writeDealsOnly(outDir string, signalsData []byte, texts Texts, textSource, model string) error {
	data, err := BuildDealsDoc(signalsData, texts, textSource, model)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "deals.json"), data, 0o644)
}

func writeRunRecord(outDir string, rec RunRecord) {
	data, _ := json.MarshalIndent(rec, "", "  ")
	data = append(data, '\n')
	_ = os.WriteFile(filepath.Join(outDir, "run.json"), data, 0o644)
}
