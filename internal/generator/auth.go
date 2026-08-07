// auth.go
//
// Generates the auth package of the admin panel application
// (internal/panel/auth): the login/logout handlers with bcrypt verification,
// the gorilla/sessions store and session helpers, the session/RBAC middleware,
// and the login templ page. RBAC middleware code is only emitted when at least
// one resource declares policies.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// generateAuth writes the whole internal/panel/auth package:
// handler.go (LoginHandler, LogoutHandler, LoginPageData),
// session.go (session store + GetSession),
// middleware.go (SessionMiddleware, AuthMiddleware, context keys) and
// login.templ (the login form page). It derives the auth table, login field
// names and redirect URL from the config. Returns an error on write failure.
func (g *Generator) generateAuth() error {
	dir := filepath.Join(g.OutDir, "internal/panel/auth")
	panelName := g.Config.Panel.Name
	panelPath := g.Config.Panel.Path
	authTable := g.Config.Auth.Table
	if authTable == "" {
		authTable = "users"
	}
	loginFields := g.Config.Auth.Login.Fields
	emailField := "email"
	passwordField := "password"
	if len(loginFields) >= 2 {
		emailField = loginFields[0]
		passwordField = loginFields[1]
	}
	redirectURL := g.Config.Auth.Login.Redirect
	if redirectURL == "" {
		redirectURL = panelPath + "/dashboard"
	}

	// RBAC middleware generation
	var rbacMiddleware string
	var hasRBAC bool
	for _, r := range g.Config.Resources {
		if r.Policies != nil && r.Policies.ViewAny != "" {
			hasRBAC = true
			break
		}
	}
	if hasRBAC {
		rbacMiddleware = `
func checkRole(required string, userRole string) bool {
    if required == "" {
        return true
    }
    roles := strings.Split(required, "|")
    for _, r := range roles {
        if strings.TrimSpace(r) == userRole {
            return true
        }
    }
    return false
}

func RBACMiddleware(resource string, action string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            userRole, ok := r.Context().Value(UserRoleKey).(string)
            if !ok {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }
`
		for _, res := range g.Config.Resources {
			if res.Policies == nil {
				continue
			}
			resLower := strings.ToLower(res.Name)
			p := res.Policies
			if p.ViewAny != "" {
				rbacMiddleware += fmt.Sprintf(`
            if resource == %q && action == "view_any" && !checkRole(%q, userRole) {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }`, resLower, p.ViewAny)
			}
			if p.View != "" {
				rbacMiddleware += fmt.Sprintf(`
            if resource == %q && action == "view" && !checkRole(%q, userRole) {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }`, resLower, p.View)
			}
			if p.Create != "" {
				rbacMiddleware += fmt.Sprintf(`
            if resource == %q && action == "create" && !checkRole(%q, userRole) {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }`, resLower, p.Create)
			}
			if p.Update != "" {
				rbacMiddleware += fmt.Sprintf(`
            if resource == %q && action == "update" && !checkRole(%q, userRole) {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }`, resLower, p.Update)
			}
			if p.Delete != "" {
				rbacMiddleware += fmt.Sprintf(`
            if resource == %q && action == "delete" && !checkRole(%q, userRole) {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }`, resLower, p.Delete)
			}
		}
		rbacMiddleware += `
            next.ServeHTTP(w, r)
        })
    }
}
`
	}

	handlerCode := fmt.Sprintf(`package auth

import (
    "database/sql"
    "net/http"
    "golang.org/x/crypto/bcrypt"
)

func LoginHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodGet {
            vd := LoginPageData{
                PanelPath: %q,
                PanelName: %q,
            }
            LoginPage(vd).Render(r.Context(), w)
            return
        }

        if err := r.ParseForm(); err != nil {
            vd := LoginPageData{
                Error:     "Invalid form submission",
                PanelPath: %q,
                PanelName: %q,
            }
            LoginPage(vd).Render(r.Context(), w)
            return
        }

        email := r.FormValue(%q)
        password := r.FormValue(%q)

        if email == "" || password == "" {
            vd := LoginPageData{
                Error:     "Email and password are required",
                PanelPath: %q,
                PanelName: %q,
            }
            LoginPage(vd).Render(r.Context(), w)
            return
        }

        var userID int64
        var displayName string
        var hashedPassword string
        var userRole string
        err := db.QueryRowContext(r.Context(),
            "SELECT id, COALESCE(name, ''), password, COALESCE(role_name, '') FROM %s WHERE %s = $1",
            email,
        ).Scan(&userID, &displayName, &hashedPassword, &userRole)
        if displayName == "" {
            displayName = email
        }
        if err != nil {
            vd := LoginPageData{
                Email:     email,
                Error:     "Invalid email or password",
                PanelPath: %q,
                PanelName: %q,
            }
            LoginPage(vd).Render(r.Context(), w)
            return
        }

        if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)); err != nil {
            vd := LoginPageData{
                Email:     email,
                Error:     "Invalid email or password",
                PanelPath: %q,
                PanelName: %q,
            }
            LoginPage(vd).Render(r.Context(), w)
            return
        }

        session, err := GetSession(r)
        if err != nil {
            http.Error(w, "Session error", http.StatusInternalServerError)
            return
        }

        session.Values["user_id"] = userID
        session.Values["role"] = userRole
        session.Values["name"] = displayName
        if err := session.Save(r, w); err != nil {
            http.Error(w, "Session save error", http.StatusInternalServerError)
            return
        }

        http.Redirect(w, r, %q, http.StatusFound)
    }
}

func LogoutHandler(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        session, err := GetSession(r)
        if err == nil {
            session.Values["user_id"] = nil
            session.Values["role"] = nil
            session.Values["name"] = nil
            session.Options.MaxAge = -1
            session.Save(r, w)
        }
        http.Redirect(w, r, %q, http.StatusFound)
    }
}

type LoginPageData struct {
    Email     string
    Error     string
    PanelPath string
    PanelName string
}
`,
		panelPath, panelName,
		panelPath, panelName,
		emailField, passwordField,
		panelPath, panelName,
		authTable, emailField,
		panelPath, panelName,
		panelPath, panelName,
		redirectURL, panelPath)

	if err := os.WriteFile(filepath.Join(dir, "handler.go"), []byte(handlerCode), 0644); err != nil {
		return err
	}

	sessionCode := `package auth

import (
    "net/http"

    "github.com/gorilla/sessions"
)

var Store = sessions.NewCookieStore([]byte("go-fila-secret-key-change-in-production"))

func GetSession(r *http.Request) (*sessions.Session, error) {
    return Store.Get(r, "go-fila-session")
}
`

	if err := os.WriteFile(filepath.Join(dir, "session.go"), []byte(sessionCode), 0644); err != nil {
		return err
	}

	middlewareCode := fmt.Sprintf(`package auth

import (
    "context"
    "net/http"
    "strings"
)

type contextKey string

const UserKey contextKey = "user"
const UserRoleKey contextKey = "role"
const UserNameKey contextKey = "user_name"

func SessionMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/static/") {
            next.ServeHTTP(w, r)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "%s/login") {
            next.ServeHTTP(w, r)
            return
        }
        session, err := GetSession(r)
        if err != nil || session.Values["user_id"] == nil {
            http.Redirect(w, r, "%s/login", http.StatusFound)
            return
        }
        ctx := context.WithValue(r.Context(), UserKey, session.Values["user_id"])
        if role, ok := session.Values["role"].(string); ok {
            ctx = context.WithValue(ctx, UserRoleKey, role)
        }
        if name, ok := session.Values["name"].(string); ok {
            ctx = context.WithValue(ctx, UserNameKey, name)
        }
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func UserName(r *http.Request) string {
    if name, ok := r.Context().Value(UserNameKey).(string); ok {
        return name
    }
    return ""
}
%s`, panelPath, panelPath, rbacMiddleware)

	if err := os.WriteFile(filepath.Join(dir, "middleware.go"), []byte(middlewareCode), 0644); err != nil {
		return err
	}

	// Generate login Templ component
	return g.generateAuthLoginTempl(dir, panelName, panelPath)
}

// generateAuthLoginTempl writes the login.templ page: a full standalone HTML
// document (Tailwind styled) that posts the email/password form to
// panelPath/login and shows any login error message. The page honors the
// panel's dark-mode default and persists the user's toggle via localStorage
// like the Base layout does.
// Params: dir (the auth package directory), panelName (panel display name),
// panelPath (panel base path used in the form action).
// Returns: an error on write failure.
func (g *Generator) generateAuthLoginTempl(dir string, panelName, panelPath string) error {
	primary := g.Config.Panel.Brand.Colors.Primary
	if primary == "" {
		primary = "#6366f1"
	}
	secondary := g.Config.Panel.Brand.Colors.Secondary
	if secondary == "" {
		secondary = "#8b5cf6"
	}
	styleFonts := ""
	if g.Config.Panel.Theme.Font.Family != "" {
		styleFonts += fmt.Sprintf("\n    body { font-family: %s; }", g.Config.Panel.Theme.Font.Family)
	}
	if g.Config.Panel.Theme.Font.Mono != "" {
		styleFonts += fmt.Sprintf("\n    code, pre { font-family: %s; }", g.Config.Panel.Theme.Font.Mono)
	}
	htmlClass := ""
	if g.Config.Panel.Theme.DarkMode {
		htmlClass = ` class="dark"`
	}

	code := fmt.Sprintf(`package auth

templ LoginPage(data LoginPageData) {
    <!DOCTYPE html>
    <html lang="en"%s>
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>Login - %s</title>
        <link href="/static/css/styles.css" rel="stylesheet" />
        <style>
            :root {
                --brand-primary: %s;
                --brand-secondary: %s;
            }%s
        </style>
    </head>
    <body class="bg-gray-50 dark:bg-gray-900 min-h-screen flex items-center justify-center">
        <div class="w-full max-w-md">
            <div class="bg-white dark:bg-gray-800 shadow rounded-lg p-8">
                <div class="text-center mb-8">
                    <h1 class="text-2xl font-bold text-gray-900 dark:text-gray-100">%s</h1>
                    <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">Sign in to your account</p>
                </div>

                if data.Error != "" {
                <div class="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-700 text-red-700 dark:text-red-300 px-4 py-3 rounded mb-4 text-sm">
                    { data.Error }
                </div>
                }

                <form action="%s/login" method="POST" class="space-y-4">
                    <div>
                        <label for="email" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Email</label>
                        <input type="email" id="email" name="email" value={ data.Email } required
                            class="w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-brand-primary focus:ring-brand-primary sm:text-sm border px-3 py-2" />
                    </div>
                    <div>
                        <label for="password" class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Password</label>
                        <input type="password" id="password" name="password" required
                            class="w-full rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-brand-primary focus:ring-brand-primary sm:text-sm border px-3 py-2" />
                    </div>
                    <div>
                        <button type="submit"
                            class="w-full bg-brand-primary text-white py-2 px-4 rounded-md text-sm font-medium hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-brand-primary">
                            Sign in
                        </button>
                    </div>
                </form>
            </div>
        </div>
        <script>
            (function() {
                var html = document.documentElement;
                var saved = localStorage.getItem('gf-theme');
                if (saved === 'dark') { html.classList.add('dark'); }
                else if (saved === 'light') { html.classList.remove('dark'); }
            })();
        </script>
    </body>
    </html>
}
`, htmlClass, panelName, primary, secondary, styleFonts, panelName, panelPath)

	return os.WriteFile(filepath.Join(dir, "login.templ"), []byte(code), 0644)
}
