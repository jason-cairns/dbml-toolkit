// Package resolver implements the DBML module system: starting from an entry
// file it follows `use`/`reuse` imports, parses every reachable file once
// (cycles are safe), and builds a single merged model.Schema.
package resolver

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/parser"
)

// Load reads the entry file from disk and resolves the whole module graph.
func Load(entry string) (*model.Schema, []model.Diagnostic, error) {
	s, _, d, err := Graph(entry, nil)
	return s, d, err
}

// LoadSource is like Load but consults overlay (path -> content) before disk,
// so the LSP can resolve against unsaved editor buffers.
func LoadSource(entry string, overlay map[string]string) (*model.Schema, []model.Diagnostic, error) {
	s, _, d, err := Graph(entry, overlay)
	return s, d, err
}

// Graph resolves the module graph and additionally returns every parsed file
// keyed by absolute path, so callers (e.g. the LSP) can inspect source spans.
func Graph(entry string, overlay map[string]string) (*model.Schema, map[string]*ast.File, []model.Diagnostic, error) {
	entry, _ = filepath.Abs(entry)
	files := map[string]*ast.File{}
	var order []string
	var diags []model.Diagnostic

	read := func(path string) (string, error) {
		if overlay != nil {
			if c, ok := overlay[path]; ok {
				return c, nil
			}
		}
		b, err := os.ReadFile(path)
		return string(b), err
	}

	var visit func(path string) error
	visit = func(path string) error {
		if _, done := files[path]; done {
			return nil
		}
		src, err := read(path)
		if err != nil {
			return err
		}
		f, pdiags := parser.Parse(path, src)
		files[path] = f
		order = append(order, path)
		for _, d := range pdiags {
			diags = append(diags, model.Diagnostic{Pos: d.Pos, End: d.End, Msg: d.Msg})
		}
		for _, imp := range f.Imports {
			child := ResolvePath(path, imp.Path)
			if err := visit(child); err != nil {
				diags = append(diags, model.Diagnostic{Pos: imp.Pos, End: imp.Pos,
					Msg: "cannot import " + imp.Path + ": " + err.Error()})
			}
		}
		return nil
	}
	if err := visit(entry); err != nil {
		return nil, files, diags, err
	}

	ordered := make([]*ast.File, 0, len(order))
	aliases := map[string]string{}
	for _, p := range order {
		f := files[p]
		ordered = append(ordered, f)
		for _, imp := range f.Imports {
			for _, it := range imp.Items {
				if it.Alias != "" {
					aliases[it.Alias] = it.Name
				}
			}
		}
	}
	schema, bdiags := model.Build(ordered, aliases)
	diags = append(diags, bdiags...)
	return schema, files, diags, nil
}

// ResolvePath resolves an import path relative to the importing file, adding
// the .dbml extension when omitted.
func ResolvePath(from, rel string) string {
	if !strings.HasSuffix(rel, ".dbml") {
		rel += ".dbml"
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(filepath.Dir(from), rel)
}
