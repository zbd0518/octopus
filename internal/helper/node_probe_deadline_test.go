package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/nodetransport"
	"github.com/lingyuins/octopus/internal/nodetransport/testpeer"
)

func TestNodeProbeHonorsCancellationAndTimeout(t *testing.T) {
	manager := nodetransport.NewManager()
	t.Cleanup(func() { _ = manager.Close() })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = manager.Accept(r.Context(), 1, w, r, nodetransport.AcceptOptions{Validate: func() bool { return true }})
	}))
	t.Cleanup(server.Close)
	testpeer.Start(t, "ws"+strings.TrimPrefix(server.URL, "http"), nil, 4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	until := time.Now().Add(5 * time.Second)
	for !manager.Snapshot(1).Online && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if !manager.Snapshot(1).Online {
		t.Fatal("fixture node did not register")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if result := probeNode(ctx, 1, true, manager); result.Code != "timeout" || result.Passed {
		t.Fatalf("timeout: %+v", result)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if result := probeNode(ctx, 1, true, manager); result.Code != "cancelled" || result.Passed {
		t.Fatalf("cancellation: %+v", result)
	}
}
