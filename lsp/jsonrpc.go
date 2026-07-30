package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// message is a JSON-RPC 2.0 request, notification, or response envelope.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// conn frames JSON-RPC messages over an LSP stdio stream.
type conn struct {
	r    *bufio.Reader
	w    io.Writer
	werr error // first write failure, e.g. the client closed the pipe
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReader(r), w: w}
}

// read parses the next Content-Length-framed message. A frame that fails to
// JSON-decode is skipped (logged, not returned): its bytes were fully consumed
// so the stream stays aligned, and dropping one bad message is far better than
// tearing down the server and stranding the editor's diagnostics. Only I/O
// errors (including EOF) and malformed framing are fatal.
func (c *conn) read() (*message, error) {
	for {
		length := -1
		for {
			line, err := c.r.ReadString('\n')
			if err != nil {
				return nil, err
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "content-length") {
				length, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		if length < 0 {
			return nil, fmt.Errorf("missing Content-Length header")
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(c.r, body); err != nil {
			return nil, err
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			fmt.Fprintf(os.Stderr, "lsp: skipping unparseable message: %v\n", err)
			continue
		}
		return &m, nil
	}
}

// reply sends a response to a request id. A nil result is encoded as an
// explicit JSON null: a JSON-RPC response must carry either "result" or
// "error", and omitting both yields a message strict clients (e.g. Helix)
// reject, tearing down the connection.
func (c *conn) reply(id json.RawMessage, result any) error {
	if result == nil {
		result = json.RawMessage("null")
	}
	return c.send(&message{JSONRPC: "2.0", ID: id, Result: result})
}

// notify sends a server-initiated notification.
func (c *conn) notify(method string, params any) error {
	raw, _ := json.Marshal(params)
	return c.send(&message{JSONRPC: "2.0", Method: method, Params: raw})
}

func (c *conn) send(m *message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		c.werr = err
		return err
	}
	if _, err := c.w.Write(body); err != nil {
		c.werr = err
		return err
	}
	return nil
}
