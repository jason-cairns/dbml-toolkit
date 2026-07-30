# dbml

A small, self-contained [DBML](https://dbml.dbdiagram.io) toolkit in Go: parse the full language (core syntax, [enrichment/visualization](https://dbml.dbdiagram.io/syntax/enrichment-visualization/), and the [module system](https://dbml.dbdiagram.io/docs/)), and render ER diagrams to DOT or SVG. It also ships a language server (go-to-definition, references, hover — cross-file), a live browser preview, and a [Typst](https://typst.app) library.

## Install

Download a binary from [Releases](https://github.com/jason-cairns/dbml-toolkit/releases), or:

```bash
go install github.com/jason-cairns/dbml-toolkit/cmd/dbml@latest
```

## Usage

```bash
dbml render --format svg --notation crowfoot --detail keys --notes schema.dbml -o schema.svg
dbml preview schema.dbml          # live browser preview, auto-refresh
dbml lsp                          # language server over stdio
```

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

Typst — build the plugin with `make wasm`, then:

```typ
#import "dbml.typ": dbml
#dbml(read("schema.dbml"), notation: "crowfoot", notes: true)
```

## Development

```bash
make test      # run tests
make build     # build the CLI
make wasm      # build the Typst plugin (needs tinygo)
make lint      # golangci-lint
```

Every merge to `main` cuts a tagged GitHub release with cross-platform binaries and the Typst `dbml.wasm`.
