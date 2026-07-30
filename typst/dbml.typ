// dbml.typ — render DBML schemas as diagrams inside `typst compile`.
//
// The bundled `dbml.wasm` plugin turns DBML source into Graphviz DOT; the
// `diagraph` package renders that DOT to SVG at compile time. Build the plugin
// once with `make wasm` (produces typst/dbml.wasm).
//
// Usage:
//   #import "dbml.typ": dbml
//   #dbml(read("schema.dbml"), notation: "crowfoot", detail: "keys", notes: true)
//
// Works for both paged (PDF/SVG/PNG) and HTML export: diagraph relies on
// `layout`/`place`, which Typst's HTML target does not support, so on the HTML
// target we wrap the diagram in `html.frame`, which embeds it as inline SVG.

#import "@preview/diagraph:0.3.5": raw-render

#let _plugin = plugin("dbml.wasm")

/// Render a DBML schema.
/// - source: DBML text (e.g. `read("schema.dbml")`).
/// - detail: "full" | "keys" | "tables".
/// - notation: "label" | "crowfoot".
/// - notes: include notes in the diagram.
#let dbml(source, detail: "full", notation: "label", notes: false, ..args) = {
  let opt = "detail=" + detail + ",notation=" + notation + ",notes=" + (if notes { "true" } else { "false" })
  let dot = str(_plugin.dbml_to_dot(bytes(source), bytes(opt)))
  let diagram = raw-render(raw(dot), ..args)
  context {
    if target() == "html" {
      html.frame(diagram)
    } else {
      diagram
    }
  }
}
