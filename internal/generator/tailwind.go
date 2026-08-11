// tailwind.go
//
// Generates the Tailwind CSS source and static assets (tailwind.config.js and
// the vendored Chart.js bundle) for the admin panel application, and runs the
// Tailwind CSS build. The build is non-fatal: it is invoked after generation
// and may be re-run manually by the user. Chart.js is embedded into the go-fila
// binary (internal/generator/assets/chart.umd.js) so the generated dashboard
// needs no npm/node and stays offline at runtime.
package generator

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed assets/chart.umd.js
var chartUmdJS []byte

// generateAssets generates all static assets for the project: the Tailwind
// input CSS, the tailwind.config.js file and the vendored Chart.js bundle at
// static/js/chart.js.
// Returns: an error if any step fails.
func (g *Generator) generateAssets() error {
	if err := g.generateTailwindCSS(); err != nil {
		return err
	}
	if err := g.generateStaticAssets(); err != nil {
		return err
	}
	return g.writeChartJS()
}

// generateTailwindCSS writes the Tailwind directives file that serves as the
// CSS build input (internal/assets/css/styles.css). Returns an error on write
// failure.
func (g *Generator) generateTailwindCSS() error {
	css := `@tailwind base;
@tailwind components;
@tailwind utilities;
`
	return os.WriteFile(filepath.Join(g.OutDir, "internal/assets/css/styles.css"), []byte(css), 0644)
}

// generateStaticAssets writes tailwind.config.js (scanning the templ views
// for class names, with dark mode enabled via the 'class' strategy and brand
// colors/fonts from the config) into the output directory. Returns an error if
// the file cannot be written.
func (g *Generator) generateStaticAssets() error {
	primary := g.Config.Panel.Brand.Colors.Primary
	if primary == "" {
		primary = "#6366f1"
	}
	secondary := g.Config.Panel.Brand.Colors.Secondary
	if secondary == "" {
		secondary = "#8b5cf6"
	}

	fontExtend := ""
	if g.Config.Panel.Theme.Font.Family != "" || g.Config.Panel.Theme.Font.Mono != "" {
		var sb strings.Builder
		sb.WriteString(",\n      fontFamily: {")
		if g.Config.Panel.Theme.Font.Family != "" {
			sb.WriteString(fmt.Sprintf("\n        sans: %s,", fontStack(g.Config.Panel.Theme.Font.Family)))
		}
		if g.Config.Panel.Theme.Font.Mono != "" {
			sb.WriteString(fmt.Sprintf("\n        mono: %s,", fontStack(g.Config.Panel.Theme.Font.Mono)))
		}
		sb.WriteString("\n      }")
		fontExtend = sb.String()
	}

	tailwindConfig := fmt.Sprintf(`/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: 'class',
  content: ["./internal/views/**/*.templ", "./internal/panel/auth/**/*.templ"],
  theme: {
    extend: {
      colors: {
        brand: {
          primary: %q,
          secondary: %q,
        },
      }%s,
    },
  },
  plugins: [],
}
`, primary, secondary, fontExtend)
	return os.WriteFile(filepath.Join(g.OutDir, "tailwind.config.js"), []byte(tailwindConfig), 0644)
}

// writeChartJS writes the embedded Chart.js UMD bundle to static/js/chart.js
// so the generated dashboard serves charts locally with no npm step and no CDN
// at runtime. Returns an error on write failure.
func (g *Generator) writeChartJS() error {
	return os.WriteFile(filepath.Join(g.OutDir, "static/js/chart.js"), chartUmdJS, 0644)
}

// fontStack converts a comma-separated CSS font stack ("Inter, sans-serif")
// into a Tailwind fontFamily array of individually quoted names
// (['Inter', 'sans-serif']), so Tailwind emits unquoted, comma-separated
// family names instead of treating the whole stack as one quoted family.
// Params: family (the comma-separated font stack from the config).
// Returns: the JS array literal as a string.
func fontStack(family string) string {
	var parts []string
	for _, p := range strings.Split(family, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("'%s'", p))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// RunTailwind executes the Tailwind CSS standalone binary to build the compiled
// CSS into static/css/styles.css, streaming output through to the user. The
// binary is resolved from the TAILWIND environment variable when set (e.g. the
// standalone binary downloaded by `make get-tailwind`), otherwise from PATH
// ("tailwindcss"). No npm/node is required.
// Returns an error if the binary is unavailable or the build fails.
func (g *Generator) RunTailwind() error {
	fmt.Println("Running Tailwind CSS build...")
	bin := os.Getenv("TAILWIND")
	if bin == "" {
		bin = "tailwindcss"
	}
	cmd := exec.Command(bin,
		"-i", "./internal/assets/css/styles.css",
		"-o", "./static/css/styles.css",
		"--minify")
	cmd.Dir = g.OutDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tailwind build failed: %w", err)
	}
	return nil
}
