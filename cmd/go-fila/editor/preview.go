package editor

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// preview dimensions: every frame/grid row renders exactly previewWidth cells
// wide (light blue grid), so all rows of the mock share the same total width.
const (
	previewWidth        = 78
	previewSideWidth    = 26
	previewContentWidth = previewWidth - previewSideWidth - 3 // 49; inner = 47
)

// previewPage lists the views that can be previewed: the dashboard (first
// default page or the first page) plus every configured page and resource.
func (e *Editor) previewPage() tview.Primitive {
	f := tview.NewForm()
	f.SetBorder(true).SetBorderColor(colBorder).SetTitle("Preview")
	f.SetLabelColor(colText)
	f.SetButtonBackgroundColor(colAccent)
	f.SetButtonTextColor(tcell.ColorWhite)

	if len(e.cfg.Pages) > 0 {
		f.AddTextView("", "Pages", 0, 1, true, false)
		for _, p := range e.cfg.Pages {
			name := p.Name
			e.addButton(f, "Page: "+name, func() {
				e.showPage("Preview/Page/"+name, e.pagePreview(name))
			})
		}
	}
	if len(e.cfg.Resources) > 0 {
		f.AddTextView("", "Resources", 0, 1, true, false)
		for _, r := range e.cfg.Resources {
			name := r.Name
			e.addButton(f, "Resource: "+name, func() {
				e.showPage("Preview/Resource/"+name, e.resourcePreview(name))
			})
		}
	}
	if len(e.cfg.Pages) == 0 && len(e.cfg.Resources) == 0 {
		f.AddTextView("", "No pages or resources configured yet.", 0, 1, true, false)
	}
	e.backButton(f)
	return f
}

// pagePreview renders a mock of a dashboard page: sidebar + topbar + widgets.
func (e *Editor) pagePreview(pageName string) tview.Primitive {
	var page *types.Page
	for i := range e.cfg.Pages {
		if e.cfg.Pages[i].Name == pageName {
			page = &e.cfg.Pages[i]
			break
		}
	}
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(true).SetBorderColor(colBorder).SetTitle("Preview: page " + pageName)
	tv.SetScrollable(true)
	body := "  [::u]" + pageName + "[::-]\n\n"
	if page != nil {
		for _, w := range page.Widgets {
			body += widgetMock(w, 1)
		}
	}
	fmt.Fprint(tv, mockFrame(e, body))
	return tv
}

// resourcePreview renders a mock of a resource list view.
func (e *Editor) resourcePreview(resName string) tview.Primitive {
	var res *types.Resource
	for i := range e.cfg.Resources {
		if e.cfg.Resources[i].Name == resName {
			res = &e.cfg.Resources[i]
			break
		}
	}
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetBorder(true).SetBorderColor(colBorder).SetTitle("Preview: resource " + resName)
	tv.SetScrollable(true)

	body := "  [::u]" + resName + "[::-]   " + e.cfg.Panel.Path + "/" + strings.ToLower(resName) + "\n\n"
	if res != nil && res.List != nil {
		body += "  Search [______]   (per-page rows)\n\n"
		var cols []string
		for _, c := range res.List.Columns {
			cols = append(cols, c.Name)
		}
		if len(cols) == 0 {
			cols = append(cols, "id")
		}
		body += "  " + strings.Join(cols, "   |   ") + "\n"
		body += "  " + strings.Repeat("-", previewContentWidth-2) + "\n"
		for row := 0; row < 4; row++ {
			cells := make([]string, len(cols))
			for i := range cells {
				cells[i] = "…"
			}
			body += "  " + strings.Join(cells, "   |   ") + "\n"
		}
		body += "\n  [::d][ Edit ]  [ Delete ]  (bulk)[-:-:-]\n"
	} else {
		body += "  [::d]No list view configured.[-:-:-]\n"
	}
	fmt.Fprint(tv, mockFrame(e, body))
	return tv
}

// widgetMock renders one widget as a boxed preview line.
func widgetMock(w types.Widget, depth int) string {
	indent := strings.Repeat("  ", depth)
	title := w.Label
	switch w.Type {
	case "chart":
		if w.Chart != nil {
			title += " (" + w.Chart.Type + " chart)"
		}
	case "stats_grid":
		title += " (grid of " + fmt.Sprint(len(w.Widgets)) + ")"
	case "table":
		title += " (table, cols=" + strings.Join(w.DataColumns, ",") + ")"
	case "list":
		title += " (list, cols=" + strings.Join(w.DataColumns, ",") + ")"
	case "html":
		title += " (html)"
	}
	line := indent + "[::b]" + w.Type + "[::-]  " + title
	if depth > 0 && w.Type == "stats_grid" {
		var subs []string
		for _, sw := range w.Widgets {
			subs = append(subs, sw.Label)
		}
		line += "\n" + indent + "    " + strings.Join(subs, " | ")
	}
	return line + "\n"
}

// mockFrame wraps content in a crude sidebar + topbar frame to show the panel
// shell (navigation groups from config on the left). The grid is drawn in light
// blue and every row is padded to the exact same total width (previewWidth).
func mockFrame(e *Editor, content string) string {
	content = colorStable(content)
	panelName := e.cfg.Panel.Name
	if panelName == "" {
		panelName = "admin"
	}

	// sidebar (first navigation group/items, else resource names)
	var side []string
	if len(e.cfg.Navigation) > 0 {
		for _, g := range e.cfg.Navigation {
			side = append(side, g.Group)
			for _, it := range g.Items {
				label := it.Label
				if label == "" {
					label = it.Resource + it.Page + it.URL
				}
				side = append(side, "  "+label)
			}
		}
	} else {
		for _, r := range e.cfg.Resources {
			side = append(side, r.Name)
		}
	}

	width := previewWidth
	sideW := previewSideWidth
	contentW := previewContentWidth
	grid, text := "[lightblue]", "[white]"
	var b strings.Builder
	b.WriteString(grid + "┌" + strings.Repeat("─", width-2) + "┐\n")
	b.WriteString(grid + "│ " + text + padVisual(panelName, width-4) + grid + " │\n")
	b.WriteString(grid + "├" + strings.Repeat("─", sideW) + "┬" + strings.Repeat("─", contentW) + "┤\n")

	contentLines := splitLines(content)
	max := len(side)
	if len(contentLines) > max {
		max = len(contentLines)
	}
	for i := 0; i < max; i++ {
		sl := ""
		if i < len(side) {
			sl = side[i]
		}
		cl := ""
		if i < len(contentLines) {
			cl = contentLines[i]
		}
		b.WriteString(grid + "│ " + text + padVisual(sl, sideW-2) + grid + " │ " + text + padVisual(cl, contentW-2) + grid + " │\n")
	}
	b.WriteString(fmt.Sprintf("%s└%s┴%s┘", grid, strings.Repeat("─", sideW), strings.Repeat("─", contentW)))
	return b.String()
}

// padVisual pads s with trailing spaces so its rendered (tag-aware) width is
// exactly n. Long untagged text is truncated; tagged overflow is left as-is
// (preview content stays short).
func padVisual(s string, n int) string {
	w := tview.TaggedStringWidth(s)
	if w < n {
		return s + strings.Repeat(" ", n-w)
	}
	if w == n {
		return s
	}
	if !strings.Contains(s, "[") {
		if rs := []rune(s); len(rs) > n {
			return string(rs[:n])
		}
	}
	return s
}

// colorStable replaces full color-reset tags with attribute-only resets so the
// preview's light-blue grid color survives content emphasis tags ([::-] has no
// "[:]"/"[-:-:-]" substring, so replacement is order-safe).
func colorStable(s string) string {
	s = strings.ReplaceAll(s, "[-:-:-]", "[::-]")
	return strings.ReplaceAll(s, "[:]", "[::-]")
}
