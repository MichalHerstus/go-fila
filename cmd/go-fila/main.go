// main.go
//
// CLI entry point for the go-fila admin panel generator. Parses the
// subcommand (init, generate, validate, version) plus the global flags
// (--config, --out, --force, --verbose) and delegates to the parser and
// generator packages.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-fila/go-fila/internal/generator"
	"github.com/go-fila/go-fila/internal/parser"
)

// version is the current go-fila release version.
const version = "0.9.0"

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
		fmt.Printf("go-fila version %s\n", version)
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
	fmt.Println(`go-fila — YAML-driven admin panel generator

Usage:
  go-fila init           Scaffold go-fila.yaml + sqlc.yaml + sql/ + working example
  go-fila init --demo    Scaffold a full-featured sqlite demo (order management)
  go-fila init --db DSN  Introspect an existing database and generate config + SQL
  go-fila edit           Interactive YAML config editor (TUI)
  go-fila generate       Run SQLC + generate admin panel Go application
  go-fila validate       Validate YAML + verify SQLC function references resolve
  go-fila version        Print version information

Flags:
  --config, -c   Path to YAML config file (default: go-fila.yaml)
  --out, -o      Output directory (default: ./admin)
  --db, -d DSN   Introspect database (postgres://..., sqlserver://... or sqlite file path)
  --force, -f    Overwrite existing files
  --verbose, -v  Enable verbose logging
  --skip-plugins, -s
                 Skip loading declared plugins (generate cannot use them)
  --demo, -D     Scaffold a populated sqlite demo project (init only)
  --admin-password, -p PASSWORD
                 Set the initial admin password for --demo / --db scaffolding
                 (a random one is generated and printed when omitted)

AI-assisted edit (edit only):
  --prompt TEXT  Edit go-fila.yaml via OpenRouter instead of the TUI
                 (the full config is sent to OpenRouter)
                 file://PATH reads the prompt from a file (~ expands to home)
  --apikey KEY   OpenRouter API key (fallback: OPENROUTER_API_KEY env, then .ENV)
  --model MODEL  Model id (fallback: .ENV, then openrouter/auto)
  --dry-run      Print proposed YAML + diff without writing`)
}

// parseGlobalFlags scans os.Args[2:] for the global flags shared by all
// subcommands. Flags that take a value (--config/-c, --out/-o, --db/-d,
// --admin-password/-p) consume the following argument.
// Returns: configPath (YAML config file path, default "go-fila.yaml"),
// outDir (output directory, default "./admin"),
// force (overwrite existing files), verbose (enable verbose logging),
// skipPlugins (skip loading declared plugins),
// demo (scaffold the populated sqlite demo project instead of the plain
// starter when initializing),
// db (connection string for --db/-d introspection mode),
// adminPassword (initial admin password for --demo / --db scaffolding, or "").
func parseGlobalFlags() (configPath, outDir, db, adminPassword string, force, verbose, skipPlugins, demo bool) {
	configPath = "go-fila.yaml"
	outDir = "./admin"
	db = ""
	adminPassword = ""
	force = false
	verbose = false
	skipPlugins = false
	demo = false

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
		case "--demo", "-D":
			demo = true
		}
	}
	return
}

// cmdInit scaffolds a starter project in the current directory: it writes
// go-fila.yaml (example configuration), sql/migrations/schema.sql and
// sql/queries/user.sql. It refuses to overwrite an existing config file or
// output directory unless --force is given. With --demo it instead scaffolds
// the full-featured sqlite demo project (see cmdInitDemo). With --db it
// connects to an existing database, introspects its schema and generates
// config and SQL files from the discovered tables (see cmdInitFromDB).
func cmdInit() {
	configPath, outDir, dbDSN, adminPassword, force, _, _, demo := parseGlobalFlags()

	if dbDSN != "" {
		if err := cmdInitFromDB(configPath, outDir, dbDSN, adminPassword, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if demo {
		if err := cmdInitDemo(configPath, outDir, adminPassword, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if !force {
		if _, err := os.Stat(configPath); err == nil {
			fmt.Printf("Error: %s already exists. Use --force to overwrite.\n", configPath)
			os.Exit(1)
		}
		if _, err := os.Stat(outDir); err == nil {
			fmt.Printf("Error: %s already exists. Use --force to overwrite.\n", outDir)
			os.Exit(1)
		}
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}

	exampleYAML := `version: "1.0"

panel:
  id: admin
  path: /admin
  name: "My Admin"

connections:
  default:
    driver: postgres
    dsn: "postgres://user:pass@localhost:5432/db?sslmode=disable"

sqlc:
  config: sqlc.yaml
  queries_dir: ./sql/queries
  schema_dir: ./sql/migrations
  output_pkg: internal/data

auth:
  guard: web
  provider: session
  table: users
  login:
    fields: [email, password]
    redirect: /admin/dashboard

resources:
  - name: User
    label: Users
    list:
      query: ListUsers
      count_query: CountUsers
      columns:
        - name: id
          type: integer
          sortable: true
        - name: name
          type: string
          sortable: true
          searchable: true
        - name: email
          type: email
          sortable: true
          searchable: true
        - name: role_name
          label: Role
          type: text
        - name: status
          type: badge
          options:
            active: success
            inactive: warning
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at
    detail:
      query: GetUser
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: email
          type: email
        - name: role_name
          label: Role
        - name: status
          type: badge
          options:
            active: success
            inactive: warning
        - name: created_at
          type: datetime
    form:
      create:
        query: CreateUser
        fields:
          - name: name
            type: text
            required: true
          - name: email
            type: email
            required: true
          - name: password
            type: password
            required: true
          - name: role_id
            type: select
            options_query: ListRoles
            options_value: id
            options_label: name
          - name: status
            type: select
            options:
              active: Active
              inactive: Inactive
      update:
        query: UpdateUser
        populate_query: GetUser
        fields:
          - name: name
            type: text
          - name: email
            type: email
          - name: role_id
            type: select
            options_query: ListRoles

pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stats_grid
        columns: 2
        widgets:
          - type: stat
            label: "Total Users"
            query: SELECT COUNT(*) FROM users
            icon: users
          - type: stat
            label: "Active Users"
            query: SELECT COUNT(*) FROM users WHERE status = 'active'
            icon: check

navigation:
  - group: "Management"
    icon: users
    sort: 1
    items:
      - resource: User
`

	if err := os.WriteFile(configPath, []byte(exampleYAML), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	os.MkdirAll(filepath.Join(outDir, "sql", "queries"), 0755)
	os.MkdirAll(filepath.Join(outDir, "sql", "migrations"), 0755)

	schemaSQL := `CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    role_id INT REFERENCES roles(id),
    role_name VARCHAR(100) DEFAULT 'user',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE roles (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

INSERT INTO roles (name) VALUES ('admin'), ('manager'), ('user');
`
	os.WriteFile(filepath.Join(outDir, "sql", "migrations", "schema.sql"), []byte(schemaSQL), 0644)

	userQueries := `-- name: ListUsers :many
SELECT u.*, r.name as role_name
FROM users u
LEFT JOIN roles r ON r.id = u.role_id
ORDER BY u.created_at DESC;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUser :one
SELECT u.*, r.name as role_name
FROM users u
LEFT JOIN roles r ON r.id = u.role_id
WHERE u.id = $1;

-- name: GetUserByEmail :one
SELECT id, password, COALESCE(role_name, '') as role_name
FROM users
WHERE email = $1;

-- name: CreateUser :one
INSERT INTO users (name, email, password, role_id, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET name = $2, email = $3, role_id = $4, status = $5
WHERE id = $1
RETURNING *;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: ListRoles :many
SELECT * FROM roles ORDER BY name;
`
	os.WriteFile(filepath.Join(outDir, "sql", "queries", "user.sql"), []byte(userQueries), 0644)

	fmt.Println("Scaffolded admin panel in", outDir)
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit go-fila.yaml with your configuration")
	fmt.Println("  2. Edit sql/migrations/schema.sql with your schema")
	fmt.Println("  3. Edit sql/queries/ with your SQLC queries")
	fmt.Println("  4. Run 'go-fila generate' to generate the admin panel")
}

// cmdValidate parses and validates the YAML config file, printing whether it
// is valid. With --verbose it also prints a short summary of the panel, the
// number of resources, pages and navigation groups.
func cmdValidate() {
	configPath, _, _, _, verbose, _, _, _ := parseGlobalFlags()

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
// application into outDir. Afterwards it attempts to run `sqlc generate` and
// the Tailwind CSS build; failures there are reported as warnings instead of
// being fatal, since the user can re-run them manually.
func cmdGenerate() {
	configPath, outDir, _, _, verbose, skipPlugins, _, _ := parseGlobalFlags()

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

	// Attempt to run sqlc generate (non-fatal if it fails)
	if err := gen.RunSQLC(); err != nil {
		fmt.Printf("Warning: sqlc generate failed: %v\n", err)
		fmt.Println("  Make sure sqlc is installed and the SQL files are valid.")
		fmt.Println("  You can run 'sqlc generate' manually later.")
	}

	// Attempt to run Tailwind build (non-fatal if it fails)
	if err := gen.RunTailwind(); err != nil {
		fmt.Printf("Warning: Tailwind build failed: %v\n", err)
		fmt.Println("  Make sure the Tailwind CSS standalone binary is installed.")
		fmt.Println("  You can run 'make get-tailwind' then 'make TAILWIND=$(CURDIR)/.tools/tailwindcss css' manually later.")
	}

	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. cd", outDir)
	fmt.Println("  2. make css        (or: make get-tailwind && make TAILWIND=$(CURDIR)/.tools/tailwindcss css)")
	fmt.Println("  3. If sqlc failed above: sqlc generate")
	fmt.Println("  4. go mod tidy")
	fmt.Println("  5. go build ./...")
}
