package preview

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/dot"
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
