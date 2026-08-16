package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/parser"
	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// stubState implements State against an in-memory config for tests.
type stubState struct {
	cfg        *types.Config
	configPath string
	saves      int
	commits    int
}

func (s *stubState) ConfigPath() string       { return s.configPath }
func (s *stubState) Config() *types.Config    { return s.cfg }
func (s *stubState) Commit(cfg *types.Config) { s.cfg = cfg; s.commits++ }
func (s *stubState) Save() error              { s.saves++; return nil }
func (s *stubState) ReadConfigFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
func (s *stubState) Parse(data []byte) (*types.Config, []string, []string) {
	var cfg types.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, []string{"parsing config: " + err.Error()}, nil
	}
	var errs, warns []string
	for _, e := range parser.ValidateAll(&cfg) {
		if _, ok := e.(parser.Warning); ok {
			warns = append(warns, e.Error())
		} else {
			errs = append(errs, e.Error())
		}
	}
	return &cfg, errs, warns
}
func (s *stubState) Report(cfg *types.Config) ([]string, []string) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return []string{err.Error()}, nil
	}
	_, errs, warns := s.Parse(data)
	return errs, warns
}

// stubConfig is a minimal valid config.
func stubConfig() *types.Config {
	return &types.Config{
		Version: "1.0",
		Panel:   types.Panel{Name: "My Admin", Path: "/admin", ID: "admin"},
		Resources: []types.Resource{
			{Name: "User", Label: "Users", Icon: "user", Table: "users",
				List: &types.ListConfig{Columns: []types.Column{{Name: "id", Type: "integer"}}}},
		},
		Navigation: []types.NavigationGroup{{
			Group: "Sales",
			Items: []types.NavigationItem{{Type: "resource", Resource: "User", Label: "Users"}},
		}},
	}
}

func newStub(t *testing.T) *Server {
	t.Helper()
	st := &stubState{cfg: stubConfig(), configPath: "yaga.yaml"}
	return New(st)
}

func stubStateOf(s *Server) *stubState { return s.state.(*stubState) }

// rpcCall issues a request with the given method+params and returns result.
func rpcCall(t *testing.T, s *Server, method string, params interface{}) interface{} {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	resp, send := s.Handle(body)
	if !send {
		t.Fatalf("method %s produced no response", method)
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("bad response: %v: %s", err, resp)
	}
	if out.Error != nil {
		t.Fatalf("rpc error %d: %s", out.Error.Code, out.Error.Message)
	}
	var res interface{}
	if err := json.Unmarshal(out.Result, &res); err != nil {
		t.Fatalf("bad result: %v", err)
	}
	return res
}

// toolCall invokes a tools/call and returns (text, isError).
func toolCall(t *testing.T, s *Server, name string, args map[string]interface{}) (string, bool) {
	t.Helper()
	res := rpcCall(t, s, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	m := res.(map[string]interface{})
	content := m["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	isErr, _ := m["isError"].(bool)
	return text, isErr
}

func TestInitialize(t *testing.T) {
	s := newStub(t)
	res := rpcCall(t, s, "initialize", map[string]interface{}{"protocolVersion": "2025-06-18"})
	m := res.(map[string]interface{})
	if m["protocolVersion"] != ProtocolVersion {
		t.Fatalf("protocolVersion = %v", m["protocolVersion"])
	}
	caps := m["capabilities"].(map[string]interface{})
	if _, ok := caps["tools"]; !ok {
		t.Fatalf("missing tools capability: %v", caps)
	}
	info := m["serverInfo"].(map[string]interface{})
	if info["name"] != "yaga" {
		t.Fatalf("serverInfo = %v", info)
	}
}

func TestToolsList(t *testing.T) {
	s := newStub(t)
	res := rpcCall(t, s, "tools/list", nil)
	tools := res.(map[string]interface{})["tools"].([]interface{})
	names := map[string]bool{}
	for _, t := range tools {
		names[t.(map[string]interface{})["name"].(string)] = true
	}
	want := []string{"validate", "save", "open", "get_config", "get_value",
		"list_resources", "list_navigation", "set_value", "merge_yaml_fragment",
		"add_resource", "remove_resource", "add_column", "add_field",
		"add_nav_item", "remove_nav_item"}
	for _, w := range want {
		if !names[w] {
			t.Errorf("missing tool %s", w)
		}
	}
	if len(names) != len(want) {
		t.Errorf("got %d tools, want %d: %v", len(names), len(want), names)
	}
}

func TestNotificationGetsNoResponse(t *testing.T) {
	s := newStub(t)
	resp, send := s.Handle([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if send || resp != nil {
		t.Fatalf("notification must not produce a response (send=%v)", send)
	}
}

func TestBatchRejected(t *testing.T) {
	s := newStub(t)
	resp, _ := s.Handle([]byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`))
	if !strings.Contains(string(resp), "batched") {
		t.Fatalf("expected batch rejection, got %s", resp)
	}
}

func TestGetAndSetValue(t *testing.T) {
	s := newStub(t)
	text, err := toolCall(t, s, "get_value", map[string]interface{}{"path": "panel/name"})
	if err || text != "My Admin" {
		t.Fatalf("get_value panel/name = %q err=%v", text, err)
	}
	text, err = toolCall(t, s, "set_value", map[string]interface{}{"path": "panel/brand/logo", "value": "newlogo.jpg"})
	if err {
		t.Fatalf("set_value failed: %s", text)
	}
	if !strings.Contains(text, "set panel/brand/logo") {
		t.Fatalf("unexpected text: %s", text)
	}
	text, _ = toolCall(t, s, "get_value", map[string]interface{}{"path": "PANEL/brand/logo"})
	if text != "newlogo.jpg" {
		t.Fatalf("case-insensitive get = %q", text)
	}
	if stubStateOf(s).commits != 1 {
		t.Fatalf("expected 1 commit, got %d", stubStateOf(s).commits)
	}
}

func TestSetValueMissingPath(t *testing.T) {
	s := newStub(t)
	text, isErr := toolCall(t, s, "set_value", map[string]interface{}{"path": "panel/nope", "value": "x"})
	if !isErr || !strings.Contains(text, "path not found") {
		t.Fatalf("expected path-not-found, got %q err=%v", text, isErr)
	}
}

func TestSetValueInvalidConfigRejected(t *testing.T) {
	s := newStub(t)
	text, isErr := toolCall(t, s, "set_value", map[string]interface{}{"path": "panel/name", "value": ""})
	if !isErr || !strings.Contains(text, "panel.name is required") {
		t.Fatalf("expected validation failure, got %q err=%v", text, isErr)
	}
	if stubStateOf(s).commits != 0 {
		t.Fatal("invalid edit must not commit")
	}
}

func TestGetValueCaseInsensitiveResources(t *testing.T) {
	s := newStub(t)
	text, isErr := toolCall(t, s, "get_value", map[string]interface{}{"path": "resources/user/list/columns/id/type"})
	if isErr || text != "integer" {
		t.Fatalf("got %q err=%v", text, isErr)
	}
}

func TestAddRemoveResource(t *testing.T) {
	s := newStub(t)
	text, isErr := toolCall(t, s, "add_resource", map[string]interface{}{
		"resource": map[string]interface{}{"name": "Order", "label": "Orders"},
	})
	if isErr {
		t.Fatalf("add_resource: %s", text)
	}
	text, isErr = toolCall(t, s, "get_value", map[string]interface{}{"path": "resources/order/label"})
	if isErr || text != "Orders" {
		t.Fatalf("new resource label = %q err=%v", text, isErr)
	}
	// duplicates rejected
	text, isErr = toolCall(t, s, "add_resource", map[string]interface{}{
		"resource": map[string]interface{}{"name": "Order"},
	})
	if !isErr || !strings.Contains(text, "already exists") {
		t.Fatalf("duplicate add not rejected: %q err=%v", text, isErr)
	}
	_, isErr = toolCall(t, s, "remove_resource", map[string]interface{}{"name": "Order"})
	if isErr {
		t.Fatalf("remove_resource: %s", text)
	}
	text, isErr = toolCall(t, s, "get_value", map[string]interface{}{"path": "resources/Order/label"})
	if !isErr || !strings.Contains(text, "path not found") {
		t.Fatalf("removed resource still present: %q err=%v", text, isErr)
	}
}

func TestAddColumnAndField(t *testing.T) {
	s := newStub(t)
	_, isErr := toolCall(t, s, "add_column", map[string]interface{}{
		"resource": "User",
		"column":   map[string]interface{}{"name": "created_at", "type": "datetime"},
	})
	if isErr {
		t.Fatal("add_column failed")
	}
	text, isErr := toolCall(t, s, "get_value", map[string]interface{}{"path": "resources/User/list/columns/created_at/type"})
	if isErr || text != "datetime" {
		t.Fatalf("column type = %q err=%v", text, isErr)
	}

	_, isErr = toolCall(t, s, "add_field", map[string]interface{}{
		"resource": "User",
		"section":  "create",
		"field":    map[string]interface{}{"name": "email", "type": "email"},
	})
	if isErr {
		t.Fatal("add_field failed")
	}
	text, isErr = toolCall(t, s, "get_value", map[string]interface{}{"path": "resources/User/form/create/fields/email/type"})
	if isErr || text != "email" {
		t.Fatalf("field type = %q err=%v", text, isErr)
	}
}

func TestNavItemAddRemove(t *testing.T) {
	s := newStub(t)
	_, isErr := toolCall(t, s, "add_nav_item", map[string]interface{}{
		"group": "Sales",
		"item":  map[string]interface{}{"type": "page", "page": "dashboard", "label": "Dashboard"},
	})
	if isErr {
		t.Fatal("add_nav_item failed")
	}
	text, isErr := toolCall(t, s, "list_navigation", nil)
	if isErr || !strings.Contains(text, "Dashboard") {
		t.Fatalf("list_navigation missing new item: %q err=%v", text, isErr)
	}
	_, isErr = toolCall(t, s, "remove_nav_item", map[string]interface{}{
		"group": "Sales",
		"item":  "Dashboard",
	})
	if isErr {
		t.Fatal("remove_nav_item failed")
	}
	text, _ = toolCall(t, s, "list_navigation", nil)
	if strings.Contains(text, "Dashboard") {
		t.Fatalf("item not removed: %s", text)
	}
}

func TestMergeYamlFragment(t *testing.T) {
	s := newStub(t)
	_, isErr := toolCall(t, s, "merge_yaml_fragment", map[string]interface{}{
		"yaml": "panel:\n  name: CRM\n",
	})
	if isErr {
		t.Fatal("merge failed")
	}
	text, _ := toolCall(t, s, "get_value", map[string]interface{}{"path": "panel/name"})
	if text != "CRM" {
		t.Fatalf("merged name = %q", text)
	}
}

func TestValidateTool(t *testing.T) {
	s := newStub(t)
	text, isErr := toolCall(t, s, "validate", nil)
	if isErr || text != "OK" {
		t.Fatalf("validate = %q err=%v", text, isErr)
	}
	// Break the in-memory config directly.
	st := stubStateOf(s)
	st.cfg.Panel.Name = ""
	text, isErr = toolCall(t, s, "validate", nil)
	if !isErr || !strings.Contains(text, "panel.name is required") {
		t.Fatalf("expected broken config error, got %q err=%v", text, isErr)
	}
}

func TestSaveToolValidatesFirst(t *testing.T) {
	s := newStub(t)
	st := stubStateOf(s)
	st.cfg.Panel.Name = ""
	text, isErr := toolCall(t, s, "save", nil)
	if !isErr || !strings.Contains(text, "save aborted") {
		t.Fatalf("save on invalid config: %q err=%v", text, isErr)
	}
	if st.saves != 0 {
		t.Fatal("invalid config must not reach disk")
	}
	st.cfg.Panel.Name = "My Admin"
	text, isErr = toolCall(t, s, "save", nil)
	if isErr || !strings.Contains(text, "Written to") {
		t.Fatalf("save: %q err=%v", text, isErr)
	}
	if st.saves != 1 {
		t.Fatalf("saves = %d", st.saves)
	}
}

func TestOpenTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "other.yaml")
	cfg := "version: \"1.0\"\npanel:\n  name: Other\n  path: /other\nresources:\n  - name: Thing\n"
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	s := newStub(t)
	text, isErr := toolCall(t, s, "open", map[string]interface{}{"path": path})
	if isErr || !strings.Contains(text, "Loaded") {
		t.Fatalf("open: %q err=%v", text, isErr)
	}
	if got := s.state.Config().Panel.Name; got != "Other" {
		t.Fatalf("in-memory panel name = %q", got)
	}
}

func TestResourcesRead(t *testing.T) {
	s := newStub(t)
	res := rpcCall(t, s, "resources/read", map[string]interface{}{"uri": "yaga://config"})
	m := res.(map[string]interface{})
	contents := m["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "My Admin") {
		t.Fatalf("yaga://config missing data: %s", text)
	}
}

func TestMethodNotFound(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "nope"})
	s := newStub(t)
	resp, _ := s.Handle(body)
	if !strings.Contains(string(resp), "method not found") {
		t.Fatalf("expected method-not-found, got %s", resp)
	}
}
