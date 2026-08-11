# dbml

A small, self-contained [DBML](https://dbml.dbdiagram.io) toolkit in Go: parse the full language (core syntax, [enrichment/visualization](https://dbml.dbdiagram.io/syntax/enrichment-visualization/), and the [module system](https://dbml.dbdiagram.io/docs/)), and render ER diagrams with [D2](https://d2lang.com) (ELK layout) or Graphviz. It also ships a language server (go-to-definition, references, hover — cross-file) and a live browser preview.

## Install

Download a binary from [Releases](https://github.com/jason-cairns/dbml-toolkit/releases), or:

```bash
go install github.com/jason-cairns/dbml-toolkit/cmd/dbml@latest
```

## Usage

```bash
dbml render schema.dbml -o schema.svg          # D2 (ELK) SVG, the default
dbml render --format ascii schema.dbml         # ASCII ER diagram
dbml render --engine graphviz schema.dbml      # Graphviz fallback
dbml render --context refs module.dbml          # compact imported context
dbml preview schema.dbml                       # live browser preview, auto-refresh
dbml fmt schema.dbml                           # print canonical formatting to stdout
dbml fmt -w schema.dbml                        # format the file in place
dbml lsp                                       # language server over stdio
```

The language server also exposes `textDocument/formatting`, so "format
document" / format-on-save in any LSP editor reformats DBML canonically
(comments are preserved; a file with syntax errors is left untouched).

- `--engine`: `d2` (default) or `graphviz`.
- `--format`: `svg` (default) / `ascii` / `d2` (d2 source) for the D2 engine; `svg` / `dot` for Graphviz.
- `--detail`: `full` (default) / `keys` / `tables`. `--notation`: `crowfoot` (default) / `label`. `--no-schema`.
- `--context`: `all` (default) / `refs` / `none`. The entry file and transitive
  `reuse` imports are rendered in full. With `refs`, tables reached only through
  `use` appear as compact external stubs containing referenced columns; `none`
  hides them. Resolution and diagnostics always use the complete import graph.
- D2 only: `--theme` (name or id, default `flagship`), `--animate` (default on). TableGroups render as containers and notes as tooltips.

Helix (`~/.config/helix/languages.toml`):

```toml
[[language]]
name = "dbml"
scope = "source.dbml"
file-types = ["dbml"]
language-servers = ["dbml"]

[language-server.dbml]
command = "dbml"
args = ["lsp"]
```

Opening a `.dbml` file in an LSP editor automatically opens a live browser
preview that re-renders from the editor buffer as you type. Set
`DBML_PREVIEW=off` to disable it, or `DBML_PREVIEW=manual` to serve the preview
without auto-opening a browser. LSP previews default to referenced context;
set `DBML_PREVIEW_CONTEXT=all|refs|none` to change it.

DBML files should declare their own direct dependencies with `use`. Aggregating
modules should re-export their owned files with `reuse`; imports in a parent
module do not become dependencies of its children.

> A Typst plugin lives under `typst/`, gated behind the `typst` build tag
> (`make wasm`). It is experimental and not part of released builds.

## Development

```bash
make test      # run tests
make build     # build the CLI
make lint      # golangci-lint
```

Every merge to `main` cuts a tagged GitHub release with cross-platform binaries.
