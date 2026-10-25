package fbregion

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/tomohiro-owada/presyo-bantay/go/internal/advice"
)

// RewriteWithLLM runs the opencode CLI over the template post text and
// returns the model's rephrased post (connecting words only; numbers,
// items and markets must stay identical). It reuses the opencode runner
// pattern from internal/advice. Callers must run CheckPost on the result
// and fall back to the template when it fails.
func RewriteWithLLM(templateText, model, opencodeBin string, timeout time.Duration) (string, error) {
	if model == "" {
		model = advice.DefaultModel
	}
	if opencodeBin == "" {
		opencodeBin = "opencode"
	}
	prompt := "Rephrase only the connecting words of the following trilingual " +
		"market-deals Facebook post. Keep every item name, every ₱ amount, " +
		"every % value, every market name, the line structure, the ✅/⚠️ " +
		"markers, the ━━━━━━━━━━ separators and the footer link EXACTLY as " +
		"they are. Do not add or remove any item, number or line. Reply with " +
		"only the rewritten post, no preamble.\n\n" + templateText
	workDir, err := os.MkdirTemp("", "presyo-fbregion-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workDir)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, opencodeBin,
		"run", "-m", model, "--format", "json", prompt)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("opencode call failed (exit %d): %s", ee.ExitCode(), string(ee.Stderr))
		}
		return "", fmt.Errorf("opencode call: %w", err)
	}
	text, _, _ := advice.ParseEvents(out)
	if text == "" {
		return "", fmt.Errorf("opencode returned no text")
	}
	return text, nil
}
