// Command dbml is a DBML toolkit: parse, render diagrams, run a language
// server, or serve a live browser preview.
package main

import (
	"os"

	"github.com/jasoncairns/dbml-parser/cli"
)

// version is set at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
