// ai.go
//
// D7 — AI-assisted config editing (go-fila edit via OpenRouter or a local LM
// Studio server). Non-interactive and opt-in: the AI path only runs when
// `go-fila edit --prompt "…"` is given, otherwise the TUI editor runs
// unchanged. One-shot write with a single retry on invalid output, `--dry-run`
// preview. The provider is selected by --model: any value routes to OpenRouter
// (the default); the sentinel "lmstudio" routes to a local LM Studio server.
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-fila/go-fila/internal/parser"
	"gopkg.in/yaml.v3"
)

const (
	// openRouterBaseURL is the OpenRouter API root. The default provider.
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	// lmStudioBaseURL is the OpenAI-compatible root of a local LM Studio
	// server. Reached when --model is the lmStudioModel sentinel; no API key
	// is required.
	lmStudioBaseURL = "http://127.0.0.1:1234/v1"
	// lmStudioModel is the --model sentinel that selects the local LM Studio
	// provider instead of OpenRouter.
	lmStudioModel = "lmstudio"
	// defaultModel is used when --model is omitted.
	defaultModel = "openrouter/auto"
	// aiHTTPTimeout caps a single chat request. 300s is generous enough for
	// slow free-tier models (e.g. nvidia/nemotron-...:free) to regenerate a
	// full multi-KB go-fila.yaml; the old 90s deadline regularly fired on them.
	aiHTTPTimeout = 300 * time.Second
	// aiModelTimeout caps the LM Studio model-discovery call (GET /models).
	// A refused connection on localhost is immediate, so a short budget is
	// enough for a fast error.
	aiModelTimeout = 10 * time.Second
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
// OPENROUTER_API_KEY environment variable, then to the current folder's .ENV
// file (see persistEnv). The model falls back to .ENV, then to the default.
// The model value "lmstudio" selects the local LM Studio provider (no key).
// Returns: apiKey (--apikey, then OPENROUTER_API_KEY env, then .ENV),
// model (--model, then .ENV, else "openrouter/auto"),
// prompt (the edit instruction), dryRun (preview without writing).
func parseEditFlags(args []string) (apiKey, model, prompt string, dryRun bool) {
	apiKey = os.Getenv("OPENROUTER_API_KEY")
	model = defaultModel
	modelSet := false
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
				modelSet = true
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
	// Fall back to the credentials persisted in the current folder's .ENV file
	// by a previous run: apiKey only when no flag/env set, model only when the
	// --model flag was absent.
	fileKey, fileModel := readEnvFile()
	if apiKey == "" {
		apiKey = fileKey
	}
	if !modelSet && fileModel != "" {
		model = fileModel
	}
	return
}

// envPathFunc returns the .ENV file that records the last used credentials (a
// dotenv file in the current folder). It is a variable so tests can redirect it
// away from the repository.
var envPathFunc = func() string {
	wd, err := os.Getwd()
	if err != nil {
		return ".ENV"
	}
	return filepath.Join(wd, ".ENV")
}

// Keys written to / read back from the .ENV file (dotenv KEY=value lines).
const (
	envKeyAPIKey = "OPENROUTER_API_KEY"
	envKeyModel  = "MODEL"
)

// readEnvFile returns the persisted API key and model from the current
// folder's .ENV file ("" when absent).
func readEnvFile() (apiKey, model string) {
	data, err := os.ReadFile(envPathFunc())
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		switch key {
		case envKeyAPIKey:
			apiKey = val
		case envKeyModel:
			model = val
		}
	}
	return
}

// writeEnvFile persists the given key/model into the current folder's .ENV
// file, updating the two known keys in place and preserving any unrelated
// entries (0600 keeps the API key private).
func writeEnvFile(apiKey, model string) error {
	path := envPathFunc()
	keys := []string{envKeyAPIKey, envKeyModel}
	seen := map[string]bool{}
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
			if !seen[key] && (key == envKeyAPIKey || key == envKeyModel) {
				seen[key] = true
				val := apiKey
				if key == envKeyModel {
					val = model
				}
				if val != "" {
					lines = append(lines, key+"="+val)
				}
				continue
			}
			lines = append(lines, line)
		}
	}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		val := apiKey
		if key == envKeyModel {
			val = model
		}
		if val != "" {
			lines = append(lines, key+"="+val)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

// persistEnv writes the credentials a successful model run used into the
// current folder's .ENV file so later invocations can omit --apikey/--model.
// It is a variable so tests can stub it out; failures only warn.
var persistEnv = func(apiKey, model string) {
	if apiKey == "" && model == "" {
		return
	}
	if err := writeEnvFile(apiKey, model); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write %s: %v\n", envPathFunc(), err)
	}
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

// chatRequest is the /chat/completions request body sent to the provider.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

// chatResponse is the subset of the provider response we consume.
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
			Content: "You edit a go-fila.yaml admin panel configuration. Return ONLY the changed sections of the config " +
				"as a YAML fragment inside a ```yaml fence, nothing else. Include only the top-level keys you changed " +
				"(panel, connections, sqlc, auth, navigation, resources, pages, plugins), nested only as deep as the " +
				"change. For lists identified by name (resources, pages, fields, actions) return just the changed items, " +
				"each identified by its name; for navigation groups use the group name and for their items use " +
				"resource/page/url. Do not include unchanged sections. Keep the top-level version field unchanged — " +
				"omit it from the fragment. Do not invent YAML keys that are not in the schema provided.",
		},
		{
			Role:    "user",
			Content: "go-fila.yaml schema cheat-sheet:\n\n" + cheatsheet + "\n\nCurrent go-fila.yaml:\n\n```yaml\n" + currentYAML + "\n```\n\nInstruction:\n" + instruction,
		},
	}
}

// chatCompletions posts the messages to the provider's /chat/completions
// endpoint and returns the assistant's reply text. When apiKey is non-empty it
// is sent only in the Authorization header and never logged or echoed; the
// local LM Studio provider needs no key and none is sent. The HTTP client is
// shared per call (request-scoped).
func chatCompletions(ctx context.Context, baseURL, apiKey, model string, messages []chatMessage) (string, error) {
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Temperature: 0})
	if err != nil {
		return "", fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: aiHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiMaxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("reading chat response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", fmt.Errorf("decoding chat response: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", fmt.Errorf("provider error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("provider returned no content")
	}
	return cr.Choices[0].Message.Content, nil
}

// lmStudioModels is the subset of the LM Studio /models response we consume.
type lmStudioModels struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// lmStudioModelID discovers the id of the model currently served by the local
// LM Studio server. The OpenAI-compatible /chat/completions endpoint rejects a
// model field that does not exactly match a loaded model id, so the real id
// must be queried from GET /models (the sentinel --model "lmstudio" value is
// not sent as-is). Errors are descriptive: the server may be down, or running
// with no model loaded.
func lmStudioModelID(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return "", fmt.Errorf("building LM Studio model request: %w", err)
	}
	client := &http.Client{Timeout: aiModelTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LM Studio not reachable at %s (start its local server and load a model): %w", baseURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiMaxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("reading LM Studio model list: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LM Studio /models returned HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	var ms lmStudioModels
	if err := json.Unmarshal(data, &ms); err != nil {
		return "", fmt.Errorf("decoding LM Studio model list: %w", err)
	}
	if len(ms.Data) == 0 {
		return "", fmt.Errorf("LM Studio has no model loaded; load one in its Server tab first")
	}
	return ms.Data[0].ID, nil
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

// topLevelKeys is the set of keys the root of go-fila.yaml may contain. A
// fragment that introduces any other top-level key is rejected (feeds the retry)
// so the model cannot silently invent new sections.
var topLevelKeys = map[string]bool{
	"version": true, "panel": true, "connections": true, "sqlc": true,
	"auth": true, "navigation": true, "resources": true, "pages": true,
	"plugins": true,
}

// mergeYAML merges a model fragment into the current go-fila.yaml. Both are
// parsed as yaml.v3 Nodes and merged recursively: mappings recurse key-by-key,
// sequences merge item-by-item by their identity key when one exists (see
// identityKeys), and everything else replaces wholesale. An unknown top-level
// key, a non-mapping fragment or an empty fragment is an error. The merged
// document is returned as YAML bytes (key order preserved).
func mergeYAML(current, fragment []byte) ([]byte, error) {
	var cur yaml.Node
	if err := yaml.Unmarshal(current, &cur); err != nil {
		return nil, fmt.Errorf("merging: parsing current config: %w", err)
	}
	var frag yaml.Node
	if err := yaml.Unmarshal(fragment, &frag); err != nil {
		return nil, fmt.Errorf("merging: parsing model fragment: %w", err)
	}
	curRoot, err := mappingOf(&cur)
	if err != nil {
		return nil, fmt.Errorf("merging: current config: %w", err)
	}
	fragRoot, err := mappingOf(&frag)
	if err != nil {
		return nil, fmt.Errorf("merging: model fragment: %w", err)
	}
	if len(fragRoot.Content) == 0 {
		return nil, fmt.Errorf("merging: model fragment is empty")
	}
	for i := 0; i < len(fragRoot.Content); i += 2 {
		key := fragRoot.Content[i].Value
		if !topLevelKeys[key] {
			return nil, fmt.Errorf("merging: model fragment contains unknown top-level key %q", key)
		}
	}
	if err := mergeMapping(curRoot, fragRoot); err != nil {
		return nil, err
	}
	return yaml.Marshal(curRoot)
}

// mappingOf unwraps document nodes and requires the result to be a mapping.
func mappingOf(n *yaml.Node) (*yaml.Node, error) {
	for n.Kind == yaml.DocumentNode {
		if len(n.Content) != 1 {
			return nil, fmt.Errorf("expected a single YAML document")
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a YAML mapping")
	}
	return n, nil
}

// mergeMapping merges src's key/value pairs into dst (both mapping nodes).
// Keys already in dst are merged recursively; new keys are appended.
func mergeMapping(dst, src *yaml.Node) error {
	for i := 0; i < len(src.Content); i += 2 {
		key := src.Content[i].Value
		j := mappingIndex(dst, key)
		if j == -1 {
			dst.Content = append(dst.Content, cloneNode(src.Content[i]), cloneNode(src.Content[i+1]))
			continue
		}
		if err := mergeValue(dst.Content[j+1], src.Content[i+1], key); err != nil {
			return err
		}
	}
	return nil
}

// mergeValue merges a single fragment value into the corresponding target
// value. Mappings recurse; sequences merge by identity key; scalars replace.
// A null fragment value leaves the target untouched (no deletion support).
func mergeValue(dst, src *yaml.Node, key string) error {
	if src.Kind == yaml.ScalarNode && src.Tag == "!!null" {
		return nil
	}
	if dst.Kind == yaml.MappingNode && src.Kind == yaml.MappingNode {
		return mergeMapping(dst, src)
	}
	if dst.Kind == yaml.SequenceNode && src.Kind == yaml.SequenceNode {
		return mergeSequence(dst, src, key)
	}
	replaceNode(dst, src)
	return nil
}

// identityKeys returns the identity key(s) a sequence's items are matched on
// during item-by-item merge. An empty result means the sequence has no stable
// identity and is replaced wholesale (e.g. page widgets).
func identityKeys(key string) []string {
	switch key {
	case "resources", "pages", "fields", "actions", "columns":
		return []string{"name"}
	case "navigation":
		return []string{"group"}
	case "items":
		return []string{"resource", "page", "url"}
	default:
		return nil
	}
}

// mergeSequence merges src sequence items into dst by identity key. New items
// append; when either side holds non-mapping items (or no identity key exists)
// the whole list is replaced.
func mergeSequence(dst, src *yaml.Node, key string) error {
	keys := identityKeys(key)
	if len(keys) == 0 || !allMappings(dst.Content) || !allMappings(src.Content) {
		replaceNode(dst, src)
		return nil
	}
	for _, s := range src.Content {
		if j := sequenceIndex(dst.Content, s, keys); j >= 0 {
			if err := mergeMapping(dst.Content[j], s); err != nil {
				return err
			}
		} else {
			dst.Content = append(dst.Content, cloneNode(s))
		}
	}
	return nil
}

// allMappings reports whether every node in items is a mapping node.
func allMappings(items []*yaml.Node) bool {
	for _, it := range items {
		if it.Kind != yaml.MappingNode {
			return false
		}
	}
	return true
}

// sequenceIndex finds the index of the item in items whose identity key (any
// of keys, in order) has the same value as want's. Returns -1 when unmatched.
func sequenceIndex(items []*yaml.Node, want *yaml.Node, keys []string) int {
	for i, it := range items {
		if it.Kind != yaml.MappingNode || want.Kind != yaml.MappingNode {
			continue
		}
		for _, k := range keys {
			w := mappingValue(want, k)
			if w != "" && mappingValue(it, k) == w {
				return i
			}
		}
	}
	return -1
}

// mappingIndex returns the index of key within a mapping node's Content
// (key/value pairs), or -1 when absent.
func mappingIndex(n *yaml.Node, key string) int {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// mappingValue returns the scalar value for key in a mapping node, or "".
func mappingValue(n *yaml.Node, key string) string {
	if i := mappingIndex(n, key); i >= 0 {
		return n.Content[i+1].Value
	}
	return ""
}

// cloneNode deep-copies a yaml node.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, cn := range n.Content {
			c.Content[i] = cloneNode(cn)
		}
	}
	return &c
}

// replaceNode copies src's value into dst, keeping dst's position in its tree.
func replaceNode(dst, src *yaml.Node) {
	*dst = *cloneNode(src)
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

// changedPaths returns one line per leaf that differs between current and
// proposed, e.g. "panel/name -> 'Order management'". Keyed-list items
// (resources/pages/fields/actions by name, navigation groups by group, nav
// items by resource/page/url) contribute their identity value to the path, so
// a column-label change reads "resources/User/list/columns/email/label".
// String values render single-quoted; numbers/bools/null render bare. New
// subtrees emit every changed leaf; removed leaves (only possible via a whole
// list replacement) render "-> (removed)".
func changedPaths(current, proposed []byte) ([]string, error) {
	var cur, prop yaml.Node
	if err := yaml.Unmarshal(current, &cur); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(proposed, &prop); err != nil {
		return nil, err
	}
	curRoot, err := mappingOf(&cur)
	if err != nil {
		return nil, err
	}
	propRoot, err := mappingOf(&prop)
	if err != nil {
		return nil, err
	}
	var out []string
	diffValue(curRoot, propRoot, "", &out)
	return out, nil
}

// diffValue records every leaf difference between two nodes as a "path ->
// value" line. path is the accumulated key path ("" at the root).
func diffValue(cur, prop *yaml.Node, path string, out *[]string) {
	// Both scalars: report when the value differs.
	if cur.Kind == yaml.ScalarNode && prop.Kind == yaml.ScalarNode {
		if cur.Tag == prop.Tag && cur.Value == prop.Value {
			return
		}
		*out = append(*out, path+" -> "+scalarValue(prop))
		return
	}
	// Both mappings: recurse per key.
	if cur.Kind == yaml.MappingNode && prop.Kind == yaml.MappingNode {
		diffMapping(cur, prop, path, out)
		return
	}
	// Both sequences: keyed items merge by identity, keyless ones by index.
	if cur.Kind == yaml.SequenceNode && prop.Kind == yaml.SequenceNode {
		diffSequence(cur, prop, path, out)
		return
	}
	// Type change or structural replacement: report the whole subtree.
	*out = append(*out, path+" -> "+nodeValue(prop))
}

// diffMapping records leaf differences between two mapping nodes.
func diffMapping(cur, prop *yaml.Node, path string, out *[]string) {
	for i := 0; i < len(prop.Content); i += 2 {
		key := prop.Content[i].Value
		child := joinPath(path, key)
		j := mappingIndex(cur, key)
		if j < 0 {
			emitNew(prop.Content[i+1], child, out)
			continue
		}
		diffValue(cur.Content[j+1], prop.Content[i+1], child, out)
	}
	for i := 0; i < len(cur.Content); i += 2 {
		key := cur.Content[i].Value
		if mappingIndex(prop, key) < 0 {
			*out = append(*out, joinPath(path, key)+" -> (removed)")
		}
	}
}

// diffSequence records leaf differences between two sequence nodes. Keyed
// sequences (identityKeys returns a non-empty set AND the items are mappings)
// align items by their identity value; keyless and non-mapping sequences (e.g.
// auth login fields, which are plain strings) align by index.
func diffSequence(cur, prop *yaml.Node, path string, out *[]string) {
	keys := identityKeys(lastPathSegment(path))
	if len(keys) == 0 || !allMappings(cur.Content) || !allMappings(prop.Content) {
		n := len(cur.Content)
		if len(prop.Content) > n {
			n = len(prop.Content)
		}
		for i := 0; i < n; i++ {
			idx := joinPath(path, strconv.Itoa(i))
			switch {
			case i >= len(cur.Content):
				emitNew(prop.Content[i], idx, out)
			case i >= len(prop.Content):
				*out = append(*out, idx+" -> (removed)")
			default:
				diffValue(cur.Content[i], prop.Content[i], idx, out)
			}
		}
		return
	}
	for _, s := range prop.Content {
		child := joinPath(path, identityValue(s, keys))
		if j := sequenceIndex(cur.Content, s, keys); j < 0 {
			emitNew(s, child, out)
		} else {
			diffValue(cur.Content[j], s, child, out)
		}
	}
	for _, s := range cur.Content {
		if sequenceIndex(prop.Content, s, keys) < 0 {
			*out = append(*out, joinPath(path, identityValue(s, keys))+" -> (removed)")
		}
	}
}

// emitNew records every leaf of a newly introduced subtree as a change line.
func emitNew(node *yaml.Node, path string, out *[]string) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			emitNew(node.Content[i+1], joinPath(path, node.Content[i].Value), out)
		}
	case yaml.SequenceNode:
		for i := 0; i < len(node.Content); i++ {
			emitNew(node.Content[i], joinPath(path, strconv.Itoa(i)), out)
		}
	default:
		*out = append(*out, path+" -> "+scalarValue(node))
	}
}

// identityValue returns the value of the first present identity key of a
// keyed-list item, used to build the path segment for that item.
func identityValue(item *yaml.Node, keys []string) string {
	for _, k := range keys {
		if v := mappingValue(item, k); v != "" {
			return v
		}
	}
	return ""
}

// lastPathSegment returns the key after the final "/" of a path (the mapping
// key a sequence sits under), used to look up the sequence's identity keys.
func lastPathSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// joinPath appends a segment to a key path without producing a leading slash.
func joinPath(path, seg string) string {
	if path == "" {
		return seg
	}
	return path + "/" + seg
}

// scalarValue renders a scalar node for the diff output: strings are wrapped
// in single quotes (an empty string renders as two adjacent quotes), everything
// else stays bare.
func scalarValue(n *yaml.Node) string {
	if n.Tag == "!!str" {
		return "'" + strings.ReplaceAll(n.Value, "'", "''") + "'"
	}
	if n.Tag == "!!null" {
		return "null"
	}
	return n.Value
}

// nodeValue renders a non-scalar node as a single-line value for a
// structural-replacement diff line.
func nodeValue(n *yaml.Node) string {
	data, err := yaml.Marshal(n)
	if err != nil {
		return "..."
	}
	return strings.Join(splitLines(string(data)), "; ")
}

// truncate shortens a string to at most max runes, appending "…" when cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// spinner renders a single animated progress line on a writer (stderr) while a
// long-running model request is in flight, so the CLI never looks frozen. It
// redraws the line in place with a rotating frame + elapsed time and clears it
// on stop. stop is idempotent and blocks until the animation goroutine exits.
type spinner struct {
	msg    string
	w      io.Writer
	done   chan struct{}
	exited chan struct{}
	once   sync.Once
}

var spinnerFrames = []string{"|", "/", "-", "\\"}

// newSpinner creates a spinner that writes to w with the given message.
func newSpinner(w io.Writer, msg string) *spinner {
	return &spinner{msg: msg, w: w, done: make(chan struct{}), exited: make(chan struct{})}
}

// start launches the animation goroutine.
func (s *spinner) start() {
	go s.run()
}

// run redraws the progress line every 150ms until done is closed, then signals
// exited.
func (s *spinner) run() {
	defer close(s.exited)
	start := time.Now()
	i := 0
	for {
		select {
		case <-s.done:
			return
		case <-time.After(150 * time.Millisecond):
			fmt.Fprintf(s.w, "\r  %s %s %s  ", spinnerFrames[i%len(spinnerFrames)], s.msg, time.Since(start).Round(100*time.Millisecond))
			i++
		}
	}
}

// stop ends the animation and clears the line. Safe to call multiple times.
func (s *spinner) stop() {
	s.once.Do(func() {
		close(s.done)
		<-s.exited
		fmt.Fprintf(s.w, "\r\x1b[2K")
	})
}

// editAI runs the full AI edit flow against baseURL (the provider root; tests
// inject an httptest server). It reads the current config, asks the model for
// the new YAML, validates the reply (retrying once on invalid output), and —
// unless dryRun — writes configPath. The proposed YAML is normalized through
// yaml.Marshal after validation so the written bytes always round-trip. The
// model "lmstudio" selects the local LM Studio provider, which needs no API
// key; every other model value uses OpenRouter.
func editAI(baseURL, configPath, apiKey, model, prompt string, dryRun bool) error {
	if model == lmStudioModel {
		// LM Studio needs no credentials; drop any stale key so it is neither
		// sent to the local server nor persisted into .ENV.
		apiKey = ""
	} else if apiKey == "" {
		return fmt.Errorf("missing API key: pass --apikey, set OPENROUTER_API_KEY, or populate .ENV")
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

	// Remember the credentials that worked so the next run can omit the flags.
	persistEnv(apiKey, model)

	// Show the changed keys and their new values, never the whole file.
	changes, err := changedPaths(current, proposed)
	if err != nil {
		return fmt.Errorf("computing changed paths: %w", err)
	}
	if len(changes) == 0 {
		fmt.Println("No changes.")
		return nil
	}

	if !dryRun {
		if err := os.WriteFile(configPath, proposed, 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("Saved %s\n", configPath)
	}

	// Print the same compact change list in both dry-run and write mode.
	for _, line := range changes {
		fmt.Println(line)
	}
	if dryRun {
		fmt.Printf("\ndry run — not written\n")
	}
	return nil
}

// proposeEdit runs the chat + extract + merge + validation loop, retrying once
// when the model fragment fails to merge or the merged config fails to
// validate (the error is fed back to the model on the second attempt). Network
// and API errors are not retried, and the original file stays untouched. For
// the LM Studio provider the real model id is discovered from the local server
// once, up front, since the sentinel "lmstudio" is not a valid model id.
func proposeEdit(baseURL, apiKey, model, prompt string, current []byte) ([]byte, error) {
	provider := "OpenRouter"
	chatModel := model
	if model == lmStudioModel {
		provider = "LM Studio"
		id, err := lmStudioModelID(context.Background(), baseURL)
		if err != nil {
			return nil, err
		}
		chatModel = id
	}
	messages := buildEditPrompt(aiSpec, string(current), prompt)

	sp := newSpinner(os.Stderr, fmt.Sprintf("Contacting %s (model %s)", provider, chatModel))
	sp.start()
	content, err := chatCompletions(context.Background(), baseURL, apiKey, chatModel, messages)
	sp.stop()
	if err != nil {
		return nil, err
	}

	proposed, mergeErr := mergeAndValidate(current, content)
	if mergeErr == nil {
		return proposed, nil
	}

	// First attempt failed merge/validation: retry once, feeding the error back.
	sp = newSpinner(os.Stderr, "First reply invalid — retrying once")
	sp.start()
	content, err = chatCompletions(context.Background(), baseURL, apiKey, chatModel, buildRetryPrompt(aiSpec, string(current), prompt, mergeErr))
	sp.stop()
	if err != nil {
		return nil, err
	}

	proposed, err = mergeAndValidate(current, content)
	if err != nil {
		return nil, fmt.Errorf("model returned invalid YAML after retry: %w", err)
	}
	return proposed, nil
}

// buildRetryPrompt builds the user message for the second attempt, appending
// the merge/validator error from the first reply so the model can fix it.
func buildRetryPrompt(cheatsheet, currentYAML, prompt string, lastErr error) []chatMessage {
	msgs := buildEditPrompt(cheatsheet, currentYAML, prompt)
	msgs[1].Content += "\n\nYour previous reply failed to merge or validate with:\n" + lastErr.Error() +
		"\nFix the problem and return the corrected fragment in a ```yaml fence."
	return msgs
}

// mergeAndValidate merges the model's YAML fragment into the current config and
// validates the result, re-marshaling it so the written bytes always
// round-trip. A failure can only come from the fragment (the current config
// already parsed and validated), so the error is safe to feed back to the model.
func mergeAndValidate(current []byte, content string) ([]byte, error) {
	block := extractYAMLBlock(content)
	merged, err := mergeYAML(current, []byte(block))
	if err != nil {
		return nil, err
	}
	cfg, err := parser.Parse(merged)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}
