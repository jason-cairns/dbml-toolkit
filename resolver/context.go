package resolver

import (
	"path/filepath"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/model"
)

// Context controls how tables imported with `use` appear in a resolved view.
// Imports are always fully resolved for diagnostics and relationship linking.
type Context int

const (
	ContextAll Context = iota
	ContextRefs
	ContextNone
)

// ParseContext maps a CLI/environment value to a Context.
func ParseContext(value string) (Context, bool) {
	switch strings.ToLower(value) {
	case "", "all":
		return ContextAll, true
	case "refs", "referenced":
		return ContextRefs, true
	case "none":
		return ContextNone, true
	default:
		return ContextAll, false
	}
}

// LoadContext resolves the complete module graph, then returns the requested
// rendering view. The entry file and the symbols its transitive `reuse`
// imports re-export are exported; `use` imports provide resolution context only.
func LoadContext(entry string, overlay map[string]string, context Context) (*model.Schema, []model.Diagnostic, error) {
	schema, files, diags, err := Graph(entry, overlay)
	if err != nil || context == ContextAll {
		return schema, diags, err
	}
	entry, _ = filepath.Abs(entry)
	exports := Exports(entry, files, schema)
	return model.ModuleView(schema, exports, context == ContextRefs), diags, nil
}

// Exports computes the symbols entry re-exports: everything it declares itself,
// plus what its `reuse` imports bring forward — the whole child module for
// `reuse *`, or just the named symbols for a selective `reuse { ... }`.
func Exports(entry string, files map[string]*ast.File, schema *model.Schema) *model.Exports {
	b := &exporter{files: files, schema: schema, modules: map[string]*module{}}
	out := model.NewExports()
	b.collect(entry, out, map[string]bool{})
	return out
}

type exporter struct {
	files   map[string]*ast.File
	schema  *model.Schema
	modules map[string]*module
}

// collect adds everything path exports to out, following `reuse` imports.
func (b *exporter) collect(path string, out *model.Exports, seen map[string]bool) {
	if seen[path] {
		return
	}
	seen[path] = true
	file := b.files[path]
	if file == nil {
		return
	}
	out.AddFile(b.schema, file)
	for _, imp := range file.Imports {
		if !imp.Reuse {
			continue
		}
		child := ResolvePath(path, imp.Path)
		if !imp.Selective {
			// `reuse *`, or a braceless `reuse from`, re-exports the child
			// whole. An empty `reuse { }` is selective and selects nothing.
			b.collect(child, out, seen)
			continue
		}
		for _, item := range imp.Items {
			b.selectItem(child, item, out)
		}
	}
}

// selectItem exports one symbol named by a selective `reuse { ... }`, resolving
// the name against the child module's own namespace.
func (b *exporter) selectItem(child string, item ast.ImportItem, out *model.Exports) {
	m := b.module(child)
	switch strings.ToLower(item.Type) {
	case "table":
		if t := m.tables[item.Name]; t != nil {
			out.Tables[t] = true
		}
	case "tablegroup":
		group := m.groups[item.Name]
		if group == nil {
			return
		}
		out.Groups[group] = true
		for _, member := range group.Members {
			if t := m.tables[model.GroupMemberName(member)]; t != nil {
				out.Tables[t] = true
			}
		}
	case "enum":
		if enum := m.enums[item.Name]; enum != nil {
			out.Enums[enum] = true
		}
	case "note":
		if note := m.notes[item.Name]; note != nil {
			out.Notes[note] = true
		}
	case "schema":
		for _, t := range m.tables {
			if strings.EqualFold(schemaOf(t.Schema), schemaOf(item.Name)) {
				out.Tables[t] = true
			}
		}
	}
	// `tablepartial` items need no export entry: partials are expanded into the
	// tables that inject them before a view is ever taken.
}

// module is one module's namespace: every file it can reach, and the names it
// offers for each kind of symbol. Names come from the declarations in those
// files plus the aliases their own import statements introduce, so an alias
// coined by an unrelated sibling is not a name this module answers to.
type module struct {
	files  map[string]bool
	tables map[string]*model.Table
	groups map[string]*ast.TableGroup
	enums  map[string]*ast.Enum
	notes  map[string]*ast.Note
}

// module builds (and caches) the namespace rooted at path. Reachability follows
// both `use` and `reuse`, since a selective import may name a symbol the child
// itself only pulls in for context.
func (b *exporter) module(path string) *module {
	if cached, ok := b.modules[path]; ok {
		return cached
	}
	m := &module{
		files:  map[string]bool{},
		tables: map[string]*model.Table{},
		groups: map[string]*ast.TableGroup{},
		enums:  map[string]*ast.Enum{},
		notes:  map[string]*ast.Note{},
	}
	b.modules[path] = m

	var visit func(string)
	visit = func(p string) {
		if m.files[p] {
			return
		}
		if b.files[p] == nil {
			return
		}
		m.files[p] = true
		for _, imp := range b.files[p].Imports {
			visit(ResolvePath(p, imp.Path))
		}
	}
	visit(path)

	for p := range m.files {
		file := b.files[p]
		for _, at := range file.Tables {
			t := b.schema.Lookup(ast.QualifiedName(at.Schema, at.Name))
			if t == nil || !m.files[t.NamePos.File] {
				continue
			}
			m.tables[t.Qualified()] = t
			if at.Schema == "" {
				m.tables[at.Name] = t
			}
			if at.Alias != "" {
				m.tables[at.Alias] = t
			}
		}
		for _, group := range file.Groups {
			m.groups[group.Name] = group
		}
		for _, enum := range file.Enums {
			m.enums[ast.QualifiedName(enum.Schema, enum.Name)] = enum
			if enum.Schema == "" {
				m.enums[enum.Name] = enum
			}
		}
		for _, note := range file.Notes {
			m.notes[note.Name] = note
		}
	}
	m.bindAliases(b)
	return m
}

// bindAliases adds the names the module's own import statements coin, so a
// group re-exported as `tablegroup gold as g` answers to `g` further up.
func (m *module) bindAliases(b *exporter) {
	type binding struct{ kind, alias, target string }
	var pending []binding
	for p := range m.files {
		for _, imp := range b.files[p].Imports {
			for _, item := range imp.Items {
				if item.Alias != "" {
					pending = append(pending, binding{strings.ToLower(item.Type), item.Alias, item.Name})
				}
			}
		}
	}
	// Aliases chain (`a as b` in one file, `b as c` in another) and the files
	// come out of a map in no particular order, so keep binding what resolves
	// until a whole pass adds nothing. Each pass drops what it bound, so this
	// always terminates.
	for progress := true; progress; {
		progress = false
		rest := pending[:0]
		for _, bind := range pending {
			if m.bind(bind.kind, bind.alias, bind.target) {
				progress = true
				continue
			}
			rest = append(rest, bind)
		}
		pending = rest
	}
}

// bind names an already-known symbol, reporting whether the target resolved.
func (m *module) bind(kind, alias, target string) bool {
	switch kind {
	case "table":
		if t := m.tables[target]; t != nil {
			m.tables[alias] = t
			return true
		}
	case "tablegroup":
		if group := m.groups[target]; group != nil {
			m.groups[alias] = group
			return true
		}
	case "enum":
		if enum := m.enums[target]; enum != nil {
			m.enums[alias] = enum
			return true
		}
	case "note":
		if note := m.notes[target]; note != nil {
			m.notes[alias] = note
			return true
		}
	}
	return false
}

// schemaOf normalises a schema name, so an omitted schema compares equal to an
// explicit `public`.
func schemaOf(name string) string {
	if name == "" {
		return "public"
	}
	return name
}
