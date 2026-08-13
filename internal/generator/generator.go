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
// project is written; ConfigDir is the directory containing the source
// go-fila.yaml (used to locate the user's sql/ tree to copy into the output).
type Generator struct {
	Config    *types.Config
	OutDir    string
	ConfigDir string
	// SkipPlugins disables the plugin loader (escape hatch for --skip-plugins).
	SkipPlugins bool
	// Verbose enables per-plugin load summaries.
	Verbose bool
	// pluginFnNames tracks fn hook names whose implementation is provided by a
	// plugin hook source, so generateHooks skips stub generation for them.
	pluginFnNames map[string]bool
	// pluginHookFiles holds every plugin-provided hooks-package source file
	// (name -> content), used to gate Scope emission even when no YAML hook
	// block is declared.
	pluginHookFiles map[string]string
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

// driver returns the database driver of the first configured connection,
// defaulting to "postgres" when no connections are configured.
// Returns: the driver name ("postgres" or "sqlite").
func (g *Generator) driver() string {
	for _, conn := range g.Config.Connections {
		if conn.Driver != "" {
			return conn.Driver
		}
	}
	return "postgres"
}

// isSQLite reports whether the configured database driver is sqlite.
// Returns: true when the driver is "sqlite" or "sqlite3".
func (g *Generator) isSQLite() bool {
	d := g.driver()
	return d == "sqlite" || d == "sqlite3"
}

// isMSSQL reports whether the configured database driver is Microsoft SQL
// Server. MSSQL connections use the go-mssqldb "mssql" driver name so the
// driver's loose placeholder parsing accepts the $N placeholders required by
// the postgres-flavored SQL (sqlc's postgres engine cannot parse @name).
// Returns: true when the driver is "mssql" or "sqlserver".
func (g *Generator) isMSSQL() bool {
	d := g.driver()
	return d == "mssql" || d == "sqlserver"
}

// placeholder returns the SQL bind placeholder for the given 1-based argument
// index. Postgres uses numbered placeholders ($1, $2, ...) while sqlite uses
// a positional "?". MSSQL keeps $N placeholders because the mssql driver name
// loosely parses them into named T-SQL parameters.
// Params: n (1-based argument position).
// Returns: the placeholder token for the configured driver.
func (g *Generator) placeholder(n int) string {
	if g.isSQLite() {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// likeOp returns the case-insensitive LIKE operator for the configured driver.
// Postgres uses ILIKE; sqlite's LIKE is already case-insensitive for ASCII and
// MSSQL's default collations are case-insensitive (LIKE).
// Returns: "ILIKE" for postgres, "LIKE" for sqlite and mssql.
func (g *Generator) likeOp() string {
	if g.isSQLite() || g.isMSSQL() {
		return "LIKE"
	}
	return "ILIKE"
}

// idGoType returns the Go type used for primary key ids by the sqlc generated
// queries: sqlite INTEGER ids map to int64 while postgres INTEGER ids map to
// int32.
// Returns: "int32" for postgres, "int64" for sqlite.
func (g *Generator) idGoType() string {
	if g.isSQLite() {
		return "int64"
	}
	return "int32"
}

// idGoTypeForResource returns the Go type used to cast the :id path parameter
// when calling a resource's sqlc query. It honours the resource's optional
// id_type override (emitted by init --db for e.g. BIGINT/SMALLINT identity
// primary keys) and falls back to the driver default otherwise.
// Params: r (the resource definition).
// Returns: the Go type name, e.g. "int32", "int64" or "int16".
func (g *Generator) idGoTypeForResource(r types.Resource) string {
	if r.IDType != "" {
		return r.IDType
	}
	return g.idGoType()
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

	if err := g.copySQLFiles(); err != nil {
		return fmt.Errorf("copying sql files: %w", err)
	}

	// Plugins run after the user's SQL is copied (plugin SQL must land in the
	// out dir before sqlc runs) and before any resource/page generation. The
	// audit log augments the config after plugins (the AuditLog resource is
	// itself list-only) but before the second ensureDirs so its handler/view
	// dirs are created. The second ensureDirs call creates handler/view dirs
	// for plugin- and audit-contributed resources.
	if err := g.loadPlugins(); err != nil {
		return fmt.Errorf("loading plugins: %w", err)
	}
	g.applyAudit()
	if err := g.ensureDirs(); err != nil {
		return fmt.Errorf("creating resource directories: %w", err)
	}
	if err := g.generateAuditSchema(); err != nil {
		return fmt.Errorf("generating audit schema: %w", err)
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

	if err := g.generateHTTPErr(); err != nil {
		return fmt.Errorf("generating httperr: %w", err)
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

	if err := g.generateHooks(); err != nil {
		return fmt.Errorf("generating hooks: %w", err)
	}

	if err := g.generateGoMod(); err != nil {
		return fmt.Errorf("generating go.mod: %w", err)
	}

	if err := g.generateMakefile(); err != nil {
		return fmt.Errorf("generating Makefile: %w", err)
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
		"internal/panel/httperr",
		"internal/panel/resources",
		"internal/panel/pages",
		"internal/hooks",
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

// copySQLFiles copies the user's sql/queries and sql/migrations trees from the
// config directory into the generated output directory. This keeps the
// generated project self-contained: sqlc runs against ./sql inside the output
// dir, and options_query lookups resolve against the copied queries at
// generation time. Source directories that do not exist are skipped.
// Returns an error if a source directory exists but cannot be copied.
func (g *Generator) copySQLFiles() error {
	if g.ConfigDir == "" {
		return nil
	}
	for _, sub := range []string{"queries", "migrations"} {
		src := filepath.Join(g.ConfigDir, "sql", sub)
		info, err := os.Stat(src)
		if err != nil || !info.IsDir() {
			continue
		}
		dst := filepath.Join(g.OutDir, "sql", sub)
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
