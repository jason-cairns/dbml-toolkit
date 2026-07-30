package render

import (
	"bytes"
	"testing"
)

func TestSVGSmoke(t *testing.T) {
	dot := `digraph { a -> b; }`
	out, err := SVG(dot, "dot")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("<svg")) {
		t.Fatalf("expected an <svg element, got %d bytes", len(out))
	}
}
