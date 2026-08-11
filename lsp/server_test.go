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

func TestGoToDefinitionOnWildcardImportPath(t *testing.T) {
	dir := t.TempDir()
	views := filepath.Join(dir, "_views")
	if err := os.Mkdir(views, 0o755); err != nil {
		t.Fatal(err)
	}
	medallion := filepath.Join(views, "medallion.dbml")
	main := filepath.Join(dir, "main.dbml")
	if err := os.WriteFile(medallion, []byte("Table medals {\n  id int [pk]\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainSrc := "use * from './_views/medallion'\n"
	if err := os.WriteFile(main, []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{docs: map[string]string{main: mainSrc, medallion: readFile(t, medallion)}}
	idx := s.index(pathToURI(main))
	pathStart := strings.Index(mainSrc, "'./_views/medallion'")
	occ := idx.at(main, position{Line: 0, Char: pathStart + 4})
	if occ == nil {
		t.Fatal("no occurrence found on wildcard import path")
	}
	def, ok := idx.defs[occ.target]
	if !ok {
		t.Fatalf("no definition for import target %q", occ.target)
	}
	if got := uriToPath(def.URI); got != medallion {
		t.Fatalf("definition should open imported file %s, got %s", medallion, got)
	}
	if def.Range.Start != (position{}) {
		t.Fatalf("definition should point to imported file start, got %+v", def.Range.Start)
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

// A panicking handler must not crash the server: dispatch recovers it, and any
// pending request still gets a reply so the client isn't left hanging. The
// single-threaded request loop makes an unrecovered panic fatal to every future
// request, so this guard is what keeps one bad message from killing the session.
func TestDispatchRecoversPanic(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{conn: newConn(strings.NewReader(""), &buf)}
	m := &message{ID: json.RawMessage("7"), Method: "textDocument/hover"}

	// Must not propagate the panic out of dispatch.
	s.dispatch(m, func(*message) { panic("boom") })

	// The pending request (id 7) must have received a reply.
	_, body, _ := strings.Cut(buf.String(), "\r\n\r\n")
	var reply map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("no valid reply written after panic: %q (%v)", body, err)
	}
	if string(reply["id"]) != "7" {
		t.Fatalf("reply should answer request id 7, got %s", reply["id"])
	}
	if _, ok := reply["result"]; !ok {
		t.Fatalf("reply missing result field: %s", body)
	}

	// The server must still handle the next message normally.
	s.dispatch(&message{Method: "textDocument/didChange"}, func(*message) {})
}

// A notification (no id) that panics is recovered without attempting a reply —
// replying to a notification would be a protocol error.
func TestDispatchRecoversNotificationPanic(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{conn: newConn(strings.NewReader(""), &buf)}
	s.dispatch(&message{Method: "textDocument/didChange"}, func(*message) { panic("boom") })
	if buf.Len() != 0 {
		t.Fatalf("no reply expected for a notification panic, wrote %q", buf.String())
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
