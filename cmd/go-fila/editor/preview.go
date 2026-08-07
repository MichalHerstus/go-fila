package editor

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
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
			f.AddButton("Page: "+name, func() {
				e.showPage("preview/page", e.pagePreview(name))
			})
		}
	}
	if len(e.cfg.Resources) > 0 {
		f.AddTextView("", "Resources", 0, 1, true, false)
		for _, r := range e.cfg.Resources {
			name := r.Name
			f.AddButton("Resource: "+name, func() {
				e.showPage("preview/resource", e.resourcePreview(name))
			})
		}
	}
	if len(e.cfg.Pages) == 0 && len(e.cfg.Resources) == 0 {
		f.AddTextView("", "No pages or resources configured yet.", 0, 1, true, false)
	}
	f.AddButton("Back", e.back)
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
		body += "  " + strings.Repeat("-", 46) + "\n"
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
// shell (navigation groups from config on the left).
func mockFrame(e *Editor, content string) string {
	var b strings.Builder
	width := 78
	panelName := e.cfg.Panel.Name
	if panelName == "" {
		panelName = "admin"
	}

	// topbar
	b.WriteString("┌" + strings.Repeat("─", width-2) + "┐\n")
	b.WriteString("│ " + padRight(panelName, width-4) + " │\n")

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

	// two-column frame: sidebar 26 chars, content the rest
	sideW := 26
	contentW := width - 2 - sideW
	b.WriteString("├" + strings.Repeat("─", sideW) + "┬" + strings.Repeat("─", contentW) + "┤\n")
	sideLines := make([]string, len(side))
	for i, s := range side {
		sideLines[i] = s
	}
	contentLines := splitLines(content)
	max := len(sideLines)
	if len(contentLines) > max {
		max = len(contentLines)
	}
	for i := 0; i < max; i++ {
		sl := ""
		if i < len(sideLines) {
			sl = sideLines[i]
		}
		cl := ""
		if i < len(contentLines) {
			cl = contentLines[i]
		}
		b.WriteString("│ " + padRight(sl, sideW-2) + " │ " + padRight(cl, contentW-2) + " │\n")
	}
	b.WriteString("└" + strings.Repeat("─", sideW) + "┴" + strings.Repeat("─", contentW) + "┘")
	return b.String()
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}
