.PHONY: build test lint wasm install typst-example clean

# Build the dbml CLI.
build:
	go build -o dbml ./cmd/dbml

# Run the full test suite.
test:
	go test ./...

# Static analysis (requires golangci-lint).
lint:
	golangci-lint run

# Experimental Typst plugin (requires tinygo), gated behind the `typst` build
# tag and not built in CI/releases. Not committed — a build artifact.
wasm:
	tinygo build -o typst/dbml.wasm -target=wasm-unknown -gc=conservative -tags typst ./typst/plugin

# Install the CLI into $GOBIN / $GOPATH/bin.
install:
	go install ./cmd/dbml

# Render the example diagram and compile the Typst example to a PDF.
typst-example: wasm
	typst compile --root . typst/example.typ typst/example.pdf

clean:
	rm -f dbml typst/dbml.wasm typst/*.pdf typst/*.svg
