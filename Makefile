.PHONY: build test lint wasm install typst-example clean

# Build the dbml CLI.
build:
	go build -o dbml .

# Run the full test suite.
test:
	go test ./...

# Static analysis (requires golangci-lint).
lint:
	golangci-lint run

# Build the Typst plugin (requires tinygo). Not committed — a release/build artifact.
wasm:
	tinygo build -o typst/dbml.wasm -target=wasm-unknown -gc=conservative ./typst/plugin

# Install the CLI into $GOBIN / $GOPATH/bin.
install:
	go install .

# Render the example diagram and compile the Typst example to a PDF.
typst-example: wasm
	typst compile --root . typst/example.typ typst/example.pdf

clean:
	rm -f dbml typst/dbml.wasm typst/*.pdf typst/*.svg
