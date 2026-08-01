// main.go
//
// Generates the top-level main.go of the generated admin panel application.
// The generated program opens a database/sql connection (from the DATABASE_URL
// env var or the configured DSN), builds the chi router and starts the HTTP
// server.
package generator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-fila/go-fila/internal/types"
)

// generateMain writes main.go for the generated app: it imports the sqlc
// output package (for the postgres driver registration) and the panel router,
// opens the database connection using getDSN, and listens on the ADDR env var
// (default ":8080"). Returns an error if the file cannot be written.
func (g *Generator) generateMain() error {
	code := fmt.Sprintf(`package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	_ "%s"
	"%s"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = %q
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	router := panel.NewRouter(db)

	log.Printf("Starting server on %%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
`, g.moduleImport(g.Config.SQLC.OutputPkg), g.moduleImport("internal/panel"), getDSN(g.Config))

	return os.WriteFile(filepath.Join(g.OutDir, "main.go"), []byte(code), 0644)
}

// getDSN returns the DSN of the first configured connection, or a localhost
// postgres fallback if no connections are configured.
// Params: cfg (parsed config whose Connections map is inspected).
// Returns: the DSN string to embed into the generated main.go.
func getDSN(cfg *types.Config) string {
	for _, conn := range cfg.Connections {
		return conn.DSN
	}
	return "postgres://localhost:5432/db?sslmode=disable"
}
