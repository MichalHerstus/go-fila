// httperr.go
//
// Generates the shared internal/panel/httperr package: small helpers that log
// the real error server-side and return a generic status text to the client.
// This keeps SQL details, table names and driver internals out of HTTP
// responses while preserving the diagnostics in the server log.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
)

// generateHTTPErr writes internal/panel/httperr/httperr.go. Internal logs the
// error and returns "Internal Server Error" (500); NotFound logs and returns
// "Not Found" (404). When the config declares any script: body, the file also
// gains BadRequest, which returns the config-author-written abort() message as
// a 400 (the message only ever originates from trusted config text). Feature-off
// output stays byte-identical (BadRequest is omitted when no script exists).
// Returns an error on write failure.
func (g *Generator) generateHTTPErr() error {
	badRequest := ""
	if g.hasAnyScript() {
		badRequest = `
func BadRequest(w http.ResponseWriter, msg string) {
    http.Error(w, msg, http.StatusBadRequest)
}
`
	}
	code := fmt.Sprintf(`package httperr

import (
    "log"
    "net/http"
)

func Internal(w http.ResponseWriter, err error) {
    log.Printf("internal error: %%v", err)
    http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

func NotFound(w http.ResponseWriter, err error) {
    log.Printf("not found: %%v", err)
    http.Error(w, "Not Found", http.StatusNotFound)
}
%s`, badRequest)
	dir := filepath.Join(g.OutDir, "internal/panel/httperr")
	return os.WriteFile(filepath.Join(dir, "httperr.go"), []byte(code), 0644)
}
