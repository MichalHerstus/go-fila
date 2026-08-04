package editor

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

func inputField(label, placeholder string, value *string) *huh.Input {
	return huh.NewInput().Title(label).Placeholder(placeholder).Value(value)
}

func intField(label string, value *int) (*huh.Input, func()) {
	s := strconv.Itoa(*value)
	return huh.NewInput().Title(label).Value(&s), func() {
		if v, err := strconv.Atoi(s); err == nil {
			*value = v
		}
	}
}

func confirmField(label string, value *bool) *huh.Confirm {
	return huh.NewConfirm().Title(label).Value(value)
}

func selectField(label string, options []huh.Option[string], value *string) *huh.Select[string] {
	return huh.NewSelect[string]().Title(label).Options(options...).Value(value)
}

func multiSelectField(label string, options []huh.Option[string], value *[]string) *huh.MultiSelect[string] {
	return huh.NewMultiSelect[string]().Title(label).Options(options...).Value(value)
}

type mapEntry struct {
	Key   string
	Value string
}

type MapEditor struct {
	title    string
	items    []mapEntry
	cursor   int
	quitting bool
	done     bool
	width    int
}

func NewMapEditor(title string, m map[string]string) *MapEditor {
	me := &MapEditor{title: title}
	if m != nil {
		for k, v := range m {
			me.items = append(me.items, mapEntry{k, v})
		}
	}
	return me
}

func (m *MapEditor) Init() tea.Cmd { return nil }

func (m *MapEditor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
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
		case "a":
			m.items = append(m.items, mapEntry{"", ""})
			m.cursor = len(m.items) - 1
		case "d", "delete", "backspace":
			if len(m.items) > 0 {
				m.items = append(m.items[:m.cursor], m.items[m.cursor+1:]...)
				if m.cursor >= len(m.items) {
					if len(m.items) == 0 {
						m.cursor = 0
					} else {
						m.cursor = len(m.items) - 1
					}
				}
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

func (m *MapEditor) View() string {
	s := "\n  " + m.title + "\n\n"
	if len(m.items) == 0 {
		s += "  (empty)\n\n"
	} else {
		for i, e := range m.items {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			s += "  " + cursor + e.Key + " = " + e.Value + "\n"
		}
	}
	s += "\n  a:add  d:del  enter:done  esc:cancel\n"
	return s
}

func (m *MapEditor) Result() map[string]string {
	result := make(map[string]string)
	for _, e := range m.items {
		if e.Key != "" {
			result[e.Key] = e.Value
		}
	}
	return result
}
