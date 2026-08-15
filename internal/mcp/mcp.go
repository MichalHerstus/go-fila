// Package mcp implements a Model Context Protocol (MCP) server that exposes the
// yaga config and editing operations as structured tools, resources and
// prompts. It is transport-agnostic: the handler processes single JSON-RPC 2.0
// messages and the hosting package (wedit) mounts it over Streamable HTTP at
// POST /mcp. Tools operate on a narrow State interface so they never touch the
// web server's internals directly.
package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// ProtocolVersion is the MCP Streamable HTTP protocol version this server
// advertises on initialize.
const ProtocolVersion = "2025-06-18"

// ServerName is the serverInfo.name reported on initialize.
const ServerName = "yaga"

// State is the narrow contract between the MCP server and the hosting editor
// (the wedit Server). Mutating tools edit a yaml.Node tree derived from
// Config(), re-parse it through Parse, and only call Commit once the config is
// valid — so the in-memory config never holds an invalid state and default
// values are never injected.
type State interface {
	// ConfigPath returns the yaga.yaml path the editor writes on save.
	ConfigPath() string
	// Config returns the current in-memory config.
	Config() *types.Config
	// Parse validates YAML bytes into a config, returning the config (nil when
	// unparseable) plus the structural error and warning messages.
	Parse(data []byte) (*types.Config, []string, []string)
	// Commit replaces the in-memory config.
	Commit(cfg *types.Config)
	// Save persists the current config (and any staged SQL rewrites) to disk.
	Save() error
	// ReadConfigFile returns raw YAML bytes for the `open` tool.
	ReadConfigFile(path string) ([]byte, error)
	// Report runs the full health check (structural validation + schema-block
	// reference pass) over a config, returning error and warning messages.
	Report(cfg *types.Config) (errs, warns []string)
	// Analyze returns a JSON-serializable schema/query sync report.
	Analyze(cfg *types.Config) interface{}
}

// Server dispatches MCP JSON-RPC messages against a State.
type Server struct {
	state State
}

// New builds an MCP server backed by state.
func New(state State) *Server {
	return &Server{state: state}
}

// rpcRequest is one inbound JSON-RPC 2.0 message.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse is a JSON-RPC 2.0 response (result xor error).
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

const (
	codeParseErr       = -32700
	codeInvalidReq     = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// Handle processes a single JSON-RPC 2.0 MCP message (batches are rejected)
// and returns the response bytes. The bool reports whether a response must be
// sent: notifications (no id) return false and the caller answers with an empty
// 202.
func (s *Server) Handle(body []byte) ([]byte, bool) {
	if len(body) == 0 {
		return s.rawError(nil, codeParseErr, "empty request body")
	}
	if body[0] == '[' {
		return s.rawError(nil, codeInvalidReq, "batched JSON-RPC requests are not supported")
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return s.rawError(nil, codeParseErr, "parse error: "+err.Error())
	}
	if req.ID == nil || len(req.ID) == 0 || string(req.ID) == "null" {
		return nil, false // notifications carry no response
	}
	result, err := s.dispatch(req)
	if err != nil {
		return s.rawError(req.ID, codeMethodNotFound, err.Error())
	}
	resp, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
	if err != nil {
		return s.rawError(req.ID, codeInternal, err.Error())
	}
	return resp, true
}

// rawError renders an error response with a "send me" flag.
func (s *Server) rawError(id json.RawMessage, code int, msg string) ([]byte, bool) {
	out, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
	if err != nil {
		panic(err)
	}
	return out, true
}

// dispatch routes an MCP method (only called for messages that carry an id).
func (s *Server) dispatch(req rpcRequest) (interface{}, error) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params)
	case "ping":
		return map[string]interface{}{}, nil
	case "tools/list":
		return map[string]interface{}{"tools": s.toolDefs()}, nil
	case "tools/call":
		return s.handleToolCall(req.Params)
	case "resources/list":
		return map[string]interface{}{"resources": s.resources()}, nil
	case "resources/read":
		return s.handleResourceRead(req.Params)
	case "prompts/list":
		return map[string]interface{}{"prompts": []interface{}{}}, nil
	case "prompts/get":
		return nil, fmt.Errorf("prompt not found")
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

// handleToolCall runs a tool and wraps the result as MCP content.
func (s *Server) handleToolCall(params json.RawMessage) (interface{}, error) {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	text, isErr := s.callTool(p.Name, p.Arguments)
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
		"isError": isErr,
	}, nil
}

// handleInitialize answers the MCP handshake.
func (s *Server) handleInitialize(params json.RawMessage) (interface{}, error) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	return map[string]interface{}{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
			"prompts":   map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{"name": ServerName, "version": "1.0.0"},
	}, nil
}

// resource is one entries/list item.
type resource struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Text     string `json:"-"`
}

// resources lists the exposed read resources.
func (s *Server) resources() []resource {
	return []resource{
		{URI: "yaga://config", Name: "config", MimeType: "application/json"},
		{URI: "yaga://resources", Name: "resources", MimeType: "application/json"},
	}
}

// handleResourceRead serves yaga://config and yaga://resources.
func (s *Server) handleResourceRead(params json.RawMessage) (interface{}, error) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %v", err)
	}
	cfg := s.state.Config()
	switch p.URI {
	case "yaga://config":
		data, err := configJSON(cfg)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"contents": []map[string]string{{"uri": p.URI, "mimeType": "application/json", "text": string(data)}}}, nil
	case "yaga://resources":
		data, err := json.Marshal(resourceList(cfg))
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"contents": []map[string]string{{"uri": p.URI, "mimeType": "application/json", "text": string(data)}}}, nil
	}
	return nil, fmt.Errorf("unknown resource: %s", p.URI)
}

// configJSON renders a config as JSON whose field names match the YAML keys
// (yaml.Marshal -> generic tree -> json), so get_config paths line up with
// get_value paths.
func configJSON(cfg *types.Config) ([]byte, error) {
	y, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var tree interface{}
	if err := yaml.Unmarshal(y, &tree); err != nil {
		return nil, err
	}
	return json.Marshal(tree)
}

// resourceList summarizes resources for yaga://resources.
func resourceList(cfg *types.Config) []map[string]string {
	out := make([]map[string]string, 0, len(cfg.Resources))
	for _, r := range cfg.Resources {
		out = append(out, map[string]string{
			"name":  r.Name,
			"label": r.Label,
			"icon":  r.Icon,
			"table": r.Table,
		})
	}
	return out
}
