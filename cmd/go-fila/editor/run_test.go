package editor

import (
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newSimScreen creates a simulation screen. Note: tview's Application.Run/Stop
// owns the screen lifecycle (Stop calls Fini), so callers must NOT Fini it.
func newSimScreen(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	return screen
}

// TestEditorRunSmoke drives the real event loop on a simulation screen:
// select the Panel nav item, back out, and quit. Verifies Run returns without
// error and nothing was saved.
func TestEditorRunSmoke(t *testing.T) {
	screen := newSimScreen(t)
	e := New(testConfig(), "testdata/go-fila.yaml")
	e.SetScreen(screen)

	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // open Panel
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyEsc, 0, tcell.ModNone) // back to home
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone) // quit (not modified)
	}()

	saved, err := e.Run()
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if saved {
		t.Error("expected no save on clean quit")
	}
}

// TestEditorFocusAfterNavSelect verifies that after opening a section from the
// left menu, focus moves to the content pane so form inputs/buttons are
// reachable (regression: Pages.SwitchToPage does not move focus on its own).
func TestEditorFocusAfterNavSelect(t *testing.T) {
	screen := newSimScreen(t)
	e := New(testConfig(), "testdata/go-fila.yaml")
	e.SetScreen(screen)

	go func() {
		time.Sleep(150 * time.Millisecond)
		// Nav is focused at startup; select the first item (Panel).
		screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
		time.Sleep(200 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone) // quit so Run returns
	}()

	if _, err := e.Run(); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	focused := e.app.GetFocus()
	if focused == nil {
		t.Fatal("no focused primitive")
	}
	// SetFocus(form) recurses to the form's first item, so we expect an
	// input field inside the Panel form — not the nav list.
	if _, ok := focused.(*tview.InputField); !ok {
		t.Errorf("expected focus on the Panel form's first input after selecting it, got %T", focused)
	}
}
func TestEditorSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/go-fila.yaml"

	screen := newSimScreen(t)
	e := New(testConfig(), path)
	e.SetScreen(screen)
	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone) // save
		time.Sleep(200 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone) // quit (now modified? no, saved)
	}()

	saved, err := e.Run()
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !saved {
		t.Error("expected save")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}

// TestSaveThenQuitSkipsConfirm reproduces the reported bug: edit a field, save
// with Ctrl+S, then quit with Ctrl+Q. The save must clear the modified flag so
// quitConfirm exits directly instead of asking to save/discard.
func TestSaveThenQuitSkipsConfirm(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/go-fila.yaml"

	screen := newSimScreen(t)
	e := New(testConfig(), path)
	e.SetScreen(screen)
	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone) // open Panel (first nav item)
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyRune, 'x', tcell.ModNone) // type into first field
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone) // save
		time.Sleep(200 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone) // quit — must not ask
	}()

	saved, err := e.Run()
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !saved {
		t.Error("expected save")
	}
	if e.modified {
		t.Error("modified should be false after Ctrl+S, so Ctrl+Q must quit directly")
	}
}
