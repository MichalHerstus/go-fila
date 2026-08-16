// mcp.go — mounts the internal/mcp server (E5) as the /mcp endpoints of the
// wedit HTTP server (MCP Streamable HTTP). The MCP tools read and mutate the
// same in-memory config as the SPA; only an explicit `save`
// tool writes to disk (with a .bak of the previous file).
package serve

import (
	"io"
	"net/http"
	"os"

	"github.com/MichalHerstus/yaga/internal/types"
)

// mcpHandler is the MCP JSON-RPC processor the Server exposes at /mcp.
type mcpHandler interface {
	Handle(body []byte) ([]byte, bool)
}

// serverMCPState adapts the wedit Server to the mcp.State contract.
type serverMCPState struct{ s *Server }

func (st serverMCPState) ConfigPath() string { return st.s.configPath }
func (st serverMCPState) Config() *types.Config {
	st.s.mu.RLock()
	defer st.s.mu.RUnlock()
	return st.s.cfg
}
func (st serverMCPState) Parse(data []byte) (*types.Config, []string, []string) {
	cfg, errs, warns := configFromYAML(data)
	return cfg, errs, warns
}
func (st serverMCPState) Commit(cfg *types.Config) {
	st.s.mu.Lock()
	st.s.cfg = cfg
	st.s.mu.Unlock()
}
func (st serverMCPState) Save() error {
	if data, err := os.ReadFile(st.s.configPath); err == nil {
		if err := os.WriteFile(st.s.configPath+".bak", data, 0644); err != nil {
			return err
		}
	}
	return st.s.saveToDisk()
}
func (st serverMCPState) ReadConfigFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
func (st serverMCPState) Report(cfg *types.Config) (errs, warns []string) {
	for _, f := range st.s.findingsOf(cfg) {
		if f.Kind == "warning" {
			warns = append(warns, f.Label)
		} else {
			errs = append(errs, f.Label)
		}
	}
	if errs == nil {
		errs = []string{}
	}
	if warns == nil {
		warns = []string{}
	}
	return errs, warns
}

// handleMCPPost answers a Streamable HTTP MCP request (JSON-RPC 2.0). Tools are
// synchronous, so every request gets a plain application/json response;
// notifications (no id) get an empty 202.
func (s *Server) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, send := s.mcp.Handle(body)
	if !send {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

// handleMCPGet tells Streamable HTTP clients this server does not stream.
func (s *Server) handleMCPGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	http.Error(w, "streaming not supported; use POST", http.StatusMethodNotAllowed)
}
