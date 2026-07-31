package d2

import (
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

func TestTableTooltipCombinesNotes(t *testing.T) {
	tbl := &model.Table{
		Name: "users",
		Note: "core records",
		Columns: []*model.Column{
			{Name: "id", Note: "the primary key"},
			{Name: "email"},
			{Name: "status", Note: "lifecycle state"},
		},
	}
	got := tableTooltip(tbl)
	want := "core records\nid: the primary key\nstatus: lifecycle state"
	if got != want {
		t.Fatalf("tableTooltip:\n got %q\nwant %q", got, want)
	}
	if tableTooltip(&model.Table{Name: "x"}) != "" {
		t.Fatalf("a note-free table should have no tooltip")
	}
}

// Column notes, previously dropped by D2's sql_table, must now reach the
// rendered SVG via the table's aggregated tooltip.
func TestColumnNoteRendersInSVG(t *testing.T) {
	src := "Table users {\n  id int [pk, note: 'COLNOTE_XYZ']\n}\n"
	path := "/mem/x.dbml"
	schema, _, err := resolver.LoadSource(path, map[string]string{path: src})
	if err != nil {
		t.Fatal(err)
	}
	out, err := New().Render(schema, diagram.Options{}, diagram.SVG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "COLNOTE_XYZ") {
		t.Fatalf("column note should be rendered into the SVG tooltip")
	}
}
