// Package preview serves a live, auto-refreshing browser preview of a .dbml
// file. It watches the file and its imports, re-renders on save, keeps the last
// good diagram on error, and streams reloads to the browser over SSE.
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
	"github.com/jason-cairns/dbml-toolkit/dot"
	"github.com/jason-cairns/dbml-toolkit/render"
	"github.com/jason-cairns/dbml-toolkit/resolver"
)

type server struct {
	entry string
	opt   dot.Options

	mu      sync.RWMutex
	svg     []byte
	errMsg  string
	clients map[chan struct{}]struct{}
}

// Serve renders entry, opens a browser (unless open is false) and blocks,
// re-rendering whenever the file or an imported file changes.
func Serve(entry string, port int, open bool, opt dot.Options) error {
	s := &server{entry: entry, opt: opt, clients: map[chan struct{}]struct{}{}}
	s.rerender()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	go s.watch()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/svg", s.handleSVG)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/events", s.handleEvents)

	addr := "http://" + ln.Addr().String()
	fmt.Println("dbml preview:", addr)
	if open {
		openBrowser(addr)
	}
	return http.Serve(ln, mux)
}

// rerender rebuilds the SVG, preserving the previous diagram on failure.
func (s *server) rerender() {
	schema, diags, err := resolver.Load(s.entry)
	msg := ""
	for _, d := range diags {
		msg += d.Pos.String() + ": " + d.Msg + "\n"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.errMsg = err.Error()
		return
	}
	svg, rerr := render.SVG(dot.Emit(schema, s.opt))
	if rerr != nil {
		s.errMsg = rerr.Error() + "\n" + msg
		return
	}
	s.svg = svg
	s.errMsg = msg
}

func (s *server) watch() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer w.Close()
	s.addWatches(w)

	var debounce <-chan time.Time
	for {
		select {
		case _, ok := <-w.Events:
			if !ok {
				return
			}
			debounce = time.After(60 * time.Millisecond)
		case <-debounce:
			s.rerender()
			s.addWatches(w) // imports may have changed
			s.broadcast()
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
		}
	}
}

// addWatches (re)adds every file in the current module graph to the watcher.
func (s *server) addWatches(w *fsnotify.Watcher) {
	_, files, _, _ := resolver.Graph(s.entry, nil)
	for path := range files {
		_ = w.Add(path)
	}
}

// --- broadcast --------------------------------------------------------------

func (s *server) broadcast() {
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

func (s *server) handleSVG(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	svg := s.svg
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(svg)
}

func (s *server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	msg := s.errMsg
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

func (s *server) handleEvents(w http.ResponseWriter, r *http.Request) {
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

func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
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
