// sqlc.go
//
// Generates the sqlc.yaml configuration for the admin panel project and
// exposes helpers to run `sqlc generate` and to look up the raw SQL body of
// a named query (used for options_query fields). The sqlc run is non-fatal:
// it is invoked after generation and may be re-run manually by the user.
package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// generateSQLCConfig writes a sqlc.yaml v2 configuration into the output
// directory. The schema and queries directories are fixed relative paths
// (./sql/migrations and ./sql/queries) and the Go code is emitted into
// internal/data using the database/sql driver.
func (g *Generator) generateSQLCConfig() error {
	engine := "postgresql"
	if g.isSQLite() {
		engine = "sqlite"
	}
	content := fmt.Sprintf(`version: "2"
sql:
  - engine: %q
    schema: "./sql/migrations"
    queries: "./sql/queries"
    gen:
      go:
        package: "data"
        out: "internal/data"
        sql_package: "database/sql"
`, engine)
	return os.WriteFile(filepath.Join(g.OutDir, "sqlc.yaml"), []byte(content), 0644)
}

// RunSQLC executes `sqlc generate` inside the generated output directory,
// streaming stdout/stderr through to the user.
// Returns an error if the sqlc binary is missing or generation fails.
func (g *Generator) RunSQLC() error {
	fmt.Println("Running sqlc generate...")
	cmd := exec.Command("sqlc", "generate")
	cmd.Dir = g.OutDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlc generate failed: %w", err)
	}
	return nil
}

// findSQLCQuery reads all .sql files from the queries directory and returns
// the raw SQL body for the named query (matching -- name: QueryName annotation).
// Returns empty string if not found.
// Params: queryDir (directory containing the .sql files), queryName (name of
// the desired query, e.g. "ListRoles").
// Returns: the trimmed raw SQL body of the query, or "" if not found.
func findSQLCQuery(queryDir string, queryName string) string {
	files, err := filepath.Glob(filepath.Join(queryDir, "*.sql"))
	if err != nil {
		return ""
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "-- name: "+queryName) ||
				strings.HasPrefix(trimmed, "--name: "+queryName) {
				var body strings.Builder
				for j := i + 1; j < len(lines); j++ {
					next := strings.TrimSpace(lines[j])
					if strings.HasPrefix(next, "-- name:") || strings.HasPrefix(next, "--name:") {
						break
					}
					if next == "" || strings.HasPrefix(next, "--") {
						continue
					}
					body.WriteString(next + " ")
				}
				return strings.TrimRight(strings.TrimSpace(body.String()), ";")
			}
		}
	}
	return ""
}
