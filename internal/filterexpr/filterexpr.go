// Package filterexpr parses and compiles the list/card `filter.where` mini-DSL
// into dialect-correct SQL fragments. The expression is trusted YAML (from the
// generator config), so it is compiled at generation time; the only runtime
// inputs are the $N param VALUES, which are bound as placeholders. This keeps
// the security posture intact — no raw user SQL ever reaches the database.
//
// Grammar (standard SQL precedence — AND binds tighter than OR):
//
//	expr      := or
//	or        := and ( "or" and )*
//	and       := primary ( "and" primary )*
//	primary   := "(" expr ")" | condition
//	condition := column OP [value]
//	column    := [A-Za-z_][A-Za-z0-9_]*
//	OP        := =  !=  <>  <  <=  >  >=  | contains | not_contains | is_null | is_not_null
//	value     := number | 'quoted string' ('' escapes) | $N
//
// `contains`/`not_contains` map to ILIKE/LIKE per driver; a literal value is
// baked into the emitted SQL, while a $N becomes a `__GFP__` placeholder token
// (described by Compiled.Bindings) that the generated handler replaces at
// request time and binds from the URL params.
package filterexpr

import (
	"fmt"
	"strings"
)

// GHOST is the placeholder token that appears in a Compiled.Frag in place of
// each runtime $N param. The generated handler replaces it (in order) with the
// driver placeholder (`?` or `$N`) at request time.
const GHOST = "__GFP__"

// Binding describes one __GFP__ placeholder token in Compiled.Frag, in text
// order. Param is the zero-based index into the filter's Params list ($N → N-1);
// Contains marks a `contains`/`not_contains` condition whose runtime value must
// be wrapped in "%" + v + "%".
type Binding struct {
	Param    int
	Contains bool
}

// Compiled is the result of compiling an expression: a dialect-correct SQL
// fragment with __GFP__ placeholder tokens plus the bindings that describe each
// token, in order.
type Compiled struct {
	Frag     string
	Bindings []Binding
}

// Expr is a parsed filter expression tree.
type Expr struct {
	root node
}

// SQL compiles the parsed expression to a dialect-correct SQL fragment for the
// given driver ("postgres"/"sqlite"/"mssql"). colPrefix ("t." or "") qualifies
// every column, needed when the generated query uses FK LEFT JOINs. Literal
// values are baked in; $N params become __GFP__ tokens described by the
// returned Bindings.
func (e *Expr) SQL(driver, colPrefix string) (*Compiled, error) {
	c := &compiler{driver: driver, colPrefix: colPrefix}
	frag, err := c.emit(e.root)
	if err != nil {
		return nil, err
	}
	return &Compiled{Frag: frag, Bindings: c.bindings}, nil
}

// Columns returns the unique (unqualified) column names referenced by the
// expression, for editor/schema validation. They are returned in first-appearance
// order.
func (e *Expr) Columns() []string {
	var out []string
	seen := map[string]bool{}
	var walk func(n node)
	walk = func(n node) {
		switch t := n.(type) {
		case *orNode:
			for _, c := range t.conds {
				walk(c)
			}
		case *andNode:
			for _, c := range t.conds {
				walk(c)
			}
		case *condNode:
			if !seen[t.col] {
				seen[t.col] = true
				out = append(out, t.col)
			}
		}
	}
	walk(e.root)
	return out
}

type node interface{}

type orNode struct{ conds []node }
type andNode struct{ conds []node }

type condNode struct {
	col      string
	op       string
	val      string // literal value text (number or unquoted string) when !hasParam
	param    int    // zero-based param index when hasParam
	hasParam bool
	isNull   bool // is_null / is_not_null (no value)
	negNull  bool // is_not_null
}

// --- Lexer ---

type tokKind int

const (
	tEOF tokKind = iota
	tLParen
	tRParen
	tIdent
	tNumber
	tString
	tParam
	tOp
)

type token struct {
	kind tokKind
	text string
	pos  int
}

type lexer struct {
	src string
	pos int
}

func (l *lexer) peekTok() (token, error) {
	old := l.pos
	tok, err := l.nextTok()
	l.pos = old
	return tok, err
}

func (l *lexer) nextTok() (token, error) {
	for l.pos < len(l.src) && isSpace(l.src[l.pos]) {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{kind: tEOF, pos: l.pos}, nil
	}
	start := l.pos
	c := l.src[l.pos]
	switch {
	case c == '(':
		l.pos++
		return token{kind: tLParen, text: "(", pos: start}, nil
	case c == ')':
		l.pos++
		return token{kind: tRParen, text: ")", pos: start}, nil
	case c == '\'':
		return l.lexString(start)
	case c == '$':
		return l.lexParam(start)
	case isDigit(c):
		return l.lexNumber(start)
	case isIdentStart(c):
		return l.lexIdent(start)
	}
	// operators
	for _, op := range []string{"<=", ">=", "<>", "!=", "=", "<", ">"} {
		if strings.HasPrefix(l.src[l.pos:], op) {
			l.pos += len(op)
			return token{kind: tOp, text: op, pos: start}, nil
		}
	}
	return token{}, fmt.Errorf("unexpected character %q at position %d", string(c), start)
}

func (l *lexer) lexString(start int) (token, error) {
	l.pos++ // opening '
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\'' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'' {
				sb.WriteByte('\'')
				l.pos += 2
				continue
			}
			l.pos++
			return token{kind: tString, text: sb.String(), pos: start}, nil
		}
		sb.WriteByte(c)
		l.pos++
	}
	return token{}, fmt.Errorf("unterminated string literal at position %d", start)
}

func (l *lexer) lexParam(start int) (token, error) {
	l.pos++ // '$'
	dstart := l.pos
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	if l.pos == dstart {
		return token{}, fmt.Errorf("expected digits after $ at position %d", start)
	}
	return token{kind: tParam, text: l.src[dstart:l.pos], pos: start}, nil
}

func (l *lexer) lexNumber(start int) (token, error) {
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
		l.pos++
	}
	return token{kind: tNumber, text: l.src[start:l.pos], pos: start}, nil
}

func (l *lexer) lexIdent(start int) (token, error) {
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	return token{kind: tIdent, text: l.src[start:l.pos], pos: start}, nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

// --- Parser ---

type parser struct {
	lex *lexer
	tok token
}

// Parse parses the expression into an AST, validating the grammar.
func Parse(expr string) (*Expr, error) {
	p := &parser{lex: &lexer{src: expr}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tEOF {
		return nil, fmt.Errorf("unexpected token %q at position %d", p.tok.text, p.tok.pos)
	}
	return &Expr{root: root}, nil
}

func (p *parser) advance() error {
	tok, err := p.lex.nextTok()
	if err != nil {
		return err
	}
	p.tok = tok
	return nil
}

func (p *parser) parseOr() (node, error) {
	first, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	conds := []node{first}
	for p.tok.kind == tIdent && strings.EqualFold(p.tok.text, "or") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		n, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		conds = append(conds, n)
	}
	if len(conds) == 1 {
		return first, nil
	}
	return &orNode{conds: conds}, nil
}

func (p *parser) parseAnd() (node, error) {
	first, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	conds := []node{first}
	for p.tok.kind == tIdent && strings.EqualFold(p.tok.text, "and") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		n, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		conds = append(conds, n)
	}
	if len(conds) == 1 {
		return first, nil
	}
	return &andNode{conds: conds}, nil
}

func (p *parser) parsePrimary() (node, error) {
	if p.tok.kind == tLParen {
		if err := p.advance(); err != nil {
			return nil, err
		}
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tRParen {
			return nil, fmt.Errorf("expected ')' at position %d", p.tok.pos)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return n, nil
	}
	return p.parseCondition()
}

func (p *parser) parseCondition() (node, error) {
	if p.tok.kind != tIdent {
		return nil, fmt.Errorf("expected column name at position %d", p.tok.pos)
	}
	col := p.tok.text
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind != tOp && p.tok.kind != tIdent {
		return nil, fmt.Errorf("expected operator after column %q at position %d", col, p.tok.pos)
	}
	op := p.tok.text
	if err := p.advance(); err != nil {
		return nil, err
	}
	switch op {
	case "=", "!=", "<>", "<", "<=", ">", ">=",
		"contains", "not_contains", "is_null", "is_not_null":
	default:
		return nil, fmt.Errorf("unknown operator %q at position %d", op, p.tok.pos-1)
	}
	cond := &condNode{col: col, op: op}
	switch op {
	case "is_null":
		cond.isNull = true
		return cond, nil
	case "is_not_null":
		cond.isNull = true
		cond.negNull = true
		return cond, nil
	}
	switch p.tok.kind {
	case tNumber:
		cond.val = p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}
	case tString:
		cond.val = p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}
	case tParam:
		n, err := paramIndex(p.tok.text)
		if err != nil {
			return nil, err
		}
		cond.param = n
		cond.hasParam = true
		if err := p.advance(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("expected value for operator %q at position %d", op, p.tok.pos)
	}
	return cond, nil
}

func paramIndex(s string) (int, error) {
	var n int
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	if n < 1 {
		return 0, fmt.Errorf("param index must be >= 1 (got $%s)", s)
	}
	return n - 1, nil
}

// --- Compiler ---

type compiler struct {
	driver    string
	colPrefix string
	bindings  []Binding
}

func (c *compiler) emit(n node) (string, error) {
	switch t := n.(type) {
	case *orNode:
		parts := make([]string, 0, len(t.conds))
		for _, cc := range t.conds {
			s, err := c.emit(cc)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	case *andNode:
		parts := make([]string, 0, len(t.conds))
		for _, cc := range t.conds {
			s, err := c.emit(cc)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, " AND ") + ")", nil
	case *condNode:
		return c.emitCond(t)
	}
	return "", fmt.Errorf("unknown node %T", n)
}

func (c *compiler) emitCond(t *condNode) (string, error) {
	col := c.colPrefix + t.col
	if t.isNull {
		if t.negNull {
			return col + " IS NOT NULL", nil
		}
		return col + " IS NULL", nil
	}
	likeOp := ""
	if t.op == "contains" || t.op == "not_contains" {
		if c.driver == "postgres" {
			likeOp = " ILIKE "
			if t.op == "not_contains" {
				likeOp = " NOT ILIKE "
			}
		} else {
			likeOp = " LIKE "
			if t.op == "not_contains" {
				likeOp = " NOT LIKE "
			}
		}
	}
	if t.hasParam {
		contains := t.op == "contains" || t.op == "not_contains"
		c.bindings = append(c.bindings, Binding{Param: t.param, Contains: contains})
		if likeOp != "" {
			return col + likeOp + GHOST, nil
		}
		return col + " " + t.op + " " + GHOST, nil
	}
	// literal value baked in
	val := t.val
	if t.op == "contains" || t.op == "not_contains" {
		val = "%" + escapeStr(val) + "%"
		return col + likeOp + "'" + val + "'", nil
	}
	if isNumeric(t.val) {
		return col + " " + t.op + " " + t.val, nil
	}
	return col + " " + t.op + " '" + escapeStr(t.val) + "'", nil
}

func escapeStr(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
