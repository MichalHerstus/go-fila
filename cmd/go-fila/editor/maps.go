package editor

import (
	"fmt"
	"sort"

	"github.com/rivo/tview"
)

// stringMapPage manages a map[string]string (column options, field options,
// query params). Keys are unique; values are edited in place.
func (e *Editor) stringMapPage(name, title string, get func() map[string]string, set func(map[string]string)) tview.Primitive {
	keys := func() []string {
		m := get()
		if m == nil {
			return nil
		}
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	spec := listSpec{
		title: title,
		labels: func() []string {
			return keys()
		},
		sub: func(i int) string {
			k := keys()[i]
			return get()[k]
		},
		add: func() {
			e.promptInput("Add "+title, "key", "", func(k string) {
				if k == "" {
					e.toast("Key is required")
					return
				}
				m := get()
				if m == nil {
					m = map[string]string{}
				}
				if _, exists := m[k]; exists {
					e.toast("Key already exists")
					return
				}
				m[k] = ""
				set(m)
				e.markModified()
			})
		},
		edit: func(i int) {
			k := keys()[i]
			e.promptInput("Edit "+title, "value", get()[k], func(v string) {
				m := get()
				m[k] = v
				set(m)
				e.markModified()
			})
		},
		remove: func(i int) {
			m := get()
			delete(m, keys()[i])
			set(m)
			e.markModified()
		},
	}
	return e.recordList(name, spec)
}

// summaryLine formats a map into a compact "k1=v1, k2=v2" line for list subs.
func summaryLine(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out string
	for i, k := range keys {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s=%s", k, m[k])
	}
	return out
}
