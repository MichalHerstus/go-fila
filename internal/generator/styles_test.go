package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/parser"
)

// TestGenerateStylesEmbedded is the coverage guard for the pre-built stylesheet
// (D12). It generates a real project from the kitchen-sink fixture and asserts:
//  1. every literal class="…" token emitted into the templ sources Tailwind
//     scans (internal/views + internal/panel/auth) exists in the embedded
//     styles.css — a template that emits a class the prebuilt CSS never saw
//     fails loudly here instead of silently missing at runtime;
//  2. the full safelist (grid-cols 1..12 × variants, the max-w-* allowlist, the
//     runtime badge classes) is present too, since those are built via
//     fmt.Sprintf and never appear literally;
//  3. the brand color is wired through an RGB-channel variable
//     (rgb(var(--brand-primary-rgb) / …)) so alpha modifiers like
//     bg-brand-primary/10 resolve at runtime.
func TestGenerateStylesEmbedded(t *testing.T) {
	cfg, err := parser.ParseFile(filepath.Join("..", "..", "testdata", "kitchen.yaml"))
	if err != nil {
		t.Fatalf("parse kitchen fixture: %v", err)
	}
	dir := t.TempDir()
	g := New(cfg, dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(stylesCSS) == 0 {
		t.Fatal("embedded styles.css is empty — run `make styles`")
	}
	css := string(stylesCSS)

	classRe := regexp.MustCompile(`class="([^"]*)"`)
	var missing []string
	seen := map[string]bool{}
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".templ") {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if !(strings.HasPrefix(rel, "internal/views"+string(filepath.Separator)) ||
			strings.HasPrefix(rel, "internal/panel/auth"+string(filepath.Separator))) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range classRe.FindAllStringSubmatch(string(b), -1) {
			for _, tok := range strings.Fields(m[1]) {
				// Skip runtime placeholders (fmt.Sprintf %v / templ {expr}).
				if strings.ContainsAny(tok, "%{}`") {
					continue
				}
				if seen[tok] {
					continue
				}
				seen[tok] = true
				if !strings.Contains(css, twEsc(tok)) {
					missing = append(missing, tok+" (in "+rel+")")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("class(es) emitted by templates are missing from the embedded stylesheet; run `make styles` after adding them to the safelist:\n  %s",
			strings.Join(missing, "\n  "))
	}

	// Full safelist the fmt.Sprintf-driven classes rely on.
	for v := 1; v <= 12; v++ {
		if !strings.Contains(css, twEsc("lg:grid-cols-"+strconv.Itoa(v))) {
			t.Errorf("safelist missing lg:grid-cols-%d", v)
		}
	}
	for _, m := range []string{
		"sm:grid-cols-1", "md:grid-cols-1", "lg:grid-cols-1",
		"sm:grid-cols-12", "md:grid-cols-12", "lg:grid-cols-12",
	} {
		if !strings.Contains(css, twEsc(m)) {
			t.Errorf("safelist missing %s", m)
		}
	}
	for _, w := range []string{
		"max-w-none", "max-w-xs", "max-w-sm", "max-w-md", "max-w-lg", "max-w-xl",
		"max-w-2xl", "max-w-3xl", "max-w-4xl", "max-w-5xl", "max-w-6xl", "max-w-7xl",
		"max-w-full", "max-w-min", "max-w-max", "max-w-fit", "max-w-prose",
		"max-w-screen-sm", "max-w-screen-md", "max-w-screen-lg", "max-w-screen-xl", "max-w-screen-2xl",
	} {
		if !strings.Contains(css, twEsc(w)) {
			t.Errorf("safelist missing %s", w)
		}
	}
	for _, b := range []string{"bg-gray-100", "text-gray-800", "dark:bg-gray-900/50", "dark:text-gray-300"} {
		if !strings.Contains(css, twEsc(b)) {
			t.Errorf("safelist missing badge class %s", b)
		}
	}
	if !strings.Contains(css, "rgb(var(--brand-primary-rgb)") {
		t.Error("stylesheet must define brand colors as rgb(var(--brand-primary-rgb) / …) so /alpha utilities work")
	}
}

// twEsc reproduces Tailwind's CSS selector escaping for a class name so a raw
// template token (e.g. "dark:bg-gray-900/50") can be checked against the
// embedded minified stylesheet, which stores it escaped ("dark\:bg-gray-900\/50").
func twEsc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ':', '/', '.', '[', ']', '%', ',', '#', '(', ')', '!', '&', '+':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
