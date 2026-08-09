package d2

import (
	"context"
	"math"
	"os"
	"oss.terrastruct.com/d2/d2graph"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

func loadSchema(t *testing.T) *model.Schema {
	t.Helper()
	s, diags, err := resolver.Load(filepath.Join("..", "testdata", "ecommerce.dbml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	return s
}

// A transiently-invalid color on a group (e.g. a half-typed "#" during live
// editing) makes d2oracle reject the edit and return a nil graph. Combined with
// a ref in the schema this used to dereference that nil graph and panic, taking
// the whole preview/LSP process down. It must now degrade to an error instead.
func TestD2InvalidColorWithRefDoesNotPanic(t *testing.T) {
	const src = "TableGroup g [color: #]{\n a\n}\n" +
		"Table a { id int [ref: > b.id] }\nTable b { id int }\n"
	f := filepath.Join(t.TempDir(), "m.dbml")
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _, err := resolver.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New().Render(s, diagram.Options{}, diagram.SVG); err == nil {
		t.Fatal("expected an error for the invalid color, got nil")
	}
}

func TestD2Script(t *testing.T) {
	s := loadSchema(t)
	out, err := New().Render(s, diagram.Options{}, diagram.D2)
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	// tables + a group container ("shop") + a connection + a tooltip note.
	for _, want := range []string{"shape: sql_table", "users", "orders", "->", "shop", "tooltip"} {
		if !strings.Contains(src, want) {
			t.Fatalf("d2 script missing %q:\n%s", want, src)
		}
	}
}

func TestD2SVG(t *testing.T) {
	s := loadSchema(t)
	out, err := New().Render(s, diagram.Options{}, diagram.SVG)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out)
	if !strings.Contains(svg, "<svg") {
		t.Fatal("SVG output missing <svg element")
	}
	for _, want := range []string{"users", "orders", "order_items"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("SVG missing table %q", want)
		}
	}
}

func TestD2ASCII(t *testing.T) {
	s := loadSchema(t)
	out, err := New().Render(s, diagram.Options{}, diagram.ASCII)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("ASCII output is empty")
	}
}

func TestD2SelfReferenceTargetsBothSQLTableRows(t *testing.T) {
	const src = `
Table nodes {
  id int [pk]
  parent_id int [ref: > nodes.id]
}
Table child {
  node_id int [ref: > nodes.id]
}
TableGroup core {
  nodes
  child
}
`
	path := filepath.Join(t.TempDir(), "self.dbml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, diags, err := resolver.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	g, err := build(schema, diagram.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("want two edges, got %d", len(g.Edges))
	}
	var edge *d2graph.Edge
	for _, candidate := range g.Edges {
		if candidate.Src == candidate.Dst {
			edge = candidate
			break
		}
	}
	if edge == nil {
		t.Fatal("self-reference edge not found")
	}
	if edge.SrcTableColumnIndex == nil || edge.DstTableColumnIndex == nil {
		t.Fatalf("self-reference must target both SQL rows: src=%v dst=%v", edge.SrcTableColumnIndex, edge.DstTableColumnIndex)
	}
	srcName := edge.Src.SQLTable.Columns[*edge.SrcTableColumnIndex].Name.Label
	dstName := edge.Dst.SQLTable.Columns[*edge.DstTableColumnIndex].Name.Label
	if srcName != "parent_id" || dstName != "id" {
		t.Fatalf("self-reference rows: got %s -> %s", srcName, dstName)
	}
	_, err = layout(quiet(context.Background()), g, defaultTheme)
	if err != nil {
		t.Fatal(err)
	}
	if len(edge.Route) < 2 {
		t.Fatalf("self-reference has no routed path: %+v", edge.Route)
	}
	rowHeight := edge.Src.Height / float64(len(edge.Src.SQLTable.Columns)+1)
	srcY := edge.Src.TopLeft.Y + rowHeight*float64(*edge.SrcTableColumnIndex+1) + rowHeight/2
	dstY := edge.Dst.TopLeft.Y + rowHeight*float64(*edge.DstTableColumnIndex+1) + rowHeight/2
	if math.Abs(edge.Route[0].Y-srcY) > 0.01 || math.Abs(edge.Route[len(edge.Route)-1].Y-dstY) > 0.01 {
		t.Fatalf("self-reference route missed rows: start=%v wantY=%v end=%v wantY=%v",
			edge.Route[0], srcY, edge.Route[len(edge.Route)-1], dstY)
	}
}
