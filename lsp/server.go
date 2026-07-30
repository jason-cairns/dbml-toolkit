// Package lsp is a small language server for DBML providing diagnostics,
// go-to-definition, find-references and hover — all resolved across the module
// graph so navigation works cross-file. It speaks LSP over stdio.
package lsp

import (
	"encoding/json"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

// --- LSP wire types (minimal subset) ---------------------------------------

type position struct {
	Line int `json:"line"`
	Char int `json:"character"`
}
type rng struct {
	Start position `json:"start"`
	End   position `json:"end"`
}
type location struct {
	URI   string `json:"uri"`
	Range rng    `json:"range"`
}
type textDocID struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}
type docParams struct {
	TextDocument textDocID `json:"textDocument"`
}
type posParams struct {
	TextDocument textDocID `json:"textDocument"`
	Position     position  `json:"position"`
}
type changeParams struct {
	TextDocument   textDocID `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}
type diagnostic struct {
	Range    rng    `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
}

// Server holds the open-document overlay.
type Server struct {
	conn *conn
	docs map[string]string // path -> content
}

// Serve runs the language server until the input stream closes.
func Serve(r io.Reader, w io.Writer) error {
	s := &Server{conn: newConn(r, w), docs: map[string]string{}}
	for {
		m, err := s.conn.read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.handle(m)
	}
}

func (s *Server) handle(m *message) {
	switch m.Method {
	case "initialize":
		s.conn.reply(m.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":   1, // full sync
				"definitionProvider": true,
				"referencesProvider": true,
				"hoverProvider":      true,
			},
			"serverInfo": map[string]any{"name": "dbml-lsp"},
		})
	case "initialized", "$/setTrace":
		// no-op
	case "shutdown":
		s.conn.reply(m.ID, nil)
	case "textDocument/didOpen":
		var p docParams
		json.Unmarshal(m.Params, &p)
		s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
	case "textDocument/didChange":
		var p changeParams
		json.Unmarshal(m.Params, &p)
		if len(p.ContentChanges) > 0 {
			s.setDoc(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
		}
	case "textDocument/definition":
		s.onDefinition(m)
	case "textDocument/references":
		s.onReferences(m)
	case "textDocument/hover":
		s.onHover(m)
	default:
		if len(m.ID) > 0 {
			s.conn.reply(m.ID, nil)
		}
	}
}

func (s *Server) setDoc(uri, text string) {
	path := uriToPath(uri)
	s.docs[path] = text
	s.publishDiagnostics(uri)
}

func (s *Server) publishDiagnostics(uri string) {
	path := uriToPath(uri)
	_, _, diags, _ := resolver.Graph(path, s.docs)
	out := []diagnostic{}
	for _, d := range diags {
		if d.Pos.File != path {
			continue
		}
		out = append(out, diagnostic{
			Range:    rangeFor(d.Pos.Line, d.Pos.Col, 1),
			Severity: 1, // error
			Message:  d.Msg,
		})
	}
	s.conn.notify("textDocument/publishDiagnostics", map[string]any{
		"uri": uri, "diagnostics": out,
	})
}

// --- navigation -------------------------------------------------------------

func (s *Server) onDefinition(m *message) {
	var p posParams
	json.Unmarshal(m.Params, &p)
	idx := s.index(p.TextDocument.URI)
	if o := idx.at(uriToPath(p.TextDocument.URI), p.Position); o != nil {
		if loc, ok := idx.defs[o.target]; ok {
			s.conn.reply(m.ID, loc)
			return
		}
	}
	s.conn.reply(m.ID, nil)
}

func (s *Server) onReferences(m *message) {
	var p posParams
	json.Unmarshal(m.Params, &p)
	idx := s.index(p.TextDocument.URI)
	var out []location
	if o := idx.at(uriToPath(p.TextDocument.URI), p.Position); o != nil {
		for _, occ := range idx.occs {
			if occ.target == o.target {
				out = append(out, occ.loc)
			}
		}
	}
	s.conn.reply(m.ID, out)
}

func (s *Server) onHover(m *message) {
	var p posParams
	json.Unmarshal(m.Params, &p)
	idx := s.index(p.TextDocument.URI)
	if o := idx.at(uriToPath(p.TextDocument.URI), p.Position); o != nil {
		if t := idx.schema.Lookup(o.target); t != nil {
			md := "**Table** `" + t.Qualified() + "`"
			if t.Note != "" {
				md += "\n\n" + t.Note
			}
			s.conn.reply(m.ID, map[string]any{
				"contents": map[string]any{"kind": "markdown", "value": md},
			})
			return
		}
	}
	s.conn.reply(m.ID, nil)
}

// --- occurrence index -------------------------------------------------------

type occurrence struct {
	file   string
	line   int // 0-based
	char   int // 0-based
	length int
	target string // qualified table key
	loc    location
}

type index struct {
	schema *model.Schema
	occs   []occurrence
	defs   map[string]location
}

func (ix *index) at(file string, p position) *occurrence {
	for i := range ix.occs {
		o := &ix.occs[i]
		if o.file == file && o.line == p.Line && p.Char >= o.char && p.Char < o.char+o.length {
			return o
		}
	}
	return nil
}

// index resolves the module graph rooted at uri and builds the occurrence set.
func (s *Server) index(uri string) *index {
	path := uriToPath(uri)
	schema, files, _, _ := resolver.Graph(path, s.docs)
	ix := &index{schema: schema, defs: map[string]location{}}
	if schema == nil {
		return ix
	}
	key := func(name string) string {
		if t := schema.Lookup(name); t != nil {
			return t.Qualified()
		}
		return name
	}
	for _, t := range schema.Tables {
		ix.defs[t.Qualified()] = locFor(t.NamePos.File, t.NamePos.Line, t.NamePos.Col, len(t.Name))
	}
	for p, f := range files {
		add := func(e ast.Endpoint) {
			if e.Table == "" {
				return
			}
			ix.occs = append(ix.occs, mkOcc(p, e.Pos.Line, e.Pos.Col, len(e.Table), key(qual(e.Schema, e.Table))))
		}
		for _, t := range f.Tables {
			ix.occs = append(ix.occs, mkOcc(p, t.NamePos.Line, t.NamePos.Col, len(t.Name), key(qual(t.Schema, t.Name))))
		}
		for _, r := range f.Refs {
			if !r.Inline {
				add(r.Left)
			}
			add(r.Right)
		}
		for _, g := range f.Groups {
			for _, mem := range g.Members {
				ix.occs = append(ix.occs, mkOcc(p, mem.Pos.Line, mem.Pos.Col, len(mem.Table), key(qual(mem.Schema, mem.Table))))
			}
		}
	}
	return ix
}

func mkOcc(file string, line, col, length int, target string) occurrence {
	return occurrence{
		file: file, line: line - 1, char: col - 1, length: length, target: target,
		loc: locFor(file, line, col, length),
	}
}

// --- helpers ----------------------------------------------------------------

func qual(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

func rangeFor(line, col, length int) rng {
	return rng{
		Start: position{Line: line - 1, Char: col - 1},
		End:   position{Line: line - 1, Char: col - 1 + length},
	}
}

func locFor(file string, line, col, length int) location {
	return location{URI: pathToURI(file), Range: rangeFor(line, col, length)}
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return strings.TrimPrefix(uri, "file://")
	}
	p, _ := url.PathUnescape(u.Path)
	return p
}

func pathToURI(path string) string {
	abs, _ := filepath.Abs(path)
	return "file://" + (&url.URL{Path: abs}).EscapedPath()
}
