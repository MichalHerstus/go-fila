// mcp_test.go — wedit /mcp endpoints (E5), the opencode Streamable HTTP flow.
package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mcpPost sends one JSON-RPC request to POST /mcp and returns the raw body.
func mcpPost(t *testing.T, h http.Handler, method string, params interface{}) (int, []byte) {
	t.Helper()
	body := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// mcpTool invokes tools/call and returns the text result.
func mcpTool(t *testing.T, h http.Handler, name string, args map[string]interface{}) string {
	t.Helper()
	code, body := mcpPost(t, h, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	if code != http.StatusOK {
		t.Fatalf("tools/call %s status = %d: %s", name, code, body)
	}
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("bad MCP response: %v: %s", err, body)
	}
	if out.Result.IsError {
		t.Fatalf("tool %s errored: %s", name, out.Result.Content[0].Text)
	}
	if len(out.Result.Content) == 0 {
		t.Fatalf("tool %s: empty content", name)
	}
	return out.Result.Content[0].Text
}

func TestMCPInitializeAndTools(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	h := s.Handler()

	code, body := mcpPost(t, h, "initialize", map[string]interface{}{"protocolVersion": "2025-06-18"})
	if code != http.StatusOK {
		t.Fatalf("initialize status = %d: %s", code, body)
	}
	if !strings.Contains(string(body), `"protocolVersion"`) {
		t.Fatalf("initialize missing protocolVersion: %s", body)
	}

	code, body = mcpPost(t, h, "tools/list", nil)
	if code != http.StatusOK || !strings.Contains(string(body), `"set_value"`) {
		t.Fatalf("tools/list = %d: %s", code, body)
	}
}

func TestMCPGetMethodNotAllowed(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp status = %d", rec.Code)
	}
}

// TestMCPFullFlow drives the opencode workflow end-to-end: initialize ->
// set_value -> get_value -> validate -> save, then checks the file + backup.
func TestMCPFullFlow(t *testing.T) {
	s, dir := setupServer(t, testConfig())
	h := s.Handler()
	configPath := filepath.Join(dir, "yaga.yaml")

	// mutate through MCP
	text := mcpTool(t, h, "set_value", map[string]interface{}{"path": "panel/name", "value": "CRM"})
	if !strings.Contains(text, "set panel/name") {
		t.Fatalf("set_value: %s", text)
	}
	// read back
	text = mcpTool(t, h, "get_value", map[string]interface{}{"path": "panel/name"})
	if text != "CRM" {
		t.Fatalf("get_value = %q", text)
	}
	// the SPA side sees the edit too
	_, data := get(t, h, "GET", "/api/config", nil)
	panel := data["config"].(map[string]interface{})["panel"].(map[string]interface{})
	if panel["name"] != "CRM" {
		t.Fatalf("SPA config did not reflect MCP edit: %v", panel["name"])
	}

	// validate must be OK
	text = mcpTool(t, h, "validate", nil)
	if !strings.Contains(text, "OK") {
		t.Fatalf("validate = %q", text)
	}

	// save writes disk + backup
	text = mcpTool(t, h, "save", nil)
	if !strings.Contains(text, "Written to "+configPath) {
		t.Fatalf("save = %q", text)
	}
	disk, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), "CRM") != true {
		t.Fatalf("saved config missing edit:\n%s", disk)
	}
	bak, err := os.ReadFile(configPath + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if strings.Contains(string(bak), "CRM") {
		t.Fatalf("backup must hold the pre-save state:\n%s", bak)
	}
}

// TestMCPGetAndResources makes sure read tooling is wired.
func TestMCPGetAndResources(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	h := s.Handler()
	text := mcpTool(t, h, "list_resources", nil)
	if !strings.Contains(text, `"name":"User"`) {
		t.Fatalf("list_resources = %s", text)
	}
	text = mcpTool(t, h, "get_config", nil)
	if !strings.Contains(text, `"path":"/admin"`) {
		t.Fatalf("get_config = %s", text)
	}
}
