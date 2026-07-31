package lsp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// call invokes an LSP method against a fresh server and returns the decoded
// "result" of the reply.
func call(t *testing.T, docs map[string]string, method string, params any) json.RawMessage {
	t.Helper()
	var buf bytes.Buffer
	s := &Server{conn: newConn(strings.NewReader(""), &buf), docs: docs}
	raw, _ := json.Marshal(params)
	s.handle(&message{ID: json.RawMessage("1"), Method: method, Params: raw})
	_, body, _ := strings.Cut(buf.String(), "\r\n\r\n")
	var m struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("bad reply %q: %v", body, err)
	}
	return m.Result
}

func TestParseHexColor(t *testing.T) {
	cases := map[string]color{
		"#fff":      {Red: 1, Green: 1, Blue: 1, Alpha: 1},
		"#FDE897":   {Red: 253.0 / 255, Green: 232.0 / 255, Blue: 151.0 / 255, Alpha: 1},
		"#00000080": {Red: 0, Green: 0, Blue: 0, Alpha: 128.0 / 255},
	}
	for in, want := range cases {
		got, ok := parseHexColor(in)
		if !ok || got != want {
			t.Fatalf("parseHexColor(%q) = %+v, %v; want %+v", in, got, ok, want)
		}
	}
	if _, ok := parseHexColor("#12"); ok {
		t.Fatalf("#12 should be rejected as an invalid colour")
	}
}

func TestHexRoundTrip(t *testing.T) {
	c, _ := parseHexColor("#fde897")
	if got := hexOf(c); got != "#fde897" {
		t.Fatalf("hexOf round trip = %q, want #fde897", got)
	}
}

func TestDocumentColorReportsSwatches(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	docs := map[string]string{path: "TableGroup g [color: #FDE897] {\n}\n"}
	res := call(t, docs, "textDocument/documentColor",
		map[string]any{"textDocument": map[string]string{"uri": pathToURI(path)}})
	var colors []colorInformation
	json.Unmarshal(res, &colors)
	if len(colors) != 1 {
		t.Fatalf("expected 1 colour, got %d (%s)", len(colors), res)
	}
	if colors[0].Range.Start.Line != 0 {
		t.Fatalf("colour should be on line 0, got %d", colors[0].Range.Start.Line)
	}
}
