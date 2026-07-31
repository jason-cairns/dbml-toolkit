// Package d2 is the D2 rendering engine. It builds a D2 graph programmatically
// via the d2oracle API (never by templating D2 source), lays it out with ELK,
// and renders to SVG or ASCII. It implements diagram.Engine.
package d2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"oss.terrastruct.com/d2/d2compiler"
	"oss.terrastruct.com/d2/d2exporter"
	"oss.terrastruct.com/d2/d2format"
	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts"
	"oss.terrastruct.com/d2/d2layouts/d2elklayout"
	"oss.terrastruct.com/d2/d2oracle"
	"oss.terrastruct.com/d2/d2renderers/d2ascii"
	"oss.terrastruct.com/d2/d2renderers/d2fonts"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2target"
	d2log "oss.terrastruct.com/d2/lib/log"
	"oss.terrastruct.com/d2/lib/textmeasure"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/model"
)

// Engine is the D2 diagram.Engine.
type Engine struct{}

// New returns the D2 engine.
func New() Engine { return Engine{} }

func (Engine) Name() string { return "d2" }

func (Engine) Formats() []diagram.Format {
	return []diagram.Format{diagram.SVG, diagram.ASCII, diagram.D2}
}

// Render builds the D2 graph, lays it out with ELK, and renders the format.
func (Engine) Render(s *model.Schema, opt diagram.Options, f diagram.Format) ([]byte, error) {
	g, err := build(s, opt)
	if err != nil {
		return nil, err
	}
	if f == diagram.D2 {
		return []byte(d2format.Format(g.AST)), nil
	}
	ctx := quiet(context.Background())
	dg, err := layout(ctx, g, themeID(opt.Theme))
	if err != nil {
		return nil, err
	}
	switch f {
	case diagram.SVG:
		svg, err := d2svg.Render(dg, &d2svg.RenderOpts{})
		if err != nil {
			return nil, err
		}
		return directional(svg), nil
	case diagram.ASCII:
		return d2ascii.NewASCIIartist().Render(ctx, dg, &d2ascii.RenderOpts{})
	default:
		return nil, fmt.Errorf("d2: unsupported format %q", f)
	}
}

// directional makes animated relationships flow one way (source -> target,
// i.e. FK -> PK). D2 treats an edge with arrowheads on both ends as
// bidirectional and splits its animation to flow outward from the midpoint,
// reversing one half; dropping that reversal makes both halves flow src -> dst.
func directional(svg []byte) []byte {
	return bytes.ReplaceAll(svg, []byte("animation-direction: reverse;"), nil)
}

// quiet silences D2's debug logging (it otherwise writes stack traces to stderr).
func quiet(ctx context.Context) context.Context {
	return d2log.With(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// defaultTheme is the D2 theme used when none is set (Flagship Terrastruct).
const defaultTheme int64 = 3

// themeID resolves the effective theme (0 = default).
func themeID(t int64) int64 {
	if t == 0 {
		return defaultTheme
	}
	return t
}

// layout runs ELK over the built graph and exports a render-ready diagram,
// mirroring d2lib's internal pipeline via exported calls (no re-serialization).
func layout(ctx context.Context, g *d2graph.Graph, theme int64) (*d2target.Diagram, error) {
	if err := g.ApplyTheme(theme); err != nil {
		return nil, err
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, err
	}
	font := d2fonts.SourceSansPro
	if err := g.SetDimensions(nil, ruler, &font, nil); err != nil {
		return nil, err
	}
	elk := func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, nil)
	}
	if err := d2layouts.LayoutNested(ctx, g, d2layouts.NestedGraphInfo(g.Root), elk, nil); err != nil {
		return nil, err
	}
	return d2exporter.Export(ctx, g, &font, nil)
}

// --- programmatic graph construction (d2oracle) -----------------------------

type builder struct {
	g   *d2graph.Graph
	err error
}

func (b *builder) create(key string) {
	if b.err != nil {
		return
	}
	b.g, _, b.err = d2oracle.Create(b.g, nil, key)
}

func (b *builder) set(key, val string) {
	if b.err != nil {
		return
	}
	v := val
	b.g, b.err = d2oracle.Set(b.g, nil, key, nil, &v)
}

func build(s *model.Schema, opt diagram.Options) (*d2graph.Graph, error) {
	g, _, err := d2compiler.Compile("", strings.NewReader(""), nil)
	if err != nil {
		return nil, err
	}
	b := &builder{g: g}
	b.set("direction", "right")

	// TableGroups become plain containers; grouped tables are created inside
	// them (container prefix in the key) so no reparenting/Move is needed.
	group := map[*model.Table]string{}
	for gi, grp := range s.Groups {
		gid := fmt.Sprintf("g%d", gi)
		b.create(gid)
		if grp.Name != "" {
			b.set(gid, grp.Name)
		}
		if c := settingColor(grp); c != "" {
			b.set(gid+".style.fill", c)
		}
		if grp.Note != "" {
			b.set(gid+".tooltip", grp.Note)
		}
		for _, m := range grp.Members {
			if t := s.Lookup(qual(m.Schema, m.Table)); t != nil {
				group[t] = gid + "."
			}
		}
	}

	ids := map[*model.Table]string{}
	for i, t := range s.Tables {
		id := group[t] + fmt.Sprintf("t%d", i)
		ids[t] = id
		b.create(id)
		b.set(id+".shape", "sql_table")
		b.set(id, tableLabel(t, opt))
		if t.HeaderColor != "" {
			b.set(id+".style.fill", t.HeaderColor)
		}
		if opt.Detail != diagram.Tables {
			for _, c := range t.Columns {
				if opt.Detail == diagram.Keys && !c.IsKey() {
					continue
				}
				col := id + "." + key(c.Name)
				b.set(col, c.Type)
				if cst := constraintOf(c); cst != "" {
					b.set(col+".constraint", cst)
				}
			}
		}
		// D2's sql_table rows can't carry their own tooltip, so fold the table's
		// note and all of its column notes into the table's single hover tooltip.
		if tip := tableTooltip(t); tip != "" {
			b.set(id+".tooltip", tip)
		}
	}

	// A failed oracle edit (e.g. a half-typed color value during live editing)
	// can leave b.g nil, so bail before the refs loop dereferences it below.
	if b.err != nil {
		return nil, b.err
	}

	// Connections are created plain via the oracle; their crow's-foot arrowheads
	// are applied afterwards directly on the edge objects, because d2oracle can't
	// address an edge whose endpoints are sql_table columns. specs[i] pairs with
	// the i-th created edge (edges keep creation order).
	var specs []edgeSpec
	for _, r := range s.Refs {
		sp, src, dst := orient(ids, r)
		if src == "" || dst == "" {
			continue
		}
		before := len(b.g.Edges)
		b.create(src + " -> " + dst)
		if b.err != nil || len(b.g.Edges) == before {
			continue
		}
		specs = append(specs, sp)
	}

	for i, n := range s.Notes {
		nid := fmt.Sprintf("note%d", i)
		b.create(nid)
		b.set(nid+".shape", "text")
		b.set(nid, n.Text)
	}
	if b.err != nil {
		return nil, b.err
	}

	// Final pass on the edge objects (no more oracle edits after this): crow's-
	// foot arrowheads and animation. The source arrowhead only renders when
	// SrcArrow is set, so enable it explicitly (otherwise only one end shows).
	for i, sp := range specs {
		if i >= len(b.g.Edges) {
			break
		}
		e := b.g.Edges[i]
		if opt.Notation == diagram.Label {
			e.SrcArrowhead = arrowLabel(card(sp.srcMany, sp.srcOpt))
			e.DstArrowhead = arrowLabel(card(sp.dstMany, sp.dstOpt))
		} else {
			e.SrcArrow = true
			e.SrcArrowhead = arrowShape(crowfoot(sp.srcMany, sp.srcOpt))
			e.DstArrowhead = arrowShape(crowfoot(sp.dstMany, sp.dstOpt))
		}
		if !opt.NoAnimate {
			e.Style.Animated = &d2graph.Scalar{Value: "true"}
		}
	}
	return b.g, nil
}

// orient returns the edge endpoints and cardinality with the source fixed to
// the many/foreign-key side when the operator gives a direction, so the edge
// (and its animation) flows reference -> referent (FK -> PK).
func orient(ids map[*model.Table]string, r *model.Ref) (sp edgeSpec, src, dst string) {
	from := endpointKey(ids, r.From)
	to := endpointKey(ids, r.To)
	fromMany, toMany := cardinality(r.Op)
	fromOpt, toOpt := optional(r.From), optional(r.To)
	if r.Op == "<" { // From is the one side, To is the many side — swap
		return edgeSpec{toMany, fromMany, toOpt, fromOpt}, to, from
	}
	return edgeSpec{fromMany, toMany, fromOpt, toOpt}, from, to
}

// settingColor returns a TableGroup's color setting, if any.
func settingColor(g *ast.TableGroup) string {
	for _, s := range g.Settings {
		if strings.EqualFold(s.Name, "color") {
			return s.Value
		}
	}
	return ""
}

// qual joins an optional schema and name.
func qual(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

type edgeSpec struct{ srcMany, dstMany, srcOpt, dstOpt bool }

func arrowShape(shape string) *d2graph.Attributes {
	a := &d2graph.Attributes{}
	a.Shape.Value = shape
	return a
}

func arrowLabel(label string) *d2graph.Attributes {
	a := &d2graph.Attributes{}
	a.Label.Value = label
	return a
}

func tableLabel(t *model.Table, opt diagram.Options) string {
	if !opt.NoSchema && t.Schema != "" && t.Schema != "public" {
		return t.Schema + "." + t.Name
	}
	return t.Name
}

// tableTooltip combines a table's own note with a line per column note, since
// D2 can only attach one tooltip to the whole sql_table shape.
func tableTooltip(t *model.Table) string {
	var lines []string
	if t.Note != "" {
		lines = append(lines, t.Note)
	}
	for _, c := range t.Columns {
		if c.Note != "" {
			lines = append(lines, c.Name+": "+c.Note)
		}
	}
	return strings.Join(lines, "\n")
}

// constraintOf maps a column's flags to a single D2 sql_table constraint,
// preferring primary key, then foreign key, then unique.
func constraintOf(c *model.Column) string {
	switch {
	case c.PK:
		return "primary_key"
	case c.FK:
		return "foreign_key"
	case c.Unique:
		return "unique"
	}
	return ""
}

func endpointKey(ids map[*model.Table]string, e model.Endpoint) string {
	if e.Table == nil {
		return ""
	}
	id, ok := ids[e.Table]
	if !ok {
		return ""
	}
	if len(e.Columns) == 1 {
		return id + "." + key(e.Columns[0])
	}
	return id
}

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

func optional(e model.Endpoint) bool {
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

// crowfoot picks a D2 crow's-foot arrowhead: many vs one, optional (circle) vs
// required (bar).
func crowfoot(many, opt bool) string {
	if many {
		if opt {
			return string(d2target.CfMany)
		}
		return string(d2target.CfManyRequired)
	}
	if opt {
		return string(d2target.CfOne)
	}
	return string(d2target.CfOneRequired)
}

func card(many, opt bool) string {
	if many {
		if opt {
			return "0..*"
		}
		return "*"
	}
	if opt {
		return "0..1"
	}
	return "1"
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// key quotes a D2 key segment when it is not a plain identifier.
func key(s string) string {
	if identRE.MatchString(s) {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
