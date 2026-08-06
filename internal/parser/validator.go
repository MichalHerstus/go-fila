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

// Validate checks a parsed config for required fields and applies defaults:
// version, panel.name and panel.path must be present; panel.id, sqlc paths,
// resource labels and page paths are defaulted when empty; at least one
// resource or page must exist.
// Params: cfg (the config to validate; may be mutated to set defaults).
// Returns: an error describing the first validation problem, or nil.
func Validate(cfg *types.Config) error {
	if cfg.Version == "" {
		return fmt.Errorf("version is required")
	}
	if cfg.Panel.Name == "" {
		return fmt.Errorf("panel.name is required")
	}
	if cfg.Panel.Path == "" {
		return fmt.Errorf("panel.path is required")
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
		return fmt.Errorf("at least one resource or page is required")
	}
	for i, r := range cfg.Resources {
		if r.Name == "" {
			return fmt.Errorf("resources[%d].name is required", i)
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
		if err := validateResourceHooks(r); err != nil {
			return fmt.Errorf("resources[%d]: %w", i, err)
		}
	}
	for i, p := range cfg.Pages {
		if p.Name == "" {
			return fmt.Errorf("pages[%d].name is required", i)
		}
		if p.Path == "" {
			cfg.Pages[i].Path = "/" + p.Name
		}
	}
	return nil
}

// validateResourceHooks checks that every hook declared on a resource's form
// actions and custom actions is well-formed: each hook needs a name and
// exactly one of fn/sql set.
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
		if err := validateHooks(a.Hooks); err != nil {
			return fmt.Errorf("actions: %w", err)
		}
	}
	return nil
}

// validateHooks validates a single Hooks block, walking the before and after
// lists and checking each hook's name/fn/sql combination.
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
			if hook.Fn == "" && hook.SQL == "" {
				return fmt.Errorf("%s[%d]: exactly one of fn or sql is required", list.name, j)
			}
			if hook.Fn != "" && hook.SQL != "" {
				return fmt.Errorf("%s[%d]: exactly one of fn or sql is required", list.name, j)
			}
		}
	}
	return nil
}
