//go:build !tinygo.wasm

// This stub lets `go build ./...` succeed on non-wasm hosts; the real plugin is
// compiled with TinyGo (see the Makefile `wasm` target).
package main

func main() {}
