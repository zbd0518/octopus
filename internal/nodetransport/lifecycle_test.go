package nodetransport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDisconnectAfterHeadersNotCompletion(t *testing.T) {
	started := make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: partial\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	resp, err := f.manager.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{"model":"allowed"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	<-started
	f.manager.Disconnect(1)
	b, err := io.ReadAll(resp.Body)
	if !IsExecutionUncertain(err) {
		t.Fatalf("partial response mistaken for completion: %q %v", b, err)
	}
}

func TestBodyTimeoutPreservesExecutionType(t *testing.T) {
	canceled := make(chan struct{})
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hi\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	})
	resp, err := f.manager.Client(1, 100*time.Millisecond).Do(modelRequest(t, context.Background(), "/responses", `{"model":"allowed"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	if !IsExecutionUncertain(err) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body timeout type %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("body timeout did not cancel peer request")
	}
}

func TestCloudOriginAndAuthenticationRequired(t *testing.T) {
	m := NewManager()
	defer m.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { return true }})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol}, HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}}})
	if c != nil {
		_ = c.CloseNow()
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("cross-origin upgrade accepted")
	}
	for _, opts := range []AcceptOptions{{}, {Validate: func() bool { return false }}} {
		rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = m.Accept(r.Context(), 1, w, r, opts) }))
		if opts.Validate == nil {
			c, resp, err = websocket.Dial(ctx, "ws"+strings.TrimPrefix(rejected.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol}})
			if c != nil {
				_ = c.CloseNow()
			}
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err == nil {
				t.Error("missing validator accepted")
			}
		} else {
			c = dialRaw(t, rejected.URL)
			if _, _, err = c.Read(ctx); err == nil {
				t.Error("false validator accepted")
			}
			if m.Status(1).Online {
				t.Error("unauthorized session registered")
			}
		}
		rejected.Close()
	}
}

func TestManagerShutdownClosesIdleSession(t *testing.T) {
	m := NewManager()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { return true }})
	}))
	defer server.Close()
	c := dialRaw(t, server.URL)
	eventually(t, func() bool { return m.Status(1).Online })
	begin := time.Now()
	_ = m.Close()
	if time.Since(begin) > time.Second {
		t.Fatal("manager close waited on peer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("session survived manager shutdown")
	}
	dialRaw(t, server.URL)
	time.Sleep(20 * time.Millisecond)
	if m.Status(1).Online {
		t.Fatal("closed manager accepted session")
	}
}
