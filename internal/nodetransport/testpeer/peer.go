// Package testpeer provides an in-memory protocol fixture for cloud-side tests.
// It is test-only: it requires testing.TB, invokes only the supplied handler, and
// never forwards HTTP requests, reads local configuration, or reconnects.
package testpeer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const subprotocol = "octopus-node-v1"
const chunkSize = 32 << 10

// Peer owns one test WebSocket connection and its in-memory handler calls.
type Peer struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// Start registers cleanup before returning. The caller waits for its Manager's
// online status before dispatching requests; the protocol has no hello ack.
func Start(t testing.TB, url string, headers http.Header, concurrency int, handler http.Handler) *Peer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dialCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{Subprotocols: []string{subprotocol}, HTTPHeader: headers})
	stop()
	if err != nil {
		cancel()
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("test peer handshake: %v", err)
	}
	conn.SetReadLimit(chunkSize + 9)
	p := &Peer{conn: conn, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	hello, err := json.Marshal(map[string]int{"version": 1, "max_concurrency": concurrency})
	if err == nil {
		err = p.send(1, 0, hello)
	}
	if err != nil {
		cancel()
		_ = conn.CloseNow()
		t.Fatalf("test peer hello: %v", err)
	}
	go p.serve(handler)
	t.Cleanup(func() {
		p.Close()
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			t.Error("test peer handler did not stop")
		}
	})
	return p
}

func (p *Peer) Close() { p.cancel(); _ = p.conn.CloseNow() }

func (p *Peer) send(kind byte, id uint64, data []byte) error {
	payload := make([]byte, 9+len(data))
	payload[0] = kind
	binary.BigEndian.PutUint64(payload[1:9], id)
	copy(payload[9:], data)
	ctx, cancel := context.WithTimeout(p.ctx, 3*time.Second)
	defer cancel()
	return p.conn.Write(ctx, websocket.MessageBinary, payload)
}

type pending struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Header http.Header `json:"header"`
	body   []byte
	cancel context.CancelFunc
}

func (p *Peer) serve(handler http.Handler) {
	var workers sync.WaitGroup
	requests := make(map[uint64]*pending)
	defer close(p.done)
	defer workers.Wait()
	defer p.Close()
	for {
		typ, b, err := p.conn.Read(p.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(b) < 9 {
			return
		}
		id := binary.BigEndian.Uint64(b[1:9])
		switch b[0] {
		case 2:
			r := new(pending)
			if json.Unmarshal(b[9:], r) != nil {
				return
			}
			requests[id] = r
		case 3:
			if r := requests[id]; r != nil {
				r.body = append(r.body, b[9:]...)
			}
		case 4:
			r := requests[id]
			if r == nil {
				return
			}
			ctx, cancel := context.WithCancel(p.ctx)
			r.cancel = cancel
			req, err := http.NewRequestWithContext(ctx, r.Method, r.Path, bytes.NewReader(r.body))
			if err != nil {
				cancel()
				return
			}
			req.Header = r.Header
			workers.Add(1)
			go func(id uint64, req *http.Request) {
				defer workers.Done()
				defer cancel()
				w := &responseWriter{peer: p, id: id, ctx: ctx, header: make(http.Header)}
				handler.ServeHTTP(w, req)
				if ctx.Err() == nil {
					w.WriteHeader(http.StatusOK)
					_ = p.send(7, id, nil)
				}
			}(id, req)
		case 9:
			if r := requests[id]; r != nil && r.cancel != nil {
				r.cancel()
			}
			delete(requests, id)
		default:
			return
		}
	}
}

type responseWriter struct {
	peer        *Peer
	id          uint64
	ctx         context.Context
	header      http.Header
	wroteHeader bool
	err         error
}

func (w *responseWriter) Header() http.Header { return w.header }
func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	data, err := json.Marshal(struct {
		Status int         `json:"status"`
		Header http.Header `json:"header"`
	}{status, w.header})
	if err == nil {
		err = w.peer.send(5, w.id, data)
	}
	w.err = err
}
func (w *responseWriter) Flush() { w.WriteHeader(http.StatusOK) }
func (w *responseWriter) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if w.err != nil {
		return 0, w.err
	}
	total := 0
	for len(b) > 0 {
		if err := w.ctx.Err(); err != nil {
			return total, err
		}
		n := min(len(b), chunkSize)
		if err := w.peer.send(6, w.id, b[:n]); err != nil {
			return total, fmt.Errorf("test peer response: %w", err)
		}
		total += n
		b = b[n:]
	}
	return total, nil
}
