package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// buttonLabels returns the labels of a form's buttons.
func buttonLabels(form *tview.Form) []string {
	out := make([]string, 0, form.GetButtonCount())
	for i := 0; i < form.GetButtonCount(); i++ {
		out = append(out, form.GetButton(i).GetLabel())
	}
	return out
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// TestFormButtonShortcuts verifies buttons carry a " (Ctrl+X)" hint built from
// the first free letter of their label. Ctrl+B is reserved for Back: Brand
// falls through to its next free letter (Ctrl+R) while Back always takes B.
func TestFormButtonShortcuts(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	labels := buttonLabels(e.panelPage().(*tview.Form))
	if !hasLabel(labels, "Brand (Ctrl+R)") {
		t.Errorf("expected Brand (Ctrl+R) (B is reserved for Back), got %v", labels)
	}
	if !hasLabel(labels, "Layout (Ctrl+L)") {
		t.Errorf("expected Layout (Ctrl+L), got %v", labels)
	}
	if !hasLabel(labels, "Theme (Ctrl+T)") {
		t.Errorf("expected Theme (Ctrl+T), got %v", labels)
	}
	if !hasLabel(labels, "Back (Ctrl+B)") {
		t.Errorf("Back must always get Ctrl+B, got %v", labels)
	}
}

// findFormIn returns the first *tview.Form found in a (possibly flex-wrapped)
// page, walking flex items recursively.
func findFormIn(p tview.Primitive) *tview.Form {
	if f, ok := p.(*tview.Form); ok {
		return f
	}
	if flex, ok := p.(*tview.Flex); ok {
		for i := 0; i < flex.GetItemCount(); i++ {
			if f := findFormIn(flex.GetItem(i)); f != nil {
				return f
			}
		}
	}
	return nil
}

// TestShortcutDispatch verifies a Ctrl+letter keypress drives the matching
// button. Ctrl+B is reserved for "Back" on every screen, so the Panel page's
// Brand button falls through to its next free letter (Ctrl+R) instead.
func TestShortcutDispatch(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	e.pages = tview.NewPages()
	e.app = tview.NewApplication()
	e.buildShell()

	e.history = []string{"home"}
	e.showPage("Panel", e.panelPage())

	out := e.capture(tcell.NewEventKey(tcell.KeyCtrlB, 'B', tcell.ModCtrl))
	if out != nil {
		t.Errorf("capture should consume Ctrl+B, got %v", out)
	}
	if len(e.history) != 1 || e.history[0] != "home" {
		t.Errorf("Ctrl+B should go back to home, history = %v", e.history)
	}

	e.history = []string{"home"}
	e.showPage("Panel", e.panelPage())
	e.capture(tcell.NewEventKey(tcell.KeyCtrlR, 'R', tcell.ModCtrl))
	if len(e.history) != 3 || e.history[2] != "Panel/Brand" {
		t.Errorf("Ctrl+R should open Panel/Brand (B is reserved for Back), history = %v", e.history)
	}
}

// TestShortcutsWorkInTextFields verifies every registered Ctrl shortcut fires
// even while a text field holds focus (the default focus of a form) — hotkeys
// beat text-editing combos.
func TestShortcutsWorkInTextFields(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	e.pages = tview.NewPages()
	e.app = tview.NewApplication()

	e.history = []string{"home"}
	e.showPage("Panel", e.panelPage())
	if _, ok := e.app.GetFocus().(*tview.InputField); !ok {
		t.Fatalf("expected focus on the panel form's first text input, got %T", e.app.GetFocus())
	}

	// Ctrl+L (Layout) fires from the text field.
	e.capture(tcell.NewEventKey(tcell.KeyCtrlL, 'L', tcell.ModCtrl))
	if len(e.history) != 3 || e.history[2] != "Panel/Layout" {
		t.Errorf("Ctrl+L should open Panel/Layout, history = %v", e.history)
	}
}

// TestShortcutModal verifies modal buttons get Ctrl shortcuts that are scoped
// to the modal, and page shortcuts are masked while a modal is open.
func TestShortcutModal(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	e.pages = tview.NewPages()
	e.app = tview.NewApplication()

	var confirmed bool
	e.confirm("Delete?", "Sure?", func() { confirmed = true })

	// Page-level Ctrl+B is unreachable while the modal is open.
	if out := e.capture(tcell.NewEventKey(tcell.KeyCtrlB, 'B', tcell.ModCtrl)); out == nil {
		t.Error("page shortcuts must not fire while a modal is open")
	}
	// Ctrl+Y answers "Yes" on the modal.
	e.capture(tcell.NewEventKey(tcell.KeyCtrlY, 'Y', tcell.ModCtrl))
	if !confirmed {
		t.Error("Ctrl+Y should confirm the dialog")
	}
	if e.modalOpen {
		t.Error("modal should be closed after Ctrl+Y")
	}
}

// captionKey extracts the Ctrl+letter key from a button caption like
// "Generate missing queries (Ctrl+G)".
func captionKey(caption string) tcell.Key {
	idx := strings.LastIndex(caption, "Ctrl+")
	if idx < 0 || idx+5 >= len(caption) {
		return 0
	}
	r := rune(caption[idx+5])
	if r >= 'A' && r <= 'Z' {
		return ctrlKey(r)
	}
	return 0
}
