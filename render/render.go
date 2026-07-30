// Package render turns Graphviz DOT text into SVG using the pure-Go
// go-graphviz engine (no system Graphviz required).
package render

import (
	"bytes"
	"context"

	"github.com/goccy/go-graphviz"
)

// SVG renders DOT source to an SVG document with the dot layout engine.
func SVG(dot string) ([]byte, error) {
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Close()

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
