package editor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ListScreen struct {
	title      string
	items      []string
	cursor     int
	done       bool
	pushScreen tea.Model
	addFunc    func() tea.Model
	editFunc   func(idx int) tea.Model
	deleteFunc func(idx int)
}

func (m *ListScreen) isDone() bool { return m.done }
func (m *ListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewListScreen(title string, items []string, addFunc func() tea.Model, editFunc func(idx int) tea.Model, deleteFunc func(idx int)) *ListScreen {
	return &ListScreen{
		title:      title,
		items:      items,
		addFunc:    addFunc,
		editFunc:   editFunc,
		deleteFunc: deleteFunc,
	}
}

func (m *ListScreen) Init() tea.Cmd { return nil }

func (m *ListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.items) && m.editFunc != nil {
				m.pushScreen = m.editFunc(m.cursor)
				return m, nil
			}
		case "a":
			if m.addFunc != nil {
				m.pushScreen = m.addFunc()
				return m, nil
			}
		case "d", "delete", "backspace":
			if len(m.items) > 0 && m.deleteFunc != nil {
				m.deleteFunc(m.cursor)
				if m.cursor >= len(m.items) {
					if len(m.items) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(m.items) - 1
					}
				}
			}
		}
	}
	return m, nil
}

func (m *ListScreen) View() string {
	var s strings.Builder
	s.WriteString("\n  " + m.title + "\n\n")

	if len(m.items) == 0 {
		s.WriteString("  (no items)\n")
	} else {
		for i, item := range m.items {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s\n", cursor, item))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}
