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
dbml preview schema.dbml                       # live browser preview, auto-refresh
dbml lsp                                       # language server over stdio
```

- `--engine`: `d2` (default) or `graphviz`.
- `--format`: `svg` (default) / `ascii` / `d2` (d2 source) for the D2 engine; `svg` / `dot` for Graphviz.
- `--detail`: `full` (default) / `keys` / `tables`. `--notation`: `crowfoot` (default) / `label`. `--notes`, `--no-schema`.

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
without auto-opening a browser.

> A Typst plugin lives under `typst/`, gated behind the `typst` build tag
> (`make wasm`). It is experimental and not part of released builds.

## Development

```bash
make test      # run tests
make build     # build the CLI
make lint      # golangci-lint
```

Every merge to `main` cuts a tagged GitHub release with cross-platform binaries.
