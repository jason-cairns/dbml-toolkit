// Package format renders a parsed DBML file back into canonical source text.
// The output is deterministic and idempotent: formatting already-formatted
// source yields the same bytes. Comments are preserved by splicing them back in
// by source position (the parser records them as trivia on ast.File.Comments).
package format

import (
	"sort"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/parser"
	"github.com/jason-cairns/dbml-toolkit/token"
)

const indentUnit = "  " // two spaces per level

// Format parses src (attributed to path) and returns canonical DBML. When the
// source has syntax errors the returned diagnostics are non-empty and the string
// is empty — callers should leave a broken file untouched rather than risk
// mangling it.
func Format(path, src string) (string, []parser.Diagnostic) {
	f, diags := parser.Parse(path, src)
	if len(diags) > 0 {
		return "", diags
	}
	return Source(f), nil
}

// Source renders an already-parsed File as canonical DBML.
func Source(f *ast.File) string {
	e := &emitter{}
	e.file(f)
	return e.finish(f.Comments)
}

// outLine is one line of rendered output. src is the source line the line is
// anchored to (0 for structural lines like blanks and closing braces); the
// comment splicer uses it to place comments by original position.
type outLine struct {
	indent  int
	text    string
	src     int
	trailer string // trailing comment appended during splicing
}

type emitter struct {
	lines []outLine
}

func (e *emitter) emit(indent, src int, text string) {
	e.lines = append(e.lines, outLine{indent: indent, src: src, text: text})
}

// blank appends a single blank separator, collapsing consecutive blanks.
func (e *emitter) blank() {
	if len(e.lines) == 0 {
		return
	}
	if last := e.lines[len(e.lines)-1]; last.text == "" {
		return
	}
	e.lines = append(e.lines, outLine{})
}

// --- top level --------------------------------------------------------------

func (e *emitter) file(f *ast.File) {
	for i, u := range topLevel(f) {
		if i > 0 {
			e.blank()
		}
		u.fn(e)
	}
}

type unit struct {
	off int
	fn  func(*emitter)
}

// topLevel gathers every top-level construct into one slice ordered by source
// offset, so declarations are emitted in the order the author wrote them (the
// AST groups them by type). Inline refs are skipped: they are rendered as column
// settings, not as standalone refs.
func topLevel(f *ast.File) []unit {
	var us []unit
	for _, p := range f.Projects {
		p := p
		us = append(us, unit{p.Pos.Off, func(e *emitter) { e.project(p) }})
	}
	for _, en := range f.Enums {
		en := en
		us = append(us, unit{en.Pos.Off, func(e *emitter) { e.enum(en) }})
	}
	for _, t := range f.Tables {
		t := t
		us = append(us, unit{t.Pos.Off, func(e *emitter) { e.table(t) }})
	}
	for _, tp := range f.Partials {
		tp := tp
		us = append(us, unit{tp.Pos.Off, func(e *emitter) { e.partial(tp) }})
	}
	for _, g := range f.Groups {
		g := g
		us = append(us, unit{g.Pos.Off, func(e *emitter) { e.group(g) }})
	}
	for _, n := range f.Notes {
		n := n
		us = append(us, unit{n.Pos.Off, func(e *emitter) { e.note(n) }})
	}
	for _, r := range f.Refs {
		if r.Inline {
			continue
		}
		r := r
		us = append(us, unit{r.Pos.Off, func(e *emitter) { e.ref(r) }})
	}
	for _, rec := range f.Records {
		rec := rec
		us = append(us, unit{rec.Pos.Off, func(e *emitter) { e.records(rec) }})
	}
	for _, im := range f.Imports {
		im := im
		us = append(us, unit{im.Pos.Off, func(e *emitter) { e.imp(im) }})
	}
	sort.SliceStable(us, func(i, j int) bool { return us[i].off < us[j].off })
	return us
}

// --- constructs -------------------------------------------------------------

func (e *emitter) project(pr *ast.Project) {
	hdr := "Project"
	if pr.Name != "" {
		hdr += " " + renderName(pr.Name)
	}
	if s := renderSettings(pr.Settings, false); s != "" {
		hdr += " " + s
	}
	e.emit(0, pr.Pos.Line, hdr+" {")
	if pr.Note != "" {
		e.emit(1, 0, "Note: "+renderNoteText(pr.Note))
	}
	for _, f := range pr.Fields {
		e.emit(1, f.Pos.Line, renderSetting(f))
	}
	e.emit(0, 0, "}")
}

func (e *emitter) table(t *ast.Table) {
	hdr := "Table " + qname(t.Schema, t.Name)
	if t.Alias != "" {
		hdr += " as " + renderName(t.Alias)
	}
	if s := renderSettings(t.Settings, true); s != "" {
		hdr += " " + s
	}
	e.emit(0, t.Pos.Line, hdr+" {")
	if t.Note != "" {
		e.emit(1, 0, "Note: "+renderNoteText(t.Note))
	}

	// Align column types: pad every column name to the widest name so the type
	// column lines up. Types themselves are not padded — settings follow after a
	// single space.
	e.columns(t.Columns)
	for _, inj := range t.Injects {
		e.emit(1, 0, "~"+renderName(inj))
	}
	if len(t.Indexes) > 0 {
		e.indexes(t.Indexes)
	}
	if len(t.Checks) > 0 {
		e.checks(t.Checks)
	}
	e.emit(0, 0, "}")
}

// columns emits a table/partial's columns with aligned name, type and settings
// columns. Names pad to the widest name so types line up; types pad to the
// widest type *among columns that carry settings* so the `[settings]` brackets
// line up too — without letting a long unbracketed type push the brackets out.
func (e *emitter) columns(cols []*ast.Column) {
	names := make([]string, len(cols))
	types := make([]string, len(cols))
	settings := make([]string, len(cols))
	nameW, typeW := 0, 0
	for i, c := range cols {
		names[i] = renderName(c.Name)
		types[i] = renderType(c.Type)
		settings[i] = renderSettings(c.Settings, false)
		if len(names[i]) > nameW {
			nameW = len(names[i])
		}
		if settings[i] != "" && len(types[i]) > typeW {
			typeW = len(types[i])
		}
	}
	for i, c := range cols {
		line := padRight(names[i], nameW) + " "
		if settings[i] != "" {
			line += padRight(types[i], typeW) + " " + settings[i]
		} else {
			line += types[i]
		}
		e.emit(1, c.NamePos.Line, strings.TrimRight(line, " "))
	}
}

func (e *emitter) partial(tp *ast.TablePartial) {
	hdr := "TablePartial " + renderName(tp.Name)
	if s := renderSettings(tp.Settings, true); s != "" {
		hdr += " " + s
	}
	e.emit(0, tp.Pos.Line, hdr+" {")
	if tp.Note != "" {
		e.emit(1, 0, "Note: "+renderNoteText(tp.Note))
	}
	e.columns(tp.Columns)
	if len(tp.Indexes) > 0 {
		e.indexes(tp.Indexes)
	}
	e.emit(0, 0, "}")
}

func (e *emitter) indexes(ix []*ast.Index) {
	e.emit(1, 0, "indexes {")
	for _, idx := range ix {
		line := renderIndexFields(idx.Fields)
		if s := renderSettings(idx.Settings, false); s != "" {
			line += " " + s
		}
		e.emit(2, idx.Pos.Line, line)
	}
	e.emit(1, 0, "}")
}

func (e *emitter) checks(cks []ast.Check) {
	e.emit(1, 0, "checks {")
	for _, c := range cks {
		line := "`" + c.Expr + "`"
		if s := renderSettings(c.Settings, false); s != "" {
			line += " " + s
		}
		e.emit(2, c.Pos.Line, line)
	}
	e.emit(1, 0, "}")
}

func (e *emitter) enum(en *ast.Enum) {
	e.emit(0, en.Pos.Line, "Enum "+qname(en.Schema, en.Name)+" {")
	for _, v := range en.Values {
		line := renderName(v.Name)
		if v.Note != "" {
			line += " [note: " + renderValue(v.Note, token.String) + "]"
		}
		e.emit(1, v.Pos.Line, line)
	}
	e.emit(0, 0, "}")
}

func (e *emitter) group(g *ast.TableGroup) {
	hdr := "TableGroup " + renderName(g.Name)
	if s := renderSettings(g.Settings, true); s != "" {
		hdr += " " + s
	}
	e.emit(0, g.Pos.Line, hdr+" {")
	if g.Note != "" {
		e.emit(1, 0, "Note: "+renderNoteText(g.Note))
	}
	for _, m := range g.Members {
		e.emit(1, m.Pos.Line, qname(m.Schema, m.Table))
	}
	e.emit(0, 0, "}")
}

func (e *emitter) note(n *ast.Note) {
	hdr := "Note"
	if n.Name != "" {
		hdr += " " + renderName(n.Name)
	}
	if s := renderSettings(n.Settings, false); s != "" {
		hdr += " " + s
	}
	e.emit(0, n.Pos.Line, hdr+" {")
	e.emitNoteBody(1, n.Text)
	e.emit(0, 0, "}")
}

func (e *emitter) ref(r *ast.Ref) {
	hdr := "Ref"
	if r.Name != "" {
		hdr += " " + renderName(r.Name)
	}
	line := hdr + ": " + renderEndpoint(r.Left) + " " + r.Op + " " + renderEndpoint(r.Right)
	if s := renderSettings(r.Settings, false); s != "" {
		line += " " + s
	}
	e.emit(0, r.Pos.Line, line)
}

func (e *emitter) records(rec *ast.Records) {
	hdr := "Records " + qname(rec.Schema, rec.Table)
	if len(rec.Columns) > 0 {
		cols := make([]string, len(rec.Columns))
		for i, c := range rec.Columns {
			cols[i] = renderName(c)
		}
		hdr += " (" + strings.Join(cols, ", ") + ")"
	}
	e.emit(0, rec.Pos.Line, hdr+" {")
	for ri, row := range rec.Rows {
		cells := make([]string, len(row))
		for ci, v := range row {
			kind := token.Ident
			if ri < len(rec.Kinds) && ci < len(rec.Kinds[ri]) {
				kind = rec.Kinds[ri][ci]
			}
			cells[ci] = renderValue(v, kind)
		}
		e.emit(1, 0, strings.Join(cells, ", "))
	}
	e.emit(0, 0, "}")
}

func (e *emitter) imp(im *ast.Import) {
	kw := "use"
	if im.Reuse {
		kw = "reuse"
	}
	line := kw
	switch {
	case im.Wildcard:
		line += " *"
	case len(im.Items) > 0:
		parts := make([]string, len(im.Items))
		for i, it := range im.Items {
			s := it.Type + " " + it.Name
			if it.Alias != "" {
				s += " as " + it.Alias
			}
			parts[i] = s
		}
		line += " { " + strings.Join(parts, ", ") + " }"
	}
	line += " from " + renderValue(im.Path, token.String)
	e.emit(0, im.Pos.Line, line)
}

// emitNoteBody renders the body of a standalone Note block. A multi-line note is
// emitted as an indented triple-quoted block (the lexer dedents on re-parse, so
// re-indenting is idempotent); a single-line note as a quoted string.
func (e *emitter) emitNoteBody(indent int, text string) {
	if strings.Contains(text, "\n") {
		e.emit(indent, 0, "'''")
		for _, ln := range strings.Split(text, "\n") {
			e.emit(indent, 0, ln)
		}
		e.emit(indent, 0, "'''")
		return
	}
	e.emit(indent, 0, "'"+escapeSingle(text)+"'")
}

// --- value / name rendering -------------------------------------------------

func renderSettings(ss []ast.Setting, skipNote bool) string {
	var parts []string
	for _, s := range ss {
		if skipNote && strings.EqualFold(s.Name, "note") {
			continue
		}
		parts = append(parts, renderSetting(s))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func renderSetting(s ast.Setting) string {
	if s.Ref != nil { // inline `ref: OP endpoint`
		return "ref: " + s.Ref.Op + " " + renderEndpoint(s.Ref.Right)
	}
	if !s.HasValue {
		return s.Name
	}
	return s.Name + ": " + renderValue(s.Value, s.Kind)
}

func renderValue(v string, k token.Kind) string {
	switch k {
	case token.String:
		return "'" + escapeSingle(v) + "'"
	case token.QIdent:
		return `"` + v + `"`
	case token.BlockString:
		return "'''" + v + "'''"
	case token.Expr:
		return "`" + v + "`"
	case token.Color:
		return v // literal already includes the leading '#'
	default:
		return v // Number, Ident, bareword
	}
}

// renderNoteText renders inline note text (a `Note: ...` line). Multi-line text
// uses a triple-quoted block; single-line uses a quoted string.
func renderNoteText(text string) string {
	if strings.Contains(text, "\n") {
		return "'''\n" + text + "\n'''"
	}
	return "'" + escapeSingle(text) + "'"
}

func renderEndpoint(e ast.Endpoint) string {
	name := renderName(e.Table)
	if e.Schema != "" {
		name = qname(e.Schema, e.Table)
	}
	switch len(e.Columns) {
	case 0:
		return name
	case 1:
		return name + "." + renderName(e.Columns[0])
	default:
		cols := make([]string, len(e.Columns))
		for i, c := range e.Columns {
			cols[i] = renderName(c)
		}
		return name + ".(" + strings.Join(cols, ", ") + ")"
	}
}

func renderIndexFields(fs []ast.IndexField) string {
	if len(fs) == 1 {
		return renderIndexField(fs[0])
	}
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = renderIndexField(f)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func renderIndexField(f ast.IndexField) string {
	if f.Expr {
		return "`" + f.Text + "`"
	}
	return renderName(f.Text)
}

// qname renders an optionally schema-qualified, optionally dotted name, quoting
// each part that is not a bare identifier.
func qname(schema, name string) string {
	if schema == "" {
		return renderName(name)
	}
	parts := strings.Split(schema, ".")
	for i := range parts {
		parts[i] = renderName(parts[i])
	}
	return strings.Join(parts, ".") + "." + renderName(name)
}

// renderName emits an identifier bare when it is a plain identifier, else
// double-quoted so names with spaces or punctuation round-trip.
func renderName(s string) string {
	if isBareIdent(s) {
		return s
	}
	return `"` + s + `"`
}

// renderType emits a column type. Types containing whitespace were originally
// double-quoted; re-quote the whole thing so it round-trips (the closing paren
// of an arg list, if any, is inside the quotes).
func renderType(t string) string {
	if strings.ContainsAny(t, " \t") {
		return `"` + t + `"`
	}
	return t
}

func isBareIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// escapeSingle escapes a value for inclusion in a single-quoted string so the
// lexer unescapes it back to the original.
func escapeSingle(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\n", `\n`,
		"\t", `\t`,
		"\r", `\r`,
	)
	return r.Replace(s)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// --- comment splicing + assembly --------------------------------------------

// finish splices comments into the rendered lines by source position and joins
// everything into the final document. Standalone comments are inserted on their
// own line above the next anchored line; trailing comments are appended to the
// line sharing their source line.
func (e *emitter) finish(comments []token.Comment) string {
	var leading, trailing []token.Comment
	for _, c := range comments {
		if c.Trailing {
			trailing = append(trailing, c)
		} else {
			leading = append(leading, c)
		}
	}

	// Attach trailing comments to the anchored line on the same source line.
	for _, c := range trailing {
		attached := false
		for i := range e.lines {
			if e.lines[i].src == c.Pos.Line && e.lines[i].trailer == "" {
				e.lines[i].trailer = renderComment(c)
				attached = true
				break
			}
		}
		if !attached {
			// No line to hang it on (e.g. after a closing brace) — treat as
			// standalone so it is never dropped.
			leading = append(leading, c)
		}
	}
	sort.SliceStable(leading, func(i, j int) bool {
		if leading[i].Pos.Line != leading[j].Pos.Line {
			return leading[i].Pos.Line < leading[j].Pos.Line
		}
		return leading[i].Pos.Col < leading[j].Pos.Col
	})

	var b strings.Builder
	writeLine := func(indent int, text, trailer string) {
		if text == "" && trailer == "" {
			b.WriteByte('\n')
			return
		}
		b.WriteString(strings.Repeat(indentUnit, indent))
		b.WriteString(text)
		if trailer != "" {
			if text != "" {
				b.WriteString("  ")
			}
			b.WriteString(trailer)
		}
		b.WriteByte('\n')
	}

	li := 0 // index into sorted leading comments
	for _, ln := range e.lines {
		// Flush standalone comments that belong above this anchored line.
		if ln.src > 0 {
			for li < len(leading) && leading[li].Pos.Line < ln.src {
				writeLine(ln.indent, renderComment(leading[li]), "")
				li++
			}
		}
		writeLine(ln.indent, ln.text, ln.trailer)
	}
	// Any comments after the last anchor.
	for li < len(leading) {
		writeLine(0, renderComment(leading[li]), "")
		li++
	}

	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return ""
	}
	return out + "\n"
}

// renderComment renders a comment back to source form, normalising interior
// spacing so the result is stable under re-formatting.
func renderComment(c token.Comment) string {
	if c.Block {
		if strings.Contains(c.Text, "\n") {
			return "/*" + c.Text + "*/"
		}
		t := strings.TrimSpace(c.Text)
		if t == "" {
			return "/**/"
		}
		return "/* " + t + " */"
	}
	t := strings.TrimSpace(c.Text)
	if t == "" {
		return "//"
	}
	return "// " + t
}
