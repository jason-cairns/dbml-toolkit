package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContextUsesReuseAsExportBoundary(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "external.dbml", `
Table users {
  id int [pk]
  email string
}
Table teams {
  id int [pk]
}
`)
	writeContextFile(t, dir, "owned.dbml", `
use * from './external'
Table posts {
  id int [pk]
  author_id int [ref: > users.id]
  body string
}
`)
	entry := writeContextFile(t, dir, "index.dbml", "reuse * from './owned'\n")

	all, diags, err := LoadContext(entry, nil, ContextAll)
	if err != nil || len(diags) != 0 {
		t.Fatalf("all context: err=%v diags=%v", err, diags)
	}
	if len(all.Tables) != 3 || len(all.Refs) != 1 {
		t.Fatalf("all context = %d tables, %d refs", len(all.Tables), len(all.Refs))
	}

	refs, diags, err := LoadContext(entry, nil, ContextRefs)
	if err != nil || len(diags) != 0 {
		t.Fatalf("refs context: err=%v diags=%v", err, diags)
	}
	if len(refs.Tables) != 2 || len(refs.Refs) != 1 {
		t.Fatalf("refs context = %d tables, %d refs", len(refs.Tables), len(refs.Refs))
	}
	users := refs.Lookup("users")
	if users == nil || !users.External || len(users.Columns) != 1 || users.Columns[0].Name != "id" {
		t.Fatalf("users context stub = %#v", users)
	}
	if refs.Lookup("teams") != nil {
		t.Fatal("unreferenced context table teams should be excluded")
	}

	none, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(none.Tables) != 1 || none.Tables[0].Name != "posts" || len(none.Refs) != 0 {
		t.Fatalf("no context = %#v", none)
	}
}

func TestParseContext(t *testing.T) {
	for input, want := range map[string]Context{
		"all": ContextAll, "refs": ContextRefs, "referenced": ContextRefs, "none": ContextNone,
	} {
		got, ok := ParseContext(input)
		if !ok || got != want {
			t.Fatalf("ParseContext(%q) = %v, %v; want %v, true", input, got, ok, want)
		}
	}
	if _, ok := ParseContext("some"); ok {
		t.Fatal("invalid context should be rejected")
	}
}

func writeContextFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
