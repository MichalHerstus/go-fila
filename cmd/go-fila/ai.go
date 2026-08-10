// ai.go
//
// D7 — AI-assisted config editing (go-fila edit via OpenRouter). Non-interactive
// and opt-in: the AI path only runs when `go-fila edit --prompt "…"` is given,
// otherwise the TUI editor runs unchanged. One-shot write with a single retry on
// invalid output, `--dry-run` preview, provider locked to OpenRouter.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-fila/go-fila/internal/parser"
	"gopkg.in/yaml.v3"
)

const (
	// openRouterBaseURL is the OpenRouter API root. The provider is locked:
	// this is the only supported endpoint.
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	// defaultModel is used when --model is omitted.
	defaultModel = "openrouter/auto"
	// aiHTTPTimeout caps a single chat request (spec: ~90 s).
	aiHTTPTimeout = 90 * time.Second
	// aiMaxResponseBytes bounds how much of the model response we buffer.
	aiMaxResponseBytes = 8 << 20
)

// aiSpec is the compact schema cheat-sheet embedded into the user message.
//
//go:embed ai_spec.md
var aiSpec string

// parseEditFlags scans the edit subcommand's argument slice for the AI-only
// flags. It deliberately ignores the global flags (--config/--out/...), which
// parseGlobalFlags handles separately — both scans read os.Args[2:] and each
// skips the flags it does not know. The API key falls back to the
// OPENROUTER_API_KEY environment variable when --apikey is absent.
// Returns: apiKey (from --apikey or env), model (default "openrouter/auto"),
// prompt (the edit instruction), dryRun (preview without writing).
func parseEditFlags(args []string) (apiKey, model, prompt string, dryRun bool) {
	apiKey = os.Getenv("OPENROUTER_API_KEY")
	model = defaultModel
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--apikey":
			if i+1 < len(args) {
				apiKey = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "--prompt":
			if i+1 < len(args) {
				prompt = args[i+1]
				i++
			}
		case "--dry-run":
			dryRun = true
		}
	}
	return
}

// resolvePrompt expands a --prompt value. When it starts with "file://" the
// rest is treated as a filesystem path — with a leading ~ expanded to the home
// directory — and the file's contents become the prompt (useful for long or
// multi-line instructions). Any other value is returned unchanged.
func resolvePrompt(prompt string) (string, error) {
	if !strings.HasPrefix(prompt, "file://") {
		return prompt, nil
	}
	path := strings.TrimPrefix(prompt, "file://")
	if path == "" {
		return "", fmt.Errorf("empty file path after file://")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading prompt file: %w", err)
	}
	return string(data), nil
}

// chatMessage is one entry in the OpenAI-style messages array.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the /chat/completions request body sent to OpenRouter.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

// chatResponse is the subset of the OpenRouter response we consume.
type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// buildEditPrompt assembles the system + user messages for one edit request.
// The system message fixes the output contract; the user message carries the
// schema cheat-sheet, the current config and the instruction.
func buildEditPrompt(cheatsheet, currentYAML, instruction string) []chatMessage {
	return []chatMessage{
		{
			Role: "system",
			Content: "You edit a go-fila.yaml admin panel configuration. Return ONLY the complete new " +
				"go-fila.yaml document inside a ```yaml fence, nothing else. Keep the top-level version " +
				"field unchanged. Do not invent YAML keys that are not in the schema provided. " +
				"Preserve all unrelated sections of the current config verbatim.",
		},
		{
			Role:    "user",
			Content: "go-fila.yaml schema cheat-sheet:\n\n" + cheatsheet + "\n\nCurrent go-fila.yaml:\n\n```yaml\n" + currentYAML + "\n```\n\nInstruction:\n" + instruction,
		},
	}
}

// openrouterChat posts the messages to OpenRouter and returns the assistant's
// reply text. The API key is sent only in the Authorization header and is never
// logged or echoed. The HTTP client is shared per call (request-scoped).
func openrouterChat(ctx context.Context, baseURL, apiKey, model string, messages []chatMessage) (string, error) {
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Temperature: 0})
	if err != nil {
		return "", fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: aiHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenRouter request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiMaxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("reading OpenRouter response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenRouter returned HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", fmt.Errorf("decoding OpenRouter response: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("OpenRouter error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("OpenRouter returned no content")
	}
	return cr.Choices[0].Message.Content, nil
}

var (
	yamlFenceRe = regexp.MustCompile("(?s)```yaml[\\r\\n]+(.*?)[\\r\\n]+```")
	anyFenceRe  = regexp.MustCompile("(?s)```[\\r\\n]+(.*?)[\\r\\n]+```")
)

// extractYAMLBlock pulls the proposed go-fila.yaml out of a model reply. It
// prefers a ```yaml fence, then any ``` fence, then the whole trimmed text as
// a last-resort heuristic.
func extractYAMLBlock(s string) string {
	s = strings.TrimSpace(s)
	if m := yamlFenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := anyFenceRe.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

// diffLines returns a compact line diff between two YAML texts. Changed lines
// are prefixed with "- " / "+ "; a short context of unchanged lines around each
// change is kept and long unchanged runs collapse to a single "..." marker.
func diffLines(oldText, newText string) []string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	n, m := len(oldLines), len(newLines)

	// Longest common subsequence table (configs are small; O(n*m) is fine).
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// Walk the table to emit the full op list (" ", "+", "-" prefixed).
	ops := make([]string, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			ops = append(ops, "  "+oldLines[i])
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, "- "+oldLines[i])
			i++
		} else {
			ops = append(ops, "+ "+newLines[j])
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, "- "+oldLines[i])
	}
	for ; j < m; j++ {
		ops = append(ops, "+ "+newLines[j])
	}

	// Compact: show changed lines plus two lines of context around each change.
	const ctx = 2
	show := make([]bool, len(ops))
	for k, op := range ops {
		if op[0] == '+' || op[0] == '-' {
			for a := k - ctx; a <= k+ctx; a++ {
				if a >= 0 && a < len(ops) {
					show[a] = true
				}
			}
		}
	}
	out := make([]string, 0, len(ops))
	for k, op := range ops {
		if show[k] {
			out = append(out, op)
		} else if len(out) == 0 || out[len(out)-1] != "..." {
			out = append(out, "...")
		}
	}
	return out
}

// splitLines splits a text into its lines, tolerating a missing trailing
// newline and returning nil for empty input.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// truncate shortens a string to at most max runes, appending "…" when cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// editAI runs the full AI edit flow against baseURL (the OpenRouter root; tests
// inject an httptest server). It reads the current config, asks the model for
// the new YAML, validates the reply (retrying once on invalid output), and —
// unless dryRun — writes configPath. The proposed YAML is normalized through
// yaml.Marshal after validation so the written bytes always round-trip.
func editAI(baseURL, configPath, apiKey, model, prompt string, dryRun bool) error {
	if apiKey == "" {
		return fmt.Errorf("missing API key: pass --apikey or set OPENROUTER_API_KEY")
	}
	prompt, err := resolvePrompt(prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("missing instruction: pass --prompt \"describe the change\"")
	}

	cfg, err := parser.ParseFile(configPath)
	if err != nil {
		return fmt.Errorf("reading current config: %w", err)
	}
	current, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling current config: %w", err)
	}

	proposed, err := proposeEdit(baseURL, apiKey, model, prompt, current)
	if err != nil {
		return err
	}

	if !dryRun {
		if err := os.WriteFile(configPath, proposed, 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("Saved %s\n", configPath)
	}

	// Print the same compact diff in both dry-run and write mode.
	fmt.Printf("Changes:\n%s\n", strings.Join(diffLines(string(current), string(proposed)), "\n"))
	if dryRun {
		fmt.Printf("\nProposed go-fila.yaml (%s, dry run — not written):\n%s\n", model, proposed)
	}
	return nil
}

// proposeEdit runs the chat + extraction + validation loop, retrying once when
// the model output fails to parse/validate (the validator error is fed back to
// the model on the second attempt). Network/API errors are not retried.
func proposeEdit(baseURL, apiKey, model, prompt string, current []byte) ([]byte, error) {
	messages := buildEditPrompt(aiSpec, string(current), prompt)
	content, err := openrouterChat(context.Background(), baseURL, apiKey, model, messages)
	if err != nil {
		return nil, err
	}
	block := extractYAMLBlock(content)
	var parseErr error
	if _, parseErr = parser.Parse([]byte(block)); parseErr == nil {
		return normalizeYAML(block)
	}

	// First attempt failed validation: retry once, feeding the error back.
	retry := buildRetryPrompt(aiSpec, string(current), prompt, parseErr)
	content, err = openrouterChat(context.Background(), baseURL, apiKey, model, retry)
	if err != nil {
		return nil, err
	}
	block = extractYAMLBlock(content)
	if _, err := parser.Parse([]byte(block)); err != nil {
		return nil, fmt.Errorf("model returned invalid YAML after retry: %w", err)
	}
	return normalizeYAML(block)
}

// buildRetryPrompt builds the user message for the second attempt, appending
// the validator error from the first reply so the model can fix it.
func buildRetryPrompt(cheatsheet, currentYAML, prompt string, lastErr error) []chatMessage {
	msgs := buildEditPrompt(cheatsheet, currentYAML, prompt)
	msgs[1].Content += "\n\nYour previous reply failed validation with:\n" + lastErr.Error() +
		"\nFix the problem and return the complete corrected go-fila.yaml in a ```yaml fence."
	return msgs
}

// normalizeYAML parses+validates the block (filling defaults) and re-marshals
// it so the written output is exactly what was validated.
func normalizeYAML(block string) ([]byte, error) {
	cfg, err := parser.Parse([]byte(block))
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}
