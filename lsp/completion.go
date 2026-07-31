package lsp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/lexer"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/resolver"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// Completion item kinds (LSP CompletionItemKind subset).
const (
	ciClass    = 7
	ciField    = 5
	ciEnum     = 13
	ciKeyword  = 14
	ciSnippet  = 15
	ciValue    = 12
	ciProperty = 10
)

type completionItem struct {
	Label            string `json:"label"`
	Kind             int    `json:"kind"`
	Detail           string `json:"detail,omitempty"`
	InsertText       string `json:"insertText,omitempty"`
	InsertTextFormat int    `json:"insertTextFormat,omitempty"` // 2 == snippet
	SortText         string `json:"sortText,omitempty"`
}

// builtinTypes are the SQL/DBML column types offered in type position, on top
// of any enums defined in the project.
var builtinTypes = []string{
	"int", "integer", "bigint", "smallint", "tinyint", "serial", "bigserial",
	"decimal", "numeric", "real", "float", "double", "money",
	"boolean", "bool", "bit",
	"char", "varchar", "text", "citext", "uuid",
	"date", "time", "timestamp", "timestamptz", "datetime", "interval",
	"json", "jsonb", "bytea", "blob", "inet", "cidr",
}

// onCompletion offers context-aware completions. The enclosing block is derived
// by re-lexing the buffer up to the cursor (so it works mid-edit), while the
// candidate names come from the resolved project schema.
func (s *Server) onCompletion(m *message) {
	var p posParams
	json.Unmarshal(m.Params, &p)
	path := uriToPath(p.TextDocument.URI)
	src := s.docs[path]

	schema, files, _, _ := resolver.Graph(path, s.docs)
	items := s.complete(path, src, files[path], schema, p.Position)
	if items == nil {
		items = []completionItem{}
	}
	s.conn.reply(m.ID, map[string]any{"isIncomplete": false, "items": items})
}

// complete is the pure core of onCompletion, split out for testing. file and
// schema may be nil when the project fails to resolve.
func (s *Server) complete(path, src string, file *ast.File, schema *model.Schema, pos position) []completionItem {
	off := byteOffset(src, pos.Line, pos.Char)
	prefix := identBefore(src, off)

	toks := lexAll(path, src)
	before := toks[:0:0]
	for _, t := range toks {
		if t.End.Off <= off {
			before = append(before, t)
		}
	}
	// Drop the identifier currently being typed: it ends at the cursor and is a
	// prefix, not part of the surrounding structure.
	if prefix != "" && len(before) > 0 {
		last := before[len(before)-1]
		if (last.Kind == token.Ident || last.Kind == token.QIdent) && last.End.Off == off {
			before = before[:len(before)-1]
		}
	}

	ctx := analyze(before, pos.Line)
	return ctx.items(prefix, file, schema)
}

// completionCtx captures the structural position of the cursor.
type completionCtx struct {
	keyword    string   // enclosing block keyword: Table, TableGroup, Ref, indexes, ""
	tableName  string   // nearest enclosing table's name (for indexes/columns)
	lineIdents []string // idents on the cursor line at bracket depth 0, before cursor
	inBrackets bool     // cursor is inside a `[ ... ]` settings group
	afterDot   string   // qualifier ident when the cursor follows `<ident>.`
	settingKey string   // setting name when the cursor follows `<name>:` in brackets
	refExpects string   // "table" or "column" inside an inline/standalone ref
}

type frame struct{ keyword, name string }

// analyze walks the token stream before the cursor to classify its position.
func analyze(before []token.Token, curLine int) completionCtx {
	var stack []frame
	var lineIdents []string // idents on the current lexer line at bracket depth 0
	prevLine := -1
	brack := 0
	for _, t := range before {
		if t.Pos.Line-1 != prevLine {
			lineIdents = lineIdents[:0]
			prevLine = t.Pos.Line - 1
			brack = 0
		}
		switch t.Kind {
		case token.Ident, token.QIdent:
			if brack == 0 {
				lineIdents = append(lineIdents, t.Lit)
			}
		case token.LBrack:
			brack++
		case token.RBrack:
			if brack > 0 {
				brack--
			}
		case token.LBrace:
			f := frame{}
			if len(lineIdents) > 0 {
				f.keyword = lineIdents[0]
			}
			if len(lineIdents) > 1 {
				f.name = lineIdents[1]
			}
			stack = append(stack, f)
		case token.RBrace:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	ctx := completionCtx{}
	if len(stack) > 0 {
		ctx.keyword = stack[len(stack)-1].keyword
		ctx.tableName = stack[len(stack)-1].name
		// For a nested `indexes {}`/`checks {}` block the owning table is the
		// frame beneath it.
		if len(stack) > 1 && ctx.tableName == "" {
			ctx.tableName = stack[len(stack)-2].name
		}
	}

	// Re-scan just the cursor line for bracket state, the leading idents and any
	// trailing `.` / `name:` cursor position.
	var lineToks []token.Token
	for _, t := range before {
		if t.Pos.Line-1 == curLine {
			lineToks = append(lineToks, t)
		}
	}
	depth := 0
	ctx.lineIdents = nil
	for _, t := range lineToks {
		switch t.Kind {
		case token.LBrack:
			depth++
		case token.RBrack:
			if depth > 0 {
				depth--
			}
		case token.Ident, token.QIdent:
			if depth == 0 {
				ctx.lineIdents = append(ctx.lineIdents, t.Lit)
			}
		}
	}
	ctx.inBrackets = depth > 0

	// Trailing dot: `<ident>.` means the cursor is completing a member of ident.
	if n := len(before); n > 0 && before[n-1].Kind == token.Dot {
		if n > 1 && (before[n-2].Kind == token.Ident || before[n-2].Kind == token.QIdent) {
			ctx.afterDot = before[n-2].Lit
		}
	}
	// Trailing `name:` inside brackets means we're completing that setting's value.
	if ctx.inBrackets {
		if n := len(before); n >= 2 && before[n-1].Kind == token.Colon {
			if before[n-2].Kind == token.Ident {
				ctx.settingKey = strings.ToLower(before[n-2].Lit)
			}
		}
	}
	// Inline column ref: `[ref: > <table>.<col>]`. Detect the `ref:` on the line
	// and whether an operator has already been typed.
	if ctx.inBrackets {
		ctx.refExpects = inlineRefExpectation(lineToks)
	}
	return ctx
}

// inlineRefExpectation inspects a column's bracket tokens and returns "table" or
// "column" when the cursor sits in an inline `ref:` endpoint, else "".
func inlineRefExpectation(lineToks []token.Token) string {
	refAt := -1
	for i := 0; i+1 < len(lineToks); i++ {
		if lineToks[i].Kind == token.Ident && strings.EqualFold(lineToks[i].Lit, "ref") &&
			lineToks[i+1].Kind == token.Colon {
			refAt = i + 1
		}
	}
	if refAt < 0 {
		return ""
	}
	rest := lineToks[refAt+1:]
	// Skip the cardinality operator if present.
	seenOp := false
	for _, t := range rest {
		switch t.Kind {
		case token.Lt, token.Gt, token.Minus, token.LtGt:
			seenOp = true
		}
	}
	if !seenOp {
		return ""
	}
	// After the operator: a bare endpoint means we still expect a table; a `.`
	// means we're on the column part (handled via afterDot).
	return "table"
}

// items builds the completion list for the classified context.
func (c completionCtx) items(prefix string, file *ast.File, schema *model.Schema) []completionItem {
	switch {
	case c.inBrackets:
		return c.settingItems(prefix, schema)
	case c.keyword == "TableGroup":
		if c.afterDot != "" {
			return tablesInSchema(schema, c.afterDot)
		}
		return tableCandidates(schema, file)
	case c.keyword == "Ref":
		if c.afterDot != "" {
			return columnsOfTable(schema, c.afterDot)
		}
		return tableCandidates(schema, file)
	case c.keyword == "indexes":
		return columnsOfTable(schema, c.tableName)
	case c.keyword == "Table" || c.keyword == "TablePartial":
		return c.tableBodyItems(schema)
	case c.keyword == "":
		if c.afterDot == "" {
			return topLevelKeywords()
		}
	}
	return nil
}

// tableBodyItems completes inside a table body but outside brackets: the type
// position offers builtin types and enums; the column-name position offers
// nothing (freeform).
func (c completionCtx) tableBodyItems(schema *model.Schema) []completionItem {
	if c.afterDot != "" { // schema-qualified enum, e.g. `col app.<enum>`
		return enumsInSchema(schema, c.afterDot)
	}
	switch len(c.lineIdents) {
	case 1: // column name typed, cursor on the type
		return append(typeItems(), enumCandidates(schema)...)
	default:
		return nil
	}
}

// settingItems completes inside `[ ... ]`, keyed off the enclosing construct.
func (c completionCtx) settingItems(prefix string, schema *model.Schema) []completionItem {
	if c.settingKey != "" {
		return settingValues(c.settingKey)
	}
	// Inline ref endpoints inside a column's brackets.
	if c.refExpects == "table" {
		if c.afterDot != "" {
			return columnsOfTable(schema, c.afterDot)
		}
		return tableCandidates(schema, nil)
	}
	switch c.keyword {
	case "indexes":
		return keywords(ciProperty, "pk", "unique", "name: ", "type: ", "note: ")
	case "Ref":
		return keywords(ciProperty, "delete: ", "update: ", "color: ")
	case "Table", "TablePartial", "":
		if c.keyword == "" && len(c.lineIdents) > 0 && strings.EqualFold(c.lineIdents[0], "TableGroup") {
			return keywords(ciProperty, "color: ", "note: ")
		}
		if isColumnLine(c) {
			return keywords(ciProperty, "pk", "primary key", "unique", "not null",
				"increment", "default: ", "note: ", "ref: ")
		}
		return keywords(ciProperty, "headercolor: ", "note: ")
	}
	return nil
}

// isColumnLine reports whether the bracket group belongs to a column (name +
// type already present) rather than a table/group header.
func isColumnLine(c completionCtx) bool {
	if c.keyword == "" {
		return false
	}
	if len(c.lineIdents) == 0 {
		return false
	}
	head := c.lineIdents[0]
	return !strings.EqualFold(head, "Table") && !strings.EqualFold(head, "TablePartial")
}

// --- candidate builders -----------------------------------------------------

func settingValues(key string) []completionItem {
	switch key {
	case "delete", "update":
		return keywords(ciValue, "cascade", "restrict", "set null", "set default", "no action")
	case "type":
		return keywords(ciValue, "btree", "hash", "gin", "gist")
	}
	return nil
}

func typeItems() []completionItem {
	out := make([]completionItem, 0, len(builtinTypes))
	for _, t := range builtinTypes {
		out = append(out, completionItem{Label: t, Kind: ciValue, Detail: "type"})
	}
	return out
}

func enumCandidates(schema *model.Schema) []completionItem {
	if schema == nil {
		return nil
	}
	var out []completionItem
	for _, e := range schema.Enums {
		name := e.Name
		if e.Schema != "" {
			name = e.Schema + "." + e.Name
		}
		out = append(out, completionItem{Label: name, Kind: ciEnum, Detail: "enum"})
	}
	return out
}

func enumsInSchema(schema *model.Schema, sch string) []completionItem {
	if schema == nil {
		return nil
	}
	var out []completionItem
	for _, e := range schema.Enums {
		if e.Schema == sch {
			out = append(out, completionItem{Label: e.Name, Kind: ciEnum, Detail: "enum"})
		}
	}
	return out
}

// tableCandidates lists every table by its canonical reference (schema-qualified
// when the table was defined under a schema, bare otherwise), which is exactly
// how the buffer must refer to it.
func tableCandidates(schema *model.Schema, _ *ast.File) []completionItem {
	if schema == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []completionItem
	for _, t := range schema.Tables {
		name := t.Name
		if t.Schema != "" {
			name = t.Schema + "." + t.Name
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, completionItem{Label: name, Kind: ciClass, Detail: "table"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func tablesInSchema(schema *model.Schema, sch string) []completionItem {
	if schema == nil {
		return nil
	}
	var out []completionItem
	for _, t := range schema.Tables {
		if t.Schema == sch {
			out = append(out, completionItem{Label: t.Name, Kind: ciClass, Detail: "table"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func columnsOfTable(schema *model.Schema, name string) []completionItem {
	if schema == nil {
		return nil
	}
	t := schema.Lookup(name)
	if t == nil {
		return nil
	}
	var out []completionItem
	for _, c := range t.Columns {
		out = append(out, completionItem{Label: c.Name, Kind: ciField, Detail: c.Type})
	}
	return out
}

// topLevelKeywords offers block-scaffolding snippets at statement start.
func topLevelKeywords() []completionItem {
	snip := func(label, body string) completionItem {
		return completionItem{Label: label, Kind: ciSnippet, InsertText: body, InsertTextFormat: 2}
	}
	return []completionItem{
		snip("Table", "Table ${1:name} {\n\t$0\n}"),
		snip("TableGroup", "TableGroup ${1:name} {\n\t$0\n}"),
		snip("Ref", "Ref ${1:name} {\n\t$0\n}"),
		snip("Enum", "Enum ${1:name} {\n\t$0\n}"),
		snip("TablePartial", "TablePartial ${1:name} {\n\t$0\n}"),
		{Label: "Project", Kind: ciKeyword},
		{Label: "Note", Kind: ciKeyword},
		{Label: "use", Kind: ciKeyword},
		{Label: "reuse", Kind: ciKeyword},
	}
}

func keywords(kind int, words ...string) []completionItem {
	out := make([]completionItem, 0, len(words))
	for _, w := range words {
		out = append(out, completionItem{Label: strings.TrimRight(w, ": "), Kind: kind, InsertText: w})
	}
	return out
}

// --- lexical helpers --------------------------------------------------------

func lexAll(path, src string) []token.Token {
	var out []token.Token
	lx := lexer.New(path, src)
	for t := lx.Next(); t.Kind != token.EOF; t = lx.Next() {
		out = append(out, t)
	}
	return out
}

// byteOffset converts a 0-based (line, char) LSP position to a byte offset,
// clamping to the buffer bounds.
func byteOffset(src string, line, char int) int {
	off, ln := 0, 0
	for off < len(src) && ln < line {
		if src[off] == '\n' {
			ln++
		}
		off++
	}
	off += char
	if off > len(src) {
		off = len(src)
	}
	return off
}

// identBefore returns the identifier characters immediately preceding off.
func identBefore(src string, off int) string {
	if off > len(src) {
		off = len(src)
	}
	i := off
	for i > 0 && isIdentByte(src[i-1]) {
		i--
	}
	return src[i:off]
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}
