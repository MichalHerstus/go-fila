package generator

import (
	"fmt"
	"os"
	"path/filepath"
)

func (g *Generator) generateGoMod() error {
	modName := filepath.Base(g.OutDir)
	code := fmt.Sprintf(`module %s

go 1.26

require (
	github.com/a-h/templ v0.3.819
	github.com/go-chi/chi/v5 v5.3.1
	github.com/gorilla/sessions v1.4.0
	golang.org/x/crypto v0.31.0
)
`, modName)

	return os.WriteFile(filepath.Join(g.OutDir, "go.mod"), []byte(code), 0644)
}
