package lsp

import (
	"path/filepath"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/resolver"
)

func TestLintUnknownGroupMember(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table users {\n  id int\n}\nTableGroup g {\n  ghost\n}\n"}
	schema, files, _, _ := resolver.Graph(path, docs)
	s := &Server{docs: docs}
	diags := s.lints(path, schema, files)
	if !hasDiag(diags, "unknown table in group: public.ghost") {
		t.Fatalf("expected unknown-group-member lint, got %+v", diags)
	}
}

func TestLintDuplicateColumnAndBadColor(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table t [headercolor: #12] {\n  id int\n  id text\n}\n"}
	schema, files, _, _ := resolver.Graph(path, docs)
	s := &Server{docs: docs}
	diags := s.lints(path, schema, files)
	if !hasDiag(diags, "duplicate column: id") {
		t.Fatalf("expected duplicate-column lint, got %+v", diags)
	}
	if !hasDiag(diags, "invalid color literal: #12") {
		t.Fatalf("expected invalid-color lint, got %+v", diags)
	}
}

func TestLintUnknownRefColumn(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table users {\n  id int\n}\nTable posts {\n  author int [ref: > users.nope]\n}\n"}
	schema, files, _, _ := resolver.Graph(path, docs)
	s := &Server{docs: docs}
	diags := s.lints(path, schema, files)
	if !hasDiag(diags, "unknown column nope in public.users") {
		t.Fatalf("expected unknown-ref-column lint, got %+v", diags)
	}
}

func hasDiag(diags []diagnostic, msg string) bool {
	for _, d := range diags {
		if d.Message == msg {
			return true
		}
	}
	return false
}
