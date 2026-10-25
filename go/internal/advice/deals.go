package advice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DealsDoc is the site deals.json document.
type DealsDoc struct {
	AsOf            string             `json:"as_of"`
	GeneratedAt     string             `json:"generated_at"`
	TextSource      string             `json:"text_source"`
	Model           string             `json:"model,omitempty"`
	Texts           Texts              `json:"texts"`
	FBPost          string             `json:"fb_post"`
	Deals           []dealLite         `json:"deals"`
	Pricier         []dealLite         `json:"pricier"`
	UsesNearbyBasis bool               `json:"uses_nearby_basis"`
	Thresholds      map[string]float64 `json:"thresholds"`
}

type dealLite struct {
	Rank            int      `json:"rank"`
	Name            string   `json:"name"`
	Basis           string   `json:"basis"`
	BasisLabel      string   `json:"basis_label"`
	Unit            string   `json:"unit"`
	Price           float64  `json:"price"`
	Median30d       float64  `json:"median_30d"`
	PctVsMedian30d  float64  `json:"pct_vs_median_30d"`
	DiffVsMedian30d float64  `json:"diff_vs_median_30d"`
	Trend7dPct      *float64 `json:"trend_7d_pct"`
	CheapestNearby  any      `json:"cheapest_nearby"`
}

// generatedAtNow returns RFC3339 Asia/Manila now, or PRESYO_GENERATED_AT.
func generatedAtNow() string {
	if v := os.Getenv("PRESYO_GENERATED_AT"); v != "" {
		return v
	}
	loc, err := time.LoadLocation("Asia/Manila")
	if err != nil {
		loc = time.FixedZone("Asia/Manila", 8*3600)
	}
	return time.Now().In(loc).Format(time.RFC3339)
}

// SplitSections splits normalized advice into its three section bodies.
func SplitSections(text string) (tl, en, ja string, ok bool) {
	text = NormalizeAdvice(text)
	lines := strings.Split(text, "\n")
	cur := ""
	var bTL, bEN, bJA []string
	seen := map[string]bool{}
	for _, ln := range lines {
		t := strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		isHeader := false
		for _, h := range RequiredHeaders {
			if t == h {
				cur = h
				seen[h] = true
				isHeader = true
				break
			}
		}
		if isHeader {
			continue
		}
		switch cur {
		case "## Taglish":
			bTL = append(bTL, ln)
		case "## English":
			bEN = append(bEN, ln)
		case "## 日本語":
			bJA = append(bJA, ln)
		}
	}
	if len(seen) != 3 {
		return "", "", "", false
	}
	trim := func(ls []string) string {
		s := strings.Join(ls, "\n")
		return strings.Trim(s, "\n")
	}
	return trim(bTL), trim(bEN), trim(bJA), true
}

// BuildDealsDoc builds the deals.json bytes from signals + texts.
func BuildDealsDoc(signalsData []byte, texts Texts, textSource, model string) ([]byte, error) {
	var sig struct {
		AsOf            string             `json:"as_of"`
		Deals           []json.RawMessage  `json:"deals"`
		Pricier         []json.RawMessage  `json:"pricier"`
		UsesNearbyBasis bool               `json:"uses_nearby_basis"`
		Thresholds      map[string]float64 `json:"thresholds"`
	}
	dec := json.NewDecoder(bytes.NewReader(signalsData))
	dec.UseNumber()
	if err := dec.Decode(&sig); err != nil {
		return nil, fmt.Errorf("signals JSON: %w", err)
	}
	// Re-marshal deals/pricier via dealLite to enforce stable keys.
	var deals []dealLite
	var pricier []dealLite
	var raw struct {
		Deals   []dealLite `json:"deals"`
		Pricier []dealLite `json:"pricier"`
	}
	if err := json.Unmarshal(signalsData, &raw); err != nil {
		return nil, err
	}
	deals = raw.Deals
	pricier = raw.Pricier
	if deals == nil {
		deals = []dealLite{}
	}
	if pricier == nil {
		pricier = []dealLite{}
	}
	// Footnote decision over displayed items (all deals, first 3 pricier).
	displayedNearby := false
	for _, d := range deals {
		if d.Basis == "nearby_median" {
			displayedNearby = true
			break
		}
	}
	if !displayedNearby {
		shown := pricier
		if len(shown) > 3 {
			shown = shown[:3]
		}
		for _, p := range shown {
			if p.Basis == "nearby_median" {
				displayedNearby = true
				break
			}
		}
	}
	// Ensure cheapest_nearby null stays null (dealLite.CheapestNearby any).
	// The combined FB post derives from the same texts (template fallback
	// when its check fails), so deals.json never carries a failing post.
	fbPost, _ := FBPostAndCheck(signalsData, texts)
	doc := struct {
		AsOf            string             `json:"as_of"`
		GeneratedAt     string             `json:"generated_at"`
		TextSource      string             `json:"text_source"`
		Model           string             `json:"model,omitempty"`
		Texts           Texts              `json:"texts"`
		FBPost          string             `json:"fb_post"`
		Deals           []dealLite         `json:"deals"`
		Pricier         []dealLite         `json:"pricier"`
		UsesNearbyBasis bool               `json:"uses_nearby_basis"`
		Thresholds      map[string]float64 `json:"thresholds"`
	}{
		AsOf: sig.AsOf, GeneratedAt: generatedAtNow(),
		TextSource: textSource, Texts: texts, FBPost: fbPost,
		Deals: deals, Pricier: pricier,
		UsesNearbyBasis: displayedNearby, Thresholds: sig.Thresholds,
	}
	if textSource == "llm" {
		doc.Model = model
	}
	// Stable order via struct + indent 2. encoding/json leaves non-ASCII
	// unescaped (only <>& escaped); unescape those for byte-stability.
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	data = bytes.ReplaceAll(data, []byte("\\u003c"), []byte("<"))
	data = bytes.ReplaceAll(data, []byte("\\u003e"), []byte(">"))
	data = bytes.ReplaceAll(data, []byte("\\u0026"), []byte("&"))
	data = append(data, '\n')
	return data, nil
}

// LoadTextsFromAdviceDir implements the deals fallback: when dir has
// run.json with text_source "llm", a passing check.json and advice.md with
// all three sections, use those texts; else RenderTemplate.
func LoadTextsFromAdviceDir(signalsData []byte, adviceDir string) (texts Texts, textSource, model string) {
	tmpl, _ := RenderTemplateJSON(signalsData)
	if adviceDir == "" {
		return tmpl, "template", ""
	}
	runData, err := os.ReadFile(filepath.Join(adviceDir, "run.json"))
	if err != nil {
		return tmpl, "template", ""
	}
	var run struct {
		TextSource string `json:"text_source"`
		Model      string `json:"model"`
	}
	if err := json.Unmarshal(runData, &run); err != nil {
		return tmpl, "template", ""
	}
	if run.TextSource != "llm" {
		return tmpl, "template", ""
	}
	chkData, err := os.ReadFile(filepath.Join(adviceDir, "check.json"))
	if err != nil {
		return tmpl, "template", ""
	}
	var chk struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(chkData, &chk); err != nil || chk.Status != StatusPass {
		return tmpl, "template", ""
	}
	advData, err := os.ReadFile(filepath.Join(adviceDir, "advice.md"))
	if err != nil {
		return tmpl, "template", ""
	}
	tl, en, ja, ok := SplitSections(string(advData))
	if !ok {
		return tmpl, "template", ""
	}
	return Texts{TL: tl, EN: en, JA: ja}, "llm", run.Model
}
