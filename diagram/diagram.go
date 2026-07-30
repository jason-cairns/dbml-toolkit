// Package diagram is the neutral contract shared by every rendering engine.
// Engines (graphviz, d2) depend only on this package and model — never on each
// other — so they are freely swappable.
package diagram

import (
	"sort"
	"strconv"
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
// notes, schema names shown, default theme, animated relationships.
type Options struct {
	Detail    Detail
	Notation  Notation
	Notes     bool
	NoSchema  bool
	Theme     int64 // D2 theme id; 0 = engine default
	NoAnimate bool  // disable animated relationship edges (D2)
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

// themes maps friendly names to D2 theme ids (see d2themescatalog).
var themes = map[string]int64{
	"neutral": 0, "neutral-grey": 1, "flagship": 3, "cool-classics": 4,
	"mixed-berry-blue": 5, "grape-soda": 6, "aubergine": 7, "colorblind": 8,
	"vanilla-nitro-cola": 100, "orange-creamsicle": 101, "shirley-temple": 102,
	"earth-tones": 103, "everglade-green": 104, "buttered-toast": 105,
	"dark-mauve": 200, "dark-flagship": 201, "terminal": 300,
	"terminal-grayscale": 301, "origami": 302, "c4": 303,
}

// ParseTheme maps a CLI string (a friendly name or a numeric id) to a theme id.
// "" yields 0, which each engine interprets as its default theme.
func ParseTheme(s string) (int64, bool) {
	if s == "" {
		return 0, true
	}
	key := strings.ReplaceAll(strings.ToLower(s), " ", "-")
	if id, ok := themes[key]; ok {
		return id, true
	}
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		return id, true
	}
	return 0, false
}

// ThemeNames lists the recognised theme names.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
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
