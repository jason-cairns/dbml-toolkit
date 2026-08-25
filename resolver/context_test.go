package resolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContextUsesReuseAsExportBoundary(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "external.dbml", `
Table users {
  id int [pk]
  email string
}
Table teams {
  id int [pk]
}
`)
	writeContextFile(t, dir, "owned.dbml", `
use * from './external'
Table posts {
  id int [pk]
  author_id int [ref: > users.id]
  body string
}
`)
	entry := writeContextFile(t, dir, "index.dbml", "reuse * from './owned'\n")

	all, diags, err := LoadContext(entry, nil, ContextAll)
	if err != nil || len(diags) != 0 {
		t.Fatalf("all context: err=%v diags=%v", err, diags)
	}
	if len(all.Tables) != 3 || len(all.Refs) != 1 {
		t.Fatalf("all context = %d tables, %d refs", len(all.Tables), len(all.Refs))
	}

	refs, diags, err := LoadContext(entry, nil, ContextRefs)
	if err != nil || len(diags) != 0 {
		t.Fatalf("refs context: err=%v diags=%v", err, diags)
	}
	if len(refs.Tables) != 2 || len(refs.Refs) != 1 {
		t.Fatalf("refs context = %d tables, %d refs", len(refs.Tables), len(refs.Refs))
	}
	users := refs.Lookup("users")
	if users == nil || !users.External || len(users.Columns) != 1 || users.Columns[0].Name != "id" {
		t.Fatalf("users context stub = %#v", users)
	}
	if refs.Lookup("teams") != nil {
		t.Fatal("unreferenced context table teams should be excluded")
	}

	none, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(none.Tables) != 1 || none.Tables[0].Name != "posts" || len(none.Refs) != 0 {
		t.Fatalf("no context = %#v", none)
	}
}

func TestParseContext(t *testing.T) {
	for input, want := range map[string]Context{
		"all": ContextAll, "refs": ContextRefs, "referenced": ContextRefs, "none": ContextNone,
	} {
		got, ok := ParseContext(input)
		if !ok || got != want {
			t.Fatalf("ParseContext(%q) = %v, %v; want %v, true", input, got, ok, want)
		}
	}
	if _, ok := ParseContext("some"); ok {
		t.Fatal("invalid context should be rejected")
	}
}

func writeContextFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadContextSelectiveReuseExportsOnlySelectedTables(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "child.dbml", `
Table a {
  id int [pk]
  b_id int [ref: > b.id]
}
Table b {
  id int [pk]
  label string
}
`)
	entry := writeContextFile(t, dir, "index.dbml", "reuse { table a } from './child'\n")

	none, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(none.Tables) != 1 || none.Tables[0].Name != "a" || len(none.Refs) != 0 {
		t.Fatalf("no context = %#v", none.Tables)
	}
	if none.Lookup("b") != nil {
		t.Fatal("unselected sibling b should not be exported")
	}

	refs, diags, err := LoadContext(entry, nil, ContextRefs)
	if err != nil || len(diags) != 0 {
		t.Fatalf("refs context: err=%v diags=%v", err, diags)
	}
	if len(refs.Tables) != 2 || len(refs.Refs) != 1 {
		t.Fatalf("refs context = %d tables, %d refs; want 2, 1", len(refs.Tables), len(refs.Refs))
	}
	stub := refs.Lookup("b")
	if stub == nil || !stub.External || len(stub.Columns) != 1 || stub.Columns[0].Name != "id" {
		t.Fatalf("unselected endpoint stub = %#v", stub)
	}
	if selected := refs.Lookup("a"); selected == nil || selected.External {
		t.Fatalf("selected table a = %#v", selected)
	}
}

func TestLoadContextSelectiveReuseOfTableGroup(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "child.dbml", `
Table a {
  id int [pk]
}
Table b {
  id int [pk]
}
TableGroup gold {
  a
}
TableGroup bronze {
  b
}
`)
	entry := writeContextFile(t, dir, "index.dbml", "reuse { tablegroup gold } from './child'\n")

	view, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(view.Tables) != 1 || view.Tables[0].Name != "a" {
		t.Fatalf("group members = %#v", view.Tables)
	}
	if len(view.Groups) != 1 || view.Groups[0].Name != "gold" || len(view.Groups[0].Members) != 1 {
		t.Fatalf("exported groups = %#v", view.Groups)
	}
}

func TestLoadContextSelectiveReuseThroughAliasesAndNestedModules(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "shared.dbml", `
Table shared.dim_date {
  date_key int [pk]
}
Table shared.dim_vehicle {
  vehicle_key int [pk]
}
`)
	writeContextFile(t, dir, "index.dbml", "reuse * from './shared'\n")
	writeContextFile(t, dir, "projection.dbml",
		"reuse { table \"shared.dim_date\" as dim_date } from './index'\n")
	entry := writeContextFile(t, dir, "gold.dbml", "reuse { table dim_date } from './projection'\n")

	view, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(view.Tables) != 1 || view.Tables[0].Qualified() != "shared.dim_date" {
		t.Fatalf("transitive selective reuse = %#v", view.Tables)
	}
	if view.Lookup("shared.dim_vehicle") != nil {
		t.Fatal("unselected shared.dim_vehicle should not be exported")
	}
}

func TestLoadContextSelectiveReuseCannotReachOutsideChildModule(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "child.dbml", `
Table a {
  id int [pk]
}
`)
	writeContextFile(t, dir, "sibling.dbml", `
Table outsider {
  id int [pk]
}
`)
	entry := writeContextFile(t, dir, "index.dbml",
		"use * from './sibling'\nreuse { table outsider } from './child'\n")

	view, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(view.Tables) != 0 {
		t.Fatalf("selective reuse should only see the child module: %#v", view.Tables)
	}
}

func TestLoadContextEmptySelectiveReuseSelectsNothing(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "child.dbml", "Table a {\n  id int [pk]\n}\n")
	empty := writeContextFile(t, dir, "empty.dbml", "reuse { } from './child'\n")
	bare := writeContextFile(t, dir, "bare.dbml", "reuse from './child'\n")

	view, diags, err := LoadContext(empty, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("empty selection: err=%v diags=%v", err, diags)
	}
	if len(view.Tables) != 0 {
		t.Fatalf("an empty `reuse { }` selects nothing, got %#v", view.Tables)
	}

	// A braceless `reuse from` is not a selection at all, and keeps re-exporting
	// the child module whole.
	view, diags, err = LoadContext(bare, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("braceless reuse: err=%v diags=%v", err, diags)
	}
	if len(view.Tables) != 1 || view.Tables[0].Name != "a" {
		t.Fatalf("braceless reuse = %#v", view.Tables)
	}
}

func TestLoadContextSelectiveReuseIgnoresAliasesFromOtherModules(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "child.dbml", "Table a {\n  id int [pk]\n}\n")
	writeContextFile(t, dir, "sibling.dbml", "use { table a as leaked } from './child'\n")
	entry := writeContextFile(t, dir, "index.dbml",
		"use * from './sibling'\nreuse { table leaked } from './child'\n")

	view, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(view.Tables) != 0 {
		t.Fatalf("an alias coined by a sibling is not a name child answers to: %#v", view.Tables)
	}
}

func TestLoadContextSelectiveReuseFollowsTableGroupAliases(t *testing.T) {
	dir := t.TempDir()
	writeContextFile(t, dir, "child.dbml", `
Table a {
  id int [pk]
}
Table b {
  id int [pk]
}
TableGroup gold {
  a
}
`)
	writeContextFile(t, dir, "mid.dbml", "reuse { tablegroup gold as g } from './child'\n")
	entry := writeContextFile(t, dir, "top.dbml", "reuse { tablegroup g } from './mid'\n")

	view, diags, err := LoadContext(entry, nil, ContextNone)
	if err != nil || len(diags) != 0 {
		t.Fatalf("no context: err=%v diags=%v", err, diags)
	}
	if len(view.Groups) != 1 || view.Groups[0].Name != "gold" {
		t.Fatalf("aliased group = %#v", view.Groups)
	}
	if len(view.Tables) != 1 || view.Tables[0].Name != "a" {
		t.Fatalf("aliased group members = %#v", view.Tables)
	}
}
