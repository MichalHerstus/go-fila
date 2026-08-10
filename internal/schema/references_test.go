package schema

import (
	"testing"

	"github.com/go-fila/go-fila/internal/types"
)

func TestIsInlineSQL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"ListCustomers", false},
		{"CountUsers", false},
		{"GetUserByEmail", false},
		{"SELECT COUNT(*) FROM users", true},
		{"UPDATE orders SET status = 'completed' WHERE id = $1", true},
		{"DELETE FROM t WHERE id = ?", true},
		{"", false},
	} {
		if got := isInlineSQL(tc.in); got != tc.want {
			t.Errorf("isInlineSQL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestCollectReferencesInlineTagging verifies that inline SQL (action/widget
// queries) is tagged Inline while real SQLC query names are not.
func TestCollectReferencesInlineTagging(t *testing.T) {
	cfg := &types.Config{
		Resources: []types.Resource{{
			Name: "User",
			List: &types.ListConfig{Query: "ListUsers"},
			Actions: []types.Action{
				{Name: "archive", Query: "UPDATE users SET archived = 1 WHERE id = ?"},
			},
		}},
	}
	refs := CollectReferences(cfg)
	var gotName, gotInline bool
	for _, q := range refs.Queries {
		if q.Name == "ListUsers" && !q.Inline {
			gotName = true
		}
		if q.Name == "UPDATE users SET archived = 1 WHERE id = ?" && q.Inline {
			gotInline = true
		}
	}
	if !gotName {
		t.Error("ListUsers should be a non-inline reference")
	}
	if !gotInline {
		t.Error("action SQL should be tagged Inline")
	}
}
