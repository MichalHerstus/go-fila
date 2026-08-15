// tailwind.go
//
// Generates the static assets for the admin panel application: the vendored
// Chart.js bundle (static/js/chart.js) and the pre-built Tailwind stylesheet
// (static/css/styles.css). Both are embedded into the yaga binary via
// //go:embed, so the generated dashboard needs no npm/node, no Tailwind binary
// and no sqlc — fully offline at build and runtime. The stylesheet is compiled
// once by `make styles` (scripts/build-styles.sh) from the kitchen-sink
// fixture; the generated project never runs Tailwind.
package generator

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed assets/chart.umd.js
var chartUmdJS []byte

//go:embed assets/styles.css
var stylesCSS []byte

// StylesCSS returns the pre-built Tailwind stylesheet (embedded). It is
// shared with the wedit preview (internal/serve) so the editor's dashboard
// mock uses the exact same compiled classes as the generated app.
func StylesCSS() []byte { return stylesCSS }

// ChartUmdJS returns the vendored Chart.js bundle (embedded). Shared with the
// wedit preview so its chart widgets render with real Chart.js, identical to
// the generated dashboard.
func ChartUmdJS() []byte { return chartUmdJS }

// HexChannels converts a "#rrggbb" hex color into the "r g b" channel triplet
// for the --brand-*-rgb CSS variables. Exported so the wedit preview emits the
// same variables as the generated Base layout.
func HexChannels(hex string) string { return hexChannels(hex) }

// generateAssets writes all static assets for the project: the pre-built
// stylesheet at static/css/styles.css and the vendored Chart.js bundle at
// static/js/chart.js, byte-identical to the embedded copies.
// Returns: an error if any step fails.
func (g *Generator) generateAssets() error {
	if err := os.WriteFile(filepath.Join(g.OutDir, "static/css/styles.css"), stylesCSS, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(g.OutDir, "static/js/chart.js"), chartUmdJS, 0644)
}

// hexChannels converts a "#rrggbb" hex color into the "r g b" channel triplet
// used for the --brand-*-rgb CSS variables. Mirrors the generated
// viewmodels.BrandChannels helper so the baked login page matches the runtime
// Base layout; both must stay in sync.
func hexChannels(hex string) string {
	if len(hex) == 7 && hex[0] == '#' {
		var r, g, b int
		if _, err := fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b); err == nil {
			return fmt.Sprintf("%d %d %d", r, g, b)
		}
	}
	return "99 102 241"
}
