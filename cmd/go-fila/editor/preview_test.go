package editor

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// TestMockFrameEqualWidths verifies the preview grid renders in light blue and
// every row spans exactly previewWidth cells.
func TestMockFrameEqualWidths(t *testing.T) {
	e := New(testConfig(), "testdata/go-fila.yaml")
	frame := mockFrame(e, "  [::b]Users[::-]\n  [::d]widgets[-:-:-]\n")

	if !strings.Contains(frame, "[lightblue]") {
		t.Error("expected light blue grid color tag")
	}
	if strings.Contains(frame, "[red]") {
		t.Error("full color resets must not leak into the light blue grid")
	}

	lines := strings.Split(frame, "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 4 {
		t.Fatalf("expected a full frame, got %d lines", len(lines))
	}
	for i, line := range lines {
		if w := tview.TaggedStringWidth(line); w != previewWidth {
			t.Errorf("line %d width = %d, want %d: %q", i, w, previewWidth, line)
		}
	}
}

// TestColorStable verifies content resets become attribute-only so the grid
// color is preserved.
func TestColorStable(t *testing.T) {
	got := colorStable("[::b]x[-:-:-] [::d]y[::-] end")
	if strings.Contains(got, "[-:-:-]") || strings.Contains(got, "[:]") {
		t.Errorf("full resets should be converted, got %q", got)
	}
	if !strings.Contains(got, "[::-]") {
		t.Errorf("attribute-only reset expected, got %q", got)
	}
}

// TestPadVisual verifies padding is computed on rendered width, ignoring tags.
func TestPadVisual(t *testing.T) {
	got := padVisual("[::b]ab[::-]c", 6)
	if w := tview.TaggedStringWidth(got); w != 6 {
		t.Errorf("padVisual width = %d, want 6 (%q)", w, got)
	}
	if got := padVisual("abcdefgh", 3); got != "abc" {
		t.Errorf("long plain text should truncate, got %q", got)
	}
}
