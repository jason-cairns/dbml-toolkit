// Package model is the resolved semantic layer: a single Schema built from one
// or more parsed files, with relationships linked to concrete columns and a
// definition/reference index used by the LSP.
package model

import (
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// Diagnostic is a problem reported during parsing or resolution.
type Diagnostic struct {
	Pos token.Pos
	End token.Pos
	Msg string
}

// Schema is the fully resolved, merged view of a DBML project.
type Schema struct {
	Project *ast.Project
	Tables  []*Table
	Enums   []*ast.Enum
	Refs    []*Ref
	Groups  []*ast.TableGroup
	Notes   []*ast.Note

	byKey map[string]*Table // qualified name / alias / bare name -> table
	Defs  []Def             // symbol definitions (for LSP go-to-definition)
}

// Table is a resolved table with partials merged in.
type Table struct {
	Schema      string
	Name        string
	Alias       string
	Columns     []*Column
	Indexes     []*ast.Index
	Note        string
	HeaderColor string
	External    bool // compact context table imported with `use`
	NamePos     token.Pos
}

// Qualified returns schema.name (schema defaults to public).
func (t *Table) Qualified() string { return ast.QualifiedName(t.Schema, t.Name) }

// Column is a resolved column with its constraint flags decoded.
type Column struct {
	Name      string
	Type      string
	PK        bool
	Unique    bool
	NotNull   bool
	Increment bool
	FK        bool
	Default   string
	Note      string
	NamePos   token.Pos
}

// IsKey reports whether the column participates in a key or relationship.
func (c *Column) IsKey() bool { return c.PK || c.Unique || c.FK }

// Ref is a resolved relationship linked to its endpoint tables.
type Ref struct {
	Name         string
	Op           string
	FromOptional bool // "?" on the From side: the From-side FK column is nullable
	ToOptional   bool // "?" on the To side: the To-side FK column is nullable
	From         Endpoint
	To           Endpoint
	OnDelete     string
	OnUpdate     string
	Pos          token.Pos
}

// Endpoint is one resolved side of a relationship.
type Endpoint struct {
	Table   *Table // nil if unresolved
	Schema  string
	Name    string
	Columns []string
	Pos     token.Pos
}

// Def is a symbol definition location for the LSP.
type Def struct {
	Key string // e.g. "table:public.users" or "table:public.users.id"
	Pos token.Pos
}

// Lookup finds a table by qualified name, alias, or bare name.
func (s *Schema) Lookup(name string) *Table {
	if t, ok := s.byKey[name]; ok {
		return t
	}
	if t, ok := s.byKey["public."+name]; ok {
		return t
	}
	return nil
}

// TableFor resolves a dotted reference name (its parts already split on ".")
// against the schema namespace. A dotted name is ambiguous — `dim.date` may be
// table `dim` column `date`, or the two-part table `dim.date` with no column —
// so instead of guessing positionally it prefers the longest leading run of
// parts that actually names a table. It returns that table (nil if none match)
// and the number of leading parts forming the table name; the remaining parts
// are columns. When nothing matches it falls back to the positional convention
// (all but the last part is the table), so an unresolved reference degrades to
// the same guess the parser made.
func (s *Schema) TableFor(parts []string) (*Table, int) {
	for n := len(parts); n >= 1; n-- {
		if t := s.Lookup(strings.Join(parts[:n], ".")); t != nil {
			return t, n
		}
	}
	if len(parts) > 1 {
		return nil, len(parts) - 1
	}
	return nil, len(parts)
}

func settingVal(ss []ast.Setting, name string) (string, bool) {
	for _, s := range ss {
		if strings.EqualFold(s.Name, name) {
			return s.Value, true
		}
	}
	return "", false
}

func hasFlag(ss []ast.Setting, names ...string) bool {
	for _, s := range ss {
		for _, n := range names {
			if strings.EqualFold(s.Name, n) {
				return true
			}
		}
	}
	return false
}
