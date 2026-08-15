package filterexpr

import (
	"strings"
	"testing"
)

func TestSQLBasicOps(t *testing.T) {
	cases := []struct {
		expr, driver, want string
	}{
		{`price = 1000`, "postgres", `t."price" = 1000`},
		{`price > 1000`, "sqlite", `t."price" > 1000`},
		{`price <= 1000`, "mssql", `t.[price] <= 1000`},
		{`prod_name != 'x'`, "postgres", `t."prod_name" != 'x'`},
		{`prod_name <> 'abc'`, "sqlite", `t."prod_name" <> 'abc'`},
		{`prod_name contains 'abc'`, "postgres", `t."prod_name" ILIKE '%abc%'`},
		{`prod_name contains 'abc'`, "sqlite", `t."prod_name" LIKE '%abc%'`},
		{`prod_name not_contains 'a''b'`, "mssql", `t.[prod_name] NOT LIKE '%a''b%'`},
		{`x is_null`, "postgres", `t."x" IS NULL`},
		{`x is_not_null`, "sqlite", `t."x" IS NOT NULL`},
	}
	for _, c := range cases {
		e, err := Parse(c.expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.expr, err)
			continue
		}
		compiled, err := e.SQL(c.driver, "t.")
		if err != nil {
			t.Errorf("SQL(%q, %s): %v", c.expr, c.driver, err)
			continue
		}
		if compiled.Frag != c.want {
			t.Errorf("SQL(%q, %s) = %q, want %q", c.expr, c.driver, compiled.Frag, c.want)
		}
	}
}

func TestSQLPrecedence(t *testing.T) {
	e, err := Parse(`(price > 1000 and prod_name contains 'abc') or prod_code = $1`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, err := e.SQL("postgres", "t.")
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}
	want := `((t."price" > 1000 AND t."prod_name" ILIKE '%abc%') OR t."prod_code" = __GFP__)`
	if c.Frag != want {
		t.Errorf("frag = %q, want %q", c.Frag, want)
	}
	if len(c.Bindings) != 1 {
		t.Fatalf("bindings = %d, want 1", len(c.Bindings))
	}
	if c.Bindings[0].Param != 0 || c.Bindings[0].Contains {
		t.Errorf("binding[0] = %+v, want Param 0 / Contains false", c.Bindings[0])
	}
}

// $2-before-$1 ordering: bindings are recorded in text order, so the generated
// handler replaces __GFP__ tokens in SQL-text order regardless of the param
// numbering used in the DSL.
func TestParamOrdering(t *testing.T) {
	e, err := Parse(`a = $2 or b = $1`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, err := e.SQL("sqlite", "")
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}
	if c.Frag != `("a" = __GFP__ OR "b" = __GFP__)` {
		t.Errorf("frag = %q", c.Frag)
	}
	if len(c.Bindings) != 2 {
		t.Fatalf("bindings = %d, want 2", len(c.Bindings))
	}
	if c.Bindings[0].Param != 1 || c.Bindings[1].Param != 0 {
		t.Errorf("bindings = %+v, want [Param 1, Param 0]", c.Bindings)
	}
}

// A repeated $N produces repeated bindings that all reference the same param.
func TestRepeatedParam(t *testing.T) {
	e, err := Parse(`a = $1 and b = $1`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, err := e.SQL("mssql", "")
	if err != nil {
		t.Fatalf("SQL: %v", err)
	}
	if c.Frag != `([a] = __GFP__ AND [b] = __GFP__)` || len(c.Bindings) != 2 {
		t.Errorf("frag=%q bindings=%d", c.Frag, len(c.Bindings))
	}
}

func TestContainsParamBinding(t *testing.T) {
	e, err := Parse(`name contains $1`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, d := range []string{"postgres", "sqlite", "mssql"} {
		c, err := e.SQL(d, "t.")
		if err != nil {
			t.Fatalf("SQL(%s): %v", d, err)
		}
		if len(c.Bindings) != 1 || !c.Bindings[0].Contains {
			t.Errorf("driver %s: want a contains binding, got %+v", d, c.Bindings)
		}
		if c.Bindings[0].Param != 0 {
			t.Errorf("driver %s: param = %d, want 0", d, c.Bindings[0].Param)
		}
	}
}

func TestColumns(t *testing.T) {
	e, err := Parse(`(a = 1 and b contains 'x') or c is_null`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := e.Columns()
	if strings.Join(got, ",") != "a,b,c" {
		t.Errorf("columns = %v, want [a b c]", got)
	}
}

func TestQualification(t *testing.T) {
	e, err := Parse(`name = 'x'`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	unqualified, _ := e.SQL("postgres", "")
	qualified, _ := e.SQL("postgres", "t.")
	if unqualified.Frag != `"name" = 'x'` || qualified.Frag != `t."name" = 'x'` {
		t.Errorf("unqualified=%q qualified=%q", unqualified.Frag, qualified.Frag)
	}
}

func TestQuoteIdent(t *testing.T) {
	cases := []struct {
		driver, name, want string
	}{
		{"postgres", "Order", `"Order"`},
		{"postgres", "select", `"select"`},
		{"postgres", "a\"b", `"a""b"`},
		{"sqlite", "Order", `"Order"`},
		{"sqlite3", "Order", `"Order"`},
		{"mssql", "Order", `[Order]`},
		{"mssql", "CeleJmeno", `[CeleJmeno]`},
		{"mssql", "a]b", `[a]]b]`},
		{"sqlserver", "Order", `[Order]`},
		{"", "Order", `"Order"`},
	}
	for _, c := range cases {
		if got := QuoteIdent(c.driver, c.name); got != c.want {
			t.Errorf("QuoteIdent(%q, %q) = %q, want %q", c.driver, c.name, got, c.want)
		}
	}
}

func TestInvalidExpressions(t *testing.T) {
	bad := []string{
		``,
		`(`,
		`)`,
		`a`,           // no operator
		`a =`,         // missing value
		`a bogusop 1`, // unknown operator
		`a = $0`,      // param index must be >= 1
		`'unterminated`,
		`1 =`,
	}
	for _, expr := range bad {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", expr)
		}
	}
}
