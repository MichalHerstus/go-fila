package editor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-fila/go-fila/internal/types"
)

type ResourceMenuScreen struct {
	ed         *EditorModel
	idx        int
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *ResourceMenuScreen) isDone() bool { return m.done }
func (m *ResourceMenuScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewResourceMenuScreen(ed *EditorModel, idx int) *ResourceMenuScreen {
	return &ResourceMenuScreen{ed: ed, idx: idx}
}

func (m *ResourceMenuScreen) Init() tea.Cmd { return nil }

func (m *ResourceMenuScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < 6 {
				m.cursor++
			}
		case "enter":
			r := &m.ed.cfg.Resources[m.idx]
			switch m.cursor {
			case 0:
				m.pushScreen = NewFormScreen(buildResourceBasicForm(r))
			case 1:
				if r.List == nil {
					r.List = &types.ListConfig{}
				}
				m.pushScreen = NewColumnListScreen(m.ed, m.idx)
			case 2:
				if r.Detail == nil {
					r.Detail = &types.DetailConfig{}
				}
				m.pushScreen = NewFormScreen(buildDetailConfigForm(r))
			case 3:
				if r.Card == nil {
					r.Card = &types.CardConfig{Columns: 4, Rows: 4}
				}
				form, applies := buildCardConfigForm(r)
				m.pushScreen = NewFormScreen(form, applies...)
			case 4:
				if r.Form == nil {
					r.Form = &types.FormConfig{}
				}
				m.pushScreen = NewFormScreen(buildFormSectionForm(r))
			case 5:
				m.pushScreen = NewActionListScreen(m.ed, m.idx)
			case 6:
				if r.Policies == nil {
					r.Policies = &types.Policy{}
				}
				m.pushScreen = NewFormScreen(buildPoliciesForm(r))
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *ResourceMenuScreen) View() string {
	var s strings.Builder
	r := m.ed.cfg.Resources[m.idx]
	s.WriteString(fmt.Sprintf("\n  Resource: %s (%s)\n\n", r.Name, r.Label))

	colCount := 0
	if r.List != nil {
		colCount = len(r.List.Columns)
	}
	items := []string{
		"Basic Info",
		fmt.Sprintf("List View (%d columns)", colCount),
		"Detail View",
		"Card View",
		"Form (Create/Update)",
		fmt.Sprintf("Actions (%d)", len(r.Actions)),
		"Policies",
	}

	for i, item := range items {
		cursor := "  "
		if i == m.cursor {
			cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
		}
		s.WriteString(fmt.Sprintf("  %s%s\n", cursor, item))
	}
	s.WriteString("\n  esc:back\n")
	return s.String()
}

func buildResourceBasicForm(r *types.Resource) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Name", "User", &r.Name),
			inputField("Label", "Users", &r.Label),
			inputField("Icon", "users", &r.Icon),
			inputField("Group", "Management", &r.Group),
		).Title("Resource > Basic"),
	)
}

func buildDetailConfigForm(r *types.Resource) *huh.Form {
	detail := r.Detail
	return huh.NewForm(
		huh.NewGroup(
			inputField("Query", "GetUser", &detail.Query),
		).Title("Detail View"),
	)
}

func buildCardConfigForm(r *types.Resource) (*huh.Form, []func()) {
	card := r.Card
	var applies []func()
	cols, colsApply := intField("Columns per Row", &card.Columns)
	applies = append(applies, colsApply)
	rows, rowsApply := intField("Rows per Page", &card.Rows)
	applies = append(applies, rowsApply)
	return huh.NewForm(
		huh.NewGroup(
			cols,
			rows,
			inputField("Kanban Field", "", &card.KanbanField),
			inputField("Default Sort", "created_at", &card.DefaultSort),
		).Title("Card View"),
	), applies
}

func buildFormSectionForm(r *types.Resource) *huh.Form {
	form := r.Form
	if form.Create == nil {
		form.Create = &types.FormAction{}
	}
	if form.Update == nil {
		form.Update = &types.FormAction{}
	}
	return huh.NewForm(
		huh.NewGroup(
			inputField("Create Query", "CreateUser", &form.Create.Query),
			inputField("Update Query", "UpdateUser", &form.Update.Query),
			inputField("Populate Query", "GetUser", &form.Update.PopulateQuery),
		).Title("Form"),
	)
}

func buildPoliciesForm(r *types.Resource) *huh.Form {
	p := r.Policies
	return huh.NewForm(
		huh.NewGroup(
			inputField("View Any", "admin", &p.ViewAny),
			inputField("View", "admin", &p.View),
			inputField("Create", "admin", &p.Create),
			inputField("Update", "admin", &p.Update),
			inputField("Delete", "admin", &p.Delete),
		).Title("Policies"),
	)
}

type ActionListScreen struct {
	ed         *EditorModel
	resIdx     int
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *ActionListScreen) isDone() bool { return m.done }
func (m *ActionListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewActionListScreen(ed *EditorModel, resIdx int) *ActionListScreen {
	return &ActionListScreen{ed: ed, resIdx: resIdx}
}

func (m *ActionListScreen) Init() tea.Cmd { return nil }

func (m *ActionListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	actions := &m.ed.cfg.Resources[m.resIdx].Actions
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
			if m.cursor < len(*actions)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(*actions) {
				idx := m.cursor
				m.pushScreen = NewFormScreen(buildActionForm(&(*actions)[idx]))
				return m, nil
			}
		case "a":
			*actions = append(*actions, types.Action{
				Name:  "new_action",
				Label: "New Action",
				Color: "primary",
			})
			m.ed.SetModified()
			m.cursor = len(*actions) - 1
		case "d", "delete", "backspace":
			if len(*actions) > 0 {
				*actions = append((*actions)[:m.cursor], (*actions)[m.cursor+1:]...)
				if m.cursor >= len(*actions) {
					if len(*actions) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(*actions) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *ActionListScreen) View() string {
	var s strings.Builder
	actions := m.ed.cfg.Resources[m.resIdx].Actions
	s.WriteString("\n  Actions\n\n")

	if len(actions) == 0 {
		s.WriteString("  (no actions)\n")
	} else {
		for i, a := range actions {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s (%s)\n", cursor, a.Name, a.Label))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

func buildActionForm(a *types.Action) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Name", "archive", &a.Name),
			inputField("Label", "Archive", &a.Label),
			selectField("Icon", iconOptions, &a.Icon),
			selectField("Color", actionColorOptions, &a.Color),
		).Title("Action > Basic"),
		huh.NewGroup(
			confirmField("Requires Confirmation", &a.RequiresConfirmation),
			confirmField("Bulk", &a.Bulk),
			inputField("Query", "", &a.Query),
		).Title("Action > Settings"),
	)
}

type ColumnListScreen struct {
	ed         *EditorModel
	resIdx     int
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *ColumnListScreen) isDone() bool { return m.done }
func (m *ColumnListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewColumnListScreen(ed *EditorModel, resIdx int) *ColumnListScreen {
	return &ColumnListScreen{ed: ed, resIdx: resIdx}
}

func (m *ColumnListScreen) Init() tea.Cmd { return nil }

func (m *ColumnListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cols := &m.ed.cfg.Resources[m.resIdx].List.Columns
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
			if m.cursor < len(*cols)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(*cols) {
				idx := m.cursor
				m.pushScreen = NewFormScreen(buildColumnForm(&(*cols)[idx]))
				return m, nil
			}
		case "a":
			*cols = append(*cols, types.Column{
				Name: "new_column",
				Type: "string",
			})
			m.ed.SetModified()
			m.cursor = len(*cols) - 1
		case "d", "delete", "backspace":
			if len(*cols) > 0 {
				*cols = append((*cols)[:m.cursor], (*cols)[m.cursor+1:]...)
				if m.cursor >= len(*cols) {
					if len(*cols) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(*cols) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *ColumnListScreen) View() string {
	var s strings.Builder
	cols := m.ed.cfg.Resources[m.resIdx].List.Columns
	s.WriteString("\n  Columns\n\n")

	if len(cols) == 0 {
		s.WriteString("  (no columns)\n")
	} else {
		for i, c := range cols {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s (%s)\n", cursor, c.Name, c.Type))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

func buildColumnForm(c *types.Column) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Name", "name", &c.Name),
			inputField("Label", "", &c.Label),
			selectField("Type", fieldTypeOptions, &c.Type),
		).Title("Column > Basic"),
		huh.NewGroup(
			confirmField("Sortable", &c.Sortable),
			confirmField("Searchable", &c.Searchable),
		).Title("Column > Flags"),
	)
}

type FieldListScreen struct {
	ed         *EditorModel
	resIdx     int
	section    string
	cursor     int
	done       bool
	pushScreen tea.Model
}

func (m *FieldListScreen) isDone() bool { return m.done }
func (m *FieldListScreen) popScreen() tea.Model {
	ps := m.pushScreen
	m.pushScreen = nil
	return ps
}

func NewFieldListScreen(ed *EditorModel, resIdx int, section string) *FieldListScreen {
	return &FieldListScreen{ed: ed, resIdx: resIdx, section: section}
}

func (m *FieldListScreen) getFields() *[]types.Field {
	r := &m.ed.cfg.Resources[m.resIdx]
	switch m.section {
	case "detail":
		return &r.Detail.Fields
	case "create":
		if r.Form.Create == nil {
			r.Form.Create = &types.FormAction{}
		}
		return &r.Form.Create.Fields
	case "update":
		if r.Form.Update == nil {
			r.Form.Update = &types.FormAction{}
		}
		return &r.Form.Update.Fields
	case "card":
		return &r.Card.Fields
	}
	return nil
}

func (m *FieldListScreen) Init() tea.Cmd { return nil }

func (m *FieldListScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	fields := m.getFields()
	if fields == nil {
		m.done = true
		return m, tea.Quit
	}
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
			if m.cursor < len(*fields)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(*fields) {
				idx := m.cursor
				m.pushScreen = NewFormScreen(buildFieldForm(&(*fields)[idx]))
				return m, nil
			}
		case "a":
			*fields = append(*fields, types.Field{
				Name: "new_field",
				Type: "string",
			})
			m.ed.SetModified()
			m.cursor = len(*fields) - 1
		case "d", "delete", "backspace":
			if len(*fields) > 0 {
				*fields = append((*fields)[:m.cursor], (*fields)[m.cursor+1:]...)
				if m.cursor >= len(*fields) {
					if len(*fields) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(*fields) - 1
					}
				}
				m.ed.SetModified()
			}
		}
	}
	return m, nil
}

func (m *FieldListScreen) View() string {
	fields := m.getFields()
	var s strings.Builder
	s.WriteString(fmt.Sprintf("\n  %s Fields\n\n", strings.Title(m.section)))

	if fields == nil || len(*fields) == 0 {
		s.WriteString("  (no fields)\n")
	} else {
		for i, f := range *fields {
			cursor := "  "
			if i == m.cursor {
				cursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true).Render("> ")
			}
			s.WriteString(fmt.Sprintf("  %s%s (%s)\n", cursor, f.Name, f.Type))
		}
	}

	s.WriteString("\n  a:add  d:del  enter:edit  esc:back\n")
	return s.String()
}

func buildFieldForm(f *types.Field) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			inputField("Name", "name", &f.Name),
			inputField("Label", "", &f.Label),
			selectField("Type", fieldTypeOptions, &f.Type),
		).Title("Field > Basic"),
		huh.NewGroup(
			confirmField("Required", &f.Required),
			multiSelectField("Visible", visibleOptions, &f.Visible),
		).Title("Field > Settings"),
		huh.NewGroup(
			inputField("Options Query", "", &f.OptionsQuery),
			inputField("Options Value", "", &f.OptionsValue),
			inputField("Options Label", "", &f.OptionsLabel),
		).Title("Field > Options (SQLC)"),
	)
}
