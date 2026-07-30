// Package lexer converts DBML source text into a stream of tokens.
package lexer

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jason-cairns/dbml-toolkit/token"
)

// Lexer scans DBML source. Construct with New and pull tokens with Next.
type Lexer struct {
	file string
	src  string
	off  int
	line int
	col  int
}

// New creates a Lexer for src attributed to file.
func New(file, src string) *Lexer {
	return &Lexer{file: file, src: src, off: 0, line: 1, col: 1}
}

func (l *Lexer) pos() token.Pos {
	return token.Pos{File: l.file, Line: l.line, Col: l.col, Off: l.off}
}

func (l *Lexer) peek() byte {
	if l.off >= len(l.src) {
		return 0
	}
	return l.src[l.off]
}

func (l *Lexer) peek2() byte {
	if l.off+1 >= len(l.src) {
		return 0
	}
	return l.src[l.off+1]
}

func (l *Lexer) advance() byte {
	c := l.src[l.off]
	l.off++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

// Next returns the next token, skipping whitespace and comments.
func (l *Lexer) Next() token.Token {
	l.skipTrivia()
	start := l.pos()
	if l.off >= len(l.src) {
		return token.Token{Kind: token.EOF, Pos: start, End: start}
	}
	c := l.peek()
	switch {
	case c == '{':
		return l.punct(token.LBrace)
	case c == '}':
		return l.punct(token.RBrace)
	case c == '[':
		return l.punct(token.LBrack)
	case c == ']':
		return l.punct(token.RBrack)
	case c == '(':
		return l.punct(token.LParen)
	case c == ')':
		return l.punct(token.RParen)
	case c == ',':
		return l.punct(token.Comma)
	case c == ':':
		return l.punct(token.Colon)
	case c == '.':
		return l.punct(token.Dot)
	case c == '~':
		return l.punct(token.Tilde)
	case c == '>':
		return l.punct(token.Gt)
	case c == '-':
		return l.punct(token.Minus)
	case c == '*':
		return l.punct(token.Star)
	case c == '<':
		if l.peek2() == '>' {
			l.advance()
			l.advance()
			return l.tok(token.LtGt, "<>", start)
		}
		return l.punct(token.Lt)
	case c == '\'':
		return l.scanSingle(start)
	case c == '"':
		return l.scanQuoted(start)
	case c == '`':
		return l.scanExpr(start)
	case c == '#':
		return l.scanColor(start)
	case c >= '0' && c <= '9':
		return l.scanNumber(start)
	case isIdentStart(c):
		return l.scanIdent(start)
	default:
		l.advance()
		return l.tok(token.Illegal, string(c), start)
	}
}

func (l *Lexer) punct(k token.Kind) token.Token {
	start := l.pos()
	ch := l.advance()
	return l.tok(k, string(ch), start)
}

func (l *Lexer) tok(k token.Kind, lit string, start token.Pos) token.Token {
	return token.Token{Kind: k, Lit: lit, Pos: start, End: l.pos()}
}

func (l *Lexer) skipTrivia() {
	for l.off < len(l.src) {
		c := l.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			l.advance()
		case c == '/' && l.peek2() == '/':
			for l.off < len(l.src) && l.peek() != '\n' {
				l.advance()
			}
		case c == '/' && l.peek2() == '*':
			l.advance()
			l.advance()
			for l.off < len(l.src) && (l.peek() != '*' || l.peek2() != '/') {
				l.advance()
			}
			if l.off < len(l.src) {
				l.advance()
				l.advance()
			}
		default:
			return
		}
	}
}

func (l *Lexer) scanSingle(start token.Pos) token.Token {
	// triple-quoted block string?
	if l.peek2() == '\'' && l.off+2 < len(l.src) && l.src[l.off+2] == '\'' {
		return l.scanBlock(start)
	}
	l.advance() // opening '
	var b strings.Builder
	for l.off < len(l.src) && l.peek() != '\n' {
		c := l.peek()
		if c == '\\' {
			l.advance()
			b.WriteByte(unescape(l.peek()))
			l.advance()
			continue
		}
		if c == '\'' {
			l.advance()
			return l.tok(token.String, b.String(), start)
		}
		b.WriteByte(l.advance())
	}
	return l.tok(token.Illegal, b.String(), start)
}

func (l *Lexer) scanBlock(start token.Pos) token.Token {
	l.advance()
	l.advance()
	l.advance() // '''
	var b strings.Builder
	for l.off < len(l.src) {
		if l.peek() == '\'' && l.peek2() == '\'' && l.off+2 < len(l.src) && l.src[l.off+2] == '\'' {
			l.advance()
			l.advance()
			l.advance()
			return l.tok(token.BlockString, dedent(b.String()), start)
		}
		if l.peek() == '\\' {
			nx := l.peek2()
			if nx == '\n' { // line continuation
				l.advance()
				l.advance()
				continue
			}
			if nx == '\'' || nx == '\\' {
				l.advance()
				b.WriteByte(unescape(l.peek()))
				l.advance()
				continue
			}
		}
		b.WriteByte(l.advance())
	}
	return l.tok(token.Illegal, b.String(), start)
}

func (l *Lexer) scanQuoted(start token.Pos) token.Token {
	l.advance()
	var b strings.Builder
	for l.off < len(l.src) && l.peek() != '\n' {
		c := l.peek()
		if c == '\\' {
			l.advance()
			b.WriteByte(unescape(l.peek()))
			l.advance()
			continue
		}
		if c == '"' {
			l.advance()
			return l.tok(token.QIdent, b.String(), start)
		}
		b.WriteByte(l.advance())
	}
	return l.tok(token.Illegal, b.String(), start)
}

func (l *Lexer) scanExpr(start token.Pos) token.Token {
	l.advance()
	var b strings.Builder
	for l.off < len(l.src) && l.peek() != '`' {
		b.WriteByte(l.advance())
	}
	if l.off < len(l.src) {
		l.advance()
	}
	return l.tok(token.Expr, b.String(), start)
}

func (l *Lexer) scanColor(start token.Pos) token.Token {
	l.advance() // #
	var b strings.Builder
	b.WriteByte('#')
	for l.off < len(l.src) && isHex(l.peek()) {
		b.WriteByte(l.advance())
	}
	return l.tok(token.Color, b.String(), start)
}

func (l *Lexer) scanNumber(start token.Pos) token.Token {
	var b strings.Builder
	for l.off < len(l.src) && (l.peek() >= '0' && l.peek() <= '9' || l.peek() == '.') {
		b.WriteByte(l.advance())
	}
	return l.tok(token.Number, b.String(), start)
}

func (l *Lexer) scanIdent(start token.Pos) token.Token {
	var b strings.Builder
	for l.off < len(l.src) && isIdentPart(l.peek()) {
		b.WriteByte(l.advance())
	}
	return l.tok(token.Ident, b.String(), start)
}

func isIdentStart(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c)) || c >= utf8.RuneSelf
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	default:
		return c
	}
}

// dedent removes the common leading-whitespace indent from a block string.
func dedent(s string) string {
	s = strings.TrimPrefix(s, "\n")
	lines := strings.Split(s, "\n")
	min := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		n := len(ln) - len(strings.TrimLeft(ln, " \t"))
		if min < 0 || n < min {
			min = n
		}
	}
	if min > 0 {
		for i, ln := range lines {
			if len(ln) >= min {
				lines[i] = ln[min:]
			}
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n \t")
}
