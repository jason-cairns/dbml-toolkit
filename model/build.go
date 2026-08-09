package model

import (
	"sort"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/token"
)

// Build assembles a resolved Schema from parsed files. aliases maps an
// import alias to the qualified name it targets (for cross-file references).
func Build(files []*ast.File, aliases map[string]string) (*Schema, []Diagnostic) {
	s := &Schema{byKey: map[string]*Table{}}
	var diags []Diagnostic
	var pendingRefs []*ast.Ref

	partials := map[string]*ast.TablePartial{}
	for _, f := range files {
		for _, tp := range f.Partials {
			partials[tp.Name] = tp
		}
	}
	for alias, target := range aliases {
		if tp := partials[target]; tp != nil {
			partials[alias] = tp
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
			t, refs, tdiags := buildTable(at, partials)
			s.Tables = append(s.Tables, t)
			s.index(t)
			pendingRefs = append(pendingRefs, refs...)
			diags = append(diags, tdiags...)
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
	for _, ar := range pendingRefs {
		r, d := s.buildRef(ar)
		s.Refs = append(s.Refs, r)
		diags = append(diags, d...)
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

func buildTable(at *ast.Table, partials map[string]*ast.TablePartial) (*Table, []*ast.Ref, []Diagnostic) {
	t := &Table{
		Schema: at.Schema, Name: at.Name, Alias: at.Alias,
		NamePos: at.NamePos,
	}

	// Table settings follow DBML's precedence rules: later partials override
	// earlier partials, and the local table definition overrides every partial.
	var settings []ast.Setting
	for _, name := range at.Injects {
		if tp := partials[name]; tp != nil {
			settings = mergeSettings(settings, tp.Settings)
			if tp.Note != "" {
				t.Note = tp.Note
			}
		}
	}
	settings = mergeSettings(settings, at.Settings)
	if at.Note != "" {
		t.Note = at.Note
	}
	if c, ok := settingVal(settings, "headercolor"); ok {
		t.HeaderColor = c
	}
	if n, ok := settingVal(settings, "note"); ok && at.Note == "" {
		t.Note = n
	}

	type bodyEvent struct {
		pos      token.Pos
		partial  *ast.TablePartial
		column   *ast.Column
		injectID int
	}
	var events []bodyEvent
	var diags []Diagnostic
	for i, name := range at.Injects {
		pos := at.Pos
		if i < len(at.InjectPos) {
			pos = at.InjectPos[i]
		}
		tp := partials[name]
		if tp == nil {
			diags = append(diags, Diagnostic{Pos: pos, End: pos, Msg: "unknown table partial: " + name})
			continue
		}
		events = append(events, bodyEvent{pos: pos, partial: tp, injectID: i})
	}
	for _, ac := range at.Columns {
		events = append(events, bodyEvent{pos: ac.NamePos, column: ac})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].pos.Off < events[j].pos.Off })

	type columnCandidate struct {
		column *ast.Column
		local  bool
	}
	var candidates []columnCandidate
	for _, event := range events {
		if event.column != nil {
			candidates = append(candidates, columnCandidate{column: event.column, local: true})
			continue
		}
		for _, ac := range event.partial.Columns {
			candidates = append(candidates, columnCandidate{column: ac})
		}
	}

	// Pick winners independently from output order: local fields always win;
	// otherwise the last injected partial wins. The winning definition remains
	// at its source/injection position in the expanded table.
	localNames := map[string]bool{}
	for _, c := range candidates {
		if c.local {
			localNames[c.column.Name] = true
		}
	}
	winner := map[string]int{}
	for i, c := range candidates {
		if c.local || !localNames[c.column.Name] {
			winner[c.column.Name] = i
		}
	}
	var refs []*ast.Ref
	for i, c := range candidates {
		if winner[c.column.Name] != i {
			continue
		}
		t.Columns = append(t.Columns, buildColumn(c.column))
		if !c.local {
			refs = append(refs, inlineRefsFor(t, c.column)...)
		}
	}

	// Indexes use the same precedence: later partial, then local. Equal field
	// lists identify the same index regardless of settings.
	var indexes []*ast.Index
	for _, name := range at.Injects {
		if tp := partials[name]; tp != nil {
			indexes = mergeIndexes(indexes, tp.Indexes)
		}
	}
	t.Indexes = mergeIndexes(indexes, at.Indexes)
	return t, refs, diags
}

func inlineRefsFor(t *Table, c *ast.Column) []*ast.Ref {
	var refs []*ast.Ref
	for _, setting := range c.Settings {
		if !strings.EqualFold(setting.Name, "ref") || setting.Ref == nil {
			continue
		}
		r := *setting.Ref
		r.Inline = true
		r.Left = ast.Endpoint{Schema: t.Schema, Table: t.Name, Columns: []string{c.Name}, Pos: c.NamePos}
		refs = append(refs, &r)
	}
	return refs
}

func mergeSettings(base, override []ast.Setting) []ast.Setting {
	out := append([]ast.Setting(nil), base...)
	positions := map[string]int{}
	for i, setting := range out {
		positions[strings.ToLower(setting.Name)] = i
	}
	for _, setting := range override {
		key := strings.ToLower(setting.Name)
		if i, ok := positions[key]; ok {
			out[i] = setting
		} else {
			positions[key] = len(out)
			out = append(out, setting)
		}
	}
	return out
}

func mergeIndexes(base, override []*ast.Index) []*ast.Index {
	out := append([]*ast.Index(nil), base...)
	positions := map[string]int{}
	for i, index := range out {
		positions[indexKey(index)] = i
	}
	for _, index := range override {
		key := indexKey(index)
		if i, ok := positions[key]; ok {
			out[i] = index
		} else {
			positions[key] = len(out)
			out = append(out, index)
		}
	}
	return out
}

func indexKey(index *ast.Index) string {
	parts := make([]string, len(index.Fields))
	for i, field := range index.Fields {
		if field.Expr {
			parts[i] = "`" + field.Text + "`"
		} else {
			parts[i] = field.Text
		}
	}
	return strings.Join(parts, "\x00")
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
	r := &Ref{Name: ar.Name, Op: ar.Op, FromOptional: ar.LeftOptional, ToOptional: ar.RightOptional, Pos: ar.Pos}
	r.OnDelete, _ = settingVal(ar.Settings, "delete")
	r.OnUpdate, _ = settingVal(ar.Settings, "update")
	var diags []Diagnostic
	r.From = s.resolveEndpoint(ar.Left, &diags)
	r.To = s.resolveEndpoint(ar.Right, &diags)
	// Mark foreign-key columns on the "many" side.
	markFK(r.From, fkOnLeft(ar.Op))
	markFK(r.To, fkOnRight(ar.Op))
	// An optional side (`?`) makes that side's column nullable.
	if r.FromOptional {
		markNullable(r.From)
	}
	if r.ToOptional {
		markNullable(r.To)
	}
	return r, diags
}

func (s *Schema) resolveEndpoint(e ast.Endpoint, diags *[]Diagnostic) Endpoint {
	parts := EndpointParts(e)
	t, tableParts := s.TableFor(parts)
	columns := parts[tableParts:]
	if t == nil {
		*diags = append(*diags, Diagnostic{Pos: e.Pos, End: e.Pos,
			Msg: "unknown table in relationship: " + strings.Join(parts[:tableParts], ".")})
		return Endpoint{Schema: e.Schema, Name: e.Table, Columns: columns, Pos: e.Pos}
	}
	return Endpoint{Table: t, Schema: t.Schema, Name: t.Name, Columns: columns, Pos: e.Pos}
}

// EndpointParts reconstructs the full dotted segment list of an endpoint —
// schema parts, then the table, then the columns — so it can be re-split against
// the namespace. The parser split it positionally; TableFor may split it
// differently once it knows which tables exist.
func EndpointParts(e ast.Endpoint) []string {
	var parts []string
	if e.Schema != "" {
		parts = append(parts, strings.Split(e.Schema, ".")...)
	}
	if e.Table != "" {
		parts = append(parts, e.Table)
	}
	return append(parts, e.Columns...)
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

// markNullable clears NotNull on an endpoint's columns; an optional (`?`)
// relationship side means those columns may be null.
func markNullable(e Endpoint) {
	if e.Table == nil {
		return
	}
	for _, col := range e.Columns {
		for _, c := range e.Table.Columns {
			if c.Name == col {
				c.NotNull = false
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
