package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModuleImportsAndCycle(t *testing.T) {
	dir := t.TempDir()
	// base defines users; main imports base and references it (with an alias)
	// and base re-uses main => a cycle, which must be handled safely.
	write(t, dir, "base.dbml", `
use * from './main'
Table users {
  id int [pk]
}
`)
	entry := write(t, dir, "main.dbml", `
use { table users as members } from './base'
Table posts {
  id int [pk]
  author int [ref: > members.id]
}
`)

	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(schema.Tables) != 2 {
		t.Fatalf("want 2 tables across files, got %d", len(schema.Tables))
	}
	// The alias `members` must resolve to the users table across files.
	if schema.Lookup("members") == nil {
		t.Fatalf("alias members did not resolve; keys unresolved")
	}
	// The cross-file relationship must resolve without an "unknown table" diag.
	for _, d := range diags {
		if d.Msg != "" && contains(d.Msg, "unknown table") {
			t.Fatalf("unexpected unresolved ref: %s", d.Msg)
		}
	}
	var resolved bool
	for _, r := range schema.Refs {
		if r.To.Table != nil && r.To.Table.Name == "users" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("cross-file ref endpoint did not resolve to users")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
