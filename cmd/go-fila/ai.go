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
	"sync"
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
	// aiHTTPTimeout caps a single chat request. 300s is generous enough for
	// slow free-tier models (e.g. nvidia/nemotron-...:free) to regenerate a
	// full multi-KB go-fila.yaml; the old 90s deadline regularly fired on them.
	aiHTTPTimeout = 300 * time.Second
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

// envPathFunc returns the .ENV file that records the last used OpenRouter
// credentials (a dotenv file in the current folder). It is a variable so tests
// can redirect it away from the repository.
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
	case "resources", "pages", "fields", "actions":
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

// changedSections returns the top-level YAML sections that differ between
// current and proposed, rendered from proposed as a standalone fragment. Only
// the sections that actually changed are included, so terminal output stays
// small instead of echoing the whole config.
func changedSections(current, proposed []byte) ([]byte, error) {
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
	var out yaml.Node
	out.Kind = yaml.MappingNode
	for i := 0; i < len(propRoot.Content); i += 2 {
		key := propRoot.Content[i]
		j := mappingIndex(curRoot, key.Value)
		if j == -1 || !nodeEqual(curRoot.Content[j+1], propRoot.Content[i+1]) {
			out.Content = append(out.Content, cloneNode(key), cloneNode(propRoot.Content[i+1]))
		}
	}
	return yaml.Marshal(&out)
}

// nodeEqual reports whether two YAML nodes render the same content, ignoring
// style and comments.
func nodeEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value {
		return false
	}
	if len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if !nodeEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
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

// editAI runs the full AI edit flow against baseURL (the OpenRouter root; tests
// inject an httptest server). It reads the current config, asks the model for
// the new YAML, validates the reply (retrying once on invalid output), and —
// unless dryRun — writes configPath. The proposed YAML is normalized through
// yaml.Marshal after validation so the written bytes always round-trip.
func editAI(baseURL, configPath, apiKey, model, prompt string, dryRun bool) error {
	if apiKey == "" {
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

	// Show only the YAML sections that actually changed, never the whole file.
	changed, err := changedSections(current, proposed)
	if err != nil {
		return fmt.Errorf("computing changed sections: %w", err)
	}
	if len(changed) == 0 {
		fmt.Println("No changes.")
		return nil
	}

	if !dryRun {
		if err := os.WriteFile(configPath, proposed, 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}
		fmt.Printf("Saved %s\n", configPath)
	}

	// Print the same compact section preview in both dry-run and write mode.
	fmt.Printf("%s\n", changed)
	if dryRun {
		fmt.Printf("\ndry run — not written\n")
	}
	return nil
}

// proposeEdit runs the chat + extract + merge + validation loop, retrying once
// when the model fragment fails to merge or the merged config fails to
// validate (the error is fed back to the model on the second attempt). Network
// and API errors are not retried, and the original file stays untouched.
func proposeEdit(baseURL, apiKey, model, prompt string, current []byte) ([]byte, error) {
	messages := buildEditPrompt(aiSpec, string(current), prompt)

	sp := newSpinner(os.Stderr, fmt.Sprintf("Contacting OpenRouter (model %s)", model))
	sp.start()
	content, err := openrouterChat(context.Background(), baseURL, apiKey, model, messages)
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
	content, err = openrouterChat(context.Background(), baseURL, apiKey, model, buildRetryPrompt(aiSpec, string(current), prompt, mergeErr))
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
