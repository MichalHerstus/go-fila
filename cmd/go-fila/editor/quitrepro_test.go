package editor

import (
	"testing"

	"github.com/rivo/tview"
)

// TestQuitConfirmClearsModified drives the edit -> save -> quit state machine
// for each widget type, asserting that save() clears the modified flag so
// quitConfirm() exits directly instead of opening the save/discard modal.
func TestQuitConfirmClearsModified(t *testing.T) {
	cases := []struct {
		name  string
		build func(*Editor) *tview.Form
		edit  func(*tview.Form)
	}{
		{
			name:  "checkbox",
			build: func(e *Editor) *tview.Form { return e.layoutPage().(*tview.Form) },
			edit: func(f *tview.Form) {
				cb := f.GetFormItem(0).(*tview.Checkbox)
				cb.SetChecked(!cb.IsChecked())
			},
		},
		{
			name:  "input",
			build: func(e *Editor) *tview.Form { return e.panelPage().(*tview.Form) },
			edit: func(f *tview.Form) {
				in := f.GetFormItem(0).(*tview.InputField)
				in.SetText(in.GetText() + "x")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New(testConfig(), t.TempDir()+"/go-fila.yaml")
			e.app = tview.NewApplication()
			e.buildShell()

			f := tc.build(e)
			tc.edit(f)
			if !e.modified {
				t.Fatal("edit should mark modified")
			}

			e.save()
			if e.modified {
				t.Error("save() must clear the modified flag")
			}

			if e.modalOpen {
				t.Error("no modal should be open before quit")
			}
			e.quitConfirm()
			if e.modalOpen {
				t.Error("quitConfirm() must quit directly when modified is false, no save/discard modal")
			}
		})
	}
}
