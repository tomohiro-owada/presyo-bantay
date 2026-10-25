package advice

import (
	"strings"
	"testing"
)

func TestBuildPromptInlinesSignals(t *testing.T) {
	p, err := BuildPrompt(`{"as_of":"2026-10-23"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, `{"as_of":"2026-10-23"}`) {
		t.Error("prompt does not inline signals JSON")
	}
	for _, h := range RequiredHeaders {
		if !strings.Contains(p, h) {
			t.Errorf("prompt missing %q", h)
		}
	}
}

func TestHasRequiredSections(t *testing.T) {
	if !HasRequiredSections("## Taglish\nx\n## English\ny\n## 日本語\nz\n") {
		t.Error("complete text rejected")
	}
	if HasRequiredSections("## Taglish\nx\n## English\ny\n") {
		t.Error("incomplete text accepted")
	}
	// Glued headers (no newline before ##) still count.
	if !HasRequiredSections("preamble JSON.## Taglish\nx\n## English\ny\n## 日本語\nz\n") {
		t.Error("glued Taglish header not recognised")
	}
	if !HasRequiredSections("## Taglish\nx## English\ny\n## 日本語\nz\n") {
		t.Error("glued English header not recognised")
	}
}

func TestNormalizeAdviceStripsPreamble(t *testing.T) {
	in := "Gagawa ako ng guide gamit lang ang JSON.## Taglish\nbody ₱100\n## English\nbody\n## 日本語\nbody\n"
	got := NormalizeAdvice(in)
	if strings.HasPrefix(got, "Gagawa") {
		t.Errorf("preamble not stripped: %q", got)
	}
	if !strings.HasPrefix(got, "## Taglish") {
		t.Errorf("should start with ## Taglish: %q", got)
	}
	if !HasRequiredSections(got) {
		t.Error("normalized text should have all sections")
	}
}

func TestNormalizeAdviceMissingSection(t *testing.T) {
	in := "preamble\n## Taglish\nx\n## English\ny\n"
	if HasRequiredSections(in) {
		t.Error("incomplete text accepted even after normalize")
	}
	if got := NormalizeAdvice(in); !strings.HasPrefix(got, "## Taglish") {
		t.Errorf("normalize should still strip to first header: %q", got)
	}
}

func TestBuildPromptBasesAndRules(t *testing.T) {
	p, err := BuildPrompt(`{"as_of":"2026-10-23"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Sulit ngayon",
		"Good deals today",
		"今日のお買い得",
		"Mas mahal ngayon",
		"いつもより高め",
		"kalapit na palengke",
		"近隣市場",
		"ラス・ピニャス",
		"アラバン",
		"no preamble",
		"## Taglish",
		"30-day median",
		"pinakamura",
		"1kg",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	for _, banned := range []string{
		`"substitutions"`,
		"focus_price",
		"ncr_median",
		"summary.expensive",
		"meaningful=true",
		"sinigang",
	} {
		if strings.Contains(strings.ToLower(p), strings.ToLower(banned)) {
			t.Errorf("v2 prompt must not contain %q", banned)
		}
	}
}

func TestParseEvents(t *testing.T) {
	raw := "{\"type\":\"text\",\"part\":{\"text\":\"Hello \"}}\n" +
		"{\"type\":\"text\",\"part\":{\"text\":\"world\"}}\n" +
		"{\"type\":\"step_finish\",\"part\":{\"tokens\":{\"input\":10,\"output\":20,\"reasoning\":5,\"cache\":{\"read\":1,\"write\":2}},\"cost\":0.01}}\n" +
		"not json\n"
	text, toks, cost := ParseEvents([]byte(raw))
	if text != "Hello world" {
		t.Errorf("text = %q", text)
	}
	if toks != (Tokens{Input: 10, Output: 20, Reasoning: 5, CacheRead: 1, CacheWrite: 2}) {
		t.Errorf("tokens = %+v", toks)
	}
	if cost != 0.01 {
		t.Errorf("cost = %v", cost)
	}
}
