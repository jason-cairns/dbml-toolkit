//go:build tinygo.wasm

// Command plugin is the Typst WebAssembly plugin. It exposes dbml_to_dot via
// the Typst "wasm minimal protocol": DBML source in, Graphviz DOT out. The DOT
// is rendered to SVG inside `typst compile` by the diagraph package (see
// dbml.typ). Graphviz itself cannot run nested in the Typst wasm sandbox, so
// the plugin stays a tiny, pure DBML→DOT converter.
//
//	Build with: tinygo build -target=wasm-unknown -gc=conservative \
//	  -o typst/dbml.wasm ./typst/plugin
package main

import (
	"unsafe"

	"github.com/jason-cairns/dbml-toolkit/ast"
	"github.com/jason-cairns/dbml-toolkit/dot"
	"github.com/jason-cairns/dbml-toolkit/model"
	"github.com/jason-cairns/dbml-toolkit/parser"
)

//go:wasmimport typst_env wasm_minimal_protocol_write_args_to_buffer
func writeArgs(ptr uint32)

//go:wasmimport typst_env wasm_minimal_protocol_send_result_to_host
func sendResult(ptr, size uint32)

var keepAlive []byte // prevents the result buffer from being collected early

func main() {}

// Exported via the deprecated //export directive rather than //go:wasmexport:
// the latter builds a wasm "reactor" whose _initialize the Typst host never
// calls, leaving the heap uninitialised. //export instantiates eagerly.
//
//export dbml_to_dot
func dbml_to_dot(srcLen, optLen int32) int32 {
	buf := make([]byte, srcLen+optLen)
	if len(buf) > 0 {
		writeArgs(uint32(uintptr(unsafe.Pointer(&buf[0]))))
	}
	src := string(buf[:srcLen])
	opt := parseOptions(string(buf[srcLen:]))

	f, _ := parser.Parse("input.dbml", src)
	schema, _ := model.Build([]*ast.File{f}, nil)
	out := []byte(dot.Emit(schema, opt))

	keepAlive = out
	if len(out) > 0 {
		sendResult(uint32(uintptr(unsafe.Pointer(&out[0]))), uint32(len(out)))
	}
	return 0
}

// parseOptions decodes "detail=full,notation=label,notes=true".
func parseOptions(s string) dot.Options {
	opt := dot.Options{}
	for _, kv := range split(s, ',') {
		k, v := cut(kv, '=')
		switch k {
		case "detail":
			opt.Detail, _ = dot.ParseDetail(v)
		case "notation":
			opt.Notation, _ = dot.ParseNotation(v)
		case "notes":
			opt.Notes = v == "true" || v == "1"
		}
	}
	return opt
}

func split(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func cut(s string, sep byte) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
