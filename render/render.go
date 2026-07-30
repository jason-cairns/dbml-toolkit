// Package render turns Graphviz DOT text into SVG using the pure-Go
// go-graphviz engine (no system Graphviz required).
package render

import (
	"bytes"
	"context"

	"github.com/goccy/go-graphviz"
)

// SVG renders DOT source to an SVG document using the given layout engine
// (e.g. "neato", "dot", "fdp"). An empty engine defaults to neato.
func SVG(dot, layout string) ([]byte, error) {
	if layout == "" {
		layout = "dot"
	}
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	g.SetLayout(graphviz.Layout(layout))

	graph, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		return nil, err
	}
	defer graph.Close()

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
