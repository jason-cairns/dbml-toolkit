package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/resolver"
)

func TestCompletionCarriesNotes(t *testing.T) {
	src := "Table users {\n  id int [note: 'the primary key']\n}\nTableGroup g {\n  us@@\n}\n"
	src, pos := markerPos(src)
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: src}
	schema, files, _, _ := resolver.Graph(path, docs)
	s := &Server{docs: docs}
	items := s.complete(path, src, files[path], schema, pos)

	var users *completionItem
	for i := range items {
		if items[i].Label == "users" {
			users = &items[i]
		}
	}
	if users == nil {
		t.Fatalf("users not offered: %v", items)
	}
	// The table itself has no note here; documentation should be absent.
	if users.Documentation != nil {
		t.Fatalf("expected no documentation for note-less table, got %v", users.Documentation)
	}
}

func TestColumnCompletionShowsNoteAndFlags(t *testing.T) {
	src := "Table users {\n  id int [pk, note: 'the primary key']\n}\nRef r {\n  a.b > users.@@\n}\n"
	src, pos := markerPos(src)
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: src}
	schema, files, _, _ := resolver.Graph(path, docs)
	s := &Server{docs: docs}
	items := s.complete(path, src, files[path], schema, pos)

	for _, it := range items {
		if it.Label != "id" {
			continue
		}
		if !strings.Contains(it.Detail, "primary key") {
			t.Fatalf("column detail should include flags, got %q", it.Detail)
		}
		doc, _ := it.Documentation.(map[string]any)
		if doc == nil || doc["value"] != "the primary key" {
			t.Fatalf("column documentation should carry the note, got %v", it.Documentation)
		}
		return
	}
	t.Fatalf("id column not offered: %v", items)
}

func TestHoverShowsTableNote(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table users {\n  id int\n  Note: 'core user records'\n}\n"}
	schema, _, _, _ := resolver.Graph(path, docs)
	md := hoverMarkdown(schema, "public.users")
	if !strings.Contains(md, "core user records") {
		t.Fatalf("table hover should include its note, got %q", md)
	}
}
