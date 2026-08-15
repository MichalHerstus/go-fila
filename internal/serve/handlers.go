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
// copy so defaults are not injected, plus the schema/queries reference pass)
// and returns every finding.
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

	rep := s.analyze(&copyCfg)
	if rep.Err != "" {
		findings = append(findings, findingJSON{"error", rep.Err, ""})
	}
	for _, m := range rep.MissingQueries {
		findings = append(findings, findingJSON{"error", "missing query: " + m.Name, m.Origin})
	}
	for _, t := range rep.MissingTables {
		findings = append(findings, findingJSON{"error", "missing table: " + t, ""})
	}
	for _, c := range rep.MissingColumns {
		findings = append(findings, findingJSON{"warning", "missing column: " + c.Resource + "." + c.Section + "." + c.Column, ""})
	}
	for _, f := range rep.FKTargets {
		findings = append(findings, findingJSON{"warning", "missing FK List query: " + f, ""})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"findings": findings})
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

// handleGenerateQueries writes SQLC query files for schema tables that do not
// yet have a file in sql/queries. Existing files are never overwritten.
func (s *Server) handleGenerateQueries(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	rep := s.analyze(cfg)
	if rep.Err != "" {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": rep.Err})
		return
	}
	matches, _ := filepath.Glob(filepath.Join(s.schemaDir(cfg), "*.sql"))
	tables, err := schema.ParseSchema(matches...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	dir := s.queriesDir(cfg)
	generated := schema.GenerateQueries(tables, schema.Driver(cfg))
	var written []string
	written = make([]string, 0)
	for fname, content := range generated {
		path := filepath.Join(dir, fname)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		written = append(written, fname)
	}
	sort.Strings(written)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "written": written})
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
