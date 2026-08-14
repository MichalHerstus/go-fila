// validator.go
//
// Validates a parsed configuration and fills in defaults for missing optional
// fields (panel id, sqlc paths, resource labels, page paths). The generator
// relies on these defaults being applied before generation.
package parser

import (
	"fmt"

	"github.com/MichalHerstus/yaga/internal/types"
)

// Warning is a non-fatal validation notice, e.g. a value silently clamped to a
// supported range (grid columns [1,12]) or a max_content_width fallback. It
// renders as a yellow row on the editor's Validate screen and never blocks a
// config save or generation (Validate skips warnings).
type Warning struct{ msg string }

// Error implements the error interface.
func (w Warning) Error() string { return w.msg }

// warn builds a Warning.
func warn(format string, a ...interface{}) error {
	return Warning{msg: fmt.Sprintf(format, a...)}
}

// maxWidths is the allowlist of max_content_width values that map onto a real
// Tailwind max-w-{V} class. Anything else (including empty) falls back to
// "none" (max-w-none) with a warning — the pre-built stylesheet only safelists
// these values.
var maxWidths = []string{
	"none", "xs", "sm", "md", "lg", "xl", "2xl", "3xl", "4xl", "5xl", "6xl", "7xl",
	"full", "min", "max", "fit", "prose",
	"screen-sm", "screen-md", "screen-lg", "screen-xl", "screen-2xl",
}

func inMaxWidths(v string) bool {
	for _, m := range maxWidths {
		if m == v {
			return true
		}
	}
	return false
}

// Validate checks a parsed config for required fields and applies defaults. It
// returns the first validation problem (or nil), ignoring non-fatal warnings;
// callers that need every problem at once (e.g. the editor's Validate screen)
// use ValidateAll.
// Params: cfg (the config to validate; may be mutated to set defaults).
// Returns: an error describing the first validation problem, or nil.
func Validate(cfg *types.Config) error {
	errs := ValidateAll(cfg)
	for _, e := range errs {
		if _, ok := e.(Warning); !ok {
			return e
		}
	}
	return nil
}

// ValidateAll checks a parsed config for required fields and applies defaults
// (see the Validate doc comment), collecting every problem found instead of
// stopping at the first. Defaulting still runs even when errors are present so
// a follow-up pass sees the same normalized config.
// Params: cfg (the config to validate; may be mutated to set defaults).
// Returns: the validation problems found (empty when the config is valid).
func ValidateAll(cfg *types.Config) []error {
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	if cfg.Version == "" {
		add(fmt.Errorf("version is required"))
	}
	if cfg.Panel.Name == "" {
		add(fmt.Errorf("panel.name is required"))
	}
	if cfg.Panel.Path == "" {
		add(fmt.Errorf("panel.path is required"))
	}
	if cfg.Panel.ID == "" {
		cfg.Panel.ID = "admin"
	}
	if cfg.Panel.Layout.MaxContentWidth != "" && !inMaxWidths(cfg.Panel.Layout.MaxContentWidth) {
		add(warn("panel.layout.max_content_width %q is not a supported width, falling back to \"none\"", cfg.Panel.Layout.MaxContentWidth))
		cfg.Panel.Layout.MaxContentWidth = "none"
	}
	if cfg.SQLC.Config == "" {
		cfg.SQLC.Config = "sqlc.yaml"
	}
	if cfg.SQLC.QueriesDir == "" {
		cfg.SQLC.QueriesDir = "./sql/queries"
	}
	if cfg.SQLC.SchemaDir == "" {
		cfg.SQLC.SchemaDir = "./sql/migrations"
	}
	if cfg.SQLC.OutputPkg == "" {
		cfg.SQLC.OutputPkg = "internal/data"
	}
	if len(cfg.Resources) == 0 && len(cfg.Pages) == 0 {
		add(fmt.Errorf("at least one resource or page is required"))
	}
	for i, pl := range cfg.Plugins {
		if pl.Name == "" {
			add(fmt.Errorf("plugins[%d].name is required", i))
		}
		if pl.Source == "" {
			add(fmt.Errorf("plugins[%d].source is required", i))
		}
		for j := 0; j < i; j++ {
			if cfg.Plugins[j].Name == pl.Name {
				add(fmt.Errorf("plugins[%d].name %q is duplicated", i, pl.Name))
			}
		}
	}
	if cfg.Audit != nil {
		if cfg.Audit.Table == "" {
			cfg.Audit.Table = "audit_log"
		}
		for _, ex := range cfg.Audit.ExcludeResources {
			found := false
			for _, r := range cfg.Resources {
				if r.Name == ex {
					found = true
					break
				}
			}
			if !found {
				add(fmt.Errorf("audit.exclude_resources references unknown resource %q", ex))
			}
		}
	}
	for i, r := range cfg.Resources {
		if r.Name == "" {
			add(fmt.Errorf("resources[%d].name is required", i))
		}
		if r.Label == "" {
			cfg.Resources[i].Label = r.Name
		}
		if r.Card != nil {
			if r.Card.Columns < 1 || r.Card.Columns > 12 {
				add(warn("resources[%d] (%s) card.columns %d is out of range [1,12], clamping to %d", i, r.Name, r.Card.Columns, clampColumns(r.Card.Columns)))
				r.Card.Columns = clampColumns(r.Card.Columns)
			}
			if r.Card.Rows < 1 {
				r.Card.Rows = 4
			}
		}
		if r.List != nil && r.List.PerPage < 1 {
			r.List.PerPage = 20
		}
		if r.List != nil {
			for _, ex := range r.List.Export {
				found := false
				for _, c := range r.List.Columns {
					if c.Name == ex {
						found = true
						break
					}
				}
				if !found {
					add(fmt.Errorf("resources[%d] (%s) list.export references unknown column %q", i, r.Name, ex))
				}
			}
		}
		if r.ImportCSV && (r.Form == nil || r.Form.Create == nil) {
			add(fmt.Errorf("resources[%d] (%s) import_csv requires a form.create section", i, r.Name))
		}
		if err := validateResourceHooks(r); err != nil {
			add(fmt.Errorf("resources[%d]: %w", i, err))
		}
	}
	validateProcedures(cfg, add)
	for i, p := range cfg.Pages {
		if p.Name == "" {
			add(fmt.Errorf("pages[%d].name is required", i))
		}
		if p.Path == "" {
			cfg.Pages[i].Path = "/" + p.Name
		}
		clampWidgetColumns(&cfg.Pages[i], i, add)
	}
	return errs
}

// clampColumns clamps a grid column count into the supported [1,12] range that
// the pre-built stylesheet safelists (lg:grid-cols-1..12). Values >12 previously
// emitted arbitrary tailwind classes.
func clampColumns(v int) int {
	if v < 1 {
		return 1
	}
	if v > 12 {
		return 12
	}
	return v
}

// clampWidgetColumns clamps every stats_grid widget's Columns into [1,12] (see
// clampColumns), recursing into nested widgets. Each out-of-range value is
// recorded as a non-fatal warning.
func clampWidgetColumns(p *types.Page, pageIdx int, add func(error)) {
	var walk func(w *types.Widget, path string)
	walk = func(w *types.Widget, path string) {
		if w == nil {
			return
		}
		if w.Type == "stats_grid" && (w.Columns < 1 || w.Columns > 12) {
			add(warn("pages[%d] %s columns %d is out of range [1,12], clamping to %d", pageIdx, path, w.Columns, clampColumns(w.Columns)))
			w.Columns = clampColumns(w.Columns)
		}
		for j := range w.Widgets {
			walk(&w.Widgets[j], fmt.Sprintf("%s[%d]", path, j))
		}
	}
	for j := range p.Widgets {
		walk(&p.Widgets[j], fmt.Sprintf("widgets[%d]", j))
	}
}

// validateResourceHooks checks that every hook declared on a resource's form
// actions and custom actions is well-formed: each hook needs a name and
// exactly one of fn/sql/proc set, and each custom action must not mix query
// with proc.
// Params: r (the resource definition).
// Returns: an error describing the first invalid hook, or nil.
func validateResourceHooks(r types.Resource) error {
	if r.Form != nil {
		for _, fa := range []*types.FormAction{r.Form.Create, r.Form.Update, r.Form.Delete} {
			if fa == nil {
				continue
			}
			if err := validateHooks(fa.Hooks); err != nil {
				return err
			}
		}
	}
	for _, a := range r.Actions {
		if err := validateAction(a); err != nil {
			return fmt.Errorf("actions: %w", err)
		}
		if err := validateHooks(a.Hooks); err != nil {
			return fmt.Errorf("actions: %w", err)
		}
	}
	return nil
}

// validateAction checks that a custom action does not mix the query and proc
// execution modes (they are mutually exclusive). Both empty is allowed so an
// action can run hooks only.
// Params: a (the action definition).
// Returns: an error when query and proc are both set, or nil.
func validateAction(a types.Action) error {
	if a.Query != "" && a.Proc != "" {
		return fmt.Errorf("%q: query and proc are mutually exclusive", a.Name)
	}
	return nil
}

// driver returns the database driver of the first configured connection,
// defaulting to "postgres" when no connections are configured (mirrors the
// generator's driver()).
// Params: cfg (the config to inspect).
// Returns: the driver name.
func driver(cfg *types.Config) string {
	for _, conn := range cfg.Connections {
		if conn.Driver != "" {
			return conn.Driver
		}
	}
	return "postgres"
}

// validateProcedures checks the `procedures:` block: every entry needs a name
// and names must be unique. When the driver is sqlite, every proc: reference
// on an action or hook must match a declared procedure — on sqlite proc:
// execution is driven by the declared body, so an undeclared reference is a
// fatal config error (mirroring the plugin-load-failure semantics). Postgres
// and mssql ignore the block entirely (real procedures come from user DDL).
// Params: cfg (the config to validate), add (collects a validation problem).
func validateProcedures(cfg *types.Config, add func(error)) {
	names := map[string]bool{}
	for i, p := range cfg.Procedures {
		if p.Name == "" {
			add(fmt.Errorf("procedures[%d].name is required", i))
			continue
		}
		if names[p.Name] {
			add(fmt.Errorf("procedures[%d].name %q is duplicated", i, p.Name))
		}
		names[p.Name] = true
	}
	d := driver(cfg)
	if d != "sqlite" && d != "sqlite3" {
		return
	}
	for i, r := range cfg.Resources {
		for label, proc := range procRefs(r) {
			if !names[proc] {
				add(fmt.Errorf("resources[%d] (%s) %s references undeclared procedure %q - add a matching procedures: entry", i, r.Name, label, proc))
			}
		}
	}
}

// procRefs returns every proc: reference on a resource as a map keyed by a
// human-readable "action <name>" / "action <name> hook <name>" label so
// validation errors name the exact site.
// Params: r (the resource definition).
// Returns: a map of site label to procedure name.
func procRefs(r types.Resource) map[string]string {
	refs := map[string]string{}
	collect := func(label string, h *types.Hooks) {
		if h == nil {
			return
		}
		for _, list := range [][]types.Hook{h.Before, h.After} {
			for _, hook := range list {
				if hook.Proc != "" {
					refs[label+" hook "+hook.Name] = hook.Proc
				}
			}
		}
	}
	for _, a := range r.Actions {
		if a.Proc != "" {
			refs["action "+a.Name] = a.Proc
		}
		collect("action "+a.Name, a.Hooks)
	}
	if r.Form != nil {
		for _, fa := range []*types.FormAction{r.Form.Create, r.Form.Update, r.Form.Delete} {
			if fa == nil {
				continue
			}
			collect("form", fa.Hooks)
		}
	}
	return refs
}

// validateHooks validates a single Hooks block, walking the before and after
// lists and checking each hook's name and fn/sql/proc combination.
// Params: h (the hooks block; nil is valid).
// Returns: an error describing the first invalid hook, or nil.
func validateHooks(h *types.Hooks) error {
	if h == nil {
		return nil
	}
	for _, list := range []struct {
		name string
		h    []types.Hook
	}{{"before", h.Before}, {"after", h.After}} {
		for j, hook := range list.h {
			if hook.Name == "" {
				return fmt.Errorf("%s[%d].name is required", list.name, j)
			}
			kindCount := 0
			if hook.Fn != "" {
				kindCount++
			}
			if hook.SQL != "" {
				kindCount++
			}
			if hook.Proc != "" {
				kindCount++
			}
			if kindCount != 1 {
				return fmt.Errorf("%s[%d]: exactly one of fn, sql or proc is required", list.name, j)
			}
		}
	}
	return nil
}
