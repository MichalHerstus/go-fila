package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/go-fila/go-fila/internal/parser"
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
	"gopkg.in/yaml.v3"
)

// Editor is the root of the tview-based config editor. It owns the terminal
// application, the persistent 3-pane layout (title bar / left nav + content /
// status bar) and the config being edited.
type Editor struct {
	app        *tview.Application
	cfg        *types.Config
	configPath string

	pages    *tview.Pages
	nav      *tview.List
	titleBar *tview.TextView
	status   *tview.TextView
	root     tview.Primitive

	history   []string // page-name stack for Esc-back
	modified  bool
	modalOpen bool
	saved     bool

	screen tcell.Screen // optional; overrides the real terminal (tests)
}

// New creates an editor bound to the given config and file path.
func New(cfg *types.Config, configPath string) *Editor {
	return &Editor{cfg: cfg, configPath: configPath}
}

// SetScreen lets tests inject a simulation screen instead of the real terminal.
func (e *Editor) SetScreen(s tcell.Screen) *Editor {
	e.screen = s
	return e
}

// Run builds the UI and runs the application event loop. It returns whether a
// save was requested before quitting.
func (e *Editor) Run() (bool, error) {
	e.app = tview.NewApplication()
	if e.screen != nil {
		e.app.SetScreen(e.screen)
	}
	e.buildShell()
	e.app.SetRoot(e.root, true)
	e.app.SetInputCapture(e.capture)
	if err := e.app.Run(); err != nil {
		return e.saved, err
	}
	return e.saved, nil
}

// buildShell assembles the persistent layout and registers the left navigation.
func (e *Editor) buildShell() {
	e.titleBar = tview.NewTextView().SetDynamicColors(true)
	e.status = tview.NewTextView().SetDynamicColors(true)
	e.pages = tview.NewPages()

	e.nav = tview.NewList().ShowSecondaryText(false)
	e.nav.SetBorder(true).SetBorderColor(colBorder).SetTitle("go-fila")
	e.nav.SetSelectedFunc(func(_ int, _ string, _ string, _ rune) {})
	e.nav.SetMainTextColor(colText)
	e.buildNav()

	middle := tview.NewFlex().SetDirection(tview.FlexColumn)
	middle.AddItem(e.nav, 26, 0, true)
	middle.AddItem(e.pages, 0, 1, true)

	root := tview.NewFlex().SetDirection(tview.FlexRow)
	root.AddItem(e.titleBar, 1, 0, false)
	root.AddItem(middle, 0, 1, true)
	root.AddItem(e.status, 1, 0, false)
	e.root = root

	e.history = nil
	e.home()
	e.renderStatus()
	e.refreshTitle()
}

// navItem adds an entry to the left navigation.
func (e *Editor) navItem(label string, page string, build func() tview.Primitive) {
	e.nav.AddItem(label, "", 0, func() {
		if build == nil {
			return
		}
		e.showPage(page, build())
	})
}

// buildNav populates the left navigation menu.
func (e *Editor) buildNav() {
	e.navItem("Panel", "panel", e.panelPage)
	e.navItem("Connections", "connections", e.connectionsPage)
	e.navItem("SQLC", "sqlc", e.sqlcPage)
	e.navItem("Auth", "auth", e.authPage)
	e.navItem("Navigation", "navigation", e.navGroupsPage)
	e.navItem("Resources", "resources", e.resourcesPage)
	e.navItem("Pages", "pages", e.pagesPage)
	e.nav.AddItem("", "", 0, nil)
	e.navItem("Sync SQL & YAML", "sync", e.syncPage)
	e.navItem("Preview", "preview", e.previewPage)
	e.nav.AddItem("", "", 0, nil)
	e.navItem("Save (Ctrl+S)", "save", nil)
	e.nav.AddItem("Quit (Ctrl+Q)", "", 0, e.quitConfirm)
}

// capture handles global keys before any widget sees them.
func (e *Editor) capture(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyCtrlS:
		e.save()
		return nil
	case tcell.KeyF10, tcell.KeyCtrlQ:
		e.quitConfirm()
		return nil
	case tcell.KeyEsc:
		if e.modalOpen {
			e.closeModal()
			return nil
		}
		e.back()
		return nil
	}
	return event
}

// home pushes the overview page.
func (e *Editor) home() {
	e.showPage("home", e.homePage())
}

// homePage renders a summary of the loaded configuration.
func (e *Editor) homePage() tview.Primitive {
	conns := make([]string, 0, len(e.cfg.Connections))
	for name, c := range e.cfg.Connections {
		conns = append(conns, fmt.Sprintf("  %s  [::d](%s)[::-]", name, c.Driver))
	}
	if len(conns) == 0 {
		conns = append(conns, "  [::d](none)[::-]")
	}
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(true).SetBorderColor(colBorder).SetTitle("Overview")
	fmt.Fprintf(tv, "Config: [::b]%s[::-]\n\n", e.configPath)
	fmt.Fprintf(tv, "Panel: [::b]%s[::-] at %s\n", e.cfg.Panel.Name, e.cfg.Panel.Path)
	fmt.Fprintf(tv, "Auth table: [::b]%s[::-]\n\n", e.cfg.Auth.Table)
	fmt.Fprintf(tv, "[::b]Resources[::-] (%d):\n", len(e.cfg.Resources))
	for _, r := range e.cfg.Resources {
		colCount := 0
		if r.List != nil {
			colCount = len(r.List.Columns)
		}
		fmt.Fprintf(tv, "  %s  [::d]%d list columns, %d actions[::-]\n", r.Name, colCount, len(r.Actions))
	}
	fmt.Fprintf(tv, "\n[::b]Pages[::-] (%d):\n", len(e.cfg.Pages))
	for _, p := range e.cfg.Pages {
		mark := ""
		if p.Default {
			mark = " [::d](default)[::-]"
		}
		fmt.Fprintf(tv, "  %s %s%s\n", p.Name, p.Path, mark)
	}
	fmt.Fprintf(tv, "\n[::b]Connections[::-]:\n%s\n", strings.Join(conns, "\n"))
	fmt.Fprintf(tv, "\n[::d]Use the left menu to edit. Ctrl+S saves, Ctrl+Q quits.[::-]")
	return tv
}

// refreshTitle updates the title bar with the file path, modified badge and
// the current page breadcrumb.
func (e *Editor) refreshTitle() {
	if e.titleBar == nil {
		return
	}
	badge := ""
	if e.modified {
		badge = " [red](modified)[::-]"
	}
	crumb := "home"
	if len(e.history) > 0 {
		crumb = e.history[len(e.history)-1]
	}
	base := filepath.Base(e.configPath)
	e.titleBar.SetText(fmt.Sprintf(" go-fila editor — [::b]%s[::-]%s  [::d]%s[::-]", base, badge, crumb))
}

// renderStatus restores the default key-hint line in the status bar.
func (e *Editor) renderStatus() {
	if e.status == nil {
		return
	}
	e.status.SetText(" [::d]↑↓/j/k navigate   Enter edit   a add   d delete   Esc back   Ctrl+S save   Ctrl+Q quit[::-]")
}

// save marshals the config and writes it back to configPath.
func (e *Editor) save() {
	if err := e.validateCopy(); err != nil {
		e.errorModal("Validation", err.Error())
		return
	}
	data, err := yaml.Marshal(e.cfg)
	if err != nil {
		e.errorModal("Save failed", err.Error())
		return
	}
	if err := os.WriteFile(e.configPath, data, 0644); err != nil {
		e.errorModal("Save failed", err.Error())
		return
	}
	e.modified = false
	e.saved = true
	e.refreshTitle()
	e.toast("Saved " + filepath.Base(e.configPath))
}

// validateCopy runs the parser validator against a copy so defaults are not
// silently written back into the live config.
func (e *Editor) validateCopy() error {
	data, err := yaml.Marshal(e.cfg)
	if err != nil {
		return err
	}
	var copyCfg types.Config
	if err := yaml.Unmarshal(data, &copyCfg); err != nil {
		return err
	}
	return parser.Validate(&copyCfg)
}

// quitConfirm prompts for save/discard/cancel when there are changes.
func (e *Editor) quitConfirm() {
	if !e.modified {
		e.quit()
		return
	}
	modal := tview.NewModal().
		SetText("Unsaved changes\n\nYou have unsaved changes. Save before quitting?").
		AddButtons([]string{"Save & Quit", "Discard", "Cancel"}).
		SetDoneFunc(func(index int, _ string) {
			e.closeModal()
			switch index {
			case 0:
				e.save()
				if !e.modified {
					e.quit()
				}
			case 1:
				e.quit()
			}
		})
	e.showModal(modal)
}

func (e *Editor) quit() {
	e.app.Stop()
}
