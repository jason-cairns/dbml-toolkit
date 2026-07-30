package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A JSON-RPC response must carry "result" or "error"; a nil result must be
// encoded as explicit null. Omitting both makes strict clients (Helix) reject
// the message and drop the connection, freezing diagnostics and the preview.
func TestReplyNilEncodesNullResult(t *testing.T) {
	var buf bytes.Buffer
	c := newConn(strings.NewReader(""), &buf)
	if err := c.reply(json.RawMessage("3"), nil); err != nil {
		t.Fatal(err)
	}
	_, body, _ := strings.Cut(buf.String(), "\r\n\r\n")
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("bad response body %q: %v", body, err)
	}
	if _, ok := m["result"]; !ok {
		t.Fatalf("response missing result field: %s", body)
	}
	if string(m["result"]) != "null" {
		t.Fatalf("result should be null, got %s", m["result"])
	}
}

func TestCrossFileNavigation(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.dbml")
	main := filepath.Join(dir, "main.dbml")
	os.WriteFile(base, []byte("Table users {\n  id int [pk]\n}\n"), 0o644)
	mainSrc := "use * from './base'\nTable posts {\n  id int [pk]\n  author int [ref: > users.id]\n}\n"
	os.WriteFile(main, []byte(mainSrc), 0o644)

	s := &Server{docs: map[string]string{base: readFile(t, base), main: mainSrc}}
	idx := s.index(pathToURI(main))

	// "users" appears on line 4 (0-based 3) of main.dbml inside the inline ref.
	col := strings.Index("  author int [ref: > users.id]", "users") + 1
	occ := idx.at(main, position{Line: 3, Char: col})
	if occ == nil {
		t.Fatalf("no occurrence found at the users reference")
	}
	def, ok := idx.defs[occ.target]
	if !ok {
		t.Fatalf("no definition for target %q", occ.target)
	}
	if !strings.HasSuffix(uriToPath(def.URI), "base.dbml") {
		t.Fatalf("definition should be cross-file in base.dbml, got %s", def.URI)
	}
	if def.Range.Start.Line != 0 {
		t.Fatalf("users defined on line 0 of base.dbml, got %d", def.Range.Start.Line)
	}
}

// A single unparseable frame must not kill the connection: its bytes are fully
// consumed so the stream stays aligned, and read must return the next valid
// message. Dying here would strand the editor's diagnostics.
func TestReadSkipsUnparseableFrame(t *testing.T) {
	frame := func(body string) string {
		return "Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body
	}
	stream := frame("{ not json ]") + frame(`{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	c := newConn(strings.NewReader(stream), &bytes.Buffer{})
	m, err := c.read()
	if err != nil {
		t.Fatalf("read returned error instead of skipping bad frame: %v", err)
	}
	if m.Method != "initialized" {
		t.Fatalf("expected to recover the initialized message, got %q", m.Method)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
