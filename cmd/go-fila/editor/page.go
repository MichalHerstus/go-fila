package editor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-fila/go-fila/internal/types"
)

type WidgetListScreen struct {
	ed         *EditorModel
	pageIdx    int
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *WidgetListScreen) isDone() bool { return m.done }
func (m *WidgetListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewWidgetListScreen(ed *EditorModel, pageIdx int) *WidgetListScreen {
	return &WidgetListScreen{ed: ed, pageIdx: pageIdx}
}

func (m *WidgetListScreen) Init() tea.Cmd { return nil }

func (m *WidgetListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	widgets := &m.ed.cfg.Pages[m.pageIdx].Widgets
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
			if m.cursor < len(*widgets)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(*widgets) {
				idx := m.cursor
				form, applies := buildWidgetForm(&(*widgets)[idx])
				m.pushScreen = NewFormScreen(form, applies...)
				return m, nil
			}
		case "a":
			*widgets = append(*widgets, types.Widget{
				Type:  "stat",
				Label: "New Widget",
			})
			m.ed.SetModified()
			m.cursor = len(*widgets) - 1
		case "d", "delete", "backspace":
			if len(*widgets) > 0 {
				*widgets = append((*widgets)[:m.cursor], (*widgets)[m.cursor+1:]...)
				if m.cursor >= len(*widgets) {
					if len(*widgets) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(*widgets) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *WidgetListScreen) View() string {
	widgets := m.ed.cfg.Pages[m.pageIdx].Widgets
	var s strings.Builder
	s.WriteString("\n  Widgets\n\n")

	if len(widgets) == 0 {
		s.WriteString("  (no widgets)\n")
	} else {
		for i, w := range widgets {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s [%s]\n", cursor, w.Label, w.Type))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

func buildPageForm(p *types.Page) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Name", "Dashboard", &p.Name),
			inputField("Path", "/dashboard", &p.Path),
			confirmField("Default Page", &p.Default),
		).Title("Page"),
	)
}

func buildWidgetForm(w *types.Widget) (*huh.Form, []func()) {
	groups := []*huh.Group{
		huh.NewGroup(
			selectField("Type", widgetTypeOptions, &w.Type),
			inputField("Label", "Widget", &w.Label),
			inputField("Query", "", &w.Query),
			selectField("Icon", iconOptions, &w.Icon),
			inputField("Color", "primary", &w.Color),
		).Title("Widget > Basic"),
	}
	var applies []func()

	if w.Type == "chart" || (w.Chart != nil) {
		if w.Chart == nil {
			w.Chart = &types.ChartConfig{}
		}
		groups = append(groups,
			huh.NewGroup(
				selectField("Chart Type", chartTypeOptions, &w.Chart.Type),
				inputField("Chart Query", "", &w.Chart.Query),
				inputField("X Axis", "", &w.Chart.X),
				inputField("Y Axis", "", &w.Chart.Y),
			).Title("Widget > Chart"),
		)
	}

	if w.Type == "stats_grid" {
		cols, colsApply := intField("Columns", &w.Columns)
		applies = append(applies, colsApply)
		groups = append(groups,
			huh.NewGroup(
				cols,
			).Title("Widget > Grid"),
		)
	}

	return huh.NewForm(groups...), applies
}
