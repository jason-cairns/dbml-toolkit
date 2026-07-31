package lsp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormattingReturnsFullEdit(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	src := "Table users{\nid int [pk]\nemail varchar\n}\n"
	docs := map[string]string{path: src}
	res := call(t, docs, "textDocument/formatting", map[string]any{
		"textDocument": map[string]string{"uri": pathToURI(path)},
	})
	var edits []textEdit
	if err := json.Unmarshal(res, &edits); err != nil {
		t.Fatalf("bad reply %s: %v", res, err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected a single full-document edit, got %d: %s", len(edits), res)
	}
	if !strings.Contains(edits[0].NewText, "  id    int [pk]") {
		t.Errorf("formatted text not canonical:\n%s", edits[0].NewText)
	}
	// The edit range must start at the top of the document.
	if edits[0].Range.Start.Line != 0 || edits[0].Range.Start.Char != 0 {
		t.Errorf("edit should start at 0:0, got %+v", edits[0].Range.Start)
	}
}

func TestFormattingLeavesBrokenFileUntouched(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table users {\n  id int [\n"}
	res := call(t, docs, "textDocument/formatting", map[string]any{
		"textDocument": map[string]string{"uri": pathToURI(path)},
	})
	if strings.TrimSpace(string(res)) != "null" {
		t.Fatalf("broken file should yield a null reply, got %s", res)
	}
}

func TestFormattingNoChangeYieldsNull(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	// Already-canonical source: the server should reply null (no edits).
	docs := map[string]string{path: "Table users {\n  id int [pk]\n}\n"}
	res := call(t, docs, "textDocument/formatting", map[string]any{
		"textDocument": map[string]string{"uri": pathToURI(path)},
	})
	if strings.TrimSpace(string(res)) != "null" {
		t.Fatalf("no-op format should yield null, got %s", res)
	}
}
