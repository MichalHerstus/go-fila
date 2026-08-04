package editor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-fila/go-fila/internal/types"
)

type NavigationListScreen struct {
	ed         *EditorModel
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *NavigationListScreen) isDone() bool { return m.done }
func (m *NavigationListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewNavigationListScreen(ed *EditorModel) *NavigationListScreen {
	return &NavigationListScreen{ed: ed}
}

func (m *NavigationListScreen) Init() tea.Cmd { return nil }

func (m *NavigationListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.ed.cfg.Navigation)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.ed.cfg.Navigation) {
				idx := m.cursor
				m.pushScreen = NewGroupItemsScreen(m.ed, idx)
				return m, nil
			}
		case "a":
			m.ed.cfg.Navigation = append(m.ed.cfg.Navigation, types.NavigationGroup{
				Group: "New Group",
				Sort:  len(m.ed.cfg.Navigation) + 1,
			})
			m.ed.SetModified()
			m.cursor = len(m.ed.cfg.Navigation) - 1
		case "d", "delete", "backspace":
			if len(m.ed.cfg.Navigation) > 0 {
				m.ed.cfg.Navigation = append(m.ed.cfg.Navigation[:m.cursor], m.ed.cfg.Navigation[m.cursor+1:]...)
				if m.cursor >= len(m.ed.cfg.Navigation) {
					if len(m.ed.cfg.Navigation) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(m.ed.cfg.Navigation) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *NavigationListScreen) View() string {
	var s strings.Builder
	s.WriteString("\n  Navigation Groups\n\n")

	if len(m.ed.cfg.Navigation) == 0 {
		s.WriteString("  (no groups)\n")
	} else {
		for i, g := range m.ed.cfg.Navigation {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s (%d items)\n", cursor, g.Group, len(g.Items)))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

type GroupItemsScreen struct {
	ed         *EditorModel
	groupIdx   int
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *GroupItemsScreen) isDone() bool { return m.done }
func (m *GroupItemsScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewGroupItemsScreen(ed *EditorModel, groupIdx int) *GroupItemsScreen {
	return &GroupItemsScreen{ed: ed, groupIdx: groupIdx}
}

func (m *GroupItemsScreen) Init() tea.Cmd { return nil }

func (m *GroupItemsScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	items := &m.ed.cfg.Navigation[m.groupIdx].Items
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
			if m.cursor < len(*items)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(*items) {
				idx := m.cursor
				item := &(*items)[idx]
				m.pushScreen = NewFormScreen(buildNavItemForm(item))
				return m, nil
			}
		case "a":
			*items = append(*items, types.NavigationItem{
				Resource: "",
				Page:     "",
				Type:     "resource",
			})
			m.ed.SetModified()
			m.cursor = len(*items) - 1
		case "d", "delete", "backspace":
			if len(*items) > 0 {
				*items = append((*items)[:m.cursor], (*items)[m.cursor+1:]...)
				if m.cursor >= len(*items) {
					if len(*items) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(*items) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *GroupItemsScreen) View() string {
	group := m.ed.cfg.Navigation[m.groupIdx]
	var s strings.Builder
	s.WriteString(fmt.Sprintf("\n  Group: %s\n\n", group.Group))

	if len(group.Items) == 0 {
		s.WriteString("  (no items)\n")
	} else {
		for i, item := range group.Items {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			label := itemLabel(item)
			s.WriteString(fmt.Sprintf("  %s%s\n", cursor, label))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

func itemLabel(item types.NavigationItem) string {
	switch item.Type {
	case "resource":
		if item.Resource != "" {
			return item.Resource
		}
		return "(resource)"
	case "page":
		if item.Page != "" {
			return item.Page
		}
		return "(page)"
	case "link":
		if item.URL != "" {
			return item.URL
		}
		return "(link)"
	default:
		return item.Resource + item.Page + item.URL
	}
}

func buildNavItemForm(item *types.NavigationItem) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			selectField("Type", navItemTypeOptions, &item.Type),
			inputField("Resource", "User", &item.Resource),
			inputField("Page", "Dashboard", &item.Page),
			inputField("Label", "", &item.Label),
			inputField("URL", "", &item.URL),
			confirmField("Opens in New Tab", &item.OpensInNewTab),
		).Title("Navigation Item"),
	)
}

func buildGroupForm(g *types.NavigationGroup) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Group Name", "Management", &g.Group),
			inputField("Icon", "users", &g.Icon),
		).Title("Navigation Group"),
	)
}
