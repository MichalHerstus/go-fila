package editor

import (
	"strconv"
	"strings"
	"time"

	"github.com/rivo/tview"
)

// Option pairs a display label with the YAML value it writes.
type Option struct {
	Label string
	Value string
}

// markModified flags the editor dirty and refreshes the title bar.
func (e *Editor) markModified() {
	if !e.modified {
		e.modified = true
		e.refreshTitle()
	}
}

// str adds a text input field bound to a config value.
func (e *Editor) str(form *tview.Form, label, value string, set func(string)) {
	form.AddInputField(label, value, 0, nil, func(text string) {
		set(text)
		e.markModified()
	})
}

// num adds a numeric input field bound to an int config value.
func (e *Editor) num(form *tview.Form, label string, value int, set func(int)) {
	form.AddInputField(label, strconv.Itoa(value), 0, func(s string, _ rune) bool {
		if s == "" || s == "-" {
			return true
		}
		_, err := strconv.Atoi(s)
		return err == nil
	}, func(s string) {
		if v, err := strconv.Atoi(s); err == nil {
			set(v)
			e.markModified()
		}
	})
}

// yesno adds a checkbox bound to a bool config value.
func (e *Editor) yesno(form *tview.Form, label string, value bool, set func(bool)) {
	form.AddCheckbox(label, value, func(checked bool) {
		set(checked)
		e.markModified()
	})
}

// password adds a masked input field.
func (e *Editor) password(form *tview.Form, label, value string, set func(string)) {
	form.AddPasswordField(label, value, 0, '*', func(text string) {
		set(text)
		e.markModified()
	})
}

// long adds a multi-line text area bound to a config value.
func (e *Editor) long(form *tview.Form, label, value string, set func(string)) {
	form.AddTextArea(label, value, 0, 3, 0, func(text string) {
		set(text)
		e.markModified()
	})
}

// pick adds a drop-down bound to an enum config value.
func (e *Editor) pick(form *tview.Form, label string, opts []Option, value string, set func(string)) {
	texts := make([]string, len(opts))
	initial := 0
	for i, o := range opts {
		texts[i] = o.Label
		if o.Value == value {
			initial = i
		}
	}
	// SetCurrentOption fires the selected callback at construction; skip it so
	// merely building a page does not mark the config modified.
	first := true
	form.AddDropDown(label, texts, initial, func(_ string, index int) {
		if first {
			first = false
			return
		}
		if index >= 0 && index < len(opts) {
			set(opts[index].Value)
			e.markModified()
		}
	})
}

// head adds a section separator row to a form.
func (e *Editor) head(form *tview.Form, title string) {
	form.AddTextView("", title, 0, 1, true, false)
}

// backButton adds a "Back" button pinned to Ctrl+B (never a different combo).
func (e *Editor) backButton(form *tview.Form) {
	e.addButtonPref(form, "Back", 'B', e.back)
}

// showPage pushes a named page and focuses it.
func (e *Editor) showPage(name string, prim tview.Primitive) {
	e.pages.AddPage(name, prim, true, true)
	e.pages.SwitchToPage(name)
	e.history = append(e.history, name)
	e.bindShortcuts(name)
	e.app.SetFocus(e.pages)
	e.refreshTitle()
}

// refreshPage re-renders an already pushed page in place (same name, no extra
// history entry) and focuses it.
func (e *Editor) refreshPage(name string, prim tview.Primitive) {
	e.pages.AddPage(name, prim, true, true)
	e.pages.SwitchToPage(name)
	e.bindShortcuts(name)
	e.app.SetFocus(e.pages)
	e.refreshTitle()
}

// back pops to the previous page, or opens the quit prompt at the root.
func (e *Editor) back() {
	if len(e.history) <= 1 {
		e.quitConfirm()
		return
	}
	e.history = e.history[:len(e.history)-1]
	prev := e.history[len(e.history)-1]
	e.pages.SwitchToPage(prev)
	// Home is a static overview; put focus back on the nav menu there.
	if prev == "home" {
		e.app.SetFocus(e.nav)
	} else {
		e.app.SetFocus(e.pages)
	}
	e.refreshTitle()
}

// toast shows a transient message in the status bar.
func (e *Editor) toast(msg string) {
	if e.status == nil {
		return
	}
	e.status.SetText(msg)
	go func() {
		time.Sleep(1500 * time.Millisecond)
		e.app.QueueUpdateDraw(func() {
			e.renderStatus()
		})
	}()
}

// errorModal shows a message box with a single OK button.
func (e *Editor) errorModal(title, msg string) {
	modal := tview.NewModal().SetText(title + "\n\n" + msg)
	labels := e.addModalButtons([]string{"OK"}, func(_ int, _ string) {
		e.closeModal()
	})
	modal.AddButtons(labels).SetDoneFunc(func(_ int, _ string) {
		e.closeModal()
	})
	e.showModal(modal)
}

// closeModal dismisses the currently open modal page.
func (e *Editor) closeModal() {
	if !e.modalOpen {
		return
	}
	e.pages.RemovePage("modal")
	e.modalOpen = false
	delete(e.shortcuts, "modal")
	if len(e.history) > 0 {
		e.pages.SwitchToPage(e.history[len(e.history)-1])
	}
	e.app.SetFocus(e.pages)
}

// showModal displays a primitive as the app-modal and focuses it.
func (e *Editor) showModal(p tview.Primitive) {
	e.pages.AddPage("modal", p, true, true)
	e.pages.SwitchToPage("modal")
	e.bindShortcuts("modal")
	e.modalOpen = true
	e.app.SetFocus(e.pages)
}

// splitLines trims and splits a string into non-empty lines.
func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}
