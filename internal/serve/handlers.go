package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/MichalHerstus/yaga/internal/fixer"
	"github.com/MichalHerstus/yaga/internal/parser"
	"github.com/MichalHerstus/yaga/internal/schema"
	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// configTree renders the config as a JSON-ready generic tree. The types have
// only YAML tags, so the bridge is yaml.Marshal -> yaml generic tree -> json.
// JSON field names therefore match the YAML field names the SPA renders and
// submits back.
func configTree(cfg *types.Config) (interface{}, error) {
	y, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var tree interface{}
	if err := yaml.Unmarshal(y, &tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// configFromJSON parses a JSON config body into a validated *types.Config,
// returning the list of structural errors (empty when valid) and warnings.
func configFromJSON(data []byte) (*types.Config, []string, []string) {
	var tree interface{}
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, []string{"invalid JSON: " + err.Error()}, nil
	}
	y, err := yaml.Marshal(tree)
	if err != nil {
		return nil, []string{"converting JSON to YAML: " + err.Error()}, nil
	}
	return configFromYAML(y)
}

// configFromYAML parses a YAML config body into a validated *types.Config.
func configFromYAML(data []byte) (*types.Config, []string, []string) {
	var cfg types.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, []string{"parsing config: " + err.Error()}, nil
	}
	var errs, warns []string
	for _, verr := range parser.ValidateAll(&cfg) {
		if _, ok := verr.(parser.Warning); ok {
			warns = append(warns, verr.Error())
		} else {
			errs = append(errs, verr.Error())
		}
	}
	return &cfg, errs, warns
}

// splitErrors partitions ValidateAll errors into errors and warnings.
func splitErrors(errs []error) ([]string, []string) {
	var out, warns []string
	for _, e := range errs {
		if _, ok := e.(parser.Warning); ok {
			warns = append(warns, e.Error())
		} else {
			out = append(out, e.Error())
		}
	}
	return out, warns
}

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	path := s.configPath
	rev := s.rev
	s.mu.RUnlock()

	tree, err := configTree(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   path,
		"config": tree,
		"rev":    rev,
	})
}

func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg, errs, warns := configFromJSON(body)
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"ok":       false,
			"errors":   errs,
			"warnings": warns,
		})
		return
	}
	rev := s.replaceConfig(cfg)
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "warnings": warns, "rev": rev})
}

// handleRev reports the current config revision so the SPA can detect changes
// made elsewhere (MCP/agent) without fetching the whole config.
func (s *Server) handleRev(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"rev": s.currentRev()})
}

// saveToDisk writes the in-memory config to disk. Shared by the SPA save
// endpoint and the MCP save tool.
func (s *Server) saveToDisk() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := yaml.Marshal(s.cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, data, 0644)
}

// handleSave writes the in-memory config to disk.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if err := s.saveToDisk(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleFix applies the auto-repair engine (the `validate --fix` logic) to the
// in-memory config and replaces it with the repaired one; nothing is written to
// disk until a global Save (POST /api/save). Returns the fixed paths and any
// unfixable errors/warnings that remain. Partial repairs still apply — like the
// CLI, fixable problems are fixed even when others cannot be.
func (s *Server) handleFix(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	out, fixed, remaining, err := fixer.Apply(data)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": err.Error(), "fixed": []string{}})
		return
	}
	norm := func(ss []string) []string {
		if ss == nil {
			return []string{}
		}
		return ss
	}
	if len(fixed) == 0 {
		errs, warns := splitErrors(remaining)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "changed": false, "fixed": []string{}, "errors": norm(errs), "warnings": norm(warns)})
		return
	}

	newCfg, errs, warns := configFromYAML(out)
	if newCfg != nil {
		s.replaceConfig(newCfg)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "changed": true, "fixed": fixed, "errors": norm(errs), "warnings": norm(warns)})
}

// findingJSON is one row of the /api/validate screen.
type findingJSON struct {
	Kind   string `json:"kind"` // "error" | "warning"
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// findingsOf runs the full health check over a config (structural validation of
// a YAML copy so defaults are not injected, plus a schema-block reference
// pass) and returns every finding. The captured `schema:` block is the source
// of truth: a resource's table must exist in it and every referenced column
// must be a column of that table.
func (s *Server) findingsOf(cfg *types.Config) []findingJSON {
	findings := make([]findingJSON, 0)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return append(findings, findingJSON{"error", "yaml.Marshal failed", err.Error()})
	}
	var copyCfg types.Config
	if err := yaml.Unmarshal(data, &copyCfg); err != nil {
		return append(findings, findingJSON{"error", "yaml.Unmarshal failed", err.Error()})
	}
	for _, verr := range parser.ValidateAll(&copyCfg) {
		kind := "error"
		if _, ok := verr.(parser.Warning); ok {
			kind = "warning"
		}
		findings = append(findings, findingJSON{kind, verr.Error(), ""})
	}
	if copyCfg.Schema == nil {
		findings = append(findings, findingJSON{"warning", "no schema block captured (re-run `yaga init --db`)", ""})
		return findings
	}
	refs := schema.CollectReferences(&copyCfg)
	for _, r := range copyCfg.Resources {
		table := refs.Tables[r.Name]
		st := schemaBlockTable(copyCfg, table)
		if st == nil {
			findings = append(findings, findingJSON{"error", r.Name + ": table not found in schema block: " + table, ""})
			continue
		}
		for _, c := range refs.ColumnRefs[r.Name] {
			if !schemaBlockHasColumn(st, c.Column) {
				findings = append(findings, findingJSON{"warning", "missing column: " + r.Name + "." + c.Section + "." + c.Column, ""})
			}
		}
	}
	return findings
}

// handleValidate runs the full health check and returns every finding.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"findings": s.findingsOf(cfg)})
}

// schemaBlockTable returns the captured `schema:` block entry for a table by
// name (case-insensitive), or nil when the block has no such table.
func schemaBlockTable(cfg types.Config, name string) *types.SchemaTable {
	if cfg.Schema == nil {
		return nil
	}
	for i := range cfg.Schema.Tables {
		t := &cfg.Schema.Tables[i]
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return nil
}

// schemaBlockHasColumn reports whether a schema-block table carries a column
// with the given name, trying exact and case-insensitive matches.
func schemaBlockHasColumn(st *types.SchemaTable, name string) bool {
	for _, c := range st.Columns {
		if c.Name == name || strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

// handleRawGet returns the in-memory config serialized to YAML for the raw
// editing tab.
func (s *Server) handleRawGet(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"yaml": string(data)})
}

// handleRawPut accepts a raw YAML config body, validates it and replaces the
// in-memory config. Validation errors are returned (422) and the in-memory
// config is untouched.
func (s *Server) handleRawPut(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg, errs, warns := configFromYAML(data)
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"ok": false, "errors": errs, "warnings": warns,
		})
		return
	}
	rev := s.replaceConfig(cfg)
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "warnings": warns, "rev": rev})
}
