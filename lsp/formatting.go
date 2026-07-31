package lsp

import (
	"encoding/json"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/format"
)

type formattingParams struct {
	TextDocument textDocID `json:"textDocument"`
}

// onFormatting handles textDocument/formatting. It reformats the whole buffer
// into canonical DBML and replies with a single full-document edit. A buffer
// that does not parse cleanly is left untouched (null reply) so a half-typed or
// broken file is never mangled.
func (s *Server) onFormatting(m *message) {
	var p formattingParams
	json.Unmarshal(m.Params, &p)
	path := uriToPath(p.TextDocument.URI)
	src, ok := s.docs[path]
	if !ok {
		s.conn.reply(m.ID, nil)
		return
	}
	out, diags := format.Format(path, src)
	if len(diags) > 0 || out == src {
		// Nothing to do, or the file has syntax errors: reply with no edits.
		s.conn.reply(m.ID, nil)
		return
	}
	s.conn.reply(m.ID, []textEdit{{
		Range:   fullRange(src),
		NewText: out,
	}})
}

// fullRange returns a range spanning the entire document, from the start to the
// position just past the last byte. Character offsets follow the same byte-based
// convention as the rest of this server.
func fullRange(src string) rng {
	line := strings.Count(src, "\n")
	lastNL := strings.LastIndexByte(src, '\n')
	char := len(src) - (lastNL + 1)
	return rng{
		Start: position{Line: 0, Char: 0},
		End:   position{Line: line, Char: char},
	}
}
