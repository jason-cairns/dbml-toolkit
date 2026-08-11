// Package dot is the graphviz rendering engine: it renders a resolved
// model.Schema into Graphviz DOT (HTML-like table labels) and, via the pure-Go
// go-graphviz engine, to SVG. It implements diagram.Engine.
package dot

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/goccy/go-graphviz"
	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/model"
)

// Engine is the graphviz diagram.Engine.
type Engine struct{}

// New returns the graphviz engine.
func New() Engine { return Engine{} }

func (Engine) Name() string { return "graphviz" }

func (Engine) Formats() []diagram.Format { return []diagram.Format{diagram.SVG, diagram.DOT} }

// Render produces DOT or SVG for the schema.
func (Engine) Render(s *model.Schema, opt diagram.Options, f diagram.Format) ([]byte, error) {
	src := Emit(s, opt)
	switch f {
	case diagram.DOT:
		return []byte(src), nil
	case diagram.SVG:
		return svg(src)
	default:
		return nil, fmt.Errorf("graphviz: unsupported format %q", f)
	}
}

// svg renders DOT source to SVG with the pure-Go graphviz engine.
func svg(dotSrc string) ([]byte, error) {
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	graph, err := graphviz.ParseBytes([]byte(dotSrc))
	if err != nil {
		return nil, err
	}
	defer graph.Close()
	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type builder struct {
	b      strings.Builder
	opt    diagram.Options
	nodeID map[*model.Table]string
	port   map[*model.Table]map[string]string
}

// Emit renders schema to DOT text.
func Emit(schema *model.Schema, opt diagram.Options) string {
	bd := &builder{opt: opt, nodeID: map[*model.Table]string{}, port: map[*model.Table]map[string]string{}}
	return bd.emit(schema)
}

func (bd *builder) emit(s *model.Schema) string {
	p := bd.printf
	p("digraph dbml {\n")
	// splines=polyline routes edges as node-avoiding segments (not ortho, which
	// ignores HTML-table ports inside clusters). Every column row is a full-width
	// port cell, so edges attach to the side facing the peer at the right row
	// without cutting through the table.
	p("  rankdir=LR;\n")
	p("  graph [splines=polyline, nodesep=0.5, ranksep=1.0, bgcolor=\"transparent\"];\n")
	p("  node [shape=plain, fontname=\"Helvetica\", fontsize=11, fontcolor=\"#0f172a\"];\n")
	p("  edge [fontname=\"Helvetica\", fontsize=10, color=\"#5b6b7b\"];\n\n")

	for i, t := range s.Tables {
		bd.nodeID[t] = fmt.Sprintf("t%d", i)
	}
	for _, t := range s.Tables {
		bd.table(t)
	}
	bd.groups(s)
	for _, r := range s.Refs {
		bd.edge(r)
	}
	p("}\n")
	return bd.b.String()
}

func (bd *builder) printf(format string, args ...any) {
	fmt.Fprintf(&bd.b, format, args...)
}

func (bd *builder) table(t *model.Table) {
	id := bd.nodeID[t]
	bd.port[t] = map[string]string{}

	var head strings.Builder
	if !bd.opt.NoSchema && t.Schema != "" && t.Schema != "public" {
		fmt.Fprintf(&head, `<font color="#cbd5e1" point-size="9">%s</font><br/>`, esc(t.Schema))
	}
	fmt.Fprintf(&head, "<b>%s</b>", esc(t.Name))
	if t.External {
		head.WriteString(` <font point-size="9">(external)</font>`)
	}
	if t.Alias != "" {
		fmt.Fprintf(&head, ` <font point-size="9">(%s)</font>`, esc(t.Alias))
	}
	hc := t.HeaderColor
	if hc == "" {
		hc = "#334155"
	}
	var rows strings.Builder
	fmt.Fprintf(&rows, `<tr><td bgcolor="%s" port="__h"><font color="white">%s</font></td></tr>`,
		hc, head.String())

	if bd.opt.Detail != diagram.Tables {
		for ci, c := range t.Columns {
			if bd.opt.Detail == diagram.Keys && !c.IsKey() {
				continue
			}
			portName := fmt.Sprintf("c%d", ci)
			bd.port[t][c.Name] = portName
			rows.WriteString(bd.columnRow(c, portName))
		}
	}
	if bd.opt.Notes && t.Note != "" {
		fmt.Fprintf(&rows, `<tr><td align="left"><font color="#64748b"><i>%s</i></font></td></tr>`, esc(t.Note))
	}

	bd.printf("  %s [label=<<table border=\"0\" cellborder=\"1\" cellspacing=\"0\" cellpadding=\"6\">%s</table>>];\n",
		id, rows.String())
}

// columnRow renders one column as a single full-width cell (so edges attach to
// whichever table side faces the peer, at this column's row, without cutting
// through the table). Inside, a borderless nested table lays out the name, the
// type, and PK/FK/NN/U chips.
func (bd *builder) columnRow(c *model.Column, port string) string {
	name := esc(c.Name)
	if c.PK {
		name = "<b>" + name + "</b>"
	}
	var in strings.Builder
	in.WriteString(`<table border="0" cellborder="0" cellspacing="6" cellpadding="1"><tr>`)
	fmt.Fprintf(&in, `<td align="left">%s</td>`, name)
	if c.Type != "" {
		fmt.Fprintf(&in, `<td><font color="#64748b">%s</font></td>`, esc(c.Type))
	}
	for _, t := range tagsFor(c) {
		fmt.Fprintf(&in, `<td bgcolor="#e2e8f0"><font point-size="8" color="#475569">%s</font></td>`, t)
	}
	in.WriteString(`</tr></table>`)

	row := fmt.Sprintf(`<tr><td port="%s">%s</td></tr>`, port, in.String())
	if bd.opt.Notes && c.Note != "" {
		row += fmt.Sprintf(`<tr><td align="left"><font color="#94a3b8"><i>%s</i></font></td></tr>`, esc(c.Note))
	}
	return row
}

// tagsFor lists the constraint badges for a column.
func tagsFor(c *model.Column) []string {
	var tags []string
	if c.PK {
		tags = append(tags, "PK")
	}
	if c.FK {
		tags = append(tags, "FK")
	}
	if c.NotNull {
		tags = append(tags, "NN")
	}
	if c.Unique {
		tags = append(tags, "U")
	}
	return tags
}

// anchor attaches an edge to a column's row via its full-width port; graphviz
// picks the table side facing the peer. No compass is used, so it works the
// same under every layout engine.
func (bd *builder) anchor(e model.Endpoint) (string, bool) {
	if e.Table == nil {
		return "", false
	}
	id := bd.nodeID[e.Table]
	if len(e.Columns) == 1 {
		if port, ok := bd.port[e.Table][e.Columns[0]]; ok {
			return id + ":" + port, true
		}
	}
	return id + ":__h", true
}

func (bd *builder) edge(r *model.Ref) {
	from, ok1 := bd.anchor(r.From)
	to, ok2 := bd.anchor(r.To)
	if !ok1 || !ok2 {
		return
	}
	fromMany, toMany := cardinality(r.Op)
	fromOpt := r.FromOptional
	toOpt := r.ToOptional
	if bd.opt.Notation == diagram.Crowfoot {
		bd.printf("  %s -> %s [dir=both, arrowtail=%s, arrowhead=%s];\n",
			from, to, crow(fromMany, fromOpt), crow(toMany, toOpt))
	} else {
		bd.printf("  %s -> %s [dir=none, taillabel=\"%s\", headlabel=\"%s\"];\n",
			from, to, card(fromMany, fromOpt), card(toMany, toOpt))
	}
}

func (bd *builder) groups(s *model.Schema) {
	for i, g := range s.Groups {
		bd.printf("  subgraph cluster_g%d {\n    label=\"%s\";\n    style=\"rounded\";\n    color=\"#cbd5e1\";\n    fontcolor=\"#334155\";\n", i, esc(g.Name))
		for _, m := range g.Members {
			if t := s.Lookup(qualified(m.Schema, m.Table)); t != nil {
				bd.printf("    %s;\n", bd.nodeID[t])
			}
		}
		bd.printf("  }\n")
	}
}

// cardinality reports whether each side of op is the "many" side.
func cardinality(op string) (fromMany, toMany bool) {
	switch op {
	case ">":
		return true, false
	case "<":
		return false, true
	case "<>":
		return true, true
	default: // "-"
		return false, false
	}
}

// crow builds a crow's-foot arrowhead glyph.
func crow(many, optional bool) string {
	outer := "tee"
	if many {
		outer = "crow"
	}
	inner := "tee"
	if optional {
		inner = "odot"
	}
	return outer + inner
}

func card(many, optional bool) string {
	if many {
		if optional {
			return "0..*"
		}
		return "*"
	}
	if optional {
		return "0..1"
	}
	return "1"
}

func qualified(schema, table string) string {
	if schema == "" {
		return table
	}
	return schema + "." + table
}

// esc escapes text for inclusion in an HTML-like DOT label.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\n", " ")
	return r.Replace(s)
}
