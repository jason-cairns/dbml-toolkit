package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossFileNavigation(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.dbml")
	main := filepath.Join(dir, "main.dbml")
	os.WriteFile(base, []byte("Table users {\n  id int [pk]\n}\n"), 0o644)
	mainSrc := "use * from './base'\nTable posts {\n  id int [pk]\n  author int [ref: > users.id]\n}\n"
	os.WriteFile(main, []byte(mainSrc), 0o644)

	s := &Server{docs: map[string]string{base: readFile(t, base), main: mainSrc}}
	idx := s.index(pathToURI(main))

	// "users" appears on line 4 (0-based 3) of main.dbml inside the inline ref.
	col := strings.Index("  author int [ref: > users.id]", "users") + 1
	occ := idx.at(main, position{Line: 3, Char: col})
	if occ == nil {
		t.Fatalf("no occurrence found at the users reference")
	}
	def, ok := idx.defs[occ.target]
	if !ok {
		t.Fatalf("no definition for target %q", occ.target)
	}
	if !strings.HasSuffix(uriToPath(def.URI), "base.dbml") {
		t.Fatalf("definition should be cross-file in base.dbml, got %s", def.URI)
	}
	if def.Range.Start.Line != 0 {
		t.Fatalf("users defined on line 0 of base.dbml, got %d", def.Range.Start.Line)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
