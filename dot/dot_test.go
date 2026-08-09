package dot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

func loadExample(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "testdata", "ecommerce.dbml")
	schema, diags, err := resolver.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	return Emit(schema, diagram.Options{})
}

func TestEmitFull(t *testing.T) {
	out := loadExample(t)
	for _, want := range []string{"digraph dbml", "users", "orders", "order_items", "-> "} {
		if !strings.Contains(out, want) {
			t.Fatalf("full output missing %q", want)
		}
	}
}

func TestDetailLevels(t *testing.T) {
	p := filepath.Join("..", "testdata", "ecommerce.dbml")
	schema, _, _ := resolver.Load(p)

	full := Emit(schema, diagram.Options{Detail: diagram.Full})
	keys := Emit(schema, diagram.Options{Detail: diagram.Keys})
	tables := Emit(schema, diagram.Options{Detail: diagram.Tables})

	// "name" is a non-key column on users; present only at full detail.
	if !strings.Contains(full, "name") {
		t.Fatal("full should include non-key column 'name'")
	}
	if strings.Contains(keys, ">name<") {
		t.Fatal("keys detail should drop non-key column 'name'")
	}
	// tables-only has no column ports at all.
	if strings.Contains(tables, "port=\"c") {
		t.Fatal("tables detail should have no column ports")
	}
}

func TestNotations(t *testing.T) {
	p := filepath.Join("..", "testdata", "ecommerce.dbml")
	schema, _, _ := resolver.Load(p)

	crow := Emit(schema, diagram.Options{Notation: diagram.Crowfoot})
	if !strings.Contains(crow, "arrowtail=crow") {
		t.Fatalf("crowfoot notation missing crow arrowtail:\n%s", crow)
	}
	label := Emit(schema, diagram.Options{Notation: diagram.Label})
	if !strings.Contains(label, "taillabel=\"*\"") {
		t.Fatalf("label notation missing cardinality label")
	}
}

// The crow's-foot "o" (odot) marks a relationship side optional only when the
// operator carries a `?`; a plain nullable FK column must not draw one.
func TestCrowfootOptionalMarker(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "opt.dbml")
	src := `
Table users { id int [pk] }
Table posts {
  id int [pk]
  author_id int [ref: > users.id]
  editor_id int [ref: >? users.id]
}
`
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	schema, diags, err := resolver.Load(p)
	if err != nil || len(diags) != 0 {
		t.Fatalf("load: %v %+v", err, diags)
	}
	out := Emit(schema, diagram.Options{Notation: diagram.Crowfoot})
	// Exactly one edge is optional (the `>?` one), so exactly one odot glyph.
	if n := strings.Count(out, "odot"); n != 1 {
		t.Fatalf("want exactly 1 odot (only the `>?` edge), got %d:\n%s", n, out)
	}
}

func TestNotesFlag(t *testing.T) {
	p := filepath.Join("..", "testdata", "ecommerce.dbml")
	schema, _, _ := resolver.Load(p)

	with := Emit(schema, diagram.Options{Notes: true})
	without := Emit(schema, diagram.Options{Notes: false})
	if !strings.Contains(with, "registered customers") {
		t.Fatal("notes flag should include the table note")
	}
	if strings.Contains(without, "registered customers") {
		t.Fatal("without notes flag the note must be absent")
	}
}
