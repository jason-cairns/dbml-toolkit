package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestDocumentSymbolOutline(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table users {\n  id int\n  name text\n}\n"}
	res := call(t, docs, "textDocument/documentSymbol",
		map[string]any{"textDocument": map[string]string{"uri": pathToURI(path)}})
	var syms []documentSymbol
	json.Unmarshal(res, &syms)
	if len(syms) != 1 || syms[0].Name != "public.users" {
		t.Fatalf("expected one table symbol, got %s", res)
	}
	if len(syms[0].Children) != 2 {
		t.Fatalf("table should have 2 column children, got %d", len(syms[0].Children))
	}
}
