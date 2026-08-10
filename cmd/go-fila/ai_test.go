package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestConfig writes a minimal valid go-fila.yaml and returns its path.
func writeTestConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "go-fila.yaml")
	cfg := "version: \"1.0\"\npanel:\n  path: /admin\n  name: Admin\nresources:\n  - name: User\n"
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validReply = "```yaml\nversion: \"1.0\"\npanel:\n  path: /admin\n  name: Order management\nresources:\n  - name: User\n```"

// stubCall records what one request sent to the fake OpenRouter.
type stubCall struct {
	authHeader string
	req        chatRequest
}

// stubOpenRouter spins up an httptest server standing in for OpenRouter. The
// respond callback receives the call index and the decoded request and returns
// the response body + HTTP status. Returns the server URL and the recorded
// calls (also closed via t.Cleanup).
func stubOpenRouter(t *testing.T, respond func(i int, r chatRequest) (string, int)) (string, *[]stubCall) {
	t.Helper()
	var calls []stubCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cr chatRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&cr)
		}
		calls = append(calls, stubCall{authHeader: r.Header.Get("Authorization"), req: cr})
		body, status := respond(len(calls)-1, cr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

// chatReply wraps content into a /chat/completions-style success response.
func chatReply(content string) string {
	resp := chatResponse{
		Choices: []struct {
			Message chatMessage `json:"message"`
		}{{Message: chatMessage{Role: "assistant", Content: content}}},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// captureStdout redirects os.Stdout to a temp file for the test's lifetime and
// returns a func that reads back everything printed so far.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	old := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = f
	t.Cleanup(func() {
		os.Stdout = old
		f.Close()
	})
	return func() string {
		b, _ := os.ReadFile(f.Name())
		return string(b)
	}
}

// TestEmbeddedAISpec ensures the ai_spec.md schema cheat-sheet is embedded in
// the binary and carries the key schema markers.
func TestEmbeddedAISpec(t *testing.T) {
	for _, want := range []string{"resources", "panel.path", "options_query", "policies", "Keep the top-level `version` field unchanged"} {
		if !strings.Contains(aiSpec, want) {
			t.Errorf("embedded ai_spec.md missing %q (%d bytes)", want, len(aiSpec))
		}
	}
	if len(aiSpec) > 8<<10 {
		t.Errorf("ai_spec.md should stay compact, got %d bytes", len(aiSpec))
	}
}

func TestParseEditFlags(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	apiKey, model, prompt, dry := parseEditFlags([]string{
		"--prompt", "Change the title", "--apikey", "sk-abc",
		"--model", "openrouter/gemini", "--dry-run",
	})
	if apiKey != "sk-abc" {
		t.Errorf("apiKey = %q, want sk-abc", apiKey)
	}
	if model != "openrouter/gemini" {
		t.Errorf("model = %q, want openrouter/gemini", model)
	}
	if prompt != "Change the title" {
		t.Errorf("prompt = %q", prompt)
	}
	if !dry {
		t.Error("dryRun should be true")
	}
}

func TestParseEditFlagsEnvFallback(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-env")
	apiKey, model, prompt, dry := parseEditFlags([]string{"--prompt", "hi"})
	if apiKey != "sk-env" {
		t.Errorf("apiKey = %q, want env fallback sk-env", apiKey)
	}
	if model != defaultModel {
		t.Errorf("model = %q, want default %q", model, defaultModel)
	}
	if prompt != "hi" {
		t.Errorf("prompt = %q", prompt)
	}
	if dry {
		t.Error("dryRun should be false")
	}
}

func TestParseEditFlagsSkipsGlobalFlags(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	_, _, prompt, _ := parseEditFlags([]string{"--config", "other.yaml", "--prompt", "go"})
	if prompt != "go" {
		t.Errorf("prompt = %q, want go (must ignore --config)", prompt)
	}
}

func TestResolvePromptPlain(t *testing.T) {
	got, err := resolvePrompt("Change the title")
	if err != nil || got != "Change the title" {
		t.Fatalf("resolvePrompt = %q, %v; want passthrough", got, err)
	}
}

func TestResolvePromptFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sampleprompt.txt")
	want := "Change the panel title to Order management\nKeep resources untouched."
	if err := os.WriteFile(p, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePrompt("file://" + p)
	if err != nil {
		t.Fatalf("resolvePrompt: %v", err)
	}
	if got != want {
		t.Errorf("resolvePrompt = %q, want %q", got, want)
	}
}

func TestResolvePromptTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "p.txt"), []byte("from tilde"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePrompt("file://~/p.txt")
	if err != nil {
		t.Fatalf("resolvePrompt: %v", err)
	}
	if got != "from tilde" {
		t.Errorf("resolvePrompt = %q, want \"from tilde\"", got)
	}
}

func TestResolvePromptErrors(t *testing.T) {
	for _, in := range []string{"file://", "file:///nonexistent/does-not-exist.txt"} {
		if _, err := resolvePrompt(in); err == nil {
			t.Errorf("resolvePrompt(%q) expected an error", in)
		}
	}
}

// TestEditAIFilePrompt verifies a file:// prompt is read and sent to the model.
func TestEditAIFilePrompt(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	promptFile := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("Use Order management for the title"), 0644); err != nil {
		t.Fatal(err)
	}
	url, _ := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		if !strings.Contains(r.Messages[1].Content, "Use Order management for the title") {
			t.Error("model request should carry the prompt file contents")
		}
		return chatReply(validReply), http.StatusOK
	})
	if err := editAI(url, path, "sk", defaultModel, "file://"+promptFile, false); err != nil {
		t.Fatalf("editAI: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Order management") {
		t.Errorf("config should contain the change, got:\n%s", data)
	}
}

func TestBuildEditPrompt(t *testing.T) {
	msgs := buildEditPrompt("CHEAT", "CUR", "INSTR")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "yaml fence") {
		t.Errorf("system message missing output contract: %q", msgs[0].Content)
	}
	if msgs[1].Role != "user" {
		t.Errorf("second message role = %q, want user", msgs[1].Role)
	}
	for _, want := range []string{"CHEAT", "CUR", "INSTR", "```yaml"} {
		if !strings.Contains(msgs[1].Content, want) {
			t.Errorf("user message missing %q", want)
		}
	}
}

func TestExtractYAMLBlock(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"yaml fence", "Here you go:\n```yaml\nversion: \"1.0\"\npanel:\n  name: X\n```\nHope that helps", "version: \"1.0\"\npanel:\n  name: X"},
		{"any fence", "```\nfoo: bar\n```", "foo: bar"},
		{"no fence", "  \nversion: x\n", "version: x"},
		{"crlf fence", "```yaml\r\nversion: \"1.0\"\r\n```", "version: \"1.0\""},
	}
	for _, c := range cases {
		if got := extractYAMLBlock(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDiffLines(t *testing.T) {
	got := diffLines("a\nb\nc\n", "a\nb\nx\n")
	want := []string{"  a", "  b", "- c", "+ x"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("diff[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestDiffLinesCollapsesUnchangedRuns(t *testing.T) {
	var oldLines, newLines []string
	for i := 0; i < 10; i++ {
		oldLines = append(oldLines, "line")
		if i == 4 {
			oldLines = append(oldLines, "gone")
		}
	}
	for i := 0; i < 10; i++ {
		newLines = append(newLines, "line")
	}
	got := diffLines(strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"))
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "- gone") {
		t.Errorf("diff should contain the removed line: %v", got)
	}
	if strings.Count(joined, "line") > 8 {
		t.Errorf("unchanged context should be collapsed, got %d context lines", strings.Count(joined, "line"))
	}
}

func TestEditAIHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	url, calls := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		if r.Temperature != 0 {
			t.Errorf("temperature = %v, want 0", r.Temperature)
		}
		if r.Model != defaultModel {
			t.Errorf("model = %q, want %q", r.Model, defaultModel)
		}
		if len(r.Messages) != 2 {
			t.Errorf("messages = %d, want 2", len(r.Messages))
		}
		if !strings.Contains(r.Messages[1].Content, "schema cheat-sheet") {
			t.Error("user message should embed the ai_spec cheat-sheet")
		}
		return chatReply(validReply), http.StatusOK
	})
	out := captureStdout(t)

	if err := editAI(url, path, "sk-test", defaultModel, "Change the dashboard title", false); err != nil {
		t.Fatalf("editAI: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected exactly 1 request, got %d", len(*calls))
	}
	if (*calls)[0].authHeader != "Bearer sk-test" {
		t.Errorf("auth header = %q, want Bearer sk-test", (*calls)[0].authHeader)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Order management") {
		t.Errorf("config file should contain the change, got:\n%s", data)
	}
	s := out()
	if !strings.Contains(s, "Saved") || !strings.Contains(s, "-") || !strings.Contains(s, "+") {
		t.Errorf("stdout should print the save message and diff, got:\n%s", s)
	}
}

func TestEditAIRetryOnInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	url, calls := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		if i == 0 {
			// Invalid: panel.name missing.
			return chatReply("```yaml\nversion: \"1.0\"\n```"), http.StatusOK
		}
		if !strings.Contains(r.Messages[1].Content, "panel.name is required") {
			t.Error("retry message should feed back the validator error")
		}
		return chatReply(validReply), http.StatusOK
	})

	if err := editAI(url, path, "sk", defaultModel, "Change the title", false); err != nil {
		t.Fatalf("editAI: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 requests (initial + retry), got %d", len(*calls))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Order management") {
		t.Errorf("file should contain the retried change, got:\n%s", data)
	}
}

func TestEditAIRetryFailsOnSecondInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	url, _ := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		return chatReply("not: yaml: [bad"), http.StatusOK
	})

	err := editAI(url, path, "sk", defaultModel, "change", false)
	if err == nil || !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("expected invalid-YAML error after retry, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Admin") {
		t.Error("original file must be untouched on failure")
	}
}

func TestEditAIDryRunNeverWrites(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	url, _ := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		return chatReply(validReply), http.StatusOK
	})
	out := captureStdout(t)

	if err := editAI(url, path, "sk", defaultModel, "Change the title", true); err != nil {
		t.Fatalf("editAI dry-run: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Admin") || strings.Contains(string(data), "Order management") {
		t.Errorf("dry-run must not write; file has:\n%s", data)
	}
	s := out()
	if !strings.Contains(s, "dry run") || !strings.Contains(s, "Order management") {
		t.Errorf("dry-run stdout should preview the proposed YAML, got:\n%s", s)
	}
}

func TestEditAIMissingKey(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	err := editAI("http://unused.invalid", path, "", defaultModel, "change", false)
	if err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("expected clear missing-key error, got %v", err)
	}
}

func TestEditAIEmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	err := editAI("http://unused.invalid", path, "sk", defaultModel, "   ", false)
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("expected clear missing-prompt error, got %v", err)
	}
}

func TestOpenRouterHTTPError(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	url, _ := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		return `{"error":{"message":"insufficient credits"}}`, http.StatusUnauthorized
	})
	err := editAI(url, path, "sk", defaultModel, "change", false)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected HTTP 401 error, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Admin") {
		t.Error("original file must be untouched on HTTP error")
	}
}

func TestEditAIConfigReadError(t *testing.T) {
	url, _ := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		return chatReply(validReply), http.StatusOK
	})
	err := editAI(url, filepath.Join(t.TempDir(), "missing.yaml"), "sk", defaultModel, "change", false)
	if err == nil || !strings.Contains(err.Error(), "reading current config") {
		t.Fatalf("expected config read error, got %v", err)
	}
}
