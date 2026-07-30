package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func (g *Generator) generateAssets() error {
	if err := g.generateTailwindCSS(); err != nil {
		return err
	}
	return g.generateStaticAssets()
}

func (g *Generator) generateTailwindCSS() error {
	css := `@tailwind base;
@tailwind components;
@tailwind utilities;
`
	return os.WriteFile(filepath.Join(g.OutDir, "internal/assets/css/styles.css"), []byte(css), 0644)
}

func (g *Generator) generateStaticAssets() error {
	tailwindConfig := `/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/views/**/*.templ", "./internal/panel/auth/**/*.templ"],
  theme: {
    extend: {},
  },
  plugins: [],
}
`
	if err := os.WriteFile(filepath.Join(g.OutDir, "tailwind.config.js"), []byte(tailwindConfig), 0644); err != nil {
		return err
	}

	packageJSON := `{
  "private": true,
  "scripts": {
    "build:css": "tailwindcss -i ./internal/assets/css/styles.css -o ./static/css/styles.css --minify"
  },
  "devDependencies": {
    "tailwindcss": "^3.4.0"
  }
}
`
	if err := os.WriteFile(filepath.Join(g.OutDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		return err
	}

	return nil
}

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
