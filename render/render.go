// Package render is the composition root: it is the only package that knows
// about every rendering engine, exposing them by name behind the neutral
// diagram.Engine interface so callers (cli, preview) stay engine-agnostic.
package render

import (
	"github.com/jason-cairns/dbml-toolkit/d2"
	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/dot"
)

// Default is the engine used when none is specified.
const Default = "d2"

// Get returns the engine registered under name ("" = Default).
func Get(name string) (diagram.Engine, bool) {
	switch name {
	case "", "d2":
		return d2.New(), true
	case "graphviz", "dot":
		return dot.New(), true
	}
	return nil, false
}

// Names lists the available engine names.
func Names() []string { return []string{"d2", "graphviz"} }
