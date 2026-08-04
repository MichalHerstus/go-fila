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
// opens the database connection using getDSN, verifies the database is usable
// (Ping plus a sanity query against the auth table) BEFORE binding the listen
// port, then serves on a port chosen by the --port flag (or the ADDR env var,
// default ":8080") with graceful shutdown on SIGINT/SIGTERM. For sqlite it
// additionally registers the sqlite driver. Returns an error if the file
// cannot be written.
func (g *Generator) generateMain() error {
	driverName := "postgres"
	driverImport := fmt.Sprintf("_ %q", g.moduleImport(g.Config.SQLC.OutputPkg))
	if g.isSQLite() {
		driverName = "sqlite3"
		driverImport = `_ "github.com/mattn/go-sqlite3"`
	}

	authTable := g.Config.Auth.Table
	if authTable == "" {
		authTable = "users"
	}

	code := fmt.Sprintf(`package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	%s
	"%s"
)

func main() {
	port := flag.Int("port", 0, "listen port (overrides ADDR env)")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = %q
	}

	db, err := sql.Open(%q, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	var one int
	if err := db.QueryRow("SELECT 1 FROM %s LIMIT 1").Scan(&one); err != nil && err != sql.ErrNoRows {
		log.Fatalf("database not initialized: %%v", err)
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if *port != 0 {
		addr = fmt.Sprintf(":%%d", *port)
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: panel.NewRouter(db),
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %%s: %%v (is another dashboard instance already running?)", addr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %%v", err)
		}
	}()

	log.Printf("Starting server on %%s", addr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
`, driverImport, g.moduleImport("internal/panel"), getDSN(g.Config), driverName, authTable)

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
