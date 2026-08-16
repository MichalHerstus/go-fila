// luascript.go
//
// Generates the shared internal/panel/luascript package: the request-time Lua
// runtime (gopher-lua) that executes script: hook bodies and script actions.
// The package is emitted only when at least one script: exists anywhere in the
// config (feature-off output stays byte-identical). Every script is wrapped by
// the runtime as the body of a single run(ctx) function and runs with a fixed
// 5 s context.WithTimeout via L.SetContext.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MichalHerstus/yaga/internal/types"
)

// hasAnyScript reports whether any script: body is declared anywhere in the
// config — on a hook (before/after) or on a custom action. Gates the emission
// of the luascript runtime package, the conditional gopher-lua go.mod
// dependency and the auth.RoleName helper in the generated middleware.
// Returns: true when at least one script: exists.
func (g *Generator) hasAnyScript() bool {
	for _, r := range g.Config.Resources {
		if r.Form != nil {
			for _, fa := range []*types.FormAction{r.Form.Create, r.Form.Update, r.Form.Delete} {
				if fa != nil && g.hooksUseScript(fa.Hooks) {
					return true
				}
			}
		}
		for _, a := range r.Actions {
			if a.Script != "" {
				return true
			}
			if g.hooksUseScript(a.Hooks) {
				return true
			}
		}
	}
	return false
}

// hooksUseScript reports whether a Hooks block declares any script: hook in
// its before or after list.
// Params: h (the hooks block; nil is valid).
// Returns: true when at least one hook has a non-empty Script body.
func (g *Generator) hooksUseScript(h *types.Hooks) bool {
	if h == nil {
		return false
	}
	for _, list := range [][]types.Hook{h.Before, h.After} {
		for _, hook := range list {
			if hook.Script != "" {
				return true
			}
		}
	}
	return false
}

// luaImport returns the import line for the luascript package, or "" when no
// script exists anywhere (feature-off output must not reference the package).
// Returns: a single import line (trailing newline) or "".
func (g *Generator) luaImport() string {
	if !g.hasAnyScript() {
		return ""
	}
	return fmt.Sprintf("    luascript %q\n", g.moduleImport("internal/panel/luascript"))
}

// generateLuascript writes internal/panel/luascript/luascript.go when at least
// one script: exists anywhere in the config. The keepQuestion constant in the
// emitted source mirrors the driver: true on sqlite (positional "?" binding
// stays as written), false on postgres/mssql (the runtime renumbers "?" to $N
// outside string literals and quoted identifiers). Nothing is written when the
// config declares no scripts.
// Returns: an error on write failure.
func (g *Generator) generateLuascript() error {
	if !g.hasAnyScript() {
		return nil
	}
	dir := filepath.Join(g.OutDir, "internal/panel/luascript")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	src := strings.ReplaceAll(luaPackageSrc, "__KEEP_QUESTION__", strconv.FormatBool(g.isSQLite()))
	return os.WriteFile(filepath.Join(dir, "luascript.go"), []byte(src), 0644)
}

// luaPackageSrc is the full source of the generated internal/panel/luascript
// package. The __KEEP_QUESTION__ token is replaced with the driver-dependent
// boolean before writing.
const luaPackageSrc = `package luascript

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    "strings"
    "time"

    lua "github.com/yuin/gopher-lua"
)

const scriptTimeout = 5 * time.Second
const keepQuestion = __KEEP_QUESTION__

type Scope struct {
    ID     int64
    Table  string
    Action string
    User   string
    Role   string
    Values map[string]interface{}
}

type Execer interface {
    ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type AbortError struct{ Msg string }

func (e *AbortError) Error() string { return e.Msg }

func IsAbort(err error) bool {
    for err != nil {
        if _, ok := err.(*AbortError); ok {
            return true
        }
        u, ok := err.(interface{ Unwrap() error })
        if !ok {
            return false
        }
        err = u.Unwrap()
    }
    return false
}

const abortPrefix = "\x00yaga-abort:"

func Run(ctx context.Context, db Execer, scope Scope, code string) error {
    lctx, cancel := context.WithTimeout(ctx, scriptTimeout)
    defer cancel()

    L := lua.NewState(lua.Options{SkipOpenLibs: true})
    defer L.Close()
    L.SetContext(lctx)
    openLib(L, lua.OpenBase, lua.BaseLibName)
    openLib(L, lua.OpenTable, lua.TabLibName)
    openLib(L, lua.OpenString, lua.StringLibName)
    openLib(L, lua.OpenMath, lua.MathLibName)

    L.SetGlobal("ctx", newCtxTable(L, scope))
    dbTbl := L.NewTable()
    dbTbl.RawSetString("exec", L.NewFunction(func(L *lua.LState) int {
        query, args := luaQueryArgs(L)
        res, err := db.ExecContext(lctx, renumber(query), args...)
        if err != nil {
            L.RaiseError("%s", err.Error())
        }
        out := L.NewTable()
        if n, err := res.RowsAffected(); err == nil {
            L.SetField(out, "rows_affected", lua.LNumber(n))
        }
        if id, err := res.LastInsertId(); err == nil {
            L.SetField(out, "last_insert_id", lua.LNumber(id))
        }
        L.Push(out)
        return 1
    }))
    dbTbl.RawSetString("query", L.NewFunction(func(L *lua.LState) int {
        query, args := luaQueryArgs(L)
        rows, err := db.QueryContext(lctx, renumber(query), args...)
        if err != nil {
            L.RaiseError("%s", err.Error())
        }
        defer rows.Close()
        cols, err := rows.Columns()
        if err != nil {
            L.RaiseError("%s", err.Error())
        }
        out := L.NewTable()
        i := 0
        for rows.Next() {
            vals := make([]interface{}, len(cols))
            ptrs := make([]interface{}, len(cols))
            for j := range vals {
                ptrs[j] = &vals[j]
            }
            if err := rows.Scan(ptrs...); err != nil {
                L.RaiseError("%s", err.Error())
            }
            row := L.NewTable()
            for j, c := range cols {
                L.SetField(row, c, goToLua(L, vals[j]))
            }
            i++
            L.SetTable(out, lua.LNumber(i), row)
        }
        if err := rows.Err(); err != nil {
            L.RaiseError("%s", err.Error())
        }
        L.Push(out)
        return 1
    }))
    dbTbl.RawSetString("query_one", L.NewFunction(func(L *lua.LState) int {
        query, args := luaQueryArgs(L)
        rows, err := db.QueryContext(lctx, renumber(query), args...)
        if err != nil {
            L.RaiseError("%s", err.Error())
        }
        defer rows.Close()
        cols, err := rows.Columns()
        if err != nil {
            L.RaiseError("%s", err.Error())
        }
        if !rows.Next() {
            if err := rows.Err(); err != nil {
                L.RaiseError("%s", err.Error())
            }
            L.Push(lua.LNil)
            return 1
        }
        vals := make([]interface{}, len(cols))
        ptrs := make([]interface{}, len(cols))
        for j := range vals {
            ptrs[j] = &vals[j]
        }
        if err := rows.Scan(ptrs...); err != nil {
            L.RaiseError("%s", err.Error())
        }
        row := L.NewTable()
        for j, c := range cols {
            L.SetField(row, c, goToLua(L, vals[j]))
        }
        L.Push(row)
        return 1
    }))
    L.SetGlobal("db", dbTbl)
    L.SetGlobal("abort", L.NewFunction(func(L *lua.LState) int {
        L.RaiseError("%s%s", abortPrefix, L.CheckString(1))
        return 0
    }))
    L.SetGlobal("log", L.NewFunction(func(L *lua.LState) int {
        log.Printf("[lua] %s", L.CheckString(1))
        return 0
    }))

    fn, err := L.LoadString("function run(ctx) " + code + "\nend")
    if err != nil {
        return err
    }
    if err := L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}); err != nil {
        return err
    }
    if err := L.CallByParam(lua.P{Fn: L.GetGlobal("run"), NRet: 0, Protect: true}, L.GetGlobal("ctx")); err != nil {
        if idx := strings.Index(err.Error(), abortPrefix); idx >= 0 {
            msg := err.Error()[idx+len(abortPrefix):]
            if nl := strings.IndexByte(msg, '\n'); nl >= 0 {
                msg = msg[:nl]
            }
            return &AbortError{Msg: msg}
        }
        return err
    }
    ct, ok := L.GetGlobal("ctx").(*lua.LTable)
    if !ok {
        return nil
    }
    if v, ok := luaToGo(L.GetField(ct, "values")).(map[string]interface{}); ok && scope.Values != nil {
        for k := range scope.Values {
            delete(scope.Values, k)
        }
        for k, val := range v {
            scope.Values[k] = val
        }
    }
    return nil
}

func openLib(L *lua.LState, fn lua.LGFunction, name string) {
    L.Push(L.NewFunction(fn))
    L.Push(lua.LString(name))
    L.Call(1, 0)
}

func newCtxTable(L *lua.LState, scope Scope) *lua.LTable {
    t := L.NewTable()
    L.SetField(t, "id", lua.LNumber(scope.ID))
    L.SetField(t, "table", lua.LString(scope.Table))
    L.SetField(t, "action", lua.LString(scope.Action))
    L.SetField(t, "user", lua.LString(scope.User))
    L.SetField(t, "role", lua.LString(scope.Role))
    values := L.NewTable()
    for k, v := range scope.Values {
        if s, ok := v.(string); ok && s == "" {
            continue
        }
        L.SetField(values, k, goToLua(L, v))
    }
    L.SetField(t, "values", values)
    return t
}

func luaQueryArgs(L *lua.LState) (string, []interface{}) {
    query := L.CheckString(1)
    var args []interface{}
    for i := 2; i <= L.GetTop(); i++ {
        args = append(args, luaToGo(L.Get(i)))
    }
    return query, args
}

func luaToGo(v lua.LValue) interface{} {
    switch n := v.(type) {
    case *lua.LNilType:
        return nil
    case lua.LBool:
        return bool(n)
    case lua.LString:
        return string(n)
    case lua.LNumber:
        f := float64(n)
        if f == float64(int64(f)) {
            return int64(f)
        }
        return f
    case *lua.LTable:
        m := map[string]interface{}{}
        n.ForEach(func(k, val lua.LValue) {
            m[lua.LVAsString(k)] = luaToGo(val)
        })
        return m
    default:
        return v.String()
    }
}

func goToLua(L *lua.LState, v interface{}) lua.LValue {
    switch x := v.(type) {
    case nil:
        return lua.LNil
    case bool:
        return lua.LBool(x)
    case string:
        return lua.LString(x)
    case int:
        return lua.LNumber(x)
    case int64:
        return lua.LNumber(x)
    case int32:
        return lua.LNumber(x)
    case float64:
        return lua.LNumber(x)
    case []byte:
        return lua.LString(string(x))
    case time.Time:
        return lua.LString(x.Format("2006-01-02T15:04:05"))
    case map[string]interface{}:
        t := L.NewTable()
        for k, val := range x {
            L.SetField(t, k, goToLua(L, val))
        }
        return t
    default:
        return lua.LString(fmt.Sprintf("%v", v))
    }
}

func renumber(sqlText string) string {
    if keepQuestion {
        return sqlText
    }
    var out strings.Builder
    n := 0
    i := 0
    ln := len(sqlText)
    for i < ln {
        c := sqlText[i]
        switch {
        case c == '\'':
            out.WriteByte(c)
            i++
            for i < ln {
                if sqlText[i] == '\'' {
                    if i+1 < ln && sqlText[i+1] == '\'' {
                        out.WriteString("''")
                        i += 2
                        continue
                    }
                    out.WriteByte('\'')
                    i++
                    break
                }
                out.WriteByte(sqlText[i])
                i++
            }
        case c == '"' || c == '[':
            close := byte(']')
            if c == '"' {
                close = '"'
            }
            out.WriteByte(c)
            i++
            for i < ln && sqlText[i] != close {
                out.WriteByte(sqlText[i])
                i++
            }
            if i < ln {
                out.WriteByte(sqlText[i])
                i++
            }
        case c == '-':
            if i+1 < ln && sqlText[i+1] == '-' {
                for i < ln && sqlText[i] != '\n' {
                    out.WriteByte(sqlText[i])
                    i++
                }
            } else {
                out.WriteByte(c)
                i++
            }
        case c == '/' && i+1 < ln && sqlText[i+1] == '*':
            for i+1 < ln && !(sqlText[i] == '*' && sqlText[i+1] == '/') {
                out.WriteByte(sqlText[i])
                i++
            }
            if i+1 < ln {
                out.WriteString("*/")
                i += 2
            }
        case c == '?':
            n++
            out.WriteString(fmt.Sprintf("$%d", n))
            i++
        default:
            out.WriteByte(c)
            i++
        }
    }
    return out.String()
}
`
