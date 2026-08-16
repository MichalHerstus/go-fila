package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Ctrl+key quick access for buttons. Every button takes the first free letter
// of its label as a shortcut (e.g. Ctrl+R for "Refresh"),
// shown as a hint in the button label. Ctrl+S (save) and Ctrl+Q (quit) are
// reserved for the editor's global keys. Shortcuts are scoped to the currently
// displayed screen (the "modal" context while one is open, otherwise the front
// page); they are collected while that screen is being built, bound in
// showPage/showModal, and dispatched from the app-level capture BEFORE any
// widget — a registered Ctrl key fires whether or not a text field is focused.

// globalKeys are Ctrl combos with a fixed meaning in the editor.
var globalKeys = map[tcell.Key]bool{
	tcell.KeyCtrlS: true,
	tcell.KeyCtrlQ: true,
	tcell.KeyCtrlV: true, // Validate
	tcell.KeyCtrlP: true, // Go to (cd navigation)
	tcell.KeyCtrlO: true, // Home (overview)
}

// ctrlKey returns the tcell key for Ctrl+<letter>, or 0 for non-letters.
func ctrlKey(r rune) tcell.Key {
	u := r
	if u >= 'a' && u <= 'z' {
		u -= 'a' - 'A'
	}
	if u < 'A' || u > 'Z' {
		return 0
	}
	return tcell.KeyCtrlA + tcell.Key(u-'A')
}

// ctrlHint renders the "Ctrl+X" hint for a shortcut key.
func ctrlHint(key tcell.Key) string {
	for r := rune('A'); r <= 'Z'; r++ {
		if ctrlKey(r) == key {
			return "Ctrl+" + string(r)
		}
	}
	return "Ctrl"
}

// takeShortcut claims a Ctrl+key shortcut for a button in the current build
// context (e.pending). The letter 'B' is reserved for Back buttons: regular
// buttons skip it so "Back" can always take Ctrl+B (pref='B' tries it first,
// then falls back to the label's free letters). Reserved and already-taken
// keys are skipped; key 0 is returned when no letter is available.
func (e *Editor) takeShortcut(label string, pref rune) tcell.Key {
	if e.pending == nil {
		e.pending = map[tcell.Key]func(){}
	}
	try := func(r rune) tcell.Key {
		key := ctrlKey(r)
		if key == 0 || globalKeys[key] {
			return 0
		}
		if _, taken := e.pending[key]; taken {
			return 0
		}
		return key
	}
	if pref != 0 {
		if key := try(pref); key != 0 {
			return key
		}
	}
	for _, r := range label {
		if pref == 0 && (r == 'B' || r == 'b') {
			continue // reserved for the Back button
		}
		if key := try(r); key != 0 {
			return key
		}
	}
	return 0
}

// addButton adds a labeled button to a form together with its Ctrl+key
// shortcut, shown as a hint in the label.
func (e *Editor) addButton(form *tview.Form, label string, fn func()) {
	e.addButtonPref(form, label, 0, fn)
}

// addButtonPref is like addButton but tries a preferred Ctrl+letter first
// (used to keep "Back" on Ctrl+B on every screen).
func (e *Editor) addButtonPref(form *tview.Form, label string, pref rune, fn func()) {
	if key := e.takeShortcut(label, pref); key != 0 {
		e.pending[key] = fn
		label += " (" + ctrlHint(key) + ")"
	}
	form.AddButton(label, fn)
}

// addModalButtons registers Ctrl+key shortcuts for tview.Modal buttons and
// returns the button labels to hand to AddButtons (with hints appended).
// Shortcut presses call done(index, label) exactly like button presses.
func (e *Editor) addModalButtons(labels []string, done func(index int, label string)) []string {
	out := make([]string, len(labels))
	for i, label := range labels {
		if key := e.takeShortcut(label, 0); key != 0 {
			idx, lbl := i, label
			e.pending[key] = func() { done(idx, lbl) }
			out[i] = label + " (" + ctrlHint(key) + ")"
		} else {
			out[i] = label
		}
	}
	return out
}

// bindShortcuts attaches the shortcuts collected since the last call to the
// given screen context (a page name or "modal").
func (e *Editor) bindShortcuts(ctx string) {
	if e.shortcuts == nil {
		e.shortcuts = map[string]map[tcell.Key]func(){}
	}
	e.shortcuts[ctx] = e.pending
	e.pending = nil
}

// shortcutFor looks up the handler registered for a key in the current screen
// context (the modal while one is open, otherwise the front page).
func (e *Editor) shortcutFor(key tcell.Key) func() {
	ctx := "home"
	if e.modalOpen {
		ctx = "modal"
	} else if len(e.history) > 0 {
		ctx = e.history[len(e.history)-1]
	}
	if handlers := e.shortcuts[ctx]; handlers != nil {
		return handlers[key]
	}
	return nil
}

// editingKey and focusIsText were formerly used to let text widgets keep their
// Ctrl editing combos; shortcuts now always take priority (a button's Ctrl key
// fires even while a text field is focused), so the guard is gone.
