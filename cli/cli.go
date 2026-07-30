// Package cli implements the `dbml` command-line interface: a thin dispatch
// over the parser, emitter, LSP server and live preview.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jasoncairns/dbml-parser/dot"
	"github.com/jasoncairns/dbml-parser/lsp"
	"github.com/jasoncairns/dbml-parser/model"
	"github.com/jasoncairns/dbml-parser/preview"
	"github.com/jasoncairns/dbml-parser/render"
	"github.com/jasoncairns/dbml-parser/resolver"
)

const usage = `dbml — a DBML toolkit

Usage:
  dbml render  [flags] <entry.dbml>   render a diagram (DOT or SVG)
  dbml preview [flags] <file.dbml>    live browser preview with auto-refresh
  dbml lsp                            run the language server over stdio
  dbml parse   [flags] <entry.dbml>   parse and report the resolved model

Run "dbml <command> -h" for command flags.`

// Run executes the CLI and returns a process exit code.
func Run(args []string, version string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("dbml", version)
		return 0
	case "render":
		return cmdRender(args[1:])
	case "preview":
		return cmdPreview(args[1:])
	case "lsp":
		return cmdLSP(args[1:])
	case "parse":
		return cmdParse(args[1:])
	case "-h", "--help", "help":
		fmt.Println(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
}

func cmdRender(args []string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	format := fs.String("format", "svg", "output format: dot|svg")
	detail := fs.String("detail", "full", "detail level: full|keys|tables")
	notation := fs.String("notation", "label", "relationship notation: label|crowfoot")
	notes := fs.Bool("notes", false, "include notes in the diagram")
	out := fs.String("o", "", "output file (default stdout)")
	pos, ok := parseArgs(fs, args)
	if !ok {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "render: missing <entry.dbml>")
		return 2
	}
	entry := pos[0]
	opt, ok := options(*detail, *notation, *notes)
	if !ok {
		return 2
	}
	schema, diags, err := resolver.Load(entry)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	reportDiags(diags)

	dotSrc := dot.Emit(schema, opt)
	var data []byte
	switch *format {
	case "dot":
		data = []byte(dotSrc)
	case "svg":
		data, err = render.SVG(dotSrc)
		if err != nil {
			fmt.Fprintln(os.Stderr, "render:", err)
			return 1
		}
	default:
		fmt.Fprintf(os.Stderr, "render: unknown format %q\n", *format)
		return 2
	}
	return writeOut(*out, data)
}

func cmdParse(args []string) int {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the resolved model as JSON")
	pos, ok := parseArgs(fs, args)
	if !ok {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "parse: missing <entry.dbml>")
		return 2
	}
	entry := pos[0]
	schema, diags, err := resolver.Load(entry)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(summary(schema, diags))
	} else {
		fmt.Printf("%d tables, %d refs, %d enums, %d groups, %d notes\n",
			len(schema.Tables), len(schema.Refs), len(schema.Enums), len(schema.Groups), len(schema.Notes))
		reportDiags(diags)
	}
	if len(diags) > 0 {
		return 1
	}
	return 0
}

func cmdLSP(args []string) int {
	if err := lsp.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "lsp:", err)
		return 1
	}
	return 0
}

func cmdPreview(args []string) int {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	port := fs.Int("port", 0, "port to listen on (0 = pick a free port)")
	noOpen := fs.Bool("no-open", false, "do not open a browser automatically")
	detail := fs.String("detail", "full", "detail level: full|keys|tables")
	notation := fs.String("notation", "label", "relationship notation: label|crowfoot")
	notes := fs.Bool("notes", false, "include notes in the diagram")
	pos, ok2 := parseArgs(fs, args)
	if !ok2 {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "preview: missing <file.dbml>")
		return 2
	}
	entry := pos[0]
	opt, ok := options(*detail, *notation, *notes)
	if !ok {
		return 2
	}
	if err := preview.Serve(entry, *port, !*noOpen, opt); err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		return 1
	}
	return 0
}

// parseArgs parses flags that may be interspersed with positional arguments
// (Go's flag package otherwise stops at the first positional) and returns the
// collected positionals. It reports false if parsing failed.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, bool) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, false
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, true
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// --- helpers ----------------------------------------------------------------

func options(detail, notation string, notes bool) (dot.Options, bool) {
	d, ok := dot.ParseDetail(detail)
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid --detail %q (want full|keys|tables)\n", detail)
		return dot.Options{}, false
	}
	n, ok := dot.ParseNotation(notation)
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid --notation %q (want label|crowfoot)\n", notation)
		return dot.Options{}, false
	}
	return dot.Options{Detail: d, Notation: n, Notes: notes}, true
}

func writeOut(path string, data []byte) int {
	if path == "" {
		os.Stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func reportDiags(diags []model.Diagnostic) {
	for _, d := range diags {
		fmt.Fprintf(os.Stderr, "%s: %s\n", d.Pos, d.Msg)
	}
}

func summary(s *model.Schema, diags []model.Diagnostic) any {
	type col struct {
		Name, Type string
		PK, FK     bool
	}
	type tbl struct {
		Name    string
		Columns []col
	}
	out := struct {
		Tables      []tbl    `json:"tables"`
		Diagnostics []string `json:"diagnostics,omitempty"`
	}{}
	for _, t := range s.Tables {
		tt := tbl{Name: t.Qualified()}
		for _, c := range t.Columns {
			tt.Columns = append(tt.Columns, col{c.Name, c.Type, c.PK, c.FK})
		}
		out.Tables = append(out.Tables, tt)
	}
	for _, d := range diags {
		out.Diagnostics = append(out.Diagnostics, fmt.Sprintf("%s: %s", d.Pos, d.Msg))
	}
	return out
}
