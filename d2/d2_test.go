package d2

import (
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
