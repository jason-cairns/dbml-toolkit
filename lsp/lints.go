package lsp

import (
	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// Diagnostic severities beyond the error level already used for parse/resolve
// failures.
const (
	sevWarning = 2
	sevInfo    = 3
)

// lints computes editor-only warnings for the file at path: references that do
// not resolve, duplicate columns, malformed colours and dead partials. These
// are surfaced only in the LSP, leaving the CLI's diagnostics untouched.
func (s *Server) lints(path string, schema *model.Schema, files map[string]*ast.File) []diagnostic {
	f := files[path]
	if f == nil || schema == nil {
		return nil
	}
	var out []diagnostic

	// Group members that reference a table the project doesn't define.
	for _, g := range f.Groups {
		for _, mem := range g.Members {
			name := ast.QualifiedName(mem.Schema, mem.Table)
			if mem.Schema == "" {
				name = mem.Table
			}
			if schema.Lookup(name) == nil {
				out = append(out, warnAt(mem.Pos, len(mem.Table),
					"unknown table in group: "+ast.QualifiedName(mem.Schema, mem.Table)))
			}
		}
	}

	// Relationship endpoints naming a column their table doesn't have.
	for _, r := range f.Refs {
		out = append(out, refColumnLints(schema, r.Left)...)
		out = append(out, refColumnLints(schema, r.Right)...)
	}

	// Duplicate column names within a table.
	for _, t := range f.Tables {
		seen := map[string]bool{}
		for _, c := range t.Columns {
			if seen[c.Name] {
				out = append(out, warnAt(c.NamePos, len(c.Name), "duplicate column: "+c.Name))
			}
			seen[c.Name] = true
		}
	}

	// Malformed colour literals.
	for _, tok := range lexAll(path, s.docs[path]) {
		if tok.Kind == token.Color {
			if _, ok := parseHexColor(tok.Lit); !ok {
				out = append(out, warnAt(tok.Pos, len(tok.Lit), "invalid color literal: "+tok.Lit))
			}
		}
	}

	// Partials defined here but injected nowhere in the project.
	if used := injectedPartials(files); used != nil {
		for _, tp := range f.Partials {
			if !used[tp.Name] {
				out = append(out, diagnostic{
					Range:    rangeFor(tp.NamePos.Line, tp.NamePos.Col, len(tp.Name)),
					Severity: sevInfo,
					Message:  "unused table partial: ~" + tp.Name,
				})
			}
		}
	}

	return out
}

func refColumnLints(schema *model.Schema, e ast.Endpoint) []diagnostic {
	if e.Table == "" {
		return nil
	}
	name := e.Table
	if e.Schema != "" {
		name = e.Schema + "." + e.Table
	}
	t := schema.Lookup(name)
	if t == nil {
		return nil // the missing table is already reported as an error
	}
	has := map[string]bool{}
	for _, c := range t.Columns {
		has[c.Name] = true
	}
	var out []diagnostic
	for i, col := range e.Columns {
		if has[col] || i >= len(e.ColPos) {
			continue
		}
		out = append(out, warnAt(e.ColPos[i], len(col),
			"unknown column "+col+" in "+t.Qualified()))
	}
	return out
}

// injectedPartials collects every partial name injected by a table anywhere in
// the project.
func injectedPartials(files map[string]*ast.File) map[string]bool {
	used := map[string]bool{}
	for _, f := range files {
		for _, t := range f.Tables {
			for _, name := range t.Injects {
				used[name] = true
			}
		}
	}
	return used
}

func warnAt(pos token.Pos, length int, msg string) diagnostic {
	return diagnostic{
		Range:    rangeFor(pos.Line, pos.Col, length),
		Severity: sevWarning,
		Message:  msg,
	}
}
