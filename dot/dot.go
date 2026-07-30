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
	// Crowfoot draws crow's-foot endpoints (crow/tee/odot arrowhead glyphs).
	// It is the zero value, so it is the default notation.
	Crowfoot Notation = iota
	// Label draws plain edges annotated with 1 / * / 0..1 cardinality text.
	Label
)

// Options configures the emitter. Zero value = crow's-foot, full detail, no
// notes, schema names shown.
type Options struct {
	Detail   Detail
	Notation Notation
	Notes    bool
	NoSchema bool // hide schema qualifiers in table headers
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
	case "crowfoot", "crow", "":
		return Crowfoot, true
	case "label":
		return Label, true
	}
	return Crowfoot, false
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

	var head strings.Builder
	if !bd.opt.NoSchema && t.Schema != "" && t.Schema != "public" {
		fmt.Fprintf(&head, `<font color="#cbd5e1" point-size="9">%s</font><br/>`, esc(t.Schema))
	}
	fmt.Fprintf(&head, "<b>%s</b>", esc(t.Name))
	if t.Alias != "" {
		fmt.Fprintf(&head, ` <font point-size="9">(%s)</font>`, esc(t.Alias))
	}
	hc := t.HeaderColor
	if hc == "" {
		hc = "#334155"
	}
	var rows strings.Builder
	fmt.Fprintf(&rows, `<tr><td colspan="3" bgcolor="%s" port="__h"><font color="white">%s</font></td></tr>`,
		hc, head.String())

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
		fmt.Fprintf(&rows, `<tr><td colspan="3" align="left"><font color="#64748b"><i>%s</i></font></td></tr>`, esc(t.Note))
	}

	bd.printf("  %s [label=<<table border=\"0\" cellborder=\"1\" cellspacing=\"0\" cellpadding=\"6\">%s</table>>];\n",
		id, rows.String())
}

// columnRow renders one column as a three-cell grid row: name (left), type
// (right), constraint badges (right). Ports "cN" (name/west side) and "cNr"
// (badge/east side) let edges attach to either table edge at this row.
func (bd *builder) columnRow(c *model.Column, port string) string {
	name := esc(c.Name)
	if c.PK {
		name = "<b>" + name + "</b>"
	}
	typ := ""
	if c.Type != "" {
		typ = `<font color="#64748b">` + esc(c.Type) + `</font>`
	}
	row := fmt.Sprintf(
		`<tr><td align="left" port="%s">%s</td><td align="right">%s</td><td align="right" port="%sr">%s</td></tr>`,
		port, name, typ, port, badges(c))
	if bd.opt.Notes && c.Note != "" {
		row += fmt.Sprintf(`<tr><td colspan="3" align="left"><font color="#94a3b8"><i>%s</i></font></td></tr>`, esc(c.Note))
	}
	return row
}

// badges renders PK/FK/NN/U constraint chips as small filled boxes.
func badges(c *model.Column) string {
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
	if len(tags) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<table border="0" cellborder="0" cellspacing="3" cellpadding="2"><tr>`)
	for _, t := range tags {
		fmt.Fprintf(&b, `<td bgcolor="#e2e8f0"><font point-size="8" color="#475569">%s</font></td>`, t)
	}
	b.WriteString(`</tr></table>`)
	return b.String()
}

// anchor returns the DOT endpoint for one side of a ref. side is "e" (east /
// table right edge, via the badge cell) or "w" (west / left edge, via the name
// cell), so edges attach to the table side aligned with the column's row.
func (bd *builder) anchor(e model.Endpoint, side string) (string, bool) {
	if e.Table == nil {
		return "", false
	}
	id := bd.nodeID[e.Table]
	if len(e.Columns) == 1 {
		if base, ok := bd.port[e.Table][e.Columns[0]]; ok {
			port := base
			if side == "e" {
				port = base + "r"
			}
			return fmt.Sprintf("%s:%s:%s", id, port, side), true
		}
	}
	return fmt.Sprintf("%s:__h:%s", id, side), true
}

func (bd *builder) edge(r *model.Ref) {
	// With rankdir=LR the tail (From) ranks left of the head (To): the tail
	// exits the east edge, the head enters the west edge.
	from, ok1 := bd.anchor(r.From, "e")
	to, ok2 := bd.anchor(r.To, "w")
	if !ok1 || !ok2 {
		return
	}
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
