package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// A schema-qualified table used only in an endpoint (no column typed yet) must
// still be navigable — go-to-definition on `dim.date` jumps to the table.
func TestDefinitionOnSchemaQualifiedEndpoint(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	src := "Table dim.date {\n  id int [pk]\n}\nTable fact.f {\n  d date [ref: > dim.date]\n}\n"
	docs := map[string]string{path: src}
	// Cursor on the "date" segment of `dim.date` in the ref endpoint (line 4).
	res := call(t, docs, "textDocument/definition", map[string]any{
		"textDocument": map[string]string{"uri": pathToURI(path)},
		"position":     map[string]int{"line": 4, "character": 20},
	})
	if string(res) == "null" {
		t.Fatalf("go-to-definition on dim.date returned null; want the table definition")
	}
	var loc location
	if err := json.Unmarshal(res, &loc); err != nil {
		t.Fatalf("bad reply %s: %v", res, err)
	}
	if loc.Range.Start.Line != 0 {
		t.Fatalf("definition should point at the `Table dim.date` line (0), got %+v", loc.Range.Start)
	}
}

// And the column part of a fully-qualified endpoint still resolves to the column.
func TestDefinitionOnQualifiedEndpointColumn(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	src := "Table dim.date {\n  id int [pk]\n}\nTable fact.f {\n  d date [ref: > dim.date.id]\n}\n"
	docs := map[string]string{path: src}
	// Cursor on the trailing `id` column (line 4).
	res := call(t, docs, "textDocument/definition", map[string]any{
		"textDocument": map[string]string{"uri": pathToURI(path)},
		"position":     map[string]int{"line": 4, "character": 26},
	})
	var loc location
	if err := json.Unmarshal(res, &loc); err != nil || string(res) == "null" {
		t.Fatalf("column endpoint should resolve, got %s", res)
	}
	if loc.Range.Start.Line != 1 {
		t.Fatalf("definition should point at the `id` column line (1), got %+v", loc.Range.Start)
	}
}

func TestCompleteColumnsAfterSchemaQualifiedInlineRef(t *testing.T) {
	src := "Table dim.date {\n  id int\n  d date\n}\nTable f {\n  x int [ref: > dim.date.@@]\n}\n"
	labels := completeAt(t, src)
	if !has(labels, "id") || !has(labels, "d") {
		t.Fatalf("inline ref after `dim.date.` should suggest that table's columns, got %v", labels)
	}
}

func TestCompleteTablesAfterSchemaInInlineRef(t *testing.T) {
	src := "Table dim.date {\n  id int\n}\nTable dim.vehicle {\n  id int\n}\nTable f {\n  x int [ref: > dim.@@]\n}\n"
	labels := completeAt(t, src)
	if !has(labels, "date") || !has(labels, "vehicle") {
		t.Fatalf("inline ref after `dim.` should suggest tables in schema dim, got %v", labels)
	}
}

func TestCompleteColumnsAfterSchemaQualifiedStandaloneRef(t *testing.T) {
	src := "Table dim.date {\n  id int\n  d date\n}\nTable f {\n  x int\n}\nRef r {\n  f.x > dim.date.@@\n}\n"
	labels := completeAt(t, src)
	if !has(labels, "id") || !has(labels, "d") {
		t.Fatalf("standalone ref after `dim.date.` should suggest columns, got %v", labels)
	}
}
