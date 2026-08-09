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
