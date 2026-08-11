// validator.go
//
// Validates a parsed configuration and fills in defaults for missing optional
// fields (panel id, sqlc paths, resource labels, page paths). The generator
// relies on these defaults being applied before generation.
package parser

import (
	"fmt"

	"github.com/go-fila/go-fila/internal/types"
)

// Validate checks a parsed config for required fields and applies defaults. It
// returns the first validation problem (or nil); callers that need every
// problem at once (e.g. the editor's Validate screen) use ValidateAll.
// Params: cfg (the config to validate; may be mutated to set defaults).
// Returns: an error describing the first validation problem, or nil.
func Validate(cfg *types.Config) error {
	errs := ValidateAll(cfg)
	if len(errs) > 0 {
		return errs[0]
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
	for i, r := range cfg.Resources {
		if r.Name == "" {
			add(fmt.Errorf("resources[%d].name is required", i))
		}
		if r.Label == "" {
			cfg.Resources[i].Label = r.Name
		}
		if r.Card != nil {
			if r.Card.Columns < 1 {
				r.Card.Columns = 4
			}
			if r.Card.Rows < 1 {
				r.Card.Rows = 4
			}
		}
		if r.List != nil && r.List.PerPage < 1 {
			r.List.PerPage = 20
		}
		if err := validateResourceHooks(r); err != nil {
			add(fmt.Errorf("resources[%d]: %w", i, err))
		}
	}
	for i, p := range cfg.Pages {
		if p.Name == "" {
			add(fmt.Errorf("pages[%d].name is required", i))
		}
		if p.Path == "" {
			cfg.Pages[i].Path = "/" + p.Name
		}
	}
	return errs
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
