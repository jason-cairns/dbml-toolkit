package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/model"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModuleImportsAndCycle(t *testing.T) {
	dir := t.TempDir()
	// base defines users; main imports base and references it (with an alias)
	// and base re-uses main => a cycle, which must be handled safely.
	write(t, dir, "base.dbml", `
use * from './main'
Table users {
  id int [pk]
}
`)
	entry := write(t, dir, "main.dbml", `
use { table users as members } from './base'
Table posts {
  id int [pk]
  author int [ref: > members.id]
}
`)

	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(schema.Tables) != 2 {
		t.Fatalf("want 2 tables across files, got %d", len(schema.Tables))
	}
	// The alias `members` must resolve to the users table across files.
	if schema.Lookup("members") == nil {
		t.Fatalf("alias members did not resolve; keys unresolved")
	}
	// The cross-file relationship must resolve without an "unknown table" diag.
	for _, d := range diags {
		if d.Msg != "" && contains(d.Msg, "unknown table") {
			t.Fatalf("unexpected unresolved ref: %s", d.Msg)
		}
	}
	var resolved bool
	for _, r := range schema.Refs {
		if r.To.Table != nil && r.To.Table.Name == "users" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("cross-file ref endpoint did not resolve to users")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// A schema-qualified table named in an endpoint without a column (e.g. mid-edit
// `> dim.date` before `.id` is typed) must resolve to that table rather than be
// misread as table `dim` column `date`.
func TestSchemaQualifiedEndpointDisambiguation(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", `
Table dim.date {
  id int [pk]
}
Table fact.f {
  d date [ref: > dim.date]
  e date [ref: > dim.date.id]
}
`)
	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, d := range diags {
		if contains(d.Msg, "unknown table") {
			t.Fatalf("dim.date should resolve, got diagnostic: %s", d.Msg)
		}
	}
	// Both endpoints must point at the dim.date table.
	for _, r := range schema.Refs {
		if r.To.Table == nil || r.To.Table.Name != "date" || r.To.Table.Schema != "dim" {
			t.Fatalf("endpoint did not resolve to dim.date: %+v", r.To)
		}
	}
	// The column form keeps its column; the bare form has none.
	var withCol, without int
	for _, r := range schema.Refs {
		if len(r.To.Columns) == 1 && r.To.Columns[0] == "id" {
			withCol++
		}
		if len(r.To.Columns) == 0 {
			without++
		}
	}
	if withCol != 1 || without != 1 {
		t.Fatalf("expected one endpoint with column id and one without, got with=%d without=%d", withCol, without)
	}
}

// An optional (`?`) relationship side makes that side's column nullable.
func TestOptionalRefClearsNotNull(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", `
Table users {
  id int [pk]
}
Table posts {
  id int [pk]
  user_id int [not null, ref: ?> users.id]
  editor_id int [not null, ref: > users.id]
}
`)
	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	col := func(table, name string) *model.Column {
		for _, tb := range schema.Tables {
			if tb.Name != table {
				continue
			}
			for _, c := range tb.Columns {
				if c.Name == name {
					return c
				}
			}
		}
		t.Fatalf("column %s.%s not found", table, name)
		return nil
	}
	// `?>` marks the source (user_id) optional: NotNull cleared despite the setting.
	if c := col("posts", "user_id"); c.NotNull {
		t.Fatalf("optional ref should clear NotNull on user_id")
	}
	// A plain `>` leaves an explicit `not null` intact.
	if c := col("posts", "editor_id"); !c.NotNull {
		t.Fatalf("non-optional ref should preserve NotNull on editor_id")
	}
}

// A genuinely missing table still reports an unknown-table diagnostic.
func TestUnknownTableStillDiagnosed(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", `
Table users { id int [pk] }
Ref: users.id > nope.id
`)
	_, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found bool
	for _, d := range diags {
		if contains(d.Msg, "unknown table in relationship: nope") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an unknown-table diagnostic for nope, got %v", diags)
	}
}

func TestTablePartialExpansionPrecedenceAndInlineRefs(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", `
Table users { id int [pk] }

TablePartial audit [headercolor: #aa0000] {
  owner_id int [not null, ref: > users.id]
  stamp timestamp
  indexes {
    stamp [unique]
  }
}

TablePartial newer [headercolor: #0000aa] {
  stamp bigint
  indexes {
    stamp [pk]
  }
}

Table orders [headercolor: #00aa00] {
  before int
  ~audit
  middle int
  ~newer
  owner_id int
  after int
  indexes {
    stamp [name: 'local_stamp']
  }
}

Table invoices {
  ~audit
}
`)
	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}

	orders := schema.Lookup("orders")
	if orders == nil {
		t.Fatal("orders table not found")
	}
	if orders.HeaderColor != "#00aa00" {
		t.Fatalf("local table setting should win, got %q", orders.HeaderColor)
	}
	var names []string
	for _, c := range orders.Columns {
		names = append(names, c.Name)
	}
	if got, want := strings.Join(names, ","), "before,middle,stamp,owner_id,after"; got != want {
		t.Fatalf("expanded column order/precedence: got %q want %q", got, want)
	}
	if c := orders.Columns[2]; c.Type != "bigint" {
		t.Fatalf("last partial should win stamp conflict, got %+v", c)
	}
	if len(orders.Indexes) != 1 || settingValue(orders.Indexes[0].Settings, "name") != "local_stamp" {
		t.Fatalf("local index should win conflict: %+v", orders.Indexes)
	}

	invoices := schema.Lookup("invoices")
	if invoices == nil || invoices.HeaderColor != "#aa0000" {
		t.Fatalf("partial setting should apply to invoices: %+v", invoices)
	}
	if len(schema.Refs) != 1 {
		t.Fatalf("only the non-overridden partial ref should expand, got %d: %+v", len(schema.Refs), schema.Refs)
	}
	r := schema.Refs[0]
	if r.From.Table != invoices || r.From.Name != "invoices" || r.To.Table != schema.Lookup("users") {
		t.Fatalf("partial inline ref was not rebound to invoices: %+v", r)
	}
	if len(r.From.Columns) != 1 || r.From.Columns[0] != "owner_id" {
		t.Fatalf("partial inline ref source column: %+v", r.From)
	}
}

func TestUnknownTablePartialDiagnosed(t *testing.T) {
	dir := t.TempDir()
	entry := write(t, dir, "s.dbml", "Table t {\n  ~missing\n}\n")
	_, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, d := range diags {
		if contains(d.Msg, "unknown table partial: missing") {
			return
		}
	}
	t.Fatalf("expected unknown-partial diagnostic, got %+v", diags)
}

func TestImportedTablePartialAliasExpands(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "base.dbml", `
Table users { id int [pk] }
TablePartial audit {
  owner_id int [ref: > users.id]
}
`)
	entry := write(t, dir, "main.dbml", `
use {
  table users
  tablepartial audit as auditable
} from './base'
Table invoices {
  ~auditable
}
`)
	schema, diags, err := Load(entry)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	invoices := schema.Lookup("invoices")
	if invoices == nil || len(invoices.Columns) != 1 || invoices.Columns[0].Name != "owner_id" {
		t.Fatalf("aliased partial did not expand: %+v", invoices)
	}
	if len(schema.Refs) != 1 || schema.Refs[0].From.Table != invoices || schema.Refs[0].To.Table != schema.Lookup("users") {
		t.Fatalf("aliased partial ref did not resolve: %+v", schema.Refs)
	}
}

func settingValue(settings []ast.Setting, name string) string {
	for _, setting := range settings {
		if strings.EqualFold(setting.Name, name) {
			return setting.Value
		}
	}
	return ""
}
