// Package cli implements the `dbml` command-line interface: a thin dispatch
// over the parser, rendering engines, LSP server and live preview.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/format"
	"github.com/jason-cairns/dbml-toolkit/lsp"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/preview"
	"github.com/jason-cairns/dbml-toolkit/render"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

const usage = `dbml — a DBML toolkit

Usage:
  dbml render  [flags] <entry.dbml>   render a diagram
  dbml preview [flags] <file.dbml>    live browser preview with auto-refresh
  dbml lsp                            run the language server over stdio
  dbml parse   [flags] <entry.dbml>   parse and report the resolved model
  dbml fmt     [flags] <file.dbml>    format DBML source canonically

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
	case "fmt":
		return cmdFmt(args[1:])
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
	engineName := fs.String("engine", render.Default, "engine: d2|graphviz")
	format := fs.String("format", "svg", "output format: svg|ascii|dot|d2 (per engine)")
	detail := fs.String("detail", "full", "detail level: full|keys|tables")
	notation := fs.String("notation", "crowfoot", "relationship notation: crowfoot|label")
	theme := fs.String("theme", "", "D2 theme name or id (default: flagship)")
	animate := fs.Bool("animate", true, "animate relationship edges (D2)")
	notes := fs.Bool("notes", false, "include inline notes (graphviz; D2 always uses tooltips)")
	noSchema := fs.Bool("no-schema", false, "hide schema names in table headers")
	contextName := fs.String("context", "all", "import context: all|refs|none")
	out := fs.String("o", "", "output file (default stdout)")
	pos, ok := parseArgs(fs, args)
	if !ok {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "render: missing <entry.dbml>")
		return 2
	}
	eng, opt, f, ok := setup(*engineName, *format, *detail, *notation, *theme, *notes, *noSchema, *animate)
	if !ok {
		return 2
	}
	contextMode, ok := resolver.ParseContext(*contextName)
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid --context %q (want all|refs|none)\n", *contextName)
		return 2
	}
	schema, diags, err := resolver.LoadContext(pos[0], nil, contextMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	reportDiags(diags)

	data, err := eng.Render(schema, opt, f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		return 1
	}
	return writeOut(*out, data)
}

func cmdPreview(args []string) int {
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	engineName := fs.String("engine", render.Default, "engine: d2|graphviz")
	port := fs.Int("port", 0, "port to listen on (0 = pick a free port)")
	noOpen := fs.Bool("no-open", false, "do not open a browser automatically")
	detail := fs.String("detail", "full", "detail level: full|keys|tables")
	notation := fs.String("notation", "crowfoot", "relationship notation: crowfoot|label")
	theme := fs.String("theme", "", "D2 theme name or id (default: flagship)")
	animate := fs.Bool("animate", true, "animate relationship edges (D2)")
	notes := fs.Bool("notes", false, "include inline notes (graphviz; D2 always uses tooltips)")
	noSchema := fs.Bool("no-schema", false, "hide schema names in table headers")
	contextName := fs.String("context", "all", "import context: all|refs|none")
	pos, ok := parseArgs(fs, args)
	if !ok {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "preview: missing <file.dbml>")
		return 2
	}
	eng, opt, _, ok := setup(*engineName, "svg", *detail, *notation, *theme, *notes, *noSchema, *animate)
	if !ok {
		return 2
	}
	contextMode, ok := resolver.ParseContext(*contextName)
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid --context %q (want all|refs|none)\n", *contextName)
		return 2
	}
	if err := preview.ServeContext(pos[0], *port, !*noOpen, eng, opt, contextMode); err != nil {
		fmt.Fprintln(os.Stderr, "preview:", err)
		return 1
	}
	return 0
}

func cmdParse(args []string) int {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the resolved model as JSON")
	contextName := fs.String("context", "all", "import context: all|refs|none")
	pos, ok := parseArgs(fs, args)
	if !ok {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "parse: missing <entry.dbml>")
		return 2
	}
	contextMode, ok := resolver.ParseContext(*contextName)
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid --context %q (want all|refs|none)\n", *contextName)
		return 2
	}
	schema, diags, err := resolver.LoadContext(pos[0], nil, contextMode)
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

func cmdFmt(args []string) int {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	write := fs.Bool("w", false, "rewrite the file in place instead of printing to stdout")
	pos, ok := parseArgs(fs, args)
	if !ok {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(os.Stderr, "fmt: missing <file.dbml>")
		return 2
	}
	path := pos[0]
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fmt:", err)
		return 1
	}
	out, diags := format.Format(path, string(src))
	if len(diags) > 0 {
		// Never rewrite a file that does not parse cleanly.
		for _, d := range diags {
			fmt.Fprintf(os.Stderr, "%s: %s\n", d.Pos, d.Msg)
		}
		return 1
	}
	if *write {
		if out == string(src) {
			return 0
		}
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "fmt:", err)
			return 1
		}
		return 0
	}
	fmt.Print(out)
	return 0
}

func cmdLSP(_ []string) int {
	if err := lsp.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "lsp:", err)
		return 1
	}
	return 0
}

// --- helpers ----------------------------------------------------------------

// setup resolves the engine, options and format, reporting usage errors.
func setup(engineName, format, detail, notation, theme string, notes, noSchema, animate bool) (diagram.Engine, diagram.Options, diagram.Format, bool) {
	fail := func(msg string) (diagram.Engine, diagram.Options, diagram.Format, bool) {
		fmt.Fprintln(os.Stderr, msg)
		return nil, diagram.Options{}, "", false
	}
	eng, ok := render.Get(engineName)
	if !ok {
		return fail(fmt.Sprintf("invalid --engine %q (want %s)", engineName, strings.Join(render.Names(), "|")))
	}
	d, ok := diagram.ParseDetail(detail)
	if !ok {
		return fail(fmt.Sprintf("invalid --detail %q (want full|keys|tables)", detail))
	}
	n, ok := diagram.ParseNotation(notation)
	if !ok {
		return fail(fmt.Sprintf("invalid --notation %q (want crowfoot|label)", notation))
	}
	th, ok := diagram.ParseTheme(theme)
	if !ok {
		return fail(fmt.Sprintf("invalid --theme %q", theme))
	}
	f, ok := diagram.ParseFormat(format)
	if !ok {
		return fail(fmt.Sprintf("invalid --format %q", format))
	}
	if !diagram.Supports(eng, f) {
		return fail(fmt.Sprintf("engine %q does not support format %q", eng.Name(), f))
	}
	return eng, diagram.Options{Detail: d, Notation: n, Notes: notes, NoSchema: noSchema, Theme: th, NoAnimate: !animate}, f, true
}

// parseArgs parses flags that may be interspersed with positional arguments
// (Go's flag package otherwise stops at the first positional).
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
