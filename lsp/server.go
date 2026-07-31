// Package lsp is a small language server for DBML providing diagnostics,
// go-to-definition, find-references and hover — all resolved across the module
// graph so navigation works cross-file. It speaks LSP over stdio.
package lsp

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/d2"
	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/preview"
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

// Server holds the open-document overlay and the live preview.
type Server struct {
	conn        *conn
	docs        map[string]string // path -> content
	preview     *preview.Server   // live browser preview (nil if disabled)
	previewOpen bool              // auto-open the browser on first document
}

// Serve runs the language server until the input stream closes. Opening a
// document launches a live browser preview that re-renders from the editor
// buffer on every change. The DBML_PREVIEW env var tunes this:
//   - "0"/"off"/"false": no preview
//   - "manual"/"noopen": serve the preview but do not open a browser
//   - anything else (default): serve and auto-open the browser
func Serve(r io.Reader, w io.Writer) error {
	s := &Server{conn: newConn(r, w), docs: map[string]string{}, previewOpen: true}
	switch strings.ToLower(os.Getenv("DBML_PREVIEW")) {
	case "0", "off", "false":
		// preview disabled
	case "manual", "noopen":
		s.preview = preview.New(d2.New(), diagram.Options{})
		s.previewOpen = false
	default:
		s.preview = preview.New(d2.New(), diagram.Options{})
	}
	// Tie the preview's lifetime to the editor connection: when the stream
	// closes (or the client stops reading and our writes start failing), tear
	// the HTTP server down instead of leaving an orphaned preview serving a
	// stale diagram.
	defer s.preview.Close()
	for {
		m, err := s.conn.read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.handle(m)
		// A failed write means the client is gone; stop rather than block
		// forever in the next read with a preview no one is driving.
		if s.conn.werr != nil {
			return s.conn.werr
		}
	}
}

func (s *Server) handle(m *message) {
	switch m.Method {
	case "initialize":
		s.conn.reply(m.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":       1, // full sync
				"definitionProvider":     true,
				"referencesProvider":     true,
				"hoverProvider":          true,
				"colorProvider":          true,
				"completionProvider":     map[string]any{"triggerCharacters": []string{".", "~"}},
				"documentSymbolProvider": true,
				"foldingRangeProvider":   true,
				"renameProvider":         map[string]any{"prepareProvider": true},
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
	case "textDocument/completion":
		s.onCompletion(m)
	case "textDocument/documentSymbol":
		s.onDocumentSymbol(m)
	case "textDocument/foldingRange":
		s.onFoldingRange(m)
	case "textDocument/prepareRename":
		s.onPrepareRename(m)
	case "textDocument/rename":
		s.onRename(m)
	case "textDocument/documentColor":
		s.onDocumentColor(m)
	case "textDocument/colorPresentation":
		s.onColorPresentation(m)
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
	s.updatePreview(path)
}

// updatePreview lazily launches the browser preview (opening the browser once)
// and re-renders it from the current editor buffers.
func (s *Server) updatePreview(path string) {
	if s.preview == nil {
		return
	}
	if _, err := s.preview.Listen(0, s.previewOpen); err != nil {
		return
	}
	s.preview.Render(path, s.docs)
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
		if md := hoverMarkdown(idx.schema, o.target); md != "" {
			s.conn.reply(m.ID, map[string]any{
				"contents": map[string]any{"kind": "markdown", "value": md},
			})
			return
		}
	}
	s.conn.reply(m.ID, nil)
}

// hoverMarkdown renders the hover card for an occurrence target, which is
// either a table key or a "col:<qualified-table>.<column>" key.
func hoverMarkdown(schema *model.Schema, target string) string {
	if schema == nil {
		return ""
	}
	if col, ok := strings.CutPrefix(target, "col:"); ok {
		qtable, name, ok := strings.Cut(reverseLastDot(col), "\x00")
		if !ok {
			return ""
		}
		t := schema.Lookup(qtable)
		if t == nil {
			return ""
		}
		for _, c := range t.Columns {
			if c.Name != name {
				continue
			}
			md := "**Column** `" + c.Name + "`"
			if c.Type != "" {
				md += " `" + c.Type + "`"
			}
			if flags := columnFlags(c); flags != "" {
				md += "\n\n" + flags
			}
			if c.Note != "" {
				md += "\n\n" + c.Note
			}
			return md
		}
		return ""
	}
	if t := schema.Lookup(target); t != nil {
		md := "**Table** `" + t.Qualified() + "`"
		if t.Note != "" {
			md += "\n\n" + t.Note
		}
		return md
	}
	return ""
}

// columnFlags summarises a column's constraint flags for hover.
func columnFlags(c *model.Column) string {
	var f []string
	if c.PK {
		f = append(f, "primary key")
	}
	if c.Unique {
		f = append(f, "unique")
	}
	if c.NotNull {
		f = append(f, "not null")
	}
	if c.Increment {
		f = append(f, "increment")
	}
	if c.FK {
		f = append(f, "foreign key")
	}
	if c.Default != "" {
		f = append(f, "default: "+c.Default)
	}
	return strings.Join(f, ", ")
}

// reverseLastDot rewrites "schema.table.col" so the column can be split off with
// a single strings.Cut: it returns "schema.table\x00col".
func reverseLastDot(s string) string {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return s
	}
	return s[:i] + "\x00" + s[i+1:]
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
	// colKey identifies a column definition: "col:<qualified-table>.<column>".
	colKey := func(qtable, col string) string { return "col:" + qtable + "." + col }
	for _, t := range schema.Tables {
		ix.defs[t.Qualified()] = locFor(t.NamePos.File, t.NamePos.Line, t.NamePos.Col, len(t.Name))
		for _, c := range t.Columns {
			ix.defs[colKey(t.Qualified(), c.Name)] = locFor(c.NamePos.File, c.NamePos.Line, c.NamePos.Col, len(c.Name))
		}
	}
	// addCols records go-to-definition occurrences for the columns of an
	// endpoint (e.g. the `id` in `users.id`) against the resolved table.
	addCols := func(p string, e ast.Endpoint) {
		qt := key(qual(e.Schema, e.Table))
		for i, col := range e.Columns {
			if i < len(e.ColPos) {
				ix.occs = append(ix.occs, mkOcc(p, e.ColPos[i].Line, e.ColPos[i].Col, len(col), colKey(qt, col)))
			}
		}
	}
	for p, f := range files {
		addTable := func(e ast.Endpoint) {
			if e.Table != "" {
				ix.occs = append(ix.occs, mkOcc(p, e.Pos.Line, e.Pos.Col, len(e.Table), key(qual(e.Schema, e.Table))))
			}
		}
		for _, t := range f.Tables {
			qt := key(qual(t.Schema, t.Name))
			ix.occs = append(ix.occs, mkOcc(p, t.NamePos.Line, t.NamePos.Col, len(t.Name), qt))
			// column definitions are also references to themselves
			for _, c := range t.Columns {
				ix.occs = append(ix.occs, mkOcc(p, c.NamePos.Line, c.NamePos.Col, len(c.Name), colKey(qt, c.Name)))
			}
			// index fields navigate to the column they index
			for _, idx := range t.Indexes {
				for _, fld := range idx.Fields {
					if !fld.Expr {
						ix.occs = append(ix.occs, mkOcc(p, fld.Pos.Line, fld.Pos.Col, len(fld.Text), colKey(qt, fld.Text)))
					}
				}
			}
		}
		for _, r := range f.Refs {
			if !r.Inline {
				addTable(r.Left)
				addCols(p, r.Left)
			}
			addTable(r.Right)
			addCols(p, r.Right)
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
