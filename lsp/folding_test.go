package lsp

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestFoldingRanges(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "Table t {\n  id int\n}\n"}
	res := call(t, docs, "textDocument/foldingRange",
		map[string]any{"textDocument": map[string]string{"uri": pathToURI(path)}})
	var folds []foldingRange
	json.Unmarshal(res, &folds)
	if len(folds) != 1 || folds[0].StartLine != 0 || folds[0].EndLine != 1 {
		t.Fatalf("expected fold 0..1, got %s", res)
	}
}
