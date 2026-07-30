package model

import "github.com/jason-cairns/dbml-toolkit/ast"

// Build assembles a resolved Schema from parsed files. aliases maps an
// import alias to the qualified name it targets (for cross-file references).
func Build(files []*ast.File, aliases map[string]string) (*Schema, []Diagnostic) {
	s := &Schema{byKey: map[string]*Table{}}
	var diags []Diagnostic

	partials := map[string]*ast.TablePartial{}
	for _, f := range files {
		for _, tp := range f.Partials {
			partials[tp.Name] = tp
		}
	}

	for _, f := range files {
		for _, p := range f.Projects {
			s.Project = p
		}
		s.Enums = append(s.Enums, f.Enums...)
		s.Groups = append(s.Groups, f.Groups...)
		s.Notes = append(s.Notes, f.Notes...)
		for _, at := range f.Tables {
			t := buildTable(at, partials, s)
			s.Tables = append(s.Tables, t)
			s.index(t)
		}
	}
	for alias, target := range aliases {
		if t := s.Lookup(target); t != nil {
			s.byKey[alias] = t
		}
	}

	for _, f := range files {
		for _, ar := range f.Refs {
			r, d := s.buildRef(ar)
			s.Refs = append(s.Refs, r)
			diags = append(diags, d...)
		}
	}
	return s, diags
}

func (s *Schema) index(t *Table) {
	s.byKey[t.Qualified()] = t
	if t.Schema == "" {
		s.byKey[t.Name] = t
	}
	if t.Alias != "" {
		s.byKey[t.Alias] = t
	}
	s.Defs = append(s.Defs, Def{Key: "table:" + t.Qualified(), Pos: t.NamePos})
	for _, c := range t.Columns {
		s.Defs = append(s.Defs, Def{Key: "column:" + t.Qualified() + "." + c.Name, Pos: c.NamePos})
	}
}

func buildTable(at *ast.Table, partials map[string]*ast.TablePartial, _ *Schema) *Table {
	t := &Table{
		Schema: at.Schema, Name: at.Name, Alias: at.Alias,
		Indexes: at.Indexes, Note: at.Note, NamePos: at.NamePos,
	}
	if c, ok := settingVal(at.Settings, "headercolor"); ok {
		t.HeaderColor = c
	}
	// Injected partials first, then local columns (local overrides by name).
	seen := map[string]int{}
	add := func(col *Column) {
		if i, ok := seen[col.Name]; ok {
			t.Columns[i] = col
			return
		}
		seen[col.Name] = len(t.Columns)
		t.Columns = append(t.Columns, col)
	}
	for _, name := range at.Injects {
		if tp, ok := partials[name]; ok {
			for _, ac := range tp.Columns {
				add(buildColumn(ac))
			}
			t.Indexes = append(t.Indexes, tp.Indexes...)
		}
	}
	for _, ac := range at.Columns {
		add(buildColumn(ac))
	}
	return t
}

func buildColumn(ac *ast.Column) *Column {
	c := &Column{Name: ac.Name, Type: ac.Type, Note: ac.Note, NamePos: ac.NamePos}
	c.PK = hasFlag(ac.Settings, "pk", "primary key")
	c.Unique = hasFlag(ac.Settings, "unique")
	c.NotNull = hasFlag(ac.Settings, "not null")
	c.Increment = hasFlag(ac.Settings, "increment")
	if d, ok := settingVal(ac.Settings, "default"); ok {
		c.Default = d
	}
	return c
}

func (s *Schema) buildRef(ar *ast.Ref) (*Ref, []Diagnostic) {
	r := &Ref{Name: ar.Name, Op: ar.Op, Pos: ar.Pos}
	r.OnDelete, _ = settingVal(ar.Settings, "delete")
	r.OnUpdate, _ = settingVal(ar.Settings, "update")
	var diags []Diagnostic
	r.From = s.resolveEndpoint(ar.Left, &diags)
	r.To = s.resolveEndpoint(ar.Right, &diags)
	// Mark foreign-key columns on the "many" side.
	markFK(r.From, fkOnLeft(ar.Op))
	markFK(r.To, fkOnRight(ar.Op))
	return r, diags
}

func (s *Schema) resolveEndpoint(e ast.Endpoint, diags *[]Diagnostic) Endpoint {
	name := e.Table
	if e.Schema != "" {
		name = e.Schema + "." + e.Table
	}
	t := s.Lookup(name)
	if t == nil {
		*diags = append(*diags, Diagnostic{Pos: e.Pos, End: e.Pos,
			Msg: "unknown table in relationship: " + name})
	}
	return Endpoint{Table: t, Schema: e.Schema, Name: e.Table, Columns: e.Columns, Pos: e.Pos}
}

func markFK(e Endpoint, on bool) {
	if !on || e.Table == nil {
		return
	}
	for _, col := range e.Columns {
		for _, c := range e.Table.Columns {
			if c.Name == col {
				c.FK = true
			}
		}
	}
}

// For `>` (many-to-one) the left side holds the FK; for `<` the right side;
// `-` puts it on the left; `<>` on both.
func fkOnLeft(op string) bool  { return op == ">" || op == "-" || op == "<>" }
func fkOnRight(op string) bool { return op == "<" || op == "<>" }

// EnumNames returns the set of qualified enum names (used by the emitter).
func (s *Schema) EnumNames() map[string]*ast.Enum {
	m := map[string]*ast.Enum{}
	for _, e := range s.Enums {
		m[ast.QualifiedName(e.Schema, e.Name)] = e
		if e.Schema == "" {
			m[e.Name] = e
		}
	}
	return m
}
