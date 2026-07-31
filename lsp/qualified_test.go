package lsp

import (
	"strings"
	"testing"
)

// A schema-qualified group member must be navigable and hoverable when the
// cursor is on the table name, and the definition it resolves to must land on
// the table name rather than the schema prefix.
func TestQualifiedGroupMemberNavigation(t *testing.T) {
	path := "/x.dbml"
	src := "Table app.serving_payload {\n  id int\n}\nTableGroup bronze {\n  app.serving_payload\n}\n"
	s := &Server{docs: map[string]string{path: src}}
	idx := s.index(pathToURI(path))

	// Cursor on "serving_payload" within the group member (after "app.").
	line := "  app.serving_payload"
	col := strings.Index(line, "serving_payload") // 0-based char
	occ := idx.at(path, position{Line: 4, Char: col})
	if occ == nil {
		t.Fatalf("no occurrence at the qualified group member name")
	}
	def, ok := idx.defs[occ.target]
	if !ok {
		t.Fatalf("group member %q has no definition", occ.target)
	}
	// The definition must anchor on the table name (col of "serving_payload" on
	// line 0), not on the "app" schema prefix.
	wantCol := strings.Index("Table app.serving_payload {", "serving_payload")
	if def.Range.Start.Line != 0 || def.Range.Start.Char != wantCol {
		t.Fatalf("definition should point at the table name (0,%d), got (%d,%d)",
			wantCol, def.Range.Start.Line, def.Range.Start.Char)
	}
}
