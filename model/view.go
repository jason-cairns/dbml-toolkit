package model

import (
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
)

// Exports is the set of resolved symbols a module re-exports. It is symbol- and
// not file-granular, so a selective `reuse { table ... }` can export part of a
// file while leaving the rest of it out of the view.
type Exports struct {
	Projects map[*ast.Project]bool
	Tables   map[*Table]bool
	Enums    map[*ast.Enum]bool
	Groups   map[*ast.TableGroup]bool
	Notes    map[*ast.Note]bool
}

// NewExports returns an empty export set ready to be populated.
func NewExports() *Exports {
	return &Exports{
		Projects: map[*ast.Project]bool{},
		Tables:   map[*Table]bool{},
		Enums:    map[*ast.Enum]bool{},
		Groups:   map[*ast.TableGroup]bool{},
		Notes:    map[*ast.Note]bool{},
	}
}

// AddFile exports every declaration of one parsed file, as a wildcard `reuse *`
// does. Tables are matched to their resolved counterparts by qualified name.
func (e *Exports) AddFile(s *Schema, f *ast.File) {
	if f == nil {
		return
	}
	for _, project := range f.Projects {
		e.Projects[project] = true
	}
	for _, enum := range f.Enums {
		e.Enums[enum] = true
	}
	for _, group := range f.Groups {
		e.Groups[group] = true
	}
	for _, note := range f.Notes {
		e.Notes[note] = true
	}
	for _, table := range f.Tables {
		if t := s.Lookup(ast.QualifiedName(table.Schema, table.Name)); t != nil {
			e.Tables[t] = true
		}
	}
}

// ModuleView returns the portion of a resolved schema exported by entry and
// its transitive `reuse` imports. When referencedContext is true, tables
// reached only through `use` — or left out by a selective `reuse` — are
// retained as compact stubs when an exported table has a relationship to them.
func ModuleView(s *Schema, exports *Exports, referencedContext bool) *Schema {
	if s == nil {
		return nil
	}
	if exports == nil {
		exports = NewExports()
	}

	exported := map[*Table]bool{}
	selected := map[*Table]bool{}
	for _, table := range s.Tables {
		if exports.Tables[table] {
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
	if s.Project != nil && exports.Projects[s.Project] {
		view.Project = s.Project
	}
	for _, enum := range s.Enums {
		if exports.Enums[enum] {
			view.Enums = append(view.Enums, enum)
		}
	}
	for _, note := range s.Notes {
		if exports.Notes[note] {
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
		if !exports.Groups[group] {
			continue
		}
		copy := *group
		copy.Members = nil
		for _, member := range group.Members {
			if table := s.Lookup(GroupMemberName(member)); table != nil && selected[table] {
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

// GroupMemberName renders a table-group member as the name a schema lookup
// expects: qualified when the member carries a schema, bare otherwise.
func GroupMemberName(member ast.GroupMember) string {
	if member.Schema == "" {
		return member.Table
	}
	return member.Schema + "." + member.Table
}
