// main.go
//
// CLI entry point for the yaga admin panel generator. Parses the
// subcommand (init, generate, validate, version) plus the global flags
// (--config, --out, --force, --verbose) and delegates to the parser and
// generator packages.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MichalHerstus/yaga/internal/generator"
	"github.com/MichalHerstus/yaga/internal/parser"
)

// version is the current yaga release version.
const version = "1.0.0"

//go:embed AGENTS_for_generated_dashboard.md
var agentsForGeneratedDashboard string

// main is the CLI entry point. It requires at least one argument and
// dispatches to cmdInit, cmdGenerate or cmdValidate, or prints the version.
// Missing or unknown arguments print the usage text and exit with code 1.
func main() {
	ensureAgentGuide()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit()
	case "edit":
		cmdEdit()
	case "generate":
		cmdGenerate()
	case "validate":
		cmdValidate()
	case "version":
		fmt.Printf("yaga version %s\n", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

// ensureAgentGuide writes the embedded AGENTS.md guide into the current
// working directory when an AGENTS.md file does not already exist there. It
// runs on every invocation so a freshly cloned project (or a directory without
// the guide) always gets the agent instructions. Failures are reported to
// stderr but do not abort the CLI.
func ensureAgentGuide() {
	target := filepath.Join(".", "AGENTS.md")
	if _, err := os.Stat(target); err == nil {
		return
	}
	if err := os.WriteFile(target, []byte(agentsForGeneratedDashboard), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", target, err)
		return
	}
	fmt.Printf("Created %s (agent guide for generated dashboards)\n", target)
}

// printUsage prints the CLI help text to stdout, listing the available
// subcommands and their flags.
func printUsage() {
	fmt.Println(`yaga — YAML-driven admin panel generator

Usage:
  yaga init --db DSN  Introspect an existing database and generate yaga.yaml
                      (the captured schema: block is the sole schema source)
  yaga edit           Interactive YAML config editor (TUI)
  yaga generate       Generate the admin panel Go application (offline, no sqlc)
  yaga validate       Validate the YAML configuration
  yaga version        Print version information

Flags:
  --config, -c   Path to YAML config file (default: yaga.yaml)
  --out, -o      Output directory (default: ./admin)
  --db, -d DSN   Introspect database (postgres://..., sqlserver://... or sqlite file path)
  --force, -f    Overwrite existing files
  --verbose, -v  Enable verbose logging
  --skip-plugins, -s
                 Skip loading declared plugins (generate cannot use them)
  --admin-password, -p PASSWORD
                 Set the initial admin password for --db scaffolding
                 (a random one is generated and printed when omitted)

AI-assisted edit (edit only):
  --prompt TEXT  Edit yaga.yaml via AI instead of the TUI
                 (the full config is sent to the AI provider)
                 file://PATH reads the prompt from a file (~ expands to home)
  --apikey KEY   OpenRouter API key (fallback: OPENROUTER_API_KEY env, then .ENV)
  --model MODEL  Model id (fallback: .ENV, then openrouter/auto);
                 "lmstudio" uses a local LM Studio server (127.0.0.1:1234, no key)
  --dry-run      Print proposed YAML + diff without writing`)
}

// parseGlobalFlags scans os.Args[2:] for the global flags shared by all
// subcommands. Flags that take a value (--config/-c, --out/-o, --db/-d,
// --admin-password/-p) consume the following argument.
// Returns: configPath (YAML config file path, default "yaga.yaml"),
// outDir (output directory, default "./admin"),
// db (connection string for --db/-d introspection mode, required by init),
// adminPassword (initial admin password for --db scaffolding, or ""),
// force (overwrite existing files), verbose (enable verbose logging),
// skipPlugins (skip loading declared plugins).
func parseGlobalFlags() (configPath, outDir, db, adminPassword string, force, verbose, skipPlugins bool) {
	configPath = "yaga.yaml"
	outDir = "./admin"
	db = ""
	adminPassword = ""
	force = false
	verbose = false
	skipPlugins = false

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		case "--out", "-o":
			if i+1 < len(args) {
				outDir = args[i+1]
				i++
			}
		case "--db", "-d":
			if i+1 < len(args) {
				db = args[i+1]
				i++
			}
		case "--admin-password", "-p":
			if i+1 < len(args) {
				adminPassword = args[i+1]
				i++
			}
		case "--force", "-f":
			force = true
		case "--verbose", "-v":
			verbose = true
		case "--skip-plugins", "-s":
			skipPlugins = true
		}
	}
	return
}

// cmdInit scaffolds a project from an existing database: it requires --db,
// connects to the database, introspects its schema and generates yaga.yaml
// (including the captured `schema:` block, the sole schema source for the
// generator) plus the admin auth tables when missing. The plain starter
// scaffold and --demo were removed in D11 — the database is the only source
// of truth.
func cmdInit() {
	configPath, outDir, dbDSN, adminPassword, force, _, _ := parseGlobalFlags()

	if dbDSN == "" {
		fmt.Fprintln(os.Stderr, "Error: init requires a database connection string: yaga init --db DSN")
		fmt.Fprintln(os.Stderr, "  (postgres://..., sqlserver://... or a sqlite file path)")
		os.Exit(1)
	}

	if err := cmdInitFromDB(configPath, outDir, dbDSN, adminPassword, force); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// cmdValidate parses and validates the YAML config file, printing whether it
// is valid. With --verbose it also prints a short summary of the panel, the
// number of resources, pages and navigation groups.
func cmdValidate() {
	configPath, _, _, _, _, verbose, _ := parseGlobalFlags()

	cfg, err := parser.ParseFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Configuration is valid!")
	if verbose {
		fmt.Printf("  Panel: %s (path: %s)\n", cfg.Panel.Name, cfg.Panel.Path)
		fmt.Printf("  Resources: %d\n", len(cfg.Resources))
		fmt.Printf("  Pages: %d\n", len(cfg.Pages))
		fmt.Printf("  Navigation groups: %d\n", len(cfg.Navigation))
	}
}

// cmdGenerate parses the YAML config and generates the admin panel
// application into outDir, fully offline: the schema comes from the config's
// `schema:` block (no sqlc). Afterwards it attempts to run the Tailwind CSS
// build; failure there is reported as a warning instead of being fatal, since
// the user can re-run it manually.
func cmdGenerate() {
	configPath, outDir, _, _, _, verbose, skipPlugins := parseGlobalFlags()

	cfg, err := parser.ParseFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config: %v\n", err)
		os.Exit(1)
	}

	if verbose {
		fmt.Println("Configuration parsed successfully")
	}

	gen := generator.New(cfg, outDir)
	gen.ConfigDir = filepath.Dir(configPath)
	gen.Verbose = verbose
	gen.SkipPlugins = skipPlugins
	if err := gen.Generate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Admin panel generated in", outDir)
	fmt.Println("")

	// Attempt to run Tailwind build (non-fatal if it fails)
	if err := gen.RunTailwind(); err != nil {
		fmt.Printf("Warning: Tailwind build failed: %v\n", err)
		fmt.Println("  Make sure the Tailwind CSS standalone binary is installed.")
		fmt.Println("  You can run 'make get-tailwind' then 'make css' manually later.")
	}

	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. cd", outDir)
	fmt.Println("  2. make css        (or: make get-tailwind && make css)")
	fmt.Println("  3. go mod tidy")
	fmt.Println("  4. go build ./...")
}
