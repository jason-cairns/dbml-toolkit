package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRenamePropagates(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	src := "Table users {\n  id int [pk]\n}\nTable posts {\n  author int [ref: > users.id]\n}\n"
	docs := map[string]string{path: src}
	// Position on the "users" table definition (line 0, char 6).
	res := call(t, docs, "textDocument/rename", map[string]any{
		"textDocument": map[string]string{"uri": pathToURI(path)},
		"position":     map[string]int{"line": 0, "character": 6},
		"newName":      "members",
	})
	var edit struct {
		Changes map[string][]textEdit `json:"changes"`
	}
	json.Unmarshal(res, &edit)
	edits := edit.Changes[pathToURI(path)]
	if len(edits) < 2 {
		t.Fatalf("rename should touch the definition and the reference, got %s", res)
	}
	for _, e := range edits {
		if e.NewText != "members" {
			t.Fatalf("edit should insert the new name, got %q", e.NewText)
		}
	}
}
