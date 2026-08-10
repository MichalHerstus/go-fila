package main

import (
	"fmt"
	"os"

	"github.com/go-fila/go-fila/cmd/go-fila/editor"
	"github.com/go-fila/go-fila/internal/parser"
	"gopkg.in/yaml.v3"
)

func cmdEdit() {
	configPath, _, _, _, _, _, _, _ := parseGlobalFlags()
	apiKey, model, prompt, dryRun := parseEditFlags(os.Args[2:])

	// D7: AI-assisted editing (opt-in). When --prompt is set, the AI path runs
	// instead of the TUI; the config is sent to OpenRouter (consent is the user
	// supplying the key + prompt). --dry-run previews without writing.
	if prompt != "" {
		if err := editAI(openRouterBaseURL, configPath, apiKey, model, prompt, dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := parser.ParseFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config: %v\n", err)
		os.Exit(1)
	}

	ed := editor.New(cfg, configPath)
	saved, err := ed.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Editor error: %v\n", err)
		os.Exit(1)
	}

	if saved {
		data, err := yaml.Marshal(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling YAML: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Saved %s\n", configPath)
	}
}
