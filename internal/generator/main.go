package generator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-fila/go-fila/internal/types"
)

func (g *Generator) generateMain() error {
	code := fmt.Sprintf(`package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	_ "%s"
	"internal/panel"
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
`, g.Config.SQLC.OutputPkg, getDSN(g.Config))

	return os.WriteFile(filepath.Join(g.OutDir, "main.go"), []byte(code), 0644)
}

func getDSN(cfg *types.Config) string {
	for _, conn := range cfg.Connections {
		return conn.DSN
	}
	return "postgres://localhost:5432/db?sslmode=disable"
}
