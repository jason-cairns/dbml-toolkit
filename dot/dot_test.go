package dot

import (
	"path/filepath"
	"strings"
	"testing"

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
	return Emit(schema, Options{})
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

	full := Emit(schema, Options{Detail: Full})
	keys := Emit(schema, Options{Detail: Keys})
	tables := Emit(schema, Options{Detail: Tables})

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

	crow := Emit(schema, Options{Notation: Crowfoot})
	if !strings.Contains(crow, "arrowtail=crow") {
		t.Fatalf("crowfoot notation missing crow arrowtail:\n%s", crow)
	}
	label := Emit(schema, Options{Notation: Label})
	if !strings.Contains(label, "taillabel=\"*\"") {
		t.Fatalf("label notation missing cardinality label")
	}
}

func TestNotesFlag(t *testing.T) {
	p := filepath.Join("..", "testdata", "ecommerce.dbml")
	schema, _, _ := resolver.Load(p)

	with := Emit(schema, Options{Notes: true})
	without := Emit(schema, Options{Notes: false})
	if !strings.Contains(with, "registered customers") {
		t.Fatal("notes flag should include the table note")
	}
	if strings.Contains(without, "registered customers") {
		t.Fatal("without notes flag the note must be absent")
	}
}
