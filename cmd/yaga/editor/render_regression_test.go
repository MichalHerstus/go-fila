package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// drawPrimitive renders p (with the given rect) onto a simulation screen and
// returns the visible text per row plus the drawable cells.
func drawPrimitive(t *testing.T, p tview.Primitive, w, h int) ([]string, []tcell.SimCell) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(w, h)
	if r, ok := p.(interface{ SetRect(x, y, width, height int) }); ok {
		r.SetRect(0, 0, w, h)
	}
	p.Draw(screen)
	screen.Show()
	contents, sw, _ := screen.GetContents()
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < sw; x++ {
			b.WriteRune(rune(contents[y*sw+x].Bytes[0]))
		}
		lines[y] = b.String()
	}
	return lines, contents
}

// TestPreviewGridColors verifies only the grid chrome is light blue while the
// content text stays white.
func TestPreviewGridColors(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetText(mockFrame(e, "  [::b]Users[::-]\n  [::d]widgets[-:-:-]\n"))
	lines, cells := drawPrimitive(t, tv, 90, 24)

	// Locate the topbar row (the line containing "Admin").
	topbar := -1
	for i, l := range lines {
		if strings.Contains(l, "Admin") {
			topbar = i
			break
		}
	}
	if topbar < 0 {
		t.Fatal("topbar not rendered")
	}
	for x := 0; x < 10; x++ {
		c := cells[topbar*90+x]
		ch := rune(c.Bytes[0])
		fg, _, _ := c.Style.Decompose()
		switch x {
		case 0: // leading grid border │
			if ch == '│' && fg != tcell.ColorLightBlue {
				t.Errorf("grid border at col %d fg = %v, want light blue", x, fg)
			}
		case 2: // first content char (A of Admin)
			if ch == 'A' && fg != tcell.ColorWhite {
				t.Errorf("content text at col %d fg = %v, want white", x, fg)
			}
		}
	}
}
