package lsp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/token"
)

// semTokenTypes is the legend advertised to the client; the index of each name
// is the token-type id used in the encoded stream. Names are standard LSP
// semantic token types so editor themes map them without extra configuration.
var semTokenTypes = []string{
	"keyword",    // 0
	"type",       // 1  column types, quoted spaced types
	"class",      // 2  table names and table references
	"enum",       // 3  enum type names
	"enumMember", // 4  enum values
	"property",   // 5  column names / index fields
	"namespace",  // 6  schemas and table-group names
	"string",     // 7
	"number",     // 8
	"comment",    // 9
	"operator",   // 10
}

const (
	stKeyword = iota
	stType
	stClass
	stEnum
	stEnumMember
	stProperty
	stNamespace
	stString
	stNumber
	stComment
	stOperator
)

// semTok is a resolved highlight span (0-based line/col).
type semTok struct {
	line, col, length, typ int
}

var blockKeywords = map[string]bool{
	"table": true, "tablepartial": true, "tablegroup": true, "enum": true,
	"ref": true, "note": true, "project": true, "use": true, "reuse": true,
	"records": true,
}

var settingKeywords = map[string]bool{
	"pk": true, "primary": true, "key": true, "unique": true, "not": true,
	"null": true, "increment": true, "note": true, "ref": true, "default": true,
	"delete": true, "update": true, "name": true, "type": true,
	"headercolor": true, "color": true, "cascade": true, "restrict": true,
	"set": true, "action": true, "no": true, "btree": true, "hash": true,
	"gin": true, "gist": true,
}

// onSemanticTokens classifies the whole buffer. It runs off the lexer plus a
// light block-context pass, so it survives partially-invalid buffers.
func (s *Server) onSemanticTokens(m *message) {
	var p docParams
	json.Unmarshal(m.Params, &p)
	path := uriToPath(p.TextDocument.URI)
	toks := semanticTokens(path, s.docs[path])
	s.conn.reply(m.ID, map[string]any{"data": encodeSemTokens(toks)})
}

// semanticTokens is the pure core: it returns the highlight spans for src.
func semanticTokens(path, src string) []semTok {
	toks := lexAll(path, src)
	out := commentSpans(src, toks)

	var stack []string // enclosing block keywords (lowercased)
	brack := 0
	prevLine := -1
	identOrd := 0     // idents seen so far on this line at bracket depth 0
	lineFirstKw := "" // first ident on the current line, lowercased
	headerExpect := -1

	emit := func(t token.Token, typ int) {
		if t.End.Line != t.Pos.Line { // spans lines: skip (semantic tokens are single-line)
			return
		}
		out = append(out, semTok{line: t.Pos.Line - 1, col: t.Pos.Col - 1, length: t.End.Col - t.Pos.Col, typ: typ})
	}

	for i, t := range toks {
		if t.Pos.Line-1 != prevLine {
			prevLine = t.Pos.Line - 1
			identOrd = 0
			lineFirstKw = ""
			headerExpect = -1
		}
		switch t.Kind {
		case token.LBrace:
			stack = append(stack, lineFirstKw)
			headerExpect = -1
		case token.RBrace:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case token.LBrack:
			brack++
		case token.RBrack:
			if brack > 0 {
				brack--
			}
		case token.String, token.BlockString:
			emit(t, stString)
		case token.QIdent:
			emit(t, stType)
		case token.Number:
			emit(t, stNumber)
		case token.Color:
			emit(t, stNumber)
		case token.Lt, token.Gt, token.Minus, token.LtGt, token.Question:
			emit(t, stOperator)
		case token.Ident:
			if brack == 0 && identOrd == 0 {
				lineFirstKw = strings.ToLower(t.Lit)
			}
			if typ, ok := classifyIdent(toks, i, stack, brack, identOrd, &headerExpect); ok {
				emit(t, typ)
			}
			if brack == 0 {
				identOrd++
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].line != out[b].line {
			return out[a].line < out[b].line
		}
		return out[a].col < out[b].col
	})
	return out
}

// classifyIdent decides the token type for an identifier from its block context,
// its position on the line and its neighbours. headerExpect carries the pending
// role of a block's name across the `schema.` prefix.
func classifyIdent(toks []token.Token, i int, stack []string, brack, ord int, headerExpect *int) (int, bool) {
	lower := strings.ToLower(toks[i].Lit)
	nextIsDot := i+1 < len(toks) && toks[i+1].Kind == token.Dot
	prevIsDot := i > 0 && toks[i-1].Kind == token.Dot

	if brack > 0 {
		if settingKeywords[lower] {
			return stKeyword, true
		}
		return 0, false
	}

	if len(stack) == 0 { // top level
		if ord == 0 && blockKeywords[lower] {
			switch lower {
			case "table", "tablepartial":
				*headerExpect = stClass
			case "tablegroup":
				*headerExpect = stNamespace
			case "enum":
				*headerExpect = stEnum
			default:
				*headerExpect = -1
			}
			return stKeyword, true
		}
		if *headerExpect != -1 {
			if nextIsDot {
				return stNamespace, true // qualifying schema
			}
			typ := *headerExpect
			*headerExpect = -1
			return typ, true
		}
		if lower == "as" || lower == "from" {
			return stKeyword, true
		}
		return 0, false
	}

	switch stack[len(stack)-1] {
	case "table", "tablepartial":
		if ord == 0 {
			if lower == "indexes" || lower == "checks" || lower == "note" {
				return stKeyword, true
			}
			return stProperty, true // column name
		}
		if ord == 1 {
			return stType, true // column type
		}
		return 0, false
	case "enum":
		if ord == 0 {
			return stEnumMember, true
		}
		return 0, false
	case "tablegroup":
		if nextIsDot {
			return stNamespace, true
		}
		return stClass, true
	case "ref":
		if prevIsDot && !nextIsDot {
			return stProperty, true // the column part of table.col
		}
		return stClass, true
	case "indexes", "checks":
		return stProperty, true
	}
	return 0, false
}

// commentSpans extracts `//` and `/* */` comments from the whitespace gaps
// between tokens (the lexer treats them as trivia). Scanning only the gaps
// avoids mistaking a `//` inside a string for a comment.
func commentSpans(src string, toks []token.Token) []semTok {
	var out []semTok
	prev := token.Pos{Line: 1, Col: 1, Off: 0}
	scan := func(text string, at token.Pos) {
		line, col := at.Line-1, at.Col-1
		for i := 0; i < len(text); {
			c := text[i]
			switch {
			case c == '\n':
				line++
				col = 0
				i++
			case c == '/' && i+1 < len(text) && text[i+1] == '/':
				j := i
				for j < len(text) && text[j] != '\n' {
					j++
				}
				out = append(out, semTok{line: line, col: col, length: j - i, typ: stComment})
				col += j - i
				i = j
			case c == '/' && i+1 < len(text) && text[i+1] == '*':
				segStart := i
				segLine, segCol := line, col
				i += 2
				col += 2
				for i < len(text) && !(text[i-1] == '*' && text[i] == '/' && i > segStart+1) {
					if text[i] == '\n' {
						out = append(out, semTok{line: segLine, col: segCol, length: i - segStart, typ: stComment})
						line++
						col = 0
						i++
						segStart, segLine, segCol = i, line, col
						continue
					}
					col++
					i++
				}
				if i < len(text) { // include closing '/'
					col++
					i++
				}
				out = append(out, semTok{line: segLine, col: segCol, length: i - segStart, typ: stComment})
			default:
				col++
				i++
			}
		}
	}
	for _, t := range toks {
		if t.Pos.Off > prev.Off {
			scan(src[prev.Off:t.Pos.Off], prev)
		}
		if t.End.Off > prev.Off {
			prev = t.End
		}
	}
	if prev.Off < len(src) {
		scan(src[prev.Off:], prev)
	}
	return out
}

// encodeSemTokens delta-encodes spans into the LSP integer stream (5 ints per
// token: deltaLine, deltaStartChar, length, tokenType, tokenModifiers).
func encodeSemTokens(toks []semTok) []int {
	data := make([]int, 0, len(toks)*5)
	prevLine, prevCol := 0, 0
	for _, s := range toks {
		dLine := s.line - prevLine
		dCol := s.col
		if dLine == 0 {
			dCol = s.col - prevCol
		}
		data = append(data, dLine, dCol, s.length, s.typ, 0)
		prevLine, prevCol = s.line, s.col
	}
	return data
}
