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
// rendering view. The entry file and transitive `reuse` imports are exported;
// `use` imports provide resolution context only.
func LoadContext(entry string, overlay map[string]string, context Context) (*model.Schema, []model.Diagnostic, error) {
	schema, files, diags, err := Graph(entry, overlay)
	if err != nil || context == ContextAll {
		return schema, diags, err
	}
	entry, _ = filepath.Abs(entry)
	exported := exportedFiles(entry, files)
	return model.ModuleView(schema, exported, context == ContextRefs), diags, nil
}

func exportedFiles(entry string, files map[string]*ast.File) map[string]bool {
	exported := map[string]bool{}
	var visit func(string)
	visit = func(path string) {
		if exported[path] {
			return
		}
		file := files[path]
		if file == nil {
			return
		}
		exported[path] = true
		for _, imp := range file.Imports {
			if imp.Reuse {
				visit(ResolvePath(path, imp.Path))
			}
		}
	}
	visit(entry)
	return exported
}
