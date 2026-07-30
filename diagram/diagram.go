// Package diagram is the neutral contract shared by every rendering engine.
// Engines (graphviz, d2) depend only on this package and model — never on each
// other — so they are freely swappable.
package diagram

import (
	"strings"

	"github.com/jason-cairns/dbml-toolkit/model"
)

// Detail controls how much of each table is drawn.
type Detail int

const (
	Full   Detail = iota // every column
	Keys                 // only pk / fk / unique columns
	Tables               // table names only
)

// Notation controls how relationship endpoints are drawn.
type Notation int

const (
	// Crowfoot draws crow's-foot endpoints. Zero value = default.
	Crowfoot Notation = iota
	// Label annotates edges with 1 / * / 0..1 cardinality text.
	Label
)

// Format is an output format. Not every engine supports every format.
type Format string

const (
	SVG   Format = "svg"
	ASCII Format = "ascii"
	DOT   Format = "dot" // graphviz DOT source
	D2    Format = "d2"  // d2 source
)

// Options configures an engine. Zero value = crow's-foot, full detail, no
// notes, schema names shown.
type Options struct {
	Detail   Detail
	Notation Notation
	Notes    bool
	NoSchema bool
}

// Engine renders a resolved schema to bytes in one of its supported formats.
type Engine interface {
	Name() string
	Formats() []Format
	Render(s *model.Schema, opt Options, f Format) ([]byte, error)
}

// Supports reports whether the engine can produce the given format.
func Supports(e Engine, f Format) bool {
	for _, x := range e.Formats() {
		if x == f {
			return true
		}
	}
	return false
}

// ParseDetail maps a CLI string to a Detail.
func ParseDetail(s string) (Detail, bool) {
	switch strings.ToLower(s) {
	case "full", "":
		return Full, true
	case "keys":
		return Keys, true
	case "tables", "tables-only":
		return Tables, true
	}
	return Full, false
}

// ParseNotation maps a CLI string to a Notation.
func ParseNotation(s string) (Notation, bool) {
	switch strings.ToLower(s) {
	case "crowfoot", "crow", "":
		return Crowfoot, true
	case "label":
		return Label, true
	}
	return Crowfoot, false
}

// ParseFormat maps a CLI string to a Format.
func ParseFormat(s string) (Format, bool) {
	switch strings.ToLower(s) {
	case "svg", "":
		return SVG, true
	case "ascii", "txt":
		return ASCII, true
	case "dot":
		return DOT, true
	case "d2":
		return D2, true
	}
	return SVG, false
}
