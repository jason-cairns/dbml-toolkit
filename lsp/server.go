// Package lsp is a small language server for DBML providing diagnostics,
// go-to-definition, find-references and hover — all resolved across the module
// graph so navigation works cross-file. It speaks LSP over stdio.
package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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

	// The diagram render is the one slow step (hundreds of ms), so it runs on a
	// dedicated goroutine instead of the request loop. renderWake signals it;
	// renderJob holds the coalesced latest request, so a burst of keystrokes
	// collapses to a single render of the newest buffer.
	renderWake chan struct{}
	renderMu   sync.Mutex
	renderJob  *renderJob
}

type renderJob struct {
	path string
	docs map[string]string // snapshot, owned by the render goroutine
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
	if s.preview != nil {
		s.renderWake = make(chan struct{}, 1)
		go s.renderLoop()
		// Stop the render goroutine before Close (defers run LIFO). No setDoc
		// runs after the read loop exits, so closing renderWake here is safe.
		defer close(s.renderWake)
	}
	for {
		m, err := s.conn.read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.dispatch(m, s.handle)
		// A failed write means the client is gone; stop rather than block
		// forever in the next read with a preview no one is driving.
		if s.conn.werr != nil {
			return s.conn.werr
		}
	}
}

// watchdogTimeout is how long a single handler may run before the watchdog logs
// a goroutine dump. It is far longer than any healthy handler (a full resolve +
// render is well under a second) but short enough to catch a hang while the
// editor still cares.
const watchdogTimeout = 5 * time.Second

// logf writes a timestamped server log line to stderr. Editors surface an LSP's
// stderr in their log (e.g. ~/.cache/helix/helix.log), so this is where panics
// and slow handlers are recorded for post-mortem.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "lsp %s "+format+"\n",
		append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

// dispatch runs one message through handle, guarded so a single bad message can
// never take the server down or strand the editor. The request loop is
// single-threaded, so an unrecovered panic or an unbounded loop in any handler
// is fatal to every future request — exactly the failure that once froze this
// server. Two guards contain that:
//   - a watchdog logs a full goroutine dump if a handler outruns
//     watchdogTimeout, so a hang is diagnosable from the editor log instead of
//     surfacing only as mysterious request timeouts. The timer fires on its own
//     goroutine, so it still reports even while the handler goroutine spins.
//   - a deferred recover turns a handler panic into a logged stack trace plus a
//     null reply to any pending request, keeping the connection alive.
func (s *Server) dispatch(m *message, handle func(*message)) {
	watchdog := time.AfterFunc(watchdogTimeout, func() {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		logf("handler for %q exceeded %s — possible hang; goroutine dump:\n%s",
			m.Method, watchdogTimeout, buf[:n])
	})
	defer func() {
		watchdog.Stop()
		if r := recover(); r != nil {
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, false)
			logf("recovered panic in handler for %q: %v\n%s", m.Method, r, buf[:n])
			// A request left without a reply hangs the client until it times
			// out; answer with null so it fails fast and the session survives.
			if len(m.ID) > 0 {
				s.conn.reply(m.ID, nil)
			}
		}
	}()
	handle(m)
}

func (s *Server) handle(m *message) {
	switch m.Method {
	case "initialize":
		s.conn.reply(m.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           1, // full sync
				"definitionProvider":         true,
				"referencesProvider":         true,
				"hoverProvider":              true,
				"colorProvider":              true,
				"completionProvider":         map[string]any{"triggerCharacters": []string{".", "~"}},
				"documentSymbolProvider":     true,
				"documentFormattingProvider": true,
				"foldingRangeProvider":       true,
				"renameProvider":             map[string]any{"prepareProvider": true},
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{
						"tokenTypes":     semTokenTypes,
						"tokenModifiers": []string{},
					},
					"full": true,
				},
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
	case "textDocument/formatting":
		s.onFormatting(m)
	case "textDocument/semanticTokens/full":
		s.onSemanticTokens(m)
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

// updatePreview hands the latest buffer state to the render goroutine without
// blocking. Rendering a diagram takes hundreds of milliseconds; doing it inline
// would stall the request loop and make interactive requests (completion,
// hover) queue behind it and time out. Rapid edits coalesce: only the newest
// snapshot is kept, so the render goroutine never falls behind a fast typist.
func (s *Server) updatePreview(path string) {
	if s.preview == nil {
		return
	}
	// Snapshot the overlay so the render goroutine never races setDoc's writes.
	snap := make(map[string]string, len(s.docs))
	for k, v := range s.docs {
		snap[k] = v
	}
	s.renderMu.Lock()
	s.renderJob = &renderJob{path: path, docs: snap}
	s.renderMu.Unlock()
	select {
	case s.renderWake <- struct{}{}:
	default: // a wake-up is already pending; it will pick up the newest job
	}
}

// renderLoop renders the diagram off the request path, one job at a time,
// always rendering the most recent snapshot. It exits when renderWake closes.
func (s *Server) renderLoop() {
	for range s.renderWake {
		s.renderMu.Lock()
		job := s.renderJob
		s.renderJob = nil
		s.renderMu.Unlock()
		if job == nil {
			continue
		}
		if _, err := s.preview.Listen(0, s.previewOpen); err != nil {
			continue
		}
		s.preview.Render(job.path, job.docs)
	}
}

func (s *Server) publishDiagnostics(uri string) {
	path := uriToPath(uri)
	schema, files, diags, _ := resolver.Graph(path, s.docs)
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
	out = append(out, s.lints(path, schema, files)...)
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
	// addEndpoint records navigation occurrences for a relationship endpoint. The
	// dotted name is re-split against the namespace (model.TableFor) rather than
	// trusting the parser's positional guess, so a schema-qualified table like
	// `dim.date` navigates to the table even before its column is typed, and the
	// trailing segment is only treated as a column when it really is one.
	addEndpoint := func(p string, e ast.Endpoint) {
		parts := model.EndpointParts(e)
		if len(parts) == 0 {
			return
		}
		t, tableParts := schema.TableFor(parts)
		target := strings.Join(parts[:tableParts], ".")
		if t != nil {
			target = t.Qualified()
		}
		// Clickable table-reference span covering `schema.table`, editable over the
		// table name only.
		schemaPrefix := strings.Join(parts[:tableParts-1], ".")
		tableName := parts[tableParts-1]
		ix.occs = append(ix.occs, tableRefOcc(p, e.Pos.Line, e.Pos.Col, schemaPrefix, tableName, target))
		// Column occurrences for the segments that remain columns after re-splitting.
		// e.Columns line up with the tail of parts, so a segment the parser called a
		// column but which was absorbed into the table name (tableParts moved past
		// it) is skipped.
		tableSegs := len(parts) - len(e.Columns)
		for i, col := range e.Columns {
			if i >= len(e.ColPos) || tableSegs+i < tableParts {
				continue
			}
			ix.occs = append(ix.occs, mkOcc(p, e.ColPos[i].Line, e.ColPos[i].Col, len(col), colKey(target, col)))
		}
	}
	for p, f := range files {
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
				addEndpoint(p, r.Left)
			}
			addEndpoint(p, r.Right)
		}
		for _, g := range f.Groups {
			for _, mem := range g.Members {
				ix.occs = append(ix.occs, tableRefOcc(p, mem.Pos.Line, mem.Pos.Col, mem.Schema, mem.Table, key(qual(mem.Schema, mem.Table))))
			}
		}
	}
	return ix
}

// tableRefOcc builds an occurrence for a (possibly schema-qualified) table
// reference. The whole `schema.table` text is clickable — DBML has no separate
// schema definition to jump to, so navigating from the schema part should still
// reach the table — while the edit range (loc) covers only the table name, so
// rename rewrites `schema.table` to `schema.newName` rather than clobbering the
// schema.
func tableRefOcc(file string, line, startCol int, schema, table, target string) occurrence {
	nameCol, hitLen := startCol, len(table)
	if schema != "" {
		nameCol = startCol + len(schema) + 1
		hitLen = len(schema) + 1 + len(table)
	}
	o := mkOcc(file, line, nameCol, len(table), target)
	o.char = startCol - 1 // widen the clickable span to include the schema prefix
	o.length = hitLen
	return o
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
