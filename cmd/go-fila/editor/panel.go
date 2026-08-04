package editor

import (
	"github.com/charmbracelet/huh"
	"github.com/go-fila/go-fila/internal/types"
)

func buildPanelForm(cfg *types.Config) (*huh.Form, []func()) {
	var applies []func()

	sw, swApply := intField("Sidebar Width", &cfg.Panel.Layout.Sidebar.Width)
	applies = append(applies, swApply)
	cw, cwApply := intField("Collapsed Width", &cfg.Panel.Layout.Sidebar.CollapsedWidth)
	applies = append(applies, cwApply)

	return huh.NewForm(
		huh.NewGroup(
			inputField("Panel ID", "admin", &cfg.Panel.ID),
			inputField("Panel Path", "/admin", &cfg.Panel.Path),
			inputField("Panel Name", "My Admin", &cfg.Panel.Name),
		).Title("Panel > Basic"),
		huh.NewGroup(
			inputField("Brand Logo", "logo.png", &cfg.Panel.Brand.Logo),
			inputField("Brand Favicon", "favicon.ico", &cfg.Panel.Brand.Favicon),
			inputField("Primary Color", "#6366f1", &cfg.Panel.Brand.Colors.Primary),
			inputField("Secondary Color", "#64748b", &cfg.Panel.Brand.Colors.Secondary),
		).Title("Panel > Brand"),
		huh.NewGroup(
			confirmField("Sidebar Collapsible", &cfg.Panel.Layout.Sidebar.Collapsible),
			sw,
			cw,
			confirmField("Topbar Sticky", &cfg.Panel.Layout.Topbar.Sticky),
			inputField("Max Content Width", "7xl", &cfg.Panel.Layout.MaxContentWidth),
		).Title("Panel > Layout"),
		huh.NewGroup(
			confirmField("Dark Mode", &cfg.Panel.Theme.DarkMode),
			inputField("Font Family", "Inter", &cfg.Panel.Theme.Font.Family),
			inputField("Font Mono", "JetBrains Mono", &cfg.Panel.Theme.Font.Mono),
		).Title("Panel > Theme"),
	), applies
}
