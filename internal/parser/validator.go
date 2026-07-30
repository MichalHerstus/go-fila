package parser

import (
	"fmt"

	"github.com/go-fila/go-fila/internal/types"
)

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
