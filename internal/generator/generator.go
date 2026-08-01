// generator.go
//
// Orchestrates the code generation pipeline for the admin panel application.
// The Generator type holds the parsed configuration and the output directory,
// and Generate() runs each code-generation step in order (sqlc config, main,
// router, auth, resource handlers, page handlers, views, go.mod, view models,
// assets). It also creates the directory layout of the generated project.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// Generator drives the code generation of one admin panel application.
// Config is the parsed go-fila configuration; OutDir is where the generated
// project is written.
type Generator struct {
	Config *types.Config
	OutDir string
}

// New creates a Generator for the given parsed configuration and output
// directory.
// Params: cfg (parsed YAML config), outDir (destination directory).
// Returns: a ready-to-use *Generator.
func New(cfg *types.Config, outDir string) *Generator {
	return &Generator{
		Config: cfg,
		OutDir: outDir,
	}
}

// moduleImport returns the module-qualified import path for a project-relative
// package path, e.g. ("internal/panel") -> "admin/internal/panel". The module
// name is the base name of the output directory, matching the go.mod generated
// by generateGoMod.
// Params: pkg (project-relative package path).
// Returns: the full import path used by the generated code.
func (g *Generator) moduleImport(pkg string) string {
	return filepath.Base(g.OutDir) + "/" + pkg
}

// Generate runs the full generation pipeline in dependency order: it ensures
// the directory layout exists, then writes the sqlc config, main.go, the
// router, the auth package, one handler file set per resource, one page
// handler per page, all templ views, go.mod, the view models and the static
// assets. Returns an error if any step fails.
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

// ensureDirs creates the fixed directory layout of the generated project
// (internal/panel, internal/views, internal/viewmodels, internal/assets,
// static, sql) plus one resource handler and view subdirectory per resource.
// Returns an error if a directory cannot be created.
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
