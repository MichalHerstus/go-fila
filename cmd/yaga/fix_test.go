// fix_test.go — tests for `yaga validate --fix` / `--dry-run` auto-repair.
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/parser"
)

const validBase = `version: "1.0"
panel:
    id: admin
    path: /admin
    name: My Admin
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "yaga.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAutoFixRemovesEmptyListFilter(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: Category
    list:
      query: ListCategory
      columns:
        - name: Id
          type: integer
      filter:
        label: ""
        where: ""
        params: []
`)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected the config to change")
	}
	if len(remaining) != 0 {
		t.Fatalf("unexpected remaining errors: %v", remaining)
	}
	want := []string{"resources/Category/list.filter"}
	if !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if _, err := parser.ParseFile(p); err != nil {
		t.Fatalf("config still invalid after fix: %v", err)
	}
	if got := readFile(t, p); strings.Contains(got, "filter") {
		t.Fatalf("filter block not removed:\n%s", got)
	}
	backup := readFile(t, p+".bak")
	if backup == readFile(t, p) {
		t.Fatal("backup must hold the original config, not the fixed one")
	}
	if !strings.Contains(backup, "filter") {
		t.Fatal("backup lost the original filter block")
	}
}

func TestAutoFixRemovesEmptyCardFilter(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: Product
    card:
      fields:
        - name: Title
          type: string
      filter:
        where: ""
`)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(remaining) != 0 {
		t.Fatalf("changed=%v remaining=%v", changed, remaining)
	}
	if want := []string{"resources/Product/card.filter"}; !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if _, err := parser.ParseFile(p); err != nil {
		t.Fatalf("config still invalid after fix: %v", err)
	}
	if strings.Contains(readFile(t, p), "filter") {
		t.Fatal("card.filter not removed")
	}
}

func TestAutoFixFixesMultipleSectionsInOneRun(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: Category
    list:
      filter:
        where: ""
  - name: Order
    list:
      filter:
        label: ""
        where: ""
        params: []
    card:
      fields:
        - name: id
          type: integer
      filter: {}
`)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil || !changed || len(remaining) != 0 {
		t.Fatalf("err=%v changed=%v remaining=%v", err, changed, remaining)
	}
	want := []string{
		"resources/Category/list.filter",
		"resources/Order/list.filter",
		"resources/Order/card.filter",
	}
	if !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if _, err := parser.ParseFile(p); err != nil {
		t.Fatalf("config still invalid after fix: %v", err)
	}
}

func TestAutoFixKeepsMeaningfulFilter(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: Category
    list:
      filter:
        label: "Advanced"
        where: "status = $1"
        params:
          - name: status
            label: "Status"
`)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a configured filter must not change")
	}
	if len(fixed) != 0 {
		t.Fatalf("unexpected fixes: %v", fixed)
	}
	if len(remaining) != 0 {
		t.Fatalf("config should be valid, remaining: %v", remaining)
	}
	if _, err := parser.ParseFile(p); err != nil {
		t.Fatalf("config invalid: %v", err)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("no backup expected for an unchanged config, stat err=%v", err)
	}
}

func TestAutoFixLabelOnlyFilterNotRemoved(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: Category
    list:
      filter:
        label: "Search"
`)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("a label-only filter is user intent and must not be dropped")
	}
	if len(fixed) != 0 {
		t.Fatalf("unexpected fixes: %v", fixed)
	}
	if len(remaining) != 1 || !strings.Contains(remaining[0].Error(), "where is required") {
		t.Fatalf("expected the where-required error to remain, got %v", remaining)
	}
	if !strings.Contains(readFile(t, p), "filter") {
		t.Fatalf("file must stay untouched:\n%s", readFile(t, p))
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup expected when nothing was written")
	}
}

func TestAutoFixKeepsNullFilter(t *testing.T) {
	cfg := validBase + `resources:
  - name: Category
    list:
      filter: null
`
	p := writeConfig(t, cfg)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil || changed || len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("err=%v changed=%v fixed=%v remaining=%v", err, changed, fixed, remaining)
	}
	if got := readFile(t, p); got != cfg {
		t.Fatal("filter: null already means 'no filter' and must stay untouched")
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup for a no-op run")
	}
}

func TestAutoFixValidConfigIsNoOp(t *testing.T) {
	cfg := validBase + "resources:\n  - name: User\n"
	p := writeConfig(t, cfg)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil || changed || len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("err=%v changed=%v fixed=%v remaining=%v", err, changed, fixed, remaining)
	}
	if got := readFile(t, p); got != cfg {
		t.Fatal("valid config bytes must be untouched")
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup for a no-op run")
	}
}

func TestAutoFixUnfixableErrorLeavesFile(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: User
    import_csv: true
`)
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed || len(fixed) != 0 {
		t.Fatalf("changed=%v fixed=%v, want untouched", changed, fixed)
	}
	if len(remaining) != 1 || !strings.Contains(remaining[0].Error(), "import_csv") {
		t.Fatalf("expected the import_csv error to remain, got %v", remaining)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup when nothing was written")
	}
}

func TestAutoFixDryRunWritesNothingEmptyFilter(t *testing.T) {
	p := writeConfig(t, validBase+`
resources:
  - name: Category
    list:
      filter:
        where: ""
`)
	before := readFile(t, p)
	fixed, changed, remaining, err := autoFixFile(p, true)
	if err != nil || !changed || len(remaining) != 0 {
		t.Fatalf("err=%v changed=%v remaining=%v", err, changed, remaining)
	}
	want := []string{"resources/Category/list.filter"}
	if !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if got := readFile(t, p); got != before {
		t.Fatal("dry run must not modify the file")
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("dry run must not write a backup")
	}
}

func TestAutoFixUnparseableYAML(t *testing.T) {
	p := writeConfig(t, "version: [\nnot: yaml: ]")
	fixed, changed, remaining, err := autoFixFile(p, false)
	if err == nil {
		t.Fatal("expected a parsing error")
	}
	if changed || len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("changed=%v fixed=%v remaining=%v", changed, fixed, remaining)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup for an unparseable file")
	}
}

func TestWantFixFlags(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"yaga", "validate", "--fix"}
	if fix, dryRun := wantFixFlags(); !fix || dryRun {
		t.Fatalf("--fix → fix=%v dryRun=%v, want true/false", fix, dryRun)
	}
	os.Args = []string{"yaga", "validate", "--dry-run"}
	if fix, dryRun := wantFixFlags(); fix || !dryRun {
		t.Fatalf("--dry-run → fix=%v dryRun=%v, want false/true", fix, dryRun)
	}
	os.Args = []string{"yaga", "validate", "--config", "other.yaml", "--fix"}
	if _, dryRun := wantFixFlags(); dryRun {
		t.Fatal("--config must be ignored by wantFixFlags")
	}
	os.Args = []string{"yaga", "validate"}
	if fix, dryRun := wantFixFlags(); fix || dryRun {
		t.Fatalf("no flags → fix=%v dryRun=%v, want false/false", fix, dryRun)
	}
}
