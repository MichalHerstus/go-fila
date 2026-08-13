package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedAgentGuide ensures the AGENTS.md guide is embedded in the binary
// and non-empty.
func TestEmbeddedAgentGuide(t *testing.T) {
	if !strings.Contains(agentsForGeneratedDashboard, "generated dashboard") {
		t.Fatalf("embedded agent guide must reference the generated dashboard, got %d bytes", len(agentsForGeneratedDashboard))
	}
	if len(agentsForGeneratedDashboard) < 100 {
		t.Fatalf("embedded agent guide suspiciously small: %d bytes", len(agentsForGeneratedDashboard))
	}
}

// TestEnsureAgentGuideWritesWhenMissing ensures a run in a directory without
// AGENTS.md creates it with the embedded content.
func TestEnsureAgentGuideWritesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	ensureAgentGuide()

	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("AGENTS.md should have been created: %v", err)
	}
	if string(data) != agentsForGeneratedDashboard {
		t.Fatal("AGENTS.md content does not match the embedded guide")
	}
}

// TestEnsureAgentGuideDoesNotOverwrite ensures an existing AGENTS.md is left
// untouched.
func TestEnsureAgentGuideDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("custom"), 0644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	ensureAgentGuide()

	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom" {
		t.Fatalf("existing AGENTS.md overwritten: %q", data)
	}
}
