// Package dot renders a resolved model.Schema into Graphviz DOT using
// HTML-like table labels. Two relationship notations are supported and three
// levels of detail.
package dot

import (
	"fmt"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/model"
)

// Detail controls how much of each table is drawn.
type Detail int

const (
	Full   Detail = iota // every column
	Keys                 // only pk / fk / unique columns
	Tables               // table names only
)

// Notation controls how relationship endpoints are drawn.
type Notation int

const (
	// Label draws plain edges annotated with 1 / * / 0..1 cardinality text.
	Label Notation = iota
	// Crowfoot draws crow's-foot endpoints (crow/tee/odot arrowhead glyphs).
	Crowfoot
)

// Options configures the emitter.
type Options struct {
	Detail   Detail
	Notation Notation
	Notes    bool
}

// ParseDetail maps a CLI string to a Detail.
func ParseDetail(s string) (Detail, bool) {
	switch strings.ToLower(s) {
	case "full", "":
		return Full, true
	case "keys":
		return Keys, true
	case "tables", "tables-only":
		return Tables, true
	}
	return Full, false
}

// ParseNotation maps a CLI string to a Notation.
func ParseNotation(s string) (Notation, bool) {
	switch strings.ToLower(s) {
	case "label", "":
		return Label, true
	case "crowfoot", "crow":
		return Crowfoot, true
	}
	return Label, false
}

type builder struct {
	b      strings.Builder
	opt    Options
	nodeID map[*model.Table]string
	port   map[*model.Table]map[string]string
}

// Emit renders schema to DOT text.
func Emit(schema *model.Schema, opt Options) string {
	bd := &builder{opt: opt, nodeID: map[*model.Table]string{}, port: map[*model.Table]map[string]string{}}
	return bd.emit(schema)
}

func (bd *builder) emit(s *model.Schema) string {
	p := bd.printf
	p("digraph dbml {\n")
	p("  rankdir=LR;\n")
	// splines=polyline (not ortho): orthogonal routing ignores HTML-table ports
	// when nodes sit inside a cluster (TableGroup), collapsing every edge to the
	// table centre. polyline honours the ports, so edges line up with columns.
	p("  graph [splines=polyline, nodesep=0.6, ranksep=1.1, bgcolor=\"transparent\"];\n")
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
	header := t.Name
	if t.Alias != "" {
		header += " (" + t.Alias + ")"
	}
	hc := t.HeaderColor
	if hc == "" {
		hc = "#334155"
	}
	var rows strings.Builder
	fmt.Fprintf(&rows, `<tr><td bgcolor="%s" port="__h"><font color="white"><b>%s</b></font></td></tr>`,
		hc, esc(header))

	if bd.opt.Detail != Tables {
		for ci, c := range t.Columns {
			if bd.opt.Detail == Keys && !c.IsKey() {
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

func (bd *builder) columnRow(c *model.Column, port string) string {
	name := esc(c.Name)
	if c.PK {
		name = "<b>" + name + "</b>"
	}
	var marks []string
	if c.PK {
		marks = append(marks, "pk")
	}
	if c.FK {
		marks = append(marks, "fk")
	}
	if c.Unique {
		marks = append(marks, "u")
	}
	suffix := ""
	if len(marks) > 0 {
		suffix = ` <font color="#94a3b8">` + esc(strings.Join(marks, ",")) + `</font>`
	}
	typ := ""
	if c.Type != "" {
		typ = `  <font color="#64748b">` + esc(c.Type) + `</font>`
	}
	note := ""
	if bd.opt.Notes && c.Note != "" {
		note = `<br/><font color="#94a3b8"><i>` + esc(c.Note) + `</i></font>`
	}
	return fmt.Sprintf(`<tr><td align="left" port="%s">%s%s%s%s</td></tr>`, port, name, typ, suffix, note)
}

// anchor returns the DOT endpoint (node:port) for one side of a ref.
func (bd *builder) anchor(e model.Endpoint) (string, bool) {
	if e.Table == nil {
		return "", false
	}
	id := bd.nodeID[e.Table]
	if len(e.Columns) == 1 {
		if port, ok := bd.port[e.Table][e.Columns[0]]; ok {
			return fmt.Sprintf("%s:%s", id, port), true
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
	// Compass points force attachment to the table's side at the column's row:
	// with rankdir=LR the tail ranks left of the head, so the tail exits east
	// and the head enters west.
	from += ":e"
	to += ":w"
	fromMany, toMany := cardinality(r.Op)
	fromOpt := endpointOptional(r.From)
	toOpt := endpointOptional(r.To)
	if bd.opt.Notation == Crowfoot {
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

func endpointOptional(e model.Endpoint) bool {
	if e.Table == nil {
		return false
	}
	for _, name := range e.Columns {
		for _, c := range e.Table.Columns {
			if c.Name == name && !c.NotNull && !c.PK {
				return true
			}
		}
	}
	return false
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
