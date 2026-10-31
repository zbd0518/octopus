package nodetransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestFeatureDisabledBetweenUpgradeAndHelloRejectsRegistration(t *testing.T) {
	var enabled atomic.Bool
	enabled.Store(true)
	old := FeatureEnabledFunc
	FeatureEnabledFunc = enabled.Load
	t.Cleanup(func() { FeatureEnabledFunc = old })
	m := NewManager()
	t.Cleanup(func() { _ = m.Close() })
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { return true }})
	}))
	t.Cleanup(cloud.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(cloud.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	enabled.Store(false)
	m.DisconnectAll()
	f, err := jsonFrame(frameHello, 0, hello{Version: 1, MaxConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, f.encode()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); err == nil || m.Status(1).Online {
		t.Fatal("handshake registered after feature disable")
	}
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(cloud.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol}})
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled upgrade: response=%v err=%v", resp, err)
	}
	enabled.Store(true)
	dialRaw(t, cloud.URL)
	eventually(t, func() bool { return m.Status(1).Online })
}
