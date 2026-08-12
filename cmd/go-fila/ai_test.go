package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain redirects the .ENV file to a throwaway temp dir for the whole
// package so editAI runs never drop a .ENV file into the repository.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "go-fila-ai-env-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	old := envPathFunc
	envPathFunc = func() string { return filepath.Join(dir, ".ENV") }
	defer func() { envPathFunc = old }()
	os.Exit(m.Run())
}

// setEnvPath points the .ENV reader/writer at a specific file for the duration
// of a test.
func setEnvPath(t *testing.T, path string) {
	t.Helper()
	old := envPathFunc
	envPathFunc = func() string { return path }
	t.Cleanup(func() { envPathFunc = old })
}

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

const validReply = "```yaml\npanel:\n  name: Order management\n```"

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

func TestParseEditFlagsUsesPersistedEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	envFile := filepath.Join(t.TempDir(), ".ENV")
	if err := os.WriteFile(envFile, []byte("OPENROUTER_API_KEY=sk-file\nMODEL=nvidia/model:free\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnvPath(t, envFile)

	apiKey, model, _, _ := parseEditFlags([]string{"--prompt", "hi"})
	if apiKey != "sk-file" {
		t.Errorf("apiKey = %q, want persisted .ENV sk-file", apiKey)
	}
	if model != "nvidia/model:free" {
		t.Errorf("model = %q, want persisted .ENV model", model)
	}
}

func TestParseEditFlagsEnvVarBeatsPersistedEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-env")
	envFile := filepath.Join(t.TempDir(), ".ENV")
	if err := os.WriteFile(envFile, []byte("OPENROUTER_API_KEY=sk-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnvPath(t, envFile)

	apiKey, _, _, _ := parseEditFlags([]string{"--prompt", "hi"})
	if apiKey != "sk-env" {
		t.Errorf("apiKey = %q, want env sk-env over .ENV", apiKey)
	}
}

func TestParseEditFlagsModelFlagBeatsPersistedEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	envFile := filepath.Join(t.TempDir(), ".ENV")
	if err := os.WriteFile(envFile, []byte("OPENROUTER_API_KEY=sk-file\nMODEL=persisted-model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnvPath(t, envFile)

	apiKey, model, _, _ := parseEditFlags([]string{"--prompt", "hi", "--model", "openrouter/gemini"})
	if apiKey != "sk-file" {
		t.Errorf("apiKey = %q, want persisted sk-file", apiKey)
	}
	if model != "openrouter/gemini" {
		t.Errorf("model = %q, want flag value over .ENV", model)
	}
}

func TestParseEditFlagsNoPersistedEnvUsesDefaults(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	setEnvPath(t, filepath.Join(t.TempDir(), "missing.env"))

	apiKey, model, _, _ := parseEditFlags([]string{"--prompt", "hi"})
	if apiKey != "" {
		t.Errorf("apiKey = %q, want empty without persisted value", apiKey)
	}
	if model != defaultModel {
		t.Errorf("model = %q, want default %q", model, defaultModel)
	}
}

func TestWriteEnvFilePreservesUnrelatedLines(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".ENV")
	if err := os.WriteFile(envFile, []byte("FOO=bar\n# comment\nMODEL=old-model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setEnvPath(t, envFile)

	if err := writeEnvFile("sk-new", "model/new"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{"FOO=bar", "# comment"} {
		if !strings.Contains(string(data), keep) {
			t.Errorf("unrelated line %q must be preserved:\n%s", keep, data)
		}
	}
	if !strings.Contains(string(data), "OPENROUTER_API_KEY=sk-new") || !strings.Contains(string(data), "MODEL=model/new") {
		t.Errorf(".ENV should be updated:\n%s", data)
	}
	if strings.Contains(string(data), "MODEL=old-model") {
		t.Errorf("old MODEL must be replaced:\n%s", data)
	}
}

func TestEditAIPersistsEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	setEnvPath(t, filepath.Join(dir, ".ENV"))
	url, _ := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		return chatReply(validReply), http.StatusOK
	})

	if err := editAI(url, path, "sk-persist", "openrouter/model-x", "Change title", false); err != nil {
		t.Fatalf("editAI: %v", err)
	}
	if err := editAI(url, path, "sk-persist", "openrouter/model-x", "Change title", true); err != nil {
		t.Fatalf("editAI dry-run: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".ENV"))
	if err != nil {
		t.Fatalf("expected .ENV to be written: %v", err)
	}
	if !strings.Contains(string(data), "OPENROUTER_API_KEY=sk-persist") || !strings.Contains(string(data), "MODEL=openrouter/model-x") {
		t.Errorf(".ENV contents:\n%s", data)
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

// mergeBase is a representative current config for mergeYAML tests.
const mergeBase = `version: "1.0"
panel:
  path: /admin
  name: Admin
  brand:
    colors:
      primary: "#6366f1"
resources:
  - name: User
    label: Users
  - name: Order
    label: Orders
navigation:
  - group: Management
    items:
      - resource: User
pages:
  - name: Dashboard
    widgets:
      - type: stat
        label: Total
`

// mergeContains asserts all of wants are substrings of the merged bytes.
func mergeContains(t *testing.T, out []byte, wants ...string) {
	t.Helper()
	s := string(out)
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("merged output missing %q:\n%s", w, s)
		}
	}
}

// mergeNotContains asserts none of nots are substrings of the merged bytes.
func mergeNotContains(t *testing.T, out []byte, nots ...string) {
	t.Helper()
	s := string(out)
	for _, n := range nots {
		if strings.Contains(s, n) {
			t.Errorf("merged output should not contain %q:\n%s", n, s)
		}
	}
}

func TestMergeYAMLMappingDeepMerge(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("panel:\n  name: Orders HQ\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "name: Orders HQ", "path: /admin", `primary: "#6366f1"`, "name: User")
}

func TestMergeYAMLMappingNewKey(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("panel:\n  id: ops\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "id: ops", "name: Admin", "path: /admin")
}

func TestMergeYAMLKeyedResource(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("resources:\n  - name: User\n    label: Members\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "name: User", "label: Members")
	mergeContains(t, out, "name: Order", "label: Orders")
	mergeNotContains(t, out, "label: Users")
}

func TestMergeYAMLKeyedResourceAppend(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("resources:\n  - name: Product\n    label: Products\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "name: Product", "label: Products", "name: User", "name: Order")
}

func TestMergeYAMLKeyedFields(t *testing.T) {
	cur := `version: "1.0"
panel:
  path: /admin
  name: Admin
resources:
  - name: User
    label: Users
    form:
      update:
        fields:
          - name: email
            label: Email
`
	out, err := mergeYAML([]byte(cur), []byte("resources:\n  - name: User\n    form:\n      update:\n        fields:\n          - name: email\n            label: Primary email\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "label: Primary email")
	mergeNotContains(t, out, "label: Email")
}

func TestMergeYAMLNavigationKeyed(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("navigation:\n  - group: Management\n    items:\n      - resource: User\n        label: Accounts\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "label: Accounts", "group: Management")
}

func TestMergeYAMLWholesaleReplace(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("pages:\n  - name: Dashboard\n    widgets:\n      - type: stat\n        label: New\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "label: New")
	mergeNotContains(t, out, "label: Total")
}

func TestMergeYAMLNullLeavesUntouched(t *testing.T) {
	out, err := mergeYAML([]byte(mergeBase), []byte("panel:\n  name: null\n"))
	if err != nil {
		t.Fatalf("mergeYAML: %v", err)
	}
	mergeContains(t, out, "name: Admin")
}

func TestMergeYAMLUnknownTopLevelKey(t *testing.T) {
	_, err := mergeYAML([]byte(mergeBase), []byte("frobnicate:\n  on: true\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown top-level key") {
		t.Fatalf("expected unknown-top-level-key error, got %v", err)
	}
}

func TestMergeYAMLMalformedFragment(t *testing.T) {
	if _, err := mergeYAML([]byte(mergeBase), []byte("panel: [unclosed")); err == nil {
		t.Fatal("expected malformed-fragment error")
	}
}

func TestMergeYAMLEmptyFragment(t *testing.T) {
	if _, err := mergeYAML([]byte(mergeBase), []byte("  \n")); err == nil {
		t.Fatal("expected empty-fragment error")
	}
}

func TestMergeYAMLNonMappingFragment(t *testing.T) {
	if _, err := mergeYAML([]byte(mergeBase), []byte("- just\n- a list\n")); err == nil {
		t.Fatal("expected non-mapping-fragment error")
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
	for _, keep := range []string{"path: /admin", "name: User", "version: \"1.0\""} {
		if !strings.Contains(string(data), keep) {
			t.Errorf("unrelated section %q should be preserved, got:\n%s", keep, data)
		}
	}
	s := out()
	if !strings.Contains(s, "Saved") || !strings.Contains(s, "Order management") {
		t.Errorf("stdout should print the save message and the changed section, got:\n%s", s)
	}
	if strings.Contains(s, "resources") {
		t.Errorf("stdout should print only the changed section, not the whole file, got:\n%s", s)
	}
}

func TestEditAIRetryOnInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir)
	url, calls := stubOpenRouter(t, func(i int, r chatRequest) (string, int) {
		if i == 0 {
			// Invalid fragment: empty panel.name fails validation after merge.
			return chatReply("```yaml\npanel:\n  name: \"\"\n```"), http.StatusOK
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
		t.Errorf("dry-run stdout should preview the proposed change, got:\n%s", s)
	}
	if strings.Contains(s, "resources") {
		t.Errorf("dry-run stdout should print only the changed section, not the whole file, got:\n%s", s)
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
