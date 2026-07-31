package lsp

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jason-cairns/dbml-toolkit/d2"
	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/preview"
)

// The diagram render must not run on the request path, and a burst of edits
// must collapse to a single pending job — otherwise interactive requests queue
// behind hundreds of milliseconds of rendering and time out.
func TestPreviewRenderIsAsyncAndCoalesces(t *testing.T) {
	path, _ := filepath.Abs(filepath.Join(t.TempDir(), "x.dbml"))
	s := &Server{
		docs:       map[string]string{path: "Table a {\n  id int\n}\n"},
		preview:    preview.New(d2.New(), diagram.Options{}),
		renderWake: make(chan struct{}, 1),
	}
	defer s.preview.Close()

	// updatePreview must return without rendering (single-threaded here, so
	// reading the preview address is safe), and two enqueues before the worker
	// starts must coalesce to one pending job and a single wake-up.
	s.updatePreview(path)
	s.updatePreview(path)
	if s.preview.Addr() != "" {
		t.Fatal("render ran on the request path; it must be async")
	}
	if got := len(s.renderWake); got != 1 {
		t.Fatalf("bursts should coalesce to one wake-up, got %d", got)
	}
	if s.renderJob == nil {
		t.Fatal("expected a pending render job")
	}

	done := make(chan struct{})
	go func() { s.renderLoop(); close(done) }()

	// The worker clears the job (under renderMu) as it starts rendering; that is
	// our proof it was processed off the request path.
	deadline := time.Now().Add(2 * time.Second)
	drained := false
	for !drained && time.Now().Before(deadline) {
		s.renderMu.Lock()
		drained = s.renderJob == nil
		s.renderMu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	if !drained {
		t.Fatal("async render never consumed the job")
	}

	close(s.renderWake) // stop the worker and join it before teardown
	<-done
}
