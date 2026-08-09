package main

import (
	"strings"
	"testing"

	"github.com/go-fila/go-fila/internal/parser"
)

// TestDemoYAMLValid ensures the demo config (extended with the Customer delete
// used by the audit plugin) still parses and validates.
func TestDemoYAMLValid(t *testing.T) {
	cfg, err := parser.Parse([]byte(demoYAML()))
	if err != nil {
		t.Fatalf("demo YAML should parse: %v", err)
	}
	if cfg.Panel.Path == "" {
		t.Fatal("demo panel path missing")
	}
	foundCustomer := false
	for _, r := range cfg.Resources {
		if r.Name == "Customer" {
			foundCustomer = true
			if r.Form == nil || r.Form.Delete == nil {
				t.Fatal("demo Customer must declare a delete form action (audit plugin hook target)")
			}
		}
	}
	if !foundCustomer {
		t.Fatal("demo YAML missing Customer resource")
	}
	if !strings.Contains(demoSchema(), "CREATE TABLE") {
		t.Fatal("demo schema missing")
	}
}

// TestRandomPassword ensures the generated one-time admin password has the
// expected length and only uses characters from the unambiguous alphabet.
func TestRandomPassword(t *testing.T) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for i := 0; i < 50; i++ {
		pw := randomPassword()
		if len(pw) != 14 {
			t.Fatalf("randomPassword() length = %d, want 14", len(pw))
		}
		for _, c := range pw {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("randomPassword() returned invalid char %q", c)
			}
		}
	}
}
