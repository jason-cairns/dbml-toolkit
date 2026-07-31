package lsp

import (
	"encoding/json"
)

type renameParams struct {
	TextDocument textDocID `json:"textDocument"`
	Position     position  `json:"position"`
	NewName      string    `json:"newName"`
}

type textEdit struct {
	Range   rng    `json:"range"`
	NewText string `json:"newText"`
}

// onPrepareRename validates that the symbol under the cursor is renameable and
// returns the exact range the client should highlight for editing.
func (s *Server) onPrepareRename(m *message) {
	var p posParams
	json.Unmarshal(m.Params, &p)
	idx := s.index(p.TextDocument.URI)
	if o := idx.at(uriToPath(p.TextDocument.URI), p.Position); o != nil {
		s.conn.reply(m.ID, o.loc.Range)
		return
	}
	s.conn.reply(m.ID, nil)
}

// onRename rewrites every occurrence of the symbol under the cursor — its
// definition and all references across every open file — to the new name.
func (s *Server) onRename(m *message) {
	var p renameParams
	json.Unmarshal(m.Params, &p)
	idx := s.index(p.TextDocument.URI)
	o := idx.at(uriToPath(p.TextDocument.URI), p.Position)
	if o == nil {
		s.conn.reply(m.ID, nil)
		return
	}
	changes := map[string][]textEdit{}
	for _, occ := range idx.occs {
		if occ.target != o.target {
			continue
		}
		uri := occ.loc.URI
		changes[uri] = append(changes[uri], textEdit{Range: occ.loc.Range, NewText: p.NewName})
	}
	s.conn.reply(m.ID, map[string]any{"changes": changes})
}
