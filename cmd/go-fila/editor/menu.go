package editor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-fila/go-fila/internal/types"
)

var (
	menuTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).MarginBottom(1)
	menuItemStyle  = lipgloss.NewStyle().PaddingLeft(2)
	menuSelStyle   = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("212")).Bold(true)
	menuHintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
)

type menuItem struct {
	label  string
	count  string
	action func()
}

type MainMenuModel struct {
	ed         *EditorModel
	items      []menuItem
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *MainMenuModel) isDone() bool { return m.done }
func (m *MainMenuModel) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewMainMenu(ed *EditorModel) *MainMenuModel {
	m := &MainMenuModel{ed: ed}
	cfg := ed.cfg

	m.items = []menuItem{
		{label: "Panel", count: "3 fields", action: func() {
			form, applies := buildPanelForm(m.ed.cfg)
			m.pushScreen = NewFormScreen(form, applies...)
		}},
		{label: "Connections", count: fmt.Sprintf("%d conn", len(cfg.Connections)), action: func() {
			form, applies := buildConnectionForm(m.ed.cfg)
			m.pushScreen = NewFormScreen(form, applies...)
		}},
		{label: "SQLC", count: "4 fields", action: func() {
			m.pushScreen = NewFormScreen(buildSQLCForm(m.ed.cfg))
		}},
		{label: "Auth", count: "8 fields", action: func() {
			m.pushScreen = NewFormScreen(buildAuthForm(m.ed.cfg))
		}},
		{label: "Navigation", count: fmt.Sprintf("%d groups", len(cfg.Navigation)), action: func() {
			m.pushScreen = NewNavigationListScreen(m.ed)
		}},
		{label: "Resources", count: fmt.Sprintf("%d items", len(cfg.Resources)), action: func() {
			m.pushScreen = NewResourceListScreen(m.ed)
		}},
		{label: "Pages", count: fmt.Sprintf("%d items", len(cfg.Pages)), action: func() {
			m.pushScreen = NewPageListScreen(m.ed)
		}},
	}
	return m
}

func (m *MainMenuModel) Init() tea.Cmd { return nil }

func (m *MainMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			m.ed.Quit()
			return m, nil
		case "s":
			m.ed.Save()
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.items) {
				m.items[m.cursor].action()
			}
		}
	case tea.WindowSizeMsg:
		return m, nil
	}
	return m, nil
}

func (m *MainMenuModel) View() string {
	var s strings.Builder
	s.WriteString("\n")
	s.WriteString(menuTitleStyle.Render("go-fila config editor"))
	s.WriteString("\n\n")

	for i, item := range m.items {
		cursor := "  "
		label := menuItemStyle.Render(item.label)
		if i == m.cursor {
			cursor = menuSelStyle.Render("> ")
			label = menuSelStyle.Render(item.label)
		}
		count := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(item.count)
		s.WriteString(fmt.Sprintf("  %s%-20s %s\n", cursor, label, count))
	}

	hint := "  s:save  q:quit  ↑↓:navigate  Enter:edit"
	s.WriteString("\n" + menuHintStyle.Render(hint))
	s.WriteString("\n")
	return s.String()
}

type ResourceListScreen struct {
	ed         *EditorModel
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *ResourceListScreen) isDone() bool { return m.done }
func (m *ResourceListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewResourceListScreen(ed *EditorModel) *ResourceListScreen {
	return &ResourceListScreen{ed: ed}
}

func (m *ResourceListScreen) Init() tea.Cmd { return nil }

func (m *ResourceListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.done = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.ed.cfg.Resources)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.ed.cfg.Resources) {
				idx := m.cursor
				m.pushScreen = NewResourceMenuScreen(m.ed, idx)
				return m, nil
			}
		case "a":
			m.ed.cfg.Resources = append(m.ed.cfg.Resources, types.Resource{
				Name:  "NewResource",
				Label: "New Resource",
			})
			m.ed.SetModified()
			m.cursor = len(m.ed.cfg.Resources) - 1
		case "d", "delete", "backspace":
			if len(m.ed.cfg.Resources) > 0 {
				m.ed.cfg.Resources = append(m.ed.cfg.Resources[:m.cursor], m.ed.cfg.Resources[m.cursor+1:]...)
				if m.cursor >= len(m.ed.cfg.Resources) {
					if len(m.ed.cfg.Resources) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(m.ed.cfg.Resources) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *ResourceListScreen) View() string {
	var s strings.Builder
	s.WriteString("\n  Resources\n\n")

	if len(m.ed.cfg.Resources) == 0 {
		s.WriteString("  (no resources)\n")
	} else {
		for i, r := range m.ed.cfg.Resources {
			cursor := "  "
			if i == m.cursor {
				cursor = menuSelStyle.Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s (%s)\n", cursor, r.Name, r.Label))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

type PageListScreen struct {
	ed         *EditorModel
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *PageListScreen) isDone() bool { return m.done }
func (m *PageListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewPageListScreen(ed *EditorModel) *PageListScreen {
	return &PageListScreen{ed: ed}
}

func (m *PageListScreen) Init() tea.Cmd { return nil }

func (m *PageListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.done = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.ed.cfg.Pages)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.ed.cfg.Pages) {
				idx := m.cursor
				m.pushScreen = NewFormScreen(buildPageForm(&m.ed.cfg.Pages[idx]))
				return m, nil
			}
		case "a":
			m.ed.cfg.Pages = append(m.ed.cfg.Pages, types.Page{
				Name: "NewPage",
				Path: "/new-page",
			})
			m.ed.SetModified()
			m.cursor = len(m.ed.cfg.Pages) - 1
		case "d", "delete", "backspace":
			if len(m.ed.cfg.Pages) > 0 {
				m.ed.cfg.Pages = append(m.ed.cfg.Pages[:m.cursor], m.ed.cfg.Pages[m.cursor+1:]...)
				if m.cursor >= len(m.ed.cfg.Pages) {
					if len(m.ed.cfg.Pages) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(m.ed.cfg.Pages) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *PageListScreen) View() string {
	var s strings.Builder
	s.WriteString("\n  Pages\n\n")

	if len(m.ed.cfg.Pages) == 0 {
		s.WriteString("  (no pages)\n")
	} else {
		for i, p := range m.ed.cfg.Pages {
			cursor := "  "
			if i == m.cursor {
				cursor = menuSelStyle.Render("> ")
			}
			def := ""
			if p.Default {
				def = " [default]"
			}
			s.WriteString(fmt.Sprintf("  %s%s (%s)%s\n", cursor, p.Name, p.Path, def))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}
