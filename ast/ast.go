// Package ast defines the DBML abstract syntax tree. Every node carries the
// source position of its defining name so the LSP can offer go-to-definition.
package ast

import "github.com/jason-cairns/dbml-toolkit/token"

// File is a single parsed .dbml source file.
type File struct {
	Path     string
	Imports  []*Import
	Projects []*Project
	Tables   []*Table
	Enums    []*Enum
	Refs     []*Ref
	Groups   []*TableGroup
	Notes    []*Note
	Partials []*TablePartial
	Records  []*Records
	Comments []token.Comment // source comments, in order, captured as trivia
}

// Setting is a bracketed `[name: value]` entry. HasValue is false for flags
// like `pk` or `not null`. Kind carries the lexical kind of the value token.
type Setting struct {
	Name     string
	Value    string
	HasValue bool
	Kind     token.Kind
	Ref      *Ref // populated for inline `ref:` column settings (Left filled by parser)
	Pos      token.Pos
}

// Import represents a `use`/`reuse ... from './path'` statement.
type Import struct {
	Reuse    bool // reuse (re-export) vs use (local only)
	Wildcard bool
	Items    []ImportItem
	Path     string
	PathPos  token.Pos // position of the quoted import path
	PathEnd  token.Pos // position just past the quoted import path
	Pos      token.Pos
}

// ImportItem is one selectively-imported symbol, e.g. `table users as u`.
type ImportItem struct {
	Type  string // table|enum|tablepartial|note|schema|tablegroup
	Name  string
	Alias string
}

// Project holds top-level project metadata.
type Project struct {
	Name     string
	Settings []Setting
	Fields   []Setting // body `key: value` lines (e.g. database_type), excluding Note
	Note     string
	Pos      token.Pos
}

// Table is a table definition.
type Table struct {
	Schema    string
	Name      string
	Alias     string
	Settings  []Setting
	Columns   []*Column
	Indexes   []*Index
	Checks    []Check
	Note      string
	Injects   []string    // ~partial names injected into this table
	InjectPos []token.Pos // source position of each injection (parallel to Injects)
	Pos       token.Pos
	NamePos   token.Pos
}

// Column is a single table column.
type Column struct {
	Name     string
	Type     string
	Settings []Setting
	Note     string
	Pos      token.Pos
	NamePos  token.Pos
}

// Index is one entry within an `indexes { }` block.
type Index struct {
	Fields   []IndexField
	Settings []Setting
	Pos      token.Pos
}

// IndexField is a column name or a backtick expression within an index.
type IndexField struct {
	Text string
	Expr bool
	Pos  token.Pos
}

// Check is one entry within a `checks { }` block.
type Check struct {
	Expr     string
	Settings []Setting
	Pos      token.Pos
}

// Ref is a relationship. Cardinality Op is one of "<", ">", "-", "<>".
// Either side of the operator may be marked optional with a "?" (e.g. ">?" or
// "?>"), meaning that side's foreign-key column is nullable; LeftOptional and
// RightOptional record which sides carried the marker.
type Ref struct {
	Name          string
	Op            string
	LeftOptional  bool // "?" on the left of the operator (source side optional)
	RightOptional bool // "?" on the right of the operator (referenced side optional)
	Left          Endpoint
	Right         Endpoint
	Settings      []Setting
	Inline        bool // true when derived from an inline column `ref:` setting
	Pos           token.Pos
}

// Endpoint is one side of a relationship: [schema.]table.(cols).
type Endpoint struct {
	Schema  string
	Table   string
	Columns []string
	ColPos  []token.Pos // source position of each column name (parallel to Columns)
	Pos     token.Pos
}

// Enum is an enum type definition.
type Enum struct {
	Schema  string
	Name    string
	Values  []EnumValue
	Pos     token.Pos
	NamePos token.Pos
}

// EnumValue is a single enum member.
type EnumValue struct {
	Name string
	Note string
	Pos  token.Pos
}

// TableGroup groups tables for visualization.
type TableGroup struct {
	Name     string
	Settings []Setting
	Note     string
	Members  []GroupMember
	Pos      token.Pos
	NamePos  token.Pos
}

// GroupMember is a [schema.]table reference inside a TableGroup.
type GroupMember struct {
	Schema string
	Table  string
	Pos    token.Pos
}

// Note is a standalone / sticky note.
type Note struct {
	Name     string
	Text     string
	Settings []Setting
	Pos      token.Pos
	NamePos  token.Pos
}

// TablePartial is a reusable block injected via `~name`.
type TablePartial struct {
	Name     string
	Settings []Setting
	Columns  []*Column
	Indexes  []*Index
	Note     string
	Pos      token.Pos
	NamePos  token.Pos
}

// Records is a sample-data block.
type Records struct {
	Schema  string
	Table   string
	Columns []string
	Rows    [][]string     // cell literals, delimiters stripped
	Kinds   [][]token.Kind // lexical kind of each cell, parallel to Rows (for faithful re-quoting)
	Pos     token.Pos
}

// QualifiedName renders schema.name, defaulting the schema to "public".
func QualifiedName(schema, name string) string {
	if schema == "" {
		schema = "public"
	}
	return schema + "." + name
}
