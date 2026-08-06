// hook.go
//
// YAML-tagged structs describing lifecycle hooks attached to form actions
// (create/update/delete) and custom actions. Each hook is either a user-
// implemented Go function in internal/hooks or a raw SQL statement executed
// inline by the generated handler.
package types

// Hook is a single lifecycle hook: a user-implemented Go function (Fn) or a
// raw SQL statement (SQL) executed before or after the enclosing action.
type Hook struct {
	Name string `yaml:"name"` // identifier (used for generated stub names)
	Fn   string `yaml:"fn"`   // Go func in internal/hooks (user-implemented)
	SQL  string `yaml:"sql"`  // raw SQL executed inline (alternative to fn)
}

// Hooks groups the before and after hooks of an action.
type Hooks struct {
	Before []Hook `yaml:"before"`
	After  []Hook `yaml:"after"`
}

// HasFn reports whether any declared hook is a function hook, which requires
// the generated handler to import the internal/hooks package.
// Returns: true when at least one hook sets the Fn field.
func (h *Hooks) HasFn() bool {
	if h == nil {
		return false
	}
	for _, list := range [][]Hook{h.Before, h.After} {
		for _, hook := range list {
			if hook.Fn != "" {
				return true
			}
		}
	}
	return false
}
