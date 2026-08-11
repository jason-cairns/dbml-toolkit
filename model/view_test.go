package model

import (
	"testing"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/token"
)

func TestModuleViewKeepsExportedTablesAndCompactReferencedContext(t *testing.T) {
	ownedFile := "/model/owned.dbml"
	externalFile := "/model/external.dbml"
	external := &Table{Name: "users", NamePos: token.Pos{File: externalFile}, Columns: []*Column{
		{Name: "id", Type: "int", PK: true},
		{Name: "email", Type: "string"},
	}}
	owned := &Table{Name: "posts", NamePos: token.Pos{File: ownedFile}, Columns: []*Column{
		{Name: "id", Type: "int", PK: true},
		{Name: "author_id", Type: "int", FK: true},
		{Name: "body", Type: "string"},
	}}
	schema := &Schema{Tables: []*Table{external, owned}, byKey: map[string]*Table{}}
	schema.index(external)
	schema.index(owned)
	schema.Refs = []*Ref{{
		Op:   ">",
		From: Endpoint{Table: owned, Name: "posts", Columns: []string{"author_id"}},
		To:   Endpoint{Table: external, Name: "users", Columns: []string{"id"}},
	}}
	schema.Groups = []*ast.TableGroup{{
		Name: "owned", Pos: token.Pos{File: ownedFile},
		Members: []ast.GroupMember{{Table: "posts"}, {Table: "users"}},
	}}

	view := ModuleView(schema, map[string]bool{ownedFile: true}, true)
	if len(view.Tables) != 2 || len(view.Refs) != 1 {
		t.Fatalf("referenced view = %d tables, %d refs; want 2, 1", len(view.Tables), len(view.Refs))
	}
	stub := view.Lookup("users")
	if stub == nil || !stub.External || len(stub.Columns) != 1 || stub.Columns[0].Name != "id" {
		t.Fatalf("external stub = %#v", stub)
	}
	if got := view.Lookup("posts"); got != owned || len(got.Columns) != 3 {
		t.Fatalf("owned table was not retained in full: %#v", got)
	}
	if view.Refs[0].To.Table != stub {
		t.Fatal("relationship endpoint was not rebound to the external stub")
	}
	if len(view.Groups) != 1 || len(view.Groups[0].Members) != 2 {
		t.Fatalf("group members = %#v", view.Groups)
	}

	withoutContext := ModuleView(schema, map[string]bool{ownedFile: true}, false)
	if len(withoutContext.Tables) != 1 || withoutContext.Tables[0] != owned || len(withoutContext.Refs) != 0 {
		t.Fatalf("no-context view = %#v", withoutContext)
	}
}
