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

	line := "  app.serving_payload"
	wantCol := strings.Index("Table app.serving_payload {", "serving_payload")

	// The whole `app.serving_payload` must be clickable: navigating from the
	// schema part, the dot, or the table name all reach the table definition,
	// which must anchor on the table name, not the schema prefix.
	for _, sub := range []string{"app", "serving_payload"} {
		col := strings.Index(line, sub)
		occ := idx.at(path, position{Line: 4, Char: col})
		if occ == nil {
			t.Fatalf("no occurrence at %q in the qualified group member", sub)
		}
		def, ok := idx.defs[occ.target]
		if !ok {
			t.Fatalf("group member %q has no definition", occ.target)
		}
		if def.Range.Start.Line != 0 || def.Range.Start.Char != wantCol {
			t.Fatalf("from %q: definition should point at the table name (0,%d), got (%d,%d)",
				sub, wantCol, def.Range.Start.Line, def.Range.Start.Char)
		}
	}

	// Rename must still touch only the table name on the member line, leaving
	// the schema prefix intact.
	occ := idx.at(path, position{Line: 4, Char: strings.Index(line, "app")})
	nameCol := strings.Index(line, "serving_payload")
	if occ.loc.Range.Start.Char != nameCol || occ.loc.Range.End.Char != nameCol+len("serving_payload") {
		t.Fatalf("edit range should cover only the table name, got %+v", occ.loc.Range)
	}
}
