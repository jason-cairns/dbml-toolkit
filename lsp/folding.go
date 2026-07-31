package lsp

import (
	"encoding/json"

	"github.com/jason-cairns/dbml-toolkit/token"
)

type foldingRange struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// onFoldingRange offers one fold per multi-line `{ ... }` block (tables, enums,
// groups, refs and their nested index/check blocks). Matching braces via the
// lexer keeps it correct even when the file does not fully parse.
func (s *Server) onFoldingRange(m *message) {
	var p docParams
	json.Unmarshal(m.Params, &p)
	path := uriToPath(p.TextDocument.URI)
	src := s.docs[path]
	out := []foldingRange{}
	var open []int // stack of opening lines (1-based)
	for _, t := range lexAll(path, src) {
		switch t.Kind {
		case token.LBrace:
			open = append(open, t.Pos.Line)
		case token.RBrace:
			if len(open) == 0 {
				continue
			}
			start := open[len(open)-1]
			open = open[:len(open)-1]
			// Fold only when the block spans multiple lines; keep the closing
			// brace line visible.
			if t.Pos.Line > start {
				out = append(out, foldingRange{StartLine: start - 1, EndLine: t.Pos.Line - 2})
			}
		}
	}
	s.conn.reply(m.ID, out)
}
