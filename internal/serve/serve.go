// Package serve implements the `yaga wedit` web-based YAML config editor
// (E4). It starts a local HTTP server exposing a small JSON REST API over the
// same Go logic the TUI editor uses (parser.ValidateAll, schema.ParseQueries,
// schema.CollectReferences) plus an embedded
// vanilla-JS single-page app. The command name is `wedit` (not the E4-drafted
// `serve`) so the web version of the editor is clearly distinguishable from a
// running generated dashboard.
package serve

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/MichalHerstus/yaga/internal/mcp"
	"github.com/MichalHerstus/yaga/internal/types"
)

//go:embed static/*
var staticFS embed.FS

// DefaultPort is the port wedit binds when --port is not given.
const DefaultPort = 9090

// Server owns the in-memory config being edited, the disk paths it maps to,
// and the HTTP routes of the editor API. It follows the same pattern as the
// TUI editor's `Editor`: an in-memory *types.Config plus a pendingSQL map for
// staged SQL query-file rewrites that are flushed together with the YAML on
// save.
type Server struct {
	mu         sync.RWMutex
	cfg        *types.Config
	configPath string
	pendingSQL map[string]string // staged query-file rewrites (abs path -> new content)

	port int
	open bool // open the browser automatically after binding

	mcp mcpHandler
	mux *http.ServeMux
}

// Options configures a wedit server.
type Options struct {
	Port        int  // listen port (0 -> DefaultPort)
	OpenBrowser bool // run `open`/`xdg-open` after the port is bound
}

// New builds a wedit server around a parsed config.
func New(cfg *types.Config, configPath string, opts Options) *Server {
	if opts.Port <= 0 {
		opts.Port = DefaultPort
	}
	s := &Server{
		cfg:        cfg,
		configPath: configPath,
		port:       opts.Port,
		open:       opts.OpenBrowser,
		mux:        http.NewServeMux(),
	}
	s.mcp = mcp.New(serverMCPState{s: s})
	s.routes()
	return s
}

// Handler returns the fully wired http.Handler (API + static SPA).
func (s *Server) Handler() http.Handler {
	return s.mux
}

// routes registers the REST API and the embedded SPA.
func (s *Server) routes() {
	mux := s.mux
	mux.HandleFunc("GET /api/config", s.handleConfigGet)
	mux.HandleFunc("PUT /api/config", s.handleConfigPut)
	mux.HandleFunc("POST /api/save", s.handleSave)
	mux.HandleFunc("GET /api/validate", s.handleValidate)
	mux.HandleFunc("POST /api/fix", s.handleFix)
	mux.HandleFunc("POST /mcp", s.handleMCPPost)
	mux.HandleFunc("GET /mcp", s.handleMCPGet)
	mux.HandleFunc("GET /api/analyze", s.handleAnalyze)
	mux.HandleFunc("GET /api/queries/{name}", s.handleQueryGet)
	mux.HandleFunc("PUT /api/queries", s.handleQueryPut)
	mux.HandleFunc("GET /api/raw", s.handleRawGet)
	mux.HandleFunc("PUT /api/raw", s.handleRawPut)

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed is static; cannot fail
	}
	mux.HandleFunc("GET /preview", s.handlePreview)
	mux.HandleFunc("GET /preview/styles.css", s.handlePreviewStyles)
	mux.HandleFunc("GET /preview/chart.js", s.handlePreviewChart)

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.serveIndex(w)
	})
}

// serveIndex renders the SPA shell.
func (s *Server) serveIndex(w http.ResponseWriter) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// Start binds the port, prints the URL (and opens the browser when --open was
// given), then serves until SIGINT/SIGTERM, which triggers a graceful
// shutdown. It never returns a bind error after the port is in use by
// someone else.
func (s *Server) Start() error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("wedit: cannot listen on :%d: %w", s.port, err)
	}
	url := fmt.Sprintf("http://localhost:%d/", s.port)
	fmt.Printf("WEdit: web config editor for %s\n", s.configPath)
	fmt.Printf("  open: %s\n", url)
	fmt.Printf("  Ctrl+C to stop.\n")
	if s.open {
		openBrowser(url)
	}

	srv := &http.Server{Handler: s.mux}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(l) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// openBrowser best-effort opens url in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}

// sqlBase returns the directory that the sqlc paths (sqlc.queries_dir /
// sqlc.schema_dir) are relative to. Same resolution the TUI editor uses: the
// config dir wins when it has any sql tree, otherwise the config dir's admin/
// subdir, otherwise the config dir itself.
func (s *Server) sqlBase(cfg *types.Config) string {
	base := filepath.Dir(s.configPath)
	for _, cand := range []string{base, filepath.Join(base, "admin")} {
		if sqlTreeExists(cfg.SQLC.QueriesDir, cfg.SQLC.SchemaDir, cand) {
			return cand
		}
	}
	return base
}

// queriesDir returns the absolute directory of the SQLC query files.
func (s *Server) queriesDir(cfg *types.Config) string {
	dir := cfg.SQLC.QueriesDir
	if dir == "" {
		dir = "./sql/queries"
	}
	return filepath.Join(s.sqlBase(cfg), dir)
}

// schemaDir returns the absolute directory of the schema migration files.
func (s *Server) schemaDir(cfg *types.Config) string {
	dir := cfg.SQLC.SchemaDir
	if dir == "" {
		dir = "./sql/migrations"
	}
	return filepath.Join(s.sqlBase(cfg), dir)
}

// sqlTreeExists reports whether either sqlc dir exists under base. Absolute
// paths resolve against nothing in the project and never match.
func sqlTreeExists(queriesDir, schemaDir, base string) bool {
	return sqlRelDir(queriesDir, "./sql/queries", base) ||
		sqlRelDir(schemaDir, "./sql/migrations", base)
}

// sqlRelDir reports whether the (possibly empty, possibly relative) sqlc
// directory rel exists under base.
func sqlRelDir(rel, def, base string) bool {
	if rel == "" {
		rel = def
	}
	return !filepath.IsAbs(rel) && isDir(filepath.Join(base, rel))
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
