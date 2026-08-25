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
	b := &exporter{files: files, schema: schema, reach: map[string]map[string]bool{}}
	out := model.NewExports()
	b.collect(entry, out, map[string]bool{})
	return out
}

type exporter struct {
	files  map[string]*ast.File
	schema *model.Schema
	reach  map[string]map[string]bool // module path -> files reachable from it
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
		if len(imp.Items) == 0 {
			// `reuse *` (or a bare `reuse from`) re-exports the child whole.
			b.collect(child, out, seen)
			continue
		}
		for _, item := range imp.Items {
			b.selectItem(child, item, out)
		}
	}
}

// selectItem exports one symbol named by a selective `reuse { ... }`. The
// symbol must be declared somewhere in the child module's own import graph, so
// a selective import cannot pull in an unrelated sibling of the entry file.
func (b *exporter) selectItem(child string, item ast.ImportItem, out *model.Exports) {
	reach := b.reachable(child)
	switch strings.ToLower(item.Type) {
	case "table":
		if t := b.lookupTable(item.Name, reach); t != nil {
			out.Tables[t] = true
		}
	case "tablegroup":
		for path := range reach {
			for _, group := range b.files[path].Groups {
				if !strings.EqualFold(group.Name, item.Name) {
					continue
				}
				out.Groups[group] = true
				for _, member := range group.Members {
					if t := b.lookupTable(model.GroupMemberName(member), reach); t != nil {
						out.Tables[t] = true
					}
				}
			}
		}
	case "enum":
		for path := range reach {
			for _, enum := range b.files[path].Enums {
				if namesSymbol(item.Name, enum.Schema, enum.Name) {
					out.Enums[enum] = true
				}
			}
		}
	case "note":
		for path := range reach {
			for _, note := range b.files[path].Notes {
				if strings.EqualFold(note.Name, item.Name) {
					out.Notes[note] = true
				}
			}
		}
	case "schema":
		for path := range reach {
			for _, table := range b.files[path].Tables {
				if !strings.EqualFold(schemaOf(table.Schema), schemaOf(item.Name)) {
					continue
				}
				if t := b.lookupTable(ast.QualifiedName(table.Schema, table.Name), reach); t != nil {
					out.Tables[t] = true
				}
			}
		}
	}
	// `tablepartial` items need no export entry: partials are expanded into the
	// tables that inject them before a view is ever taken.
}

// lookupTable resolves an imported table name — qualified, bare, table alias or
// import alias — and keeps it only when it is declared inside the child module.
func (b *exporter) lookupTable(name string, reach map[string]bool) *model.Table {
	t := b.schema.Lookup(name)
	if t == nil || !reach[t.NamePos.File] {
		return nil
	}
	return t
}

// reachable returns every file the given module can see, following both `use`
// and `reuse`, since a selective import may name a symbol the child itself only
// uses for context.
func (b *exporter) reachable(path string) map[string]bool {
	if cached, ok := b.reach[path]; ok {
		return cached
	}
	out := map[string]bool{}
	b.reach[path] = out
	var visit func(string)
	visit = func(p string) {
		if out[p] {
			return
		}
		file := b.files[p]
		if file == nil {
			return
		}
		out[p] = true
		for _, imp := range file.Imports {
			visit(ResolvePath(p, imp.Path))
		}
	}
	visit(path)
	return out
}

// namesSymbol reports whether an import item names a declaration, accepting
// either the bare or the schema-qualified spelling.
func namesSymbol(want, schema, name string) bool {
	return strings.EqualFold(want, name) || strings.EqualFold(want, ast.QualifiedName(schema, name))
}

// schemaOf normalises a schema name, so an omitted schema compares equal to an
// explicit `public`.
func schemaOf(name string) string {
	if name == "" {
		return "public"
	}
	return name
}
