// Package preview serves a live, auto-refreshing browser preview of a .dbml
// file. It keeps the last good diagram on error and streams reloads to the
// browser over SSE. It renders from an optional in-memory overlay, so the LSP
// can drive a live preview straight from the editor buffer (before save); the
// standalone `dbml preview` command renders from disk and watches for changes.
package preview

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jason-cairns/dbml-toolkit/diagram"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

// Server is a live-preview HTTP server. Create with New, bind with Listen, and
// push new content with Render.
type Server struct {
	engine diagram.Engine
	opt    diagram.Options

	mu      sync.RWMutex
	svg     []byte
	errMsg  string
	title   string
	clients map[chan struct{}]struct{}

	addr string
	once sync.Once
	http *http.Server
}

// New creates a preview server that renders with the given engine and options.
func New(engine diagram.Engine, opt diagram.Options) *Server {
	return &Server{engine: engine, opt: opt, clients: map[chan struct{}]struct{}{}}
}

// Listen binds a port (0 picks a free one), serves in the background and, if
// open is true, opens a browser at the address. It is idempotent: repeated
// calls return the same address and never rebind or reopen the browser.
func (s *Server) Listen(port int, open bool) (string, error) {
	var err error
	s.once.Do(func() {
		var ln net.Listener
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return
		}
		s.addr = "http://" + ln.Addr().String()
		mux := http.NewServeMux()
		mux.HandleFunc("/", s.handleIndex)
		mux.HandleFunc("/svg", s.handleSVG)
		mux.HandleFunc("/status", s.handleStatus)
		mux.HandleFunc("/events", s.handleEvents)
		s.http = &http.Server{Handler: mux}
		go s.http.Serve(ln)
		if open {
			openBrowser(s.addr)
		}
	})
	return s.addr, err
}

// Addr returns the server address (empty until Listen succeeds).
func (s *Server) Addr() string { return s.addr }

// Close shuts down the HTTP server and frees its port. It is safe to call on a
// server that never bound (Listen was not called or failed) and is idempotent.
// Any connected browsers get a dropped SSE stream and stop refreshing. Once
// closed, the server should not be reused.
func (s *Server) Close() error {
	if s == nil || s.http == nil {
		return nil
	}
	return s.http.Close()
}

// Render rebuilds the diagram for entry (resolving imports through overlay when
// non-nil) and notifies connected browsers. The previous diagram is preserved
// on error so the view never goes blank.
func (s *Server) Render(entry string, overlay map[string]string) {
	svg, errMsg := s.renderResult(entry, overlay)
	s.mu.Lock()
	if svg != nil { // keep the last good diagram on error so the view never blanks
		s.svg = svg
	}
	s.errMsg = errMsg
	s.title = entry
	s.mu.Unlock()
	s.broadcast()
}

// renderResult resolves and renders entry, returning the new SVG (nil to keep
// the previous one) and a status/error string. It recovers from panics in the
// resolver or diagram engine: a live editor buffer passes through many
// transiently-invalid states, and a crash here must never take down a caller
// such as the language server — it degrades to an error message instead.
func (s *Server) renderResult(entry string, overlay map[string]string) (svg []byte, errMsg string) {
	defer func() {
		if r := recover(); r != nil {
			svg, errMsg = nil, fmt.Sprintf("preview render panicked: %v", r)
		}
	}()
	schema, diags, err := resolver.LoadSource(entry, overlay)
	msg := ""
	for _, d := range diags {
		msg += d.Pos.String() + ": " + d.Msg + "\n"
	}
	if err != nil {
		return nil, err.Error()
	}
	out, rerr := s.engine.Render(schema, s.opt, diagram.SVG)
	if rerr != nil {
		return nil, rerr.Error() + "\n" + msg
	}
	return out, msg
}

// Serve is the standalone `dbml preview` entry point: it renders entry from
// disk, opens a browser, then blocks, re-rendering whenever the file or one of
// its imports changes on disk.
func Serve(entry string, port int, open bool, engine diagram.Engine, opt diagram.Options) error {
	s := New(engine, opt)
	addr, err := s.Listen(port, open)
	if err != nil {
		return err
	}
	s.Render(entry, nil)
	fmt.Println("dbml preview:", addr)
	s.watch(entry) // blocks
	return nil
}

// watch re-renders on filesystem changes to entry and its imports.
func (s *Server) watch(entry string) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer w.Close()
	s.addWatches(w, entry)

	var debounce <-chan time.Time
	for {
		select {
		case _, ok := <-w.Events:
			if !ok {
				return
			}
			debounce = time.After(60 * time.Millisecond)
		case <-debounce:
			s.Render(entry, nil)
			s.addWatches(w, entry) // imports may have changed
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// addWatches (re)adds every file in entry's module graph to the watcher.
func (s *Server) addWatches(w *fsnotify.Watcher, entry string) {
	_, files, _, _ := resolver.Graph(entry, nil)
	for path := range files {
		_ = w.Add(path)
	}
}

// --- broadcast --------------------------------------------------------------

func (s *Server) broadcast() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// --- handlers ---------------------------------------------------------------

func (s *Server) handleSVG(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	svg := s.svg
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(svg)
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	msg, title := s.errMsg, s.title
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"error":%q,"title":%q}`, msg, title)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, "retry: 1000\n\n")
	fl.Flush()
	for {
		select {
		case <-ch:
			fmt.Fprint(w, "data: update\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

// --- browser ----------------------------------------------------------------

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "cmd", []string{"/c", "start"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
}
