package lexer

import (
	"testing"

	"github.com/jason-cairns/dbml-toolkit/token"
)

// drain pulls every token so the lexer scans the whole source (comments are
// recorded as a side effect of skipTrivia).
func drain(src string) []token.Comment {
	l := New("t.dbml", src)
	for {
		if l.Next().Kind == token.EOF {
			break
		}
	}
	return l.Comments()
}

func TestCommentsCaptured(t *testing.T) {
	src := "// leading\nTable users { // trailing\n  id int\n}\n/* block */\n"
	cs := drain(src)
	if len(cs) != 3 {
		t.Fatalf("expected 3 comments, got %d: %+v", len(cs), cs)
	}

	if cs[0].Trailing || cs[0].Block || cs[0].Text != " leading" {
		t.Errorf("leading comment wrong: %+v", cs[0])
	}
	if cs[0].Pos.Line != 1 {
		t.Errorf("leading comment line = %d, want 1", cs[0].Pos.Line)
	}

	if !cs[1].Trailing || cs[1].Text != " trailing" {
		t.Errorf("trailing comment wrong: %+v", cs[1])
	}
	if cs[1].Pos.Line != 2 {
		t.Errorf("trailing comment line = %d, want 2", cs[1].Pos.Line)
	}

	if !cs[2].Block || cs[2].Trailing || cs[2].Text != " block " {
		t.Errorf("block comment wrong: %+v", cs[2])
	}
}

func TestBlockCommentMultiline(t *testing.T) {
	cs := drain("/* a\n b */\nTable t { id int }\n")
	if len(cs) != 1 || !cs[0].Block {
		t.Fatalf("expected one block comment, got %+v", cs)
	}
	if cs[0].Text != " a\n b " {
		t.Errorf("multiline block text = %q", cs[0].Text)
	}
}
