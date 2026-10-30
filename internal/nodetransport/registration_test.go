package nodetransport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestInvalidHelloNeverRegistersOrValidates(t *testing.T) {
	for i, payload := range []frame{
		{kind: frameRequest},
		{kind: frameHello, id: 1, data: []byte(`{"version":1,"max_concurrency":4}`)},
		{kind: frameHello, data: []byte(`{"version":2,"max_concurrency":4}`)},
		{kind: frameHello, data: []byte(`{"version":1,"max_concurrency":0}`)},
		{kind: frameHello, data: []byte(`{"version":1,"max_concurrency":33}`)},
		{kind: frameHello, data: []byte(`{`)},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			m := NewManager()
			defer m.Close()
			validated := make(chan struct{}, 1)
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { validated <- struct{}{}; return true }})
			}))
			defer cloud.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(cloud.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			if err := c.Write(ctx, websocket.MessageBinary, payload.encode()); err != nil {
				t.Fatal(err)
			}
			if _, _, err := c.Read(ctx); err == nil {
				t.Fatal("invalid hello retained connection")
			}
			if m.Status(1).Online {
				t.Fatal("invalid hello registered")
			}
			select {
			case <-validated:
				t.Fatal("invalid hello reached authorization callback")
			default:
			}
		})
	}
}

func TestConcurrencyNegotiationUsesLowerBound(t *testing.T) {
	for _, item := range []struct{ server, peer, want int }{{2, 4, 2}, {8, 2, 2}, {0, 8, 4}, {100, 32, 32}} {
		m := NewManager()
		cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{MaxConcurrency: item.server, Validate: func() bool { return true }})
		}))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(cloud.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol}})
		if err != nil {
			cancel()
			cloud.Close()
			t.Fatal(err)
		}
		f, err := jsonFrame(frameHello, 0, hello{Version: 1, MaxConcurrency: item.peer})
		if err == nil {
			err = c.Write(ctx, websocket.MessageBinary, f.encode())
		}
		if err != nil {
			_ = c.CloseNow()
			cancel()
			cloud.Close()
			t.Fatal(err)
		}
		eventually(t, func() bool { return m.Status(1).Online })
		if got := m.Status(1).MaxConcurrency; got != item.want {
			t.Errorf("server=%d peer=%d limit=%d want=%d", item.server, item.peer, got, item.want)
		}
		_ = c.CloseNow()
		_ = m.Close()
		cancel()
		cloud.Close()
	}
}
