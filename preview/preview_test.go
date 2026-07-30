package preview

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/dot"
	"github.com/jason-cairns/dbml-toolkit/model"
)

func TestServerRenders(t *testing.T) {
	s := New(dot.New(), diagram.Options{})
	addr, err := s.Listen(0, false) // bind, do not open a browser
	if err != nil {
		t.Fatal(err)
	}
	s.Render(filepath.Join("..", "testdata", "ecommerce.dbml"), nil)

	body := get(t, addr+"/svg")
	if !strings.Contains(body, "<svg") {
		t.Fatalf("/svg did not return an SVG document")
	}
	if status := get(t, addr+"/status"); !strings.Contains(status, "ecommerce.dbml") {
		t.Fatalf("/status missing active file title: %s", status)
	}
}

// panicEngine stands in for a diagram engine that blows up mid-render.
type panicEngine struct{}

func (panicEngine) Name() string              { return "boom" }
func (panicEngine) Formats() []diagram.Format { return []diagram.Format{diagram.SVG} }
func (panicEngine) Render(*model.Schema, diagram.Options, diagram.Format) ([]byte, error) {
	panic("kaboom")
}

// A panic in the diagram engine must not escape Render: the live LSP drives it
// from its main goroutine, so a crash here would take the whole server down.
func TestRenderRecoversFromEnginePanic(t *testing.T) {
	s := New(panicEngine{}, diagram.Options{})
	if _, err := s.Listen(0, false); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "m.dbml")
	if err := os.WriteFile(f, []byte("Table a { id int }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Render(f, map[string]string{f: "Table a { id int }\n"}) // must not panic
	if status := get(t, s.Addr()+"/status"); !strings.Contains(status, "panicked") {
		t.Fatalf("status should report the render panic, got: %s", status)
	}
}

func TestCloseStopsServing(t *testing.T) {
	s := New(dot.New(), diagram.Options{})
	addr, err := s.Listen(0, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get(addr + "/status"); err != nil {
		t.Fatalf("server should serve before Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := http.Get(addr + "/status"); err == nil {
		t.Fatalf("server should refuse connections after Close")
	}
	// Close is idempotent and safe on an unbound server.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := New(dot.New(), diagram.Options{}).Close(); err != nil {
		t.Fatalf("Close on unbound server: %v", err)
	}
}

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
