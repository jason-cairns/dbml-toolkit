// Package parser turns DBML source into an *ast.File. It recovers from errors
// so a single mistake never blocks the rest of the file (important for the LSP).
package parser

import (
	"fmt"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/lexer"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// Diagnostic is a parse error with a source range.
type Diagnostic struct {
	Pos token.Pos
	End token.Pos
	Msg string
}

type parser struct {
	lex  *lexer.Lexer
	toks []token.Token // lookahead buffer
	file *ast.File
	errs []Diagnostic
}

// Parse parses src (attributed to path) into a File plus any diagnostics.
func Parse(path, src string) (*ast.File, []Diagnostic) {
	p := &parser{lex: lexer.New(path, src), file: &ast.File{Path: path}}
	p.parseFile()
	// The lexer has now scanned to EOF, so every comment (including any trailing
	// the final token) has been recorded. Carry them on the File so the formatter
	// can splice them back by position.
	p.file.Comments = p.lex.Comments()
	return p.file, p.errs
}

// --- token buffer -----------------------------------------------------------

func (p *parser) peek(i int) token.Token {
	for len(p.toks) <= i {
		p.toks = append(p.toks, p.lex.Next())
	}
	return p.toks[i]
}

func (p *parser) cur() token.Token { return p.peek(0) }

func (p *parser) next() token.Token {
	t := p.peek(0)
	p.toks = p.toks[1:]
	return t
}

// forceProgress guarantees a block-body loop advances. Those loops consume a
// variable number of tokens through helpers (name, expect, setting, dotted)
// that report an error but do NOT consume the current token when it is
// unexpected. If none of a loop's arms consume the token, the loop spins
// forever, allocating on every turn — a half-typed buffer once drove the
// language server into a 100%-CPU, multi-gigabyte loop and froze it. Every such
// loop passes the token it began the iteration on; if the parser has not moved
// past it, we skip one token so the loop is guaranteed to terminate. EOF is
// left untouched: the loops all exit on it themselves.
func (p *parser) forceProgress(since token.Token) {
	if p.cur().Kind != token.EOF && p.cur().Pos.Off == since.Pos.Off {
		p.next()
	}
}

func (p *parser) accept(k token.Kind) (token.Token, bool) {
	if p.cur().Kind == k {
		return p.next(), true
	}
	return p.cur(), false
}

func (p *parser) expect(k token.Kind) token.Token {
	if p.cur().Kind == k {
		return p.next()
	}
	p.errf(p.cur(), "expected %s, got %q", k, p.cur().Lit)
	return p.cur()
}

func (p *parser) errf(t token.Token, format string, args ...any) {
	p.errs = append(p.errs, Diagnostic{Pos: t.Pos, End: t.End, Msg: fmt.Sprintf(format, args...)})
}

// isKw reports whether the current token is the given keyword (case-insensitive).
func (p *parser) isKw(kw string) bool {
	t := p.cur()
	return t.Kind == token.Ident && strings.EqualFold(t.Lit, kw)
}

// nameLike reports whether a token can serve as an identifier/name.
func nameLike(k token.Kind) bool {
	return k == token.Ident || k == token.QIdent || k == token.String || k == token.Number
}

func (p *parser) name() (string, token.Pos) {
	t := p.cur()
	if nameLike(t.Kind) {
		p.next()
		return t.Lit, t.Pos
	}
	p.errf(t, "expected a name, got %q", t.Lit)
	return "", t.Pos
}

// --- top level --------------------------------------------------------------

func (p *parser) parseFile() {
	for p.cur().Kind != token.EOF {
		switch {
		case p.isKw("project"):
			p.parseProject()
		case p.isKw("table") && !p.isKw("tablegroup") && !p.isKw("tablepartial"):
			p.parseTable()
		case p.isKw("tablepartial"):
			p.parseTablePartial()
		case p.isKw("tablegroup"):
			p.parseTableGroup()
		case p.isKw("enum"):
			p.parseEnum()
		case p.isKw("ref"):
			p.parseRef()
		case p.isKw("note"):
			p.parseNote()
		case p.isKw("records"):
			p.parseRecords()
		case p.isKw("use") || p.isKw("reuse"):
			p.parseImport()
		default:
			p.errf(p.cur(), "unexpected token %q at top level", p.cur().Lit)
			p.next() // recover
		}
	}
}

// dotted parses `a.b.c` returning the parts and the position of the last one —
// the entity's own name (the earlier parts are the qualifying schema), so
// callers anchor NamePos on the name the user actually navigates to.
func (p *parser) dotted() ([]string, token.Pos) {
	first, pos := p.name()
	parts := []string{first}
	for p.cur().Kind == token.Dot {
		p.next()
		n, npos := p.name()
		parts = append(parts, n)
		pos = npos
	}
	return parts, pos
}

// --- project ----------------------------------------------------------------

func (p *parser) parseProject() {
	p.next()
	pr := &ast.Project{Pos: p.cur().Pos}
	if nameLike(p.cur().Kind) {
		pr.Name, _ = p.name()
	}
	pr.Settings = p.settings()
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		if p.isKw("note") {
			p.next()
			p.expect(token.Colon)
			pr.Note = p.stringValue()
		} else {
			// database_type: 'x' and other key: value lines
			fld := ast.Setting{Pos: p.cur().Pos}
			fld.Name, _ = p.name()
			if p.cur().Kind == token.Colon {
				p.next()
				fld.HasValue = true
				fld.Value, fld.Kind = p.value()
			}
			pr.Fields = append(pr.Fields, fld)
		}
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	p.file.Projects = append(p.file.Projects, pr)
}

// --- table ------------------------------------------------------------------

func (p *parser) parseTable() {
	start := p.next().Pos
	parts, npos := p.dotted()
	t := &ast.Table{Pos: start, NamePos: npos}
	setSchemaName(parts, &t.Schema, &t.Name)
	if p.isKw("as") {
		p.next()
		t.Alias, _ = p.name()
	}
	t.Settings = p.settings()
	t.Note = settingNote(t.Settings)
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		switch {
		case p.isKw("indexes"):
			p.next()
			t.Indexes = p.parseIndexes()
		case p.isKw("checks"):
			p.next()
			t.Checks = p.parseChecks()
		case p.isKw("note") && p.peek(1).Kind == token.Colon:
			p.next()
			p.next()
			t.Note = p.stringValue()
		case p.cur().Kind == token.Tilde:
			p.next()
			n, _ := p.name()
			t.Injects = append(t.Injects, n)
		default:
			t.Columns = append(t.Columns, p.parseColumn(t.Schema, t.Name))
		}
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	p.file.Tables = append(p.file.Tables, t)
}

func (p *parser) parseColumn(schema, table string) *ast.Column {
	name, npos := p.name()
	c := &ast.Column{Name: name, Pos: npos, NamePos: npos}
	c.Type = p.parseType()
	c.Settings = p.settings()
	c.Note = settingNote(c.Settings)
	// Promote any inline `ref:` setting into a first-class relationship.
	for _, s := range c.Settings {
		if strings.EqualFold(s.Name, "ref") && s.Ref != nil {
			r := s.Ref
			r.Inline = true
			r.Left = ast.Endpoint{Schema: schema, Table: table, Columns: []string{name}, Pos: npos}
			p.file.Refs = append(p.file.Refs, r)
		}
	}
	return c
}

// parseType reads a column type: dotted name, optional "quoted", optional (args).
func (p *parser) parseType() string {
	var b strings.Builder
	if p.cur().Kind == token.QIdent {
		b.WriteString(p.next().Lit)
	} else {
		parts, _ := p.dotted()
		b.WriteString(strings.Join(parts, "."))
	}
	if p.cur().Kind == token.LParen {
		b.WriteString(p.rawGroup())
	}
	// trailing array marker like [] would be settings; ignore here
	return b.String()
}

// rawGroup captures a balanced (...) group verbatim.
func (p *parser) rawGroup() string {
	var b strings.Builder
	b.WriteString("(")
	p.next()
	depth := 1
	for depth > 0 && p.cur().Kind != token.EOF {
		t := p.next()
		switch t.Kind {
		case token.LParen:
			depth++
		case token.RParen:
			depth--
			if depth == 0 {
				b.WriteString(")")
				return b.String()
			}
		}
		b.WriteString(t.Lit)
	}
	return b.String()
}

// --- indexes & checks -------------------------------------------------------

func (p *parser) parseIndexes() []*ast.Index {
	var out []*ast.Index
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		idx := &ast.Index{Pos: p.cur().Pos}
		if p.cur().Kind == token.LParen {
			p.next()
			for p.cur().Kind != token.RParen && p.cur().Kind != token.EOF {
				idx.Fields = append(idx.Fields, p.indexField())
				if !acceptComma(p) {
					break
				}
			}
			p.expect(token.RParen)
		} else {
			idx.Fields = append(idx.Fields, p.indexField())
		}
		idx.Settings = p.settings()
		out = append(out, idx)
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	return out
}

func (p *parser) indexField() ast.IndexField {
	pos := p.cur().Pos
	if p.cur().Kind == token.Expr {
		return ast.IndexField{Text: p.next().Lit, Expr: true, Pos: pos}
	}
	n, _ := p.name()
	return ast.IndexField{Text: n, Pos: pos}
}

func (p *parser) parseChecks() []ast.Check {
	var out []ast.Check
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		ck := ast.Check{Pos: p.cur().Pos}
		if p.cur().Kind == token.Expr {
			ck.Expr = p.next().Lit
		} else {
			ck.Expr, _ = p.name()
		}
		ck.Settings = p.settings()
		out = append(out, ck)
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	return out
}

// --- refs -------------------------------------------------------------------

func (p *parser) parseRef() {
	start := p.next().Pos
	r := &ast.Ref{Pos: start}
	// optional name
	if nameLike(p.cur().Kind) && p.cur().Kind != token.Colon {
		if p.cur().Kind != token.LBrace {
			r.Name, _ = p.name()
		}
	}
	if p.cur().Kind == token.Colon { // short form
		p.next()
		p.refBody(r)
		r.Settings = p.settings()
	} else { // long form
		p.expect(token.LBrace)
		p.refBody(r)
		r.Settings = p.settings()
		p.expect(token.RBrace)
	}
	p.file.Refs = append(p.file.Refs, r)
}

// refBody parses `endpoint OP endpoint`.
func (p *parser) refBody(r *ast.Ref) {
	r.Left = p.endpoint()
	r.Op = p.refOp()
	r.Right = p.endpoint()
}

func (p *parser) refOp() string {
	switch p.cur().Kind {
	case token.Lt:
		p.next()
		return "<"
	case token.Gt:
		p.next()
		return ">"
	case token.Minus:
		p.next()
		return "-"
	case token.LtGt:
		p.next()
		return "<>"
	default:
		p.errf(p.cur(), "expected relationship operator, got %q", p.cur().Lit)
		return ""
	}
}

// endpoint parses [schema.]table.column or [schema.]table.(col, col).
func (p *parser) endpoint() ast.Endpoint {
	e := ast.Endpoint{Pos: p.cur().Pos}
	first, fpos := p.name()
	parts := []string{first}
	partPos := []token.Pos{fpos}
	for p.cur().Kind == token.Dot {
		p.next()
		if p.cur().Kind == token.LParen { // composite columns
			p.next()
			for p.cur().Kind != token.RParen && p.cur().Kind != token.EOF {
				n, npos := p.name()
				e.Columns = append(e.Columns, n)
				e.ColPos = append(e.ColPos, npos)
				if !acceptComma(p) {
					break
				}
			}
			p.expect(token.RParen)
			setSchemaName(parts, &e.Schema, &e.Table)
			return e
		}
		n, npos := p.name()
		parts = append(parts, n)
		partPos = append(partPos, npos)
	}
	// last part is the single column
	e.Columns = []string{parts[len(parts)-1]}
	e.ColPos = []token.Pos{partPos[len(partPos)-1]}
	setSchemaName(parts[:len(parts)-1], &e.Schema, &e.Table)
	return e
}

// --- enum -------------------------------------------------------------------

func (p *parser) parseEnum() {
	start := p.next().Pos
	parts, npos := p.dotted()
	e := &ast.Enum{Pos: start, NamePos: npos}
	setSchemaName(parts, &e.Schema, &e.Name)
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		v := ast.EnumValue{Pos: p.cur().Pos}
		v.Name, _ = p.name()
		for _, s := range p.settings() {
			if strings.EqualFold(s.Name, "note") {
				v.Note = s.Value
			}
		}
		e.Values = append(e.Values, v)
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	p.file.Enums = append(p.file.Enums, e)
}

// --- table group ------------------------------------------------------------

func (p *parser) parseTableGroup() {
	start := p.next().Pos
	g := &ast.TableGroup{Pos: start}
	g.NamePos = p.cur().Pos
	if nameLike(p.cur().Kind) {
		g.Name, _ = p.name()
	}
	g.Settings = p.settings()
	g.Note = settingNote(g.Settings)
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		if p.isKw("note") && p.peek(1).Kind == token.Colon {
			p.next()
			p.next()
			g.Note = p.stringValue()
			continue
		}
		m := ast.GroupMember{Pos: p.cur().Pos}
		parts, _ := p.dotted()
		setSchemaName(parts, &m.Schema, &m.Table)
		g.Members = append(g.Members, m)
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	p.file.Groups = append(p.file.Groups, g)
}

// --- note -------------------------------------------------------------------

func (p *parser) parseNote() {
	start := p.next().Pos
	n := &ast.Note{Pos: start, NamePos: p.cur().Pos}
	if nameLike(p.cur().Kind) {
		n.Name, _ = p.name()
	}
	n.Settings = p.settings()
	p.expect(token.LBrace)
	n.Text = p.stringValue()
	p.expect(token.RBrace)
	p.file.Notes = append(p.file.Notes, n)
}

// --- table partial ----------------------------------------------------------

func (p *parser) parseTablePartial() {
	start := p.next().Pos
	tp := &ast.TablePartial{Pos: start, NamePos: p.cur().Pos}
	tp.Name, _ = p.name()
	tp.Settings = p.settings()
	p.expect(token.LBrace)
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		since := p.cur()
		switch {
		case p.isKw("indexes"):
			p.next()
			tp.Indexes = p.parseIndexes()
		case p.isKw("note") && p.peek(1).Kind == token.Colon:
			p.next()
			p.next()
			tp.Note = p.stringValue()
		default:
			tp.Columns = append(tp.Columns, p.parseColumn("", tp.Name))
		}
		p.forceProgress(since)
	}
	p.expect(token.RBrace)
	p.file.Partials = append(p.file.Partials, tp)
}

// --- records ----------------------------------------------------------------

func (p *parser) parseRecords() {
	start := p.next().Pos
	rec := &ast.Records{Pos: start}
	parts, _ := p.dotted()
	setSchemaName(parts, &rec.Schema, &rec.Table)
	if p.cur().Kind == token.LParen {
		p.next()
		for p.cur().Kind != token.RParen && p.cur().Kind != token.EOF {
			n, _ := p.name()
			rec.Columns = append(rec.Columns, n)
			if !acceptComma(p) {
				break
			}
		}
		p.expect(token.RParen)
	}
	p.expect(token.LBrace)
	// Group value tokens into rows by source line.
	line := -1
	var row []string
	var kinds []token.Kind
	flush := func() {
		if len(row) > 0 {
			rec.Rows = append(rec.Rows, row)
			rec.Kinds = append(rec.Kinds, kinds)
			row = nil
			kinds = nil
		}
	}
	for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
		if p.cur().Kind == token.Comma {
			p.next()
			continue
		}
		t := p.next()
		if line >= 0 && t.Pos.Line != line {
			flush()
		}
		line = t.Pos.Line
		row = append(row, t.Lit)
		kinds = append(kinds, t.Kind)
	}
	flush()
	p.expect(token.RBrace)
	p.file.Records = append(p.file.Records, rec)
}

// --- imports ----------------------------------------------------------------

func (p *parser) parseImport() {
	imp := &ast.Import{Pos: p.cur().Pos, Reuse: p.isKw("reuse")}
	p.next()
	if p.cur().Kind == token.Star {
		p.next()
		imp.Wildcard = true
	} else if p.cur().Kind == token.LBrace {
		p.next()
		for p.cur().Kind != token.RBrace && p.cur().Kind != token.EOF {
			since := p.cur()
			it := ast.ImportItem{}
			it.Type, _ = p.name()
			it.Name, _ = p.name()
			if p.isKw("as") {
				p.next()
				it.Alias, _ = p.name()
			}
			imp.Items = append(imp.Items, it)
			acceptComma(p)
			p.forceProgress(since)
		}
		p.expect(token.RBrace)
	}
	if p.isKw("from") {
		p.next()
	}
	imp.Path = p.stringValue()
	p.file.Imports = append(p.file.Imports, imp)
}

// --- settings ---------------------------------------------------------------

// settings parses an optional `[ ... ]` block into a slice of Setting.
func (p *parser) settings() []ast.Setting {
	if p.cur().Kind != token.LBrack {
		return nil
	}
	p.next()
	var out []ast.Setting
	for p.cur().Kind != token.RBrack && p.cur().Kind != token.EOF {
		since := p.cur()
		out = append(out, p.setting())
		acceptComma(p)
		p.forceProgress(since)
	}
	p.expect(token.RBrack)
	return out
}

func (p *parser) setting() ast.Setting {
	s := ast.Setting{Pos: p.cur().Pos}
	// Setting name may be multiple words (e.g. "primary key", "not null").
	var words []string
	for p.cur().Kind == token.Ident && p.peek(1).Kind != token.Colon {
		words = append(words, p.next().Lit)
		if p.cur().Kind == token.Colon || p.cur().Kind == token.Comma || p.cur().Kind == token.RBrack {
			break
		}
	}
	if len(words) == 0 && nameLike(p.cur().Kind) {
		words = append(words, p.next().Lit)
	}
	s.Name = strings.Join(words, " ")
	if p.cur().Kind == token.Colon {
		p.next()
		s.HasValue = true
		if strings.EqualFold(s.Name, "ref") {
			r := &ast.Ref{Pos: s.Pos}
			r.Op = p.refOp()
			r.Right = p.endpoint()
			s.Ref = r
			s.Value = r.Op
			s.Kind = token.Ident
		} else {
			s.Value, s.Kind = p.value()
		}
	}
	return s
}

// value reads a single setting value token and returns its text and kind.
func (p *parser) value() (string, token.Kind) {
	t := p.cur()
	if t.Kind == token.Minus { // negative number
		p.next()
		n := p.next()
		return "-" + n.Lit, token.Number
	}
	if t.Kind == token.Color {
		p.next()
		return t.Lit, token.Color
	}
	if nameLike(t.Kind) || t.Kind == token.Expr {
		p.next()
		return t.Lit, t.Kind
	}
	p.errf(t, "expected a value, got %q", t.Lit)
	return "", token.Illegal
}

// stringValue reads a string/block-string body (used for notes).
func (p *parser) stringValue() string {
	t := p.cur()
	if t.Kind == token.String || t.Kind == token.BlockString || t.Kind == token.QIdent {
		p.next()
		return t.Lit
	}
	// allow bare value fallback
	v, _ := p.value()
	return v
}

// --- helpers ----------------------------------------------------------------

func acceptComma(p *parser) bool {
	_, ok := p.accept(token.Comma)
	return ok
}

// setSchemaName maps dotted parts onto schema+name (schema optional).
func setSchemaName(parts []string, schema, name *string) {
	switch len(parts) {
	case 0:
	case 1:
		*name = parts[0]
	default:
		*schema = strings.Join(parts[:len(parts)-1], ".")
		*name = parts[len(parts)-1]
	}
}

func settingNote(ss []ast.Setting) string {
	for _, s := range ss {
		if strings.EqualFold(s.Name, "note") {
			return s.Value
		}
	}
	return ""
}
