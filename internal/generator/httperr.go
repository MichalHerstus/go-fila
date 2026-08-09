// httperr.go
//
// Generates the shared internal/panel/httperr package: small helpers that log
// the real error server-side and return a generic status text to the client.
// This keeps SQL details, table names and driver internals out of HTTP
// responses while preserving the diagnostics in the server log.
package generator

import (
	"os"
	"path/filepath"
)

// generateHTTPErr writes internal/panel/httperr/httperr.go. Internal logs the
// error and returns "Internal Server Error" (500); NotFound logs and returns
// "Not Found" (404). Both are used by every generated handler instead of
// http.Error(w, err.Error(), ...).
// Returns an error on write failure.
func (g *Generator) generateHTTPErr() error {
	code := `package httperr

import (
    "log"
    "net/http"
)

func Internal(w http.ResponseWriter, err error) {
    log.Printf("internal error: %v", err)
    http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

func NotFound(w http.ResponseWriter, err error) {
    log.Printf("not found: %v", err)
    http.Error(w, "Not Found", http.StatusNotFound)
}
`
	dir := filepath.Join(g.OutDir, "internal/panel/httperr")
	return os.WriteFile(filepath.Join(dir, "httperr.go"), []byte(code), 0644)
}
