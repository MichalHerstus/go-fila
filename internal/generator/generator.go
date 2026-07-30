package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

type Generator struct {
	Config *types.Config
	OutDir string
}

func New(cfg *types.Config, outDir string) *Generator {
	return &Generator{
		Config: cfg,
		OutDir: outDir,
	}
}

func (g *Generator) Generate() error {
	if err := g.ensureDirs(); err != nil {
		return fmt.Errorf("creating directories: %w", err)
	}

	if g.Config.SQLC.Config != "" {
		if err := g.generateSQLCConfig(); err != nil {
			return fmt.Errorf("generating sqlc config: %w", err)
		}
	}

	if err := g.generateMain(); err != nil {
		return fmt.Errorf("generating main.go: %w", err)
	}

	if err := g.generateRouter(); err != nil {
		return fmt.Errorf("generating router: %w", err)
	}

	if err := g.generateAuth(); err != nil {
		return fmt.Errorf("generating auth: %w", err)
	}

	for _, r := range g.Config.Resources {
		if err := g.generateResource(r); err != nil {
			return fmt.Errorf("generating resource %s: %w", r.Name, err)
		}
	}

	for _, p := range g.Config.Pages {
		if err := g.generatePage(p); err != nil {
			return fmt.Errorf("generating page %s: %w", p.Name, err)
		}
	}

	if err := g.generateViews(); err != nil {
		return fmt.Errorf("generating views: %w", err)
	}

	if err := g.generateGoMod(); err != nil {
		return fmt.Errorf("generating go.mod: %w", err)
	}

	if err := g.generateViewModels(); err != nil {
		return fmt.Errorf("generating view models: %w", err)
	}

	if err := g.generateAssets(); err != nil {
		return fmt.Errorf("generating assets: %w", err)
	}

	return nil
}

func (g *Generator) ensureDirs() error {
	dirs := []string{
		"internal/panel/auth",
		"internal/panel/resources",
		"internal/panel/pages",
		"internal/views/layout",
		"internal/views/resources",
		"internal/views/pages",
		"internal/views/widgets",
		"internal/views/components",
		"internal/viewmodels",
		"internal/assets/css",
		"static/js",
		"sql/queries",
		"sql/migrations",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(g.OutDir, d), 0755); err != nil {
			return err
		}
	}

	for _, r := range g.Config.Resources {
		resDir := filepath.Join("internal/panel/resources", strings.ToLower(r.Name))
		if err := os.MkdirAll(filepath.Join(g.OutDir, resDir), 0755); err != nil {
			return err
		}
		viewDir := filepath.Join("internal/views/resources", strings.ToLower(r.Name))
		if err := os.MkdirAll(filepath.Join(g.OutDir, viewDir), 0755); err != nil {
			return err
		}
	}

	return nil
}
