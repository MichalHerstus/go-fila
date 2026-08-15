package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	s.mu.RUnlock()

	tree, err := configTree(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   path,
		"config": tree,
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
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "warnings": warns})
}

// handleSave writes the in-memory config (and any staged query-file rewrites)
// to disk. Mirrors the TUI editor's save(): staged SQL files are flushed
// before the YAML.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	pending := make(map[string]string, len(s.pendingSQL))
	for k, v := range s.pendingSQL {
		pending[k] = v
	}
	path := s.configPath
	s.mu.RUnlock()

	for p, content := range pending {
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	s.mu.Lock()
	s.pendingSQL = nil
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// findingJSON is one row of the /api/validate screen.
type findingJSON struct {
	Kind   string `json:"kind"` // "error" | "warning"
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// handleValidate runs the full health check (structural validation of a YAML
// copy so defaults are not injected, plus a schema-block reference pass) and
// returns every finding. Since D11 the captured `schema:` block is the source
// of truth: a resource's table must exist in it and every referenced column
// must be a column of that table.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	findings := make([]findingJSON, 0)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		findings = append(findings, findingJSON{"error", "yaml.Marshal failed", err.Error()})
		writeJSON(w, http.StatusOK, map[string]interface{}{"findings": findings})
		return
	}
	var copyCfg types.Config
	if err := yaml.Unmarshal(data, &copyCfg); err != nil {
		findings = append(findings, findingJSON{"error", "yaml.Unmarshal failed", err.Error()})
		writeJSON(w, http.StatusOK, map[string]interface{}{"findings": findings})
		return
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
		writeJSON(w, http.StatusOK, map[string]interface{}{"findings": findings})
		return
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

	writeJSON(w, http.StatusOK, map[string]interface{}{"findings": findings})
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

// analyzeReport mirrors the TUI editor's syncReport, JSON-flavored.
type analyzeReport struct {
	Err            string              `json:"err"`
	Tables         []tableInfo         `json:"tables"`
	Queries        []queryInfo         `json:"queries"`
	MissingQueries []queryRefInfo      `json:"missing_queries"`
	InlineSQL      []queryRefInfo      `json:"inline_sql"`
	MissingTables  []string            `json:"missing_tables"`
	MissingColumns []missingColumnInfo `json:"missing_columns"`
	FKTargets      []string            `json:"fk_targets"`
}

type tableInfo struct {
	Name string `json:"name"`
	Cols int    `json:"cols"`
}

type queryInfo struct {
	Name string `json:"name"`
	File string `json:"file"`
	Body string `json:"body"`
}

type queryRefInfo struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
}

type missingColumnInfo struct {
	Resource string `json:"resource"`
	Section  string `json:"section"`
	Column   string `json:"column"`
}

// analyze runs the schema/queries/YAML sync analysis, mirroring the TUI
// editor's analyze() (cmd/yaga/editor/sync.go).
func (s *Server) analyze(cfg *types.Config) *analyzeReport {
	rep := &analyzeReport{
		Tables:         make([]tableInfo, 0),
		Queries:        make([]queryInfo, 0),
		MissingQueries: make([]queryRefInfo, 0),
		InlineSQL:      make([]queryRefInfo, 0),
		MissingTables:  make([]string, 0),
		MissingColumns: make([]missingColumnInfo, 0),
		FKTargets:      make([]string, 0),
	}
	matches, _ := filepath.Glob(filepath.Join(s.schemaDir(cfg), "*.sql"))
	tables, err := schema.ParseSchema(matches...)
	if err != nil {
		rep.Err = "schema: " + err.Error()
		return rep
	}
	for _, t := range tables {
		rep.Tables = append(rep.Tables, tableInfo{Name: t.Name, Cols: len(t.Columns)})
	}

	qs := schema.ParseQueries(s.queriesDir(cfg))
	names := make([]string, 0, len(qs))
	for n := range qs {
		names = append(names, n)
	}
	sort.Strings(names)
	rep.Queries = make([]queryInfo, 0, len(qs))
	for _, n := range names {
		q := qs[n]
		rep.Queries = append(rep.Queries, queryInfo{Name: q.Name, File: q.File, Body: q.RawBody})
	}

	refs := schema.CollectReferences(cfg)
	seenInline := map[string]bool{}
	for _, q := range refs.Queries {
		if q.Inline {
			if !seenInline[q.Name] {
				seenInline[q.Name] = true
				rep.InlineSQL = append(rep.InlineSQL, queryRefInfo{Name: q.Name, Origin: q.Origin})
			}
			continue
		}
		if _, ok := qs[q.Name]; !ok {
			rep.MissingQueries = append(rep.MissingQueries, queryRefInfo{Name: q.Name, Origin: q.Origin})
		}
	}

	for rname, table := range refs.Tables {
		if schema.FindTableByName(tables, table) == nil {
			rep.MissingTables = append(rep.MissingTables, rname+" -> "+table)
		}
	}

	for rname, refsList := range refs.ColumnRefs {
		t := schema.FindTableByName(tables, refs.Tables[rname])
		if t == nil {
			continue
		}
		for _, ref := range refsList {
			if !tableHasColumn(*t, ref.Column) {
				rep.MissingColumns = append(rep.MissingColumns, missingColumnInfo{
					Resource: rname, Section: ref.Section, Column: ref.Column,
				})
			}
		}
	}

	for _, ti := range tables {
		for _, fk := range ti.FKs() {
			listName := "List" + schema.ToPascalCase(fk.ForeignTable)
			if _, ok := qs[listName]; !ok {
				if !contains(rep.FKTargets, listName) {
					rep.FKTargets = append(rep.FKTargets, listName+" (for "+fk.ForeignTable+")")
				}
			}
		}
	}
	return rep
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	rep := s.analyze(cfg)
	if rep.Err != "" {
		writeJSON(w, http.StatusOK, rep)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleQueryGet returns one SQLC query's current body (staged rewrite overlay
// on top of the on-disk file), the same effective text the TUI SQL editor
// shows.
func (s *Server) handleQueryGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.RLock()
	cfg := s.cfg
	pending := make(map[string]string, len(s.pendingSQL))
	for k, v := range s.pendingSQL {
		pending[k] = v
	}
	s.mu.RUnlock()

	qs := schema.ParseQueries(s.queriesDir(cfg))
	q, ok := qs[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "query not found: " + name})
		return
	}
	path := filepath.Join(s.queriesDir(cfg), q.File)
	text := pending[path]
	if text == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		text = string(data)
	}
	body := ""
	if q, present := schema.ParseQueriesForFile(text, filepath.Base(path))[name]; present {
		body = q.RawBody
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name": name,
		"file": q.File,
		"body": body,
	})
}

// handleQueryPut stages a SQLC query body rewrite into pendingSQL, mirroring
// the TUI editor's stageQueryEdit. Nothing is written to disk until a global
// save (POST /api/save).
func (s *Server) handleQueryPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.mu.RLock()
	cfg := s.cfg
	pending := make(map[string]string, len(s.pendingSQL))
	for k, v := range s.pendingSQL {
		pending[k] = v
	}
	s.mu.RUnlock()

	qs := schema.ParseQueries(s.queriesDir(cfg))
	q, ok := qs[req.Name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "query not found: " + req.Name})
		return
	}
	path := filepath.Join(s.queriesDir(cfg), q.File)
	text := pending[path]
	if text == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		text = string(data)
	}
	newText := schema.RewriteQueryBody(text, req.Name, req.Body)

	s.mu.Lock()
	if s.pendingSQL == nil {
		s.pendingSQL = map[string]string{}
	}
	if newText == text {
		delete(s.pendingSQL, path)
	} else {
		s.pendingSQL[path] = newText
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
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
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "warnings": warns})
}

// tableHasColumn reports whether a table has a column, trying raw and
// lowercased matches (sqlc lowercases identifiers).
func tableHasColumn(t schema.Table, col string) bool {
	for _, c := range t.Columns {
		if c.Name == col || strings.EqualFold(c.Name, col) {
			return true
		}
	}
	return false
}
