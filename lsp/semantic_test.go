package lsp

import (
	"strings"
	"testing"
)

func semAt(toks []semTok, line, col int) (int, bool) {
	for _, s := range toks {
		if s.line == line && s.col == col {
			return s.typ, true
		}
	}
	return 0, false
}

func TestSemanticTokenClassification(t *testing.T) {
	lines := []string{
		"// a comment",      // 0
		"Table app.users {", // 1
		"  id int [pk]",     // 2
		"  status varchar",  // 3
		"}",                 // 4
		"Enum kind {",       // 5
		"  active",          // 6
		"}",                 // 7
		"TableGroup g {",    // 8
		"  app.users",       // 9
		"}",                 // 10
	}
	src := strings.Join(lines, "\n") + "\n"
	toks := semanticTokens("/x.dbml", src)

	want := []struct {
		line int
		sub  string
		typ  int
		desc string
	}{
		{0, "// a comment", stComment, "line comment"},
		{1, "Table", stKeyword, "Table keyword"},
		{1, "app", stNamespace, "schema"},
		{1, "users", stClass, "table name"},
		{2, "id", stProperty, "column name"},
		{2, "int", stType, "column type"},
		{2, "pk", stKeyword, "setting flag"},
		{3, "varchar", stType, "column type"},
		{5, "Enum", stKeyword, "Enum keyword"},
		{5, "kind", stEnum, "enum name"},
		{6, "active", stEnumMember, "enum value"},
		{8, "TableGroup", stKeyword, "TableGroup keyword"},
		{8, "g", stNamespace, "group name"},
		{9, "app", stNamespace, "group member schema"},
		{9, "users", stClass, "group member table"},
	}
	for _, w := range want {
		col := strings.Index(lines[w.line], w.sub)
		got, ok := semAt(toks, w.line, col)
		if !ok {
			t.Errorf("%s: no token at line %d col %d (%q)", w.desc, w.line, col, w.sub)
			continue
		}
		if got != w.typ {
			t.Errorf("%s: got type %s, want %s", w.desc, semTokenTypes[got], semTokenTypes[w.typ])
		}
	}
}

func TestSemanticBlockComment(t *testing.T) {
	src := "Table t {\n  /* multi\n     line */ id int\n}\n"
	toks := semanticTokens("/x.dbml", src)
	// Two comment segments: one per line the block comment spans.
	n := 0
	for _, s := range toks {
		if s.typ == stComment {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("block comment should yield 2 line segments, got %d", n)
	}
}

func TestSemanticEncodingIsSortedDeltas(t *testing.T) {
	src := "Table t {\n  id int\n}\n"
	data := encodeSemTokens(semanticTokens("/x.dbml", src))
	if len(data)%5 != 0 {
		t.Fatalf("encoded stream must be a multiple of 5, got %d", len(data))
	}
	// deltaLine (every 5th, index 0) must be non-negative — tokens are sorted.
	for i := 0; i < len(data); i += 5 {
		if data[i] < 0 {
			t.Fatalf("negative deltaLine at %d: %v", i, data)
		}
	}
}
