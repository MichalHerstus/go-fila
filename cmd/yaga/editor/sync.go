package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MichalHerstus/yaga/internal/schema"
	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// colMissing is a column reference that does not exist in its table, with the
// exact config location (resource + section + index) so the Validate screen can
// jump to it.
type colMissing struct {
	resource string
	ref      schema.ColumnRef
}

// syncReport is the result of one analysis pass over schema/queries/YAML.
type syncReport struct {
	tables      []schema.Table
	queries     map[string]schema.Query
	missingQ    []schema.QueryRef
	inlineQ     []schema.QueryRef // inline SQL refs (widgets/actions), not query names
	missingTabs []string          // resource names with no matching table
	missingCols []colMissing      // column refs with no matching column
	fkTargets   []string          // FK target tables lacking a List query
	err         string
}

// analyze runs the sync analysis over the config's schema/queries dirs.
func (e *Editor) analyze() *syncReport {
	rep := &syncReport{}
	base := e.sqlBase()
	schemaDir := e.cfg.SQLC.SchemaDir
	if schemaDir == "" {
		schemaDir = "./sql/migrations"
	}
	queriesDir := e.cfg.SQLC.QueriesDir
	if queriesDir == "" {
		queriesDir = "./sql/queries"
	}
	schemaDir = filepath.Join(base, schemaDir)
	queriesDir = filepath.Join(base, queriesDir)

	matches, _ := filepath.Glob(filepath.Join(schemaDir, "*.sql"))
	tables, err := schema.ParseSchema(matches...)
	if err != nil {
		rep.err = "schema: " + err.Error()
		return rep
	}
	rep.tables = tables
	rep.queries = schema.ParseQueries(queriesDir)

	refs := schema.CollectReferences(e.cfg)
	seenInline := map[string]bool{}
	for _, q := range refs.Queries {
		if q.Inline {
			// Inline SQL (widget/action queries) is complete SQL in the YAML —
			// it can never resolve to a -- name: definition, so it is reported
			// separately as informational, not as a missing query.
			if !seenInline[q.Name] {
				seenInline[q.Name] = true
				rep.inlineQ = append(rep.inlineQ, q)
			}
			continue
		}
		if _, ok := rep.queries[q.Name]; !ok {
			rep.missingQ = append(rep.missingQ, q)
		}
	}

	for rname, table := range refs.Tables {
		if schema.FindTableByName(tables, table) == nil {
			rep.missingTabs = append(rep.missingTabs, rname+" -> "+table)
		}
	}

	for rname, refsList := range refs.ColumnRefs {
		table := refs.Tables[rname]
		t := schema.FindTableByName(tables, table)
		if t == nil {
			continue
		}
		for _, ref := range refsList {
			if !tableHasColumn(*t, ref.Column) {
				rep.missingCols = append(rep.missingCols, colMissing{resource: rname, ref: ref})
			}
		}
	}

	// FK target tables without a List query (used by relation options_query)
	for _, ti := range tables {
		for _, fk := range ti.FKs() {
			listName := "List" + schema.ToPascalCase(fk.ForeignTable)
			if _, ok := rep.queries[listName]; !ok {
				if !containsString(rep.fkTargets, listName) {
					rep.fkTargets = append(rep.fkTargets, listName+" (for "+fk.ForeignTable+")")
				}
			}
		}
	}
	return rep
}

// tableHasColumn reports whether a table has a column, trying raw and
// lowercased matches (sqlc lowercases identifiers).
func tableHasColumn(t schema.Table, col string) bool {
	for _, c := range t.Columns {
		if c.Name == col || strings.EqualFold(c.Name, col) {
			return true
		}
	}
	return false
}

// importResourcesFromSchema appends resource blocks for schema tables that are
// not yet mapped to a resource, using the driver-aware YAML builder. Kept out
// of the UI; used by tests.
func (e *Editor) importResourcesFromSchema(rep *syncReport) {
	driver := schema.Driver(e.cfg)
	existing := map[string]bool{}
	for _, r := range e.cfg.Resources {
		existing[schema.TableNameFor(r)] = true
	}
	var added []string
	for _, ti := range rep.tables {
		if existing[strings.ToLower(ti.Name)] {
			continue
		}
		block := schema.GenerateResourceYAML(ti, rep.tables, driver)
		resource, err := parseResourceBlock(block)
		if err != nil {
			e.errorModal("Import failed", err.Error())
			return
		}
		e.cfg.Resources = append(e.cfg.Resources, resource)
		existing[strings.ToLower(ti.Name)] = true
		added = append(added, ti.Name)
	}
	if len(added) == 0 {
		e.toast("No new tables to import")
		return
	}
	e.markModified()
	e.toast(fmt.Sprintf("Imported %d resource(s)", len(added)))
}

// parseResourceBlock parses a generated "- name: X" YAML block into a Resource.
func parseResourceBlock(block string) (types.Resource, error) {
	var doc struct {
		Resources []types.Resource `yaml:"resources"`
	}
	wrapped := "resources:\n" + block
	if err := yaml.Unmarshal([]byte(wrapped), &doc); err != nil {
		return types.Resource{}, err
	}
	if len(doc.Resources) != 1 {
		return types.Resource{}, fmt.Errorf("expected exactly one resource, got %d", len(doc.Resources))
	}
	return doc.Resources[0], nil
}
