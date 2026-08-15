// sqlutil.go
//
// Generates the shared internal/panel/sqlutil package: a single runtime helper
// Ident(s) that quotes an identifier the way the configured database expects
// (double quotes on postgres/sqlite, brackets on mssql). The list/card
// handlers use it for the sort column because `sort` is a runtime URL
// parameter that cannot be pre-quoted at generation time; quoting it both
// supports keyword-named columns and prevents identifier injection.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
)

// generateSQLUtil writes internal/panel/sqlutil/sqlutil.go with the driver
// baked in. Embedded " characters are doubled (mssql doubles ]).
// Returns an error on write failure.
func (g *Generator) generateSQLUtil() error {
	open, close, esc := `"`, `"`, `""`
	if g.isMSSQL() {
		open, close, esc = "[", "]", "]]"
	}
	code := fmt.Sprintf(`package sqlutil

import "strings"

func Ident(s string) string {
    s = strings.ReplaceAll(s, %q, %q)
    return %q + s + %q
}
`, esc, esc, open, close)
	dir := filepath.Join(g.OutDir, "internal", "panel", "sqlutil")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sqlutil.go"), []byte(code), 0644)
}
