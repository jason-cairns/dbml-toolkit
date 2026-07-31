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

// A schema-qualified table named in an endpoint without a column (e.g. mid-edit
// `> dim.date` before `.id` is typed) must resolve to that table rather than be
// misread as table `dim` column `date`.
func TestSchemaQualifiedEndpointDisambiguation(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", `
Table dim.date {
  id int [pk]
}
Table fact.f {
  d date [ref: > dim.date]
  e date [ref: > dim.date.id]
}
`)
	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, d := range diags {
		if contains(d.Msg, "unknown table") {
			t.Fatalf("dim.date should resolve, got diagnostic: %s", d.Msg)
		}
	}
	// Both endpoints must point at the dim.date table.
	for _, r := range schema.Refs {
		if r.To.Table == nil || r.To.Table.Name != "date" || r.To.Table.Schema != "dim" {
			t.Fatalf("endpoint did not resolve to dim.date: %+v", r.To)
		}
	}
	// The column form keeps its column; the bare form has none.
	var withCol, without int
	for _, r := range schema.Refs {
		if len(r.To.Columns) == 1 && r.To.Columns[0] == "id" {
			withCol++
		}
		if len(r.To.Columns) == 0 {
			without++
		}
	}
	if withCol != 1 || without != 1 {
		t.Fatalf("expected one endpoint with column id and one without, got with=%d without=%d", withCol, without)
	}
}

// A genuinely missing table still reports an unknown-table diagnostic.
func TestUnknownTableStillDiagnosed(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", `
Table users { id int [pk] }
Ref: users.id > nope.id
`)
	_, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found bool
	for _, d := range diags {
		if contains(d.Msg, "unknown table in relationship: nope") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unknown-table diagnostic for nope, got %v", diags)
	}
}
