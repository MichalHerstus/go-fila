package fixer

import (
	"reflect"
	"strings"
	"testing"
)

const validBase = `version: "1.0"
panel:
    id: admin
    path: /admin
    name: My Admin
`

func mustApply(t *testing.T, input string) ([]byte, []string, []error) {
	t.Helper()
	out, fixed, remaining, err := Apply([]byte(input))
	if err != nil {
		t.Fatalf("Apply err: %v", err)
	}
	return out, fixed, remaining
}

func TestApplyRemovesEmptyListFilter(t *testing.T) {
	out, fixed, remaining := mustApply(t, validBase+`
resources:
  - name: Category
    list:
      filter:
        label: ""
        where: ""
        params: []
`)
	if len(remaining) != 0 {
		t.Fatalf("remaining: %v", remaining)
	}
	want := []string{"resources/Category/list.filter"}
	if !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if strings.Contains(string(out), "filter") {
		t.Fatalf("filter block not removed:\n%s", out)
	}
}

func TestApplyRemovesEmptyCardFilter(t *testing.T) {
	out, fixed, remaining := mustApply(t, validBase+`
resources:
  - name: Product
    card:
      fields:
        - name: Title
          type: string
      filter:
        where: ""
`)
	if len(remaining) != 0 {
		t.Fatalf("remaining: %v", remaining)
	}
	if want := []string{"resources/Product/card.filter"}; !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if strings.Contains(string(out), "filter") {
		t.Fatal("filter block not removed")
	}
}

func TestApplyFixesMultipleSectionsAndResources(t *testing.T) {
	out, fixed, remaining := mustApply(t, validBase+`
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
	if len(remaining) != 0 {
		t.Fatalf("remaining: %v", remaining)
	}
	want := []string{
		"resources/Category/list.filter",
		"resources/Order/list.filter",
		"resources/Order/card.filter",
	}
	if !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if strings.Contains(string(out), "filter") {
		t.Fatal("filter blocks not all removed")
	}
}

func TestApplyKeepsMeaningfulFilter(t *testing.T) {
	cfg := validBase + `
resources:
  - name: Category
    list:
      filter:
        label: "Advanced"
        where: "status = $1"
        params:
          - name: status
            label: "Status"
`
	out, fixed, remaining := mustApply(t, cfg)
	if len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("fixed=%v remaining=%v", fixed, remaining)
	}
	if string(out) != cfg {
		t.Fatal("a configured filter must not change")
	}
}

func TestApplyLabelOnlyFilterNotRemoved(t *testing.T) {
	cfg := validBase + `
resources:
  - name: Category
    list:
      filter:
        label: "Search"
`
	out, fixed, remaining := mustApply(t, cfg)
	if len(fixed) != 0 {
		t.Fatalf("fixed: %v", fixed)
	}
	if len(remaining) != 1 || !strings.Contains(remaining[0].Error(), "where is required") {
		t.Fatalf("remaining = %v", remaining)
	}
	if string(out) != cfg {
		t.Fatal("a label-only filter must stay untouched")
	}
}

func TestApplyKeepsNullFilter(t *testing.T) {
	cfg := validBase + "resources:\n  - name: Category\n    list:\n      filter: null\n"
	out, fixed, remaining := mustApply(t, cfg)
	if len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("fixed=%v remaining=%v", fixed, remaining)
	}
	if string(out) != cfg {
		t.Fatal("filter: null already means 'no filter' and must stay untouched")
	}
}

func TestApplyPartialRepairKeepsUnfixableErrors(t *testing.T) {
	out, fixed, remaining := mustApply(t, validBase+`
resources:
  - name: Category
    list:
      filter:
        where: ""
    import_csv: true
`)
	if want := []string{"resources/Category/list.filter"}; !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if len(remaining) != 1 || !strings.Contains(remaining[0].Error(), "import_csv") {
		t.Fatalf("remaining = %v", remaining)
	}
	if strings.Contains(string(out), "list:") {
		// filter removed but import_csv error still reported
		if strings.Contains(string(out), "filter") {
			t.Fatal("fixed bytes must carry the partial repair (filter removed)")
		}
	}
}

func TestApplyValidConfigIsNoOp(t *testing.T) {
	cfg := validBase + "resources:\n  - name: User\n"
	out, fixed, remaining := mustApply(t, cfg)
	if len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("fixed=%v remaining=%v", fixed, remaining)
	}
	if string(out) != cfg {
		t.Fatal("valid config bytes must round-trip untouched")
	}
}

func TestApplyUnparseableYAML(t *testing.T) {
	_, _, _, err := Apply([]byte("version: [\nnot: yaml: ]"))
	if err == nil {
		t.Fatal("expected a parsing error")
	}
}
