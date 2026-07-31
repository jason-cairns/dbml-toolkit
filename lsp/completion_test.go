package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/resolver"
)

// markerPos strips the "@@" cursor marker from src and returns the cleaned
// source together with the 0-based position where the marker stood.
func markerPos(src string) (string, position) {
	i := strings.Index(src, "@@")
	if i < 0 {
		panic("no @@ marker in source")
	}
	before := src[:i]
	line := strings.Count(before, "\n")
	char := len(before) - (strings.LastIndex(before, "\n") + 1)
	return strings.Replace(src, "@@", "", 1), position{Line: line, Char: char}
}

// completeAt resolves src (with an @@ marker) and returns the completion labels
// offered at the marker.
func completeAt(t *testing.T, src string) []string {
	t.Helper()
	src, pos := markerPos(src)
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: src}
	schema, files, _, _ := resolver.Graph(path, docs)
	s := &Server{docs: docs}
	items := s.complete(path, src, files[path], schema, pos)
	labels := make([]string, len(items))
	for i, it := range items {
		labels[i] = it.Label
	}
	return labels
}

func has(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestCompleteTableNamesInGroup(t *testing.T) {
	labels := completeAt(t, "Table users {\n  id int\n}\nTable posts {\n  id int\n}\nTableGroup g {\n  us@@\n}\n")
	if !has(labels, "users") || !has(labels, "posts") {
		t.Fatalf("group body should suggest table names, got %v", labels)
	}
}

func TestCompleteTypesAndEnumsInTypePosition(t *testing.T) {
	labels := completeAt(t, "Enum status {\n  active\n}\nTable t {\n  col @@\n}\n")
	if !has(labels, "int") {
		t.Fatalf("type position should suggest builtin types, got %v", labels)
	}
	if !has(labels, "status") {
		t.Fatalf("type position should suggest enums, got %v", labels)
	}
}

func TestCompleteColumnsAfterDotInRef(t *testing.T) {
	labels := completeAt(t, "Table users {\n  id int\n  name text\n}\nRef r {\n  posts.author > users.@@\n}\n")
	if !has(labels, "id") || !has(labels, "name") {
		t.Fatalf("ref endpoint after dot should suggest columns, got %v", labels)
	}
}

func TestCompleteTablesInSchemaAfterDot(t *testing.T) {
	labels := completeAt(t, "Table app.users {\n  id int\n}\nTableGroup g {\n  app.@@\n}\n")
	if !has(labels, "users") {
		t.Fatalf("schema-qualified dot should suggest tables in schema, got %v", labels)
	}
	if has(labels, "app.users") {
		t.Fatalf("after `app.` the schema should not be repeated, got %v", labels)
	}
}

func TestCompleteColumnSettings(t *testing.T) {
	labels := completeAt(t, "Table t {\n  id int [@@]\n}\n")
	if !has(labels, "pk") || !has(labels, "not null") {
		t.Fatalf("column brackets should suggest settings, got %v", labels)
	}
}

func TestCompleteRefSettingValues(t *testing.T) {
	labels := completeAt(t, "Table users {\n  id int\n}\nRef r {\n  a.b > users.id [delete: @@]\n}\n")
	if !has(labels, "cascade") {
		t.Fatalf("delete: should suggest referential actions, got %v", labels)
	}
}

func TestCompleteTopLevelKeywords(t *testing.T) {
	labels := completeAt(t, "@@\n")
	if !has(labels, "Table") || !has(labels, "TableGroup") {
		t.Fatalf("top level should suggest block keywords, got %v", labels)
	}
}

func TestCompleteInlineRefTable(t *testing.T) {
	labels := completeAt(t, "Table users {\n  id int\n}\nTable posts {\n  author int [ref: > us@@]\n}\n")
	if !has(labels, "users") {
		t.Fatalf("inline ref should suggest tables, got %v", labels)
	}
}
