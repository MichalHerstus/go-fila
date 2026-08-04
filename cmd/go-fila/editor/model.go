package editor

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/go-fila/go-fila/internal/types"
)

type screen interface {
	tea.Model
	isDone() bool
	popScreen() tea.Model
}

type EditorModel struct {
	cfg        *types.Config
	configPath string
	stack      []tea.Model
	modified   bool
	quitting   bool
	saved      bool
}

func New(cfg *types.Config, configPath string) *EditorModel {
	ed := &EditorModel{
		cfg:        cfg,
		configPath: configPath,
	}
	ed.stack = []tea.Model{NewMainMenu(ed)}
	return ed
}

func (e *EditorModel) Run() (bool, error) {
	p := tea.NewProgram(e, tea.WithAltScreen())
	_, err := p.Run()
	return e.saved, err
}

func (e *EditorModel) Push(m tea.Model) {
	e.stack = append(e.stack, m)
}

func (e *EditorModel) Pop() {
	if len(e.stack) > 1 {
		e.stack = e.stack[:len(e.stack)-1]
	}
}

func (e *EditorModel) Config() *types.Config {
	return e.cfg
}

func (e *EditorModel) SetModified() {
	e.modified = true
}

func (e *EditorModel) Save() {
	e.saved = true
	e.quitting = true
}

func (e *EditorModel) Quit() {
	e.quitting = true
}

func (e *EditorModel) Init() tea.Cmd {
	return nil
}

func (e *EditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if e.quitting {
		return e, tea.Quit
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			e.quitting = true
			return e, tea.Quit
		}
	}

	if len(e.stack) == 0 {
		return e, tea.Quit
	}

	top := e.stack[len(e.stack)-1]
	updated, cmd := top.Update(msg)

	if e.quitting {
		return e, tea.Quit
	}

	if s, ok := updated.(screen); ok {
		if s.isDone() {
			e.Pop()
			return e, nil
		}
		if ps := s.popScreen(); ps != nil {
			e.Push(ps)
			return e, ps.Init()
		}
		e.stack[len(e.stack)-1] = s
	} else {
		e.stack[len(e.stack)-1] = updated
	}

	return e, cmd
}

func (e *EditorModel) View() string {
	if len(e.stack) == 0 {
		return ""
	}
	return e.stack[len(e.stack)-1].View()
}

type FormScreen struct {
	form    *huh.Form
	done    bool
	applies []func()
}

func NewFormScreen(form *huh.Form, applies ...func()) *FormScreen {
	return &FormScreen{form: form, applies: applies}
}

func (f *FormScreen) Init() tea.Cmd {
	return f.form.Init()
}

func (f *FormScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if f.done {
		return f, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			f.done = true
			return f, tea.Quit
		}
	}

	updated, cmd := f.form.Update(msg)
	f.form = updated.(*huh.Form)

	if f.form.State == huh.StateCompleted || f.form.State == huh.StateAborted {
		if f.form.State == huh.StateCompleted {
			for _, a := range f.applies {
				a()
			}
		}
		f.done = true
		return f, tea.Quit
	}

	return f, cmd
}

func (f *FormScreen) View() string {
	return f.form.View()
}

func (f *FormScreen) isDone() bool         { return f.done }
func (f *FormScreen) popScreen() tea.Model { return nil }
