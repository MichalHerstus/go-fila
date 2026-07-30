package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (g *Generator) generateSQLCConfig() error {
	content := `version: "2"
sql:
  - engine: "postgresql"
    schema: "./sql/migrations"
    queries: "./sql/queries"
    gen:
      go:
        package: "data"
        out: "internal/data"
        sql_package: "database/sql"
`
	return os.WriteFile(filepath.Join(g.OutDir, "sqlc.yaml"), []byte(content), 0644)
}

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
					if next == "" || strings.HasPrefix(next, "--") {
						continue
					}
					if strings.HasPrefix(next, "-- name:") || strings.HasPrefix(next, "--name:") {
						break
					}
					body.WriteString(next + " ")
				}
				return strings.TrimSpace(body.String())
			}
		}
	}
	return ""
}
