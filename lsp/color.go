package lsp

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/lexer"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// color is an RGBA colour with channels in the 0..1 range, matching the LSP
// Color type.
type color struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

type colorInformation struct {
	Range rng   `json:"range"`
	Color color `json:"color"`
}

type colorPresentation struct {
	Label string `json:"label"`
}

type colorPresentationParams struct {
	TextDocument textDocID `json:"textDocument"`
	Color        color     `json:"color"`
	Range        rng       `json:"range"`
}

// onDocumentColor reports every `#rrggbb` colour literal in the buffer so the
// editor can render an inline swatch. It works purely off the lexer, so it
// keeps functioning while the surrounding document is mid-edit and unparseable.
func (s *Server) onDocumentColor(m *message) {
	var p docParams
	json.Unmarshal(m.Params, &p)
	path := uriToPath(p.TextDocument.URI)
	src, ok := s.docs[path]
	if !ok {
		s.conn.reply(m.ID, []colorInformation{})
		return
	}
	out := []colorInformation{}
	lx := lexer.New(path, src)
	for t := lx.Next(); t.Kind != token.EOF; t = lx.Next() {
		if t.Kind != token.Color {
			continue
		}
		c, ok := parseHexColor(t.Lit)
		if !ok {
			continue
		}
		out = append(out, colorInformation{
			Range: rng{
				Start: position{Line: t.Pos.Line - 1, Char: t.Pos.Col - 1},
				End:   position{Line: t.End.Line - 1, Char: t.End.Col - 1},
			},
			Color: c,
		})
	}
	s.conn.reply(m.ID, out)
}

// onColorPresentation turns a colour the user picked back into the `#rrggbb`
// text that replaces the swatch's range.
func (s *Server) onColorPresentation(m *message) {
	var p colorPresentationParams
	json.Unmarshal(m.Params, &p)
	s.conn.reply(m.ID, []colorPresentation{{Label: hexOf(p.Color)}})
}

// parseHexColor decodes #rgb, #rgba, #rrggbb and #rrggbbaa into an RGBA colour.
func parseHexColor(s string) (color, bool) {
	h := strings.TrimPrefix(s, "#")
	switch len(h) {
	case 3, 4: // shorthand: each digit is doubled (f -> ff)
		var expanded strings.Builder
		for i := 0; i < len(h); i++ {
			expanded.WriteByte(h[i])
			expanded.WriteByte(h[i])
		}
		h = expanded.String()
	case 6, 8:
	default:
		return color{}, false
	}
	ch := func(i int) (float64, bool) {
		v, err := strconv.ParseInt(h[i:i+2], 16, 0)
		if err != nil {
			return 0, false
		}
		return float64(v) / 255, true
	}
	r, ok1 := ch(0)
	g, ok2 := ch(2)
	b, ok3 := ch(4)
	if !ok1 || !ok2 || !ok3 {
		return color{}, false
	}
	a := 1.0
	if len(h) == 8 {
		var ok bool
		if a, ok = ch(6); !ok {
			return color{}, false
		}
	}
	return color{Red: r, Green: g, Blue: b, Alpha: a}, true
}

// hexOf renders a colour as #rrggbb, appending the alpha byte only when the
// colour is not fully opaque.
func hexOf(c color) string {
	b := func(f float64) string {
		v := int(f*255 + 0.5)
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		s := strconv.FormatInt(int64(v), 16)
		if len(s) == 1 {
			s = "0" + s
		}
		return s
	}
	out := "#" + b(c.Red) + b(c.Green) + b(c.Blue)
	if c.Alpha < 1 {
		out += b(c.Alpha)
	}
	return out
}
