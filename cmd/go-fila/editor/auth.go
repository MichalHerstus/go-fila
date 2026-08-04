package editor

import (
	"github.com/charmbracelet/huh"
	"github.com/go-fila/go-fila/internal/types"
)

func buildAuthForm(cfg *types.Config) *huh.Form {
	auth := &cfg.Auth
	login := &auth.Login

	return huh.NewForm(
		huh.NewGroup(
			selectField("Guard", authGuardOptions, &auth.Guard),
			selectField("Provider", authProviderOptions, &auth.Provider),
			inputField("Auth Table", "users", &auth.Table),
		).Title("Auth > General"),
		huh.NewGroup(
			multiSelectField("Login Fields", loginFieldOptions, &login.Fields),
			inputField("Login Redirect", "/admin/dashboard", &login.Redirect),
		).Title("Auth > Login"),
		huh.NewGroup(
			confirmField("Registration", &auth.Registration),
			confirmField("Password Reset", &auth.PasswordReset),
			confirmField("Remember Me", &auth.RememberMe),
		).Title("Auth > Features"),
	)
}
