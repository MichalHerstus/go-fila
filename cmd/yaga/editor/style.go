package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Color palette for the editor chrome.
var (
	colAccent   = tcell.NewHexColor(0x6366f1)
	colAccentHi = tcell.NewHexColor(0x8b5cf6)
	colText     = tcell.NewHexColor(0xd4d4d8)
	colMuted    = tcell.NewHexColor(0x71717a)
	colBorder   = tcell.NewHexColor(0x3f3f46)
	colBg       = tcell.NewHexColor(0x1c1917)
	colOk       = tcell.NewHexColor(0x22c55e)
	colWarn     = tcell.NewHexColor(0xeab308)
	colErr      = tcell.NewHexColor(0xef4444)
	colInfo     = tcell.NewHexColor(0x3b82f6)
)

// boxed wraps p in a tview.Flex frame with a border and title so that any
// primitive gets a titled border.
func boxed(p tview.Primitive, title string) tview.Primitive {
	f := tview.NewFlex()
	f.SetBorder(true).SetBorderColor(colBorder).SetTitle(title)
	f.AddItem(p, 0, 1, true)
	return f
}

// statusColor maps a status keyword to a color name used in dynamic colors.
func statusColor(status string) string {
	switch status {
	case "error":
		return "red"
	case "warning":
		return "yellow"
	case "ok":
		return "green"
	case "info":
		return "blue"
	default:
		return "white"
	}
}
