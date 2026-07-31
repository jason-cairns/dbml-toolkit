// Package token defines the lexical tokens of the DBML language.
package token

import "fmt"

// Kind enumerates token categories.
type Kind int

const (
	EOF Kind = iota
	Illegal
	Ident       // bare identifier or keyword (keywords are contextual)
	Number      // 42, 3.14
	String      // 'single quoted'
	QIdent      // "double quoted" identifier / spaced type
	BlockString // '''triple quoted'''
	Expr        // `backtick expression`
	Color       // #fff or #rrggbb

	LBrace // {
	RBrace // }
	LBrack // [
	RBrack // ]
	LParen // (
	RParen // )
	Comma  // ,
	Colon  // :
	Dot    // .
	Tilde  // ~
	Lt     // <
	Gt     // >
	Minus  // -
	LtGt   // <>
	Star   // *
)

// Pos is a source position.
type Pos struct {
	File string
	Line int // 1-based
	Col  int // 1-based
	Off  int // 0-based byte offset
}

func (p Pos) String() string { return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col) }

// Token is a lexical token with its literal text and position.
type Token struct {
	Kind Kind
	Lit  string
	Pos  Pos
	End  Pos // position just past the token
}

// Comment is a source comment captured as lexical trivia. The lexer discards
// comments from the token stream but records them here so tools like the
// formatter can splice them back in by position. Text is the comment body
// without its delimiters. Block distinguishes /* */ from //. Trailing is true
// when code preceded the comment on the same source line (an inline comment).
type Comment struct {
	Text     string
	Pos      Pos
	End      Pos
	Block    bool
	Trailing bool
}

var names = map[Kind]string{
	EOF: "EOF", Illegal: "ILLEGAL", Ident: "IDENT", Number: "NUMBER",
	String: "STRING", QIdent: "QIDENT", BlockString: "BLOCKSTRING", Expr: "EXPR",
	Color: "COLOR", LBrace: "{", RBrace: "}", LBrack: "[", RBrack: "]",
	LParen: "(", RParen: ")", Comma: ",", Colon: ":", Dot: ".", Tilde: "~",
	Lt: "<", Gt: ">", Minus: "-", LtGt: "<>", Star: "*",
}

func (k Kind) String() string {
	if s, ok := names[k]; ok {
		return s
	}
	return "UNKNOWN"
}
