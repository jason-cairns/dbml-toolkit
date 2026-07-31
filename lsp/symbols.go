package lsp

import (
	"encoding/json"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/resolver"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// LSP SymbolKind subset.
const (
	skNamespace = 3
	skEnum      = 10
	skField     = 8
	skEnumMem   = 22
	skStruct    = 23
)

type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          rng              `json:"range"`
	SelectionRange rng              `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
}

// onDocumentSymbol returns a hierarchical outline of the current file: tables
// with their columns, enums with their values, and table groups.
func (s *Server) onDocumentSymbol(m *message) {
	var p docParams
	json.Unmarshal(m.Params, &p)
	path := uriToPath(p.TextDocument.URI)
	_, files, _, _ := resolver.Graph(path, s.docs)
	f := files[path]
	if f == nil {
		s.conn.reply(m.ID, []documentSymbol{})
		return
	}
	out := []documentSymbol{}

	for _, t := range f.Tables {
		sym := documentSymbol{
			Name:           ast.QualifiedName(t.Schema, t.Name),
			Kind:           skStruct,
			SelectionRange: spanRange(t.NamePos, len(t.Name)),
		}
		endLine := t.NamePos.Line
		for _, c := range t.Columns {
			sym.Children = append(sym.Children, documentSymbol{
				Name:           c.Name,
				Detail:         c.Type,
				Kind:           skField,
				Range:          spanRange(c.NamePos, len(c.Name)),
				SelectionRange: spanRange(c.NamePos, len(c.Name)),
			})
			if c.NamePos.Line > endLine {
				endLine = c.NamePos.Line
			}
		}
		for _, idx := range t.Indexes {
			for _, fld := range idx.Fields {
				if fld.Pos.Line > endLine {
					endLine = fld.Pos.Line
				}
			}
		}
		sym.Range = blockRange(t.NamePos, endLine)
		out = append(out, sym)
	}

	for _, e := range f.Enums {
		sym := documentSymbol{
			Name:           ast.QualifiedName(e.Schema, e.Name),
			Kind:           skEnum,
			SelectionRange: spanRange(e.NamePos, len(e.Name)),
		}
		endLine := e.NamePos.Line
		for _, v := range e.Values {
			sym.Children = append(sym.Children, documentSymbol{
				Name:           v.Name,
				Kind:           skEnumMem,
				Range:          spanRange(v.Pos, len(v.Name)),
				SelectionRange: spanRange(v.Pos, len(v.Name)),
			})
			if v.Pos.Line > endLine {
				endLine = v.Pos.Line
			}
		}
		sym.Range = blockRange(e.NamePos, endLine)
		out = append(out, sym)
	}

	for _, g := range f.Groups {
		sym := documentSymbol{
			Name:           g.Name,
			Kind:           skNamespace,
			SelectionRange: spanRange(g.NamePos, len(g.Name)),
		}
		endLine := g.NamePos.Line
		for _, mem := range g.Members {
			sym.Children = append(sym.Children, documentSymbol{
				Name:           ast.QualifiedName(mem.Schema, mem.Table),
				Kind:           skStruct,
				Range:          spanRange(mem.Pos, len(mem.Table)),
				SelectionRange: spanRange(mem.Pos, len(mem.Table)),
			})
			if mem.Pos.Line > endLine {
				endLine = mem.Pos.Line
			}
		}
		sym.Range = blockRange(g.NamePos, endLine)
		out = append(out, sym)
	}

	s.conn.reply(m.ID, out)
}

// spanRange is the range covering length characters from pos.
func spanRange(pos token.Pos, length int) rng {
	return rangeFor(pos.Line, pos.Col, length)
}

// blockRange spans from a block's name down to the last line it encloses, so
// clients that require a parent range to contain its children are satisfied.
func blockRange(name token.Pos, endLine int) rng {
	return rng{
		Start: position{Line: name.Line - 1, Char: 0},
		End:   position{Line: endLine - 1, Char: 0},
	}
}
