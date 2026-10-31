package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/nodetransport"
	"github.com/lingyuins/octopus/internal/nodetransport/testpeer"
)

func TestNodeProbeAcceptsLargeValidModelsResponse(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	models := make([]map[string]string, 1024)
	for i := range models {
		models[i] = map[string]string{"id": fmt.Sprintf("%04d-", i) + strings.Repeat("m", 506)}
	}
	body, err := json.Marshal(map[string]any{"data": models})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 533514 {
		t.Fatalf("fixture size: %d", len(body))
	}
	manager := nodetransport.NewManager()
	t.Cleanup(func() { _ = manager.Close() })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = manager.Accept(r.Context(), 1, w, r, nodetransport.AcceptOptions{Validate: func() bool { return true }})
	}))
	t.Cleanup(server.Close)
	testpeer.Start(t, "ws"+strings.TrimPrefix(server.URL, "http"), nil, 4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("unexpected probe: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	until := time.Now().Add(3 * time.Second)
	for !manager.Snapshot(1).Online && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	if !manager.Snapshot(1).Online {
		t.Fatal("fixture node did not register")
	}
	result := probeNode(context.Background(), 1, true, manager)
	if !result.Passed || result.Code != "ok" || result.ModelCount != len(models) {
		t.Fatalf("legal large model list rejected: %+v", result)
	}
	if status := manager.Snapshot(1); status.InFlight != 0 {
		t.Fatalf("probe retained concurrency capacity: %+v", status)
	}
}
