// tailwind.go
//
// Generates the Tailwind CSS source and static assets (tailwind.config.js and
// package.json) for the admin panel application, and runs the Tailwind CSS
// build. The build is non-fatal: it is invoked after generation and may be
// re-run manually by the user.
package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// generateAssets generates all static assets for the project: the Tailwind
// input CSS and the tailwind.config.js + package.json files.
// Returns: an error if either step fails.
func (g *Generator) generateAssets() error {
	if err := g.generateTailwindCSS(); err != nil {
		return err
	}
	return g.generateStaticAssets()
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
// colors/fonts from the config) and package.json (with the build:css script)
// into the output directory. Returns an error if either file cannot be written.
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
	if err := os.WriteFile(filepath.Join(g.OutDir, "tailwind.config.js"), []byte(tailwindConfig), 0644); err != nil {
		return err
	}

	packageJSON := `{
  "private": true,
  "scripts": {
    "build:css": "tailwindcss -i ./internal/assets/css/styles.css -o ./static/css/styles.css --minify",
    "copy:chartjs": "mkdir -p static/js && cp node_modules/chart.js/dist/chart.umd.js static/js/chart.js"
  },
  "devDependencies": {
    "tailwindcss": "^3.4.0",
    "chart.js": "^4.4.1"
  }
}
`
	if err := os.WriteFile(filepath.Join(g.OutDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		return err
	}

	return nil
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

// RunTailwind executes `npx tailwindcss` to build the compiled CSS into
// static/css/styles.css, streaming output through to the user.
// Returns an error if npx/node are unavailable or the build fails.
func (g *Generator) RunTailwind() error {
	fmt.Println("Running Tailwind CSS build...")
	cmd := exec.Command("npx", "tailwindcss",
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
