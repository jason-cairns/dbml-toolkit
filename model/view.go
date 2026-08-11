package model

import (
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
)

// ModuleView returns the portion of a resolved schema exported by entry and
// its transitive `reuse` imports. When referencedContext is true, tables
// reached only through `use` are retained as compact stubs when an exported
// table has a relationship to them.
func ModuleView(s *Schema, exportedFiles map[string]bool, referencedContext bool) *Schema {
	if s == nil {
		return nil
	}

	exported := map[*Table]bool{}
	selected := map[*Table]bool{}
	for _, table := range s.Tables {
		if exportedFiles[table.NamePos.File] {
			exported[table] = true
			selected[table] = true
		}
	}

	var refs []*Ref
	referencedColumns := map[*Table]map[string]bool{}
	for _, ref := range s.Refs {
		fromExported := exported[ref.From.Table]
		toExported := exported[ref.To.Table]
		if fromExported && toExported {
			refs = append(refs, ref)
			continue
		}
		if !referencedContext || (!fromExported && !toExported) {
			continue
		}
		refs = append(refs, ref)
		for _, endpoint := range []Endpoint{ref.From, ref.To} {
			if endpoint.Table == nil || exported[endpoint.Table] {
				continue
			}
			selected[endpoint.Table] = true
			if referencedColumns[endpoint.Table] == nil {
				referencedColumns[endpoint.Table] = map[string]bool{}
			}
			for _, column := range endpoint.Columns {
				referencedColumns[endpoint.Table][column] = true
			}
		}
	}

	view := &Schema{byKey: map[string]*Table{}}
	if s.Project != nil && exportedFiles[s.Project.Pos.File] {
		view.Project = s.Project
	}
	for _, enum := range s.Enums {
		if exportedFiles[enum.Pos.File] {
			view.Enums = append(view.Enums, enum)
		}
	}
	for _, note := range s.Notes {
		if exportedFiles[note.Pos.File] {
			view.Notes = append(view.Notes, note)
		}
	}

	tableMap := map[*Table]*Table{}
	for _, table := range s.Tables {
		if !selected[table] {
			continue
		}
		visible := table
		if !exported[table] {
			visible = externalStub(table, referencedColumns[table])
		}
		tableMap[table] = visible
		view.Tables = append(view.Tables, visible)
		view.index(visible)
	}

	for _, ref := range refs {
		copy := *ref
		copy.From.Table = tableMap[ref.From.Table]
		copy.To.Table = tableMap[ref.To.Table]
		view.Refs = append(view.Refs, &copy)
	}
	for _, group := range s.Groups {
		if !exportedFiles[group.Pos.File] {
			continue
		}
		copy := *group
		copy.Members = nil
		for _, member := range group.Members {
			if table := s.Lookup(groupMemberName(member)); table != nil && selected[table] {
				copy.Members = append(copy.Members, member)
			}
		}
		if len(copy.Members) > 0 {
			view.Groups = append(view.Groups, &copy)
		}
	}
	return view
}

func externalStub(table *Table, referenced map[string]bool) *Table {
	stub := *table
	stub.External = true
	stub.Indexes = nil
	stub.HeaderColor = "#64748B"
	stub.Note = strings.TrimSpace(strings.Join([]string{"External context.", table.Note}, " "))
	stub.Columns = nil
	for _, column := range table.Columns {
		if referenced[column.Name] || (len(referenced) == 0 && (column.PK || column.Unique)) {
			copy := *column
			stub.Columns = append(stub.Columns, &copy)
		}
	}
	return &stub
}

func groupMemberName(member ast.GroupMember) string {
	if member.Schema == "" {
		return member.Table
	}
	return member.Schema + "." + member.Table
}
