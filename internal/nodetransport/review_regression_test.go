package nodetransport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lingyuins/octopus/internal/nodetransport/testpeer"
)

func TestCompletedUnreadBodiesKeepConcurrencyCapacity(t *testing.T) {
	var completed atomic.Int32
	f := setup(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "small-complete-response")
		completed.Add(1)
	})
	bodies := make([]io.ReadCloser, 0, 4)
	for i := 0; i < 4; i++ {
		resp, err := f.manager.Client(1, 0).Do(modelRequest(t, context.Background(), "/responses", `{"model":"allowed"}`))
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, resp.Body)
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	eventually(t, func() bool { return completed.Load() == 4 })
	eventually(t, func() bool {
		f.manager.mu.Lock()
		s := f.manager.sessions[1]
		f.manager.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.streams) != 4 {
			return false
		}
		for _, st := range s.streams {
			select {
			case <-st.done:
			default:
				return false
			}
		}
		return true
	})
	if status := f.manager.Status(1); status.InFlight != 4 {
		t.Fatalf("completed unread bodies must count: %+v", status)
	}
	for i := 0; i < 20; i++ {
		_, err := f.manager.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{"model":"allowed"}`))
		if !errors.Is(err, ErrNodeBusy) || IsExecutionUncertain(err) {
			t.Fatalf("unread responses bypassed limit: %v", err)
		}
	}
	if completed.Load() != 4 {
		t.Fatal("busy request reached peer")
	}
	// EOF, without Close, returns exactly one slot.
	if _, err := io.Copy(io.Discard, bodies[0]); err != nil {
		t.Fatal(err)
	}
	if f.manager.Status(1).InFlight != 3 {
		t.Fatal("EOF did not return capacity")
	}
	// Close must also release an unread completed body and be idempotent.
	_ = bodies[1].Close()
	_ = bodies[1].Close()
	if f.manager.Status(1).InFlight != 2 {
		t.Fatal("Close did not return exactly one slot")
	}
	resp, err := f.manager.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{"model":"allowed"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if f.manager.Status(1).InFlight != 2 {
		t.Fatal("replacement body leaked capacity")
	}
}

func TestDeploymentPrefixRealWebSocket(t *testing.T) {
	m := NewManager()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/octopus/team-1/node/v1/connect" {
			http.NotFound(w, r)
			return
		}
		_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { return r.Header.Get("Authorization") == "Bearer prefix-test" }})
	}))
	t.Cleanup(cloud.Close)
	t.Cleanup(func() { _ = m.Close() })
	testpeer.Start(t, "ws"+strings.TrimPrefix(cloud.URL, "http")+"/octopus/team-1/node/v1/connect", http.Header{"Authorization": []string{"Bearer prefix-test"}}, 4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "prefix-ok") }))
	eventually(t, func() bool { return m.Status(1).Online })
	resp, err := m.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{"model":"allowed"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(b) != "prefix-ok" {
		t.Fatalf("prefix roundtrip %q %v", b, err)
	}
}

func TestPeerErrorsAreSanitizedAndPreserveDeadline(t *testing.T) {
	for _, payload := range []string{"http://127.0.0.1/private?key=secret", "deadline", "local_unavailable", "local_authentication", "local_http", "invalid_models"} {
		t.Run(payload, func(t *testing.T) {
			m := NewManager()
			defer m.Close()
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { return true }})
			}))
			defer cloud.Close()
			c := dialRaw(t, cloud.URL)
			eventually(t, func() bool { return m.Status(1).Online })
			done := make(chan error, 1)
			go func() {
				_, err := m.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{}`))
				done <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			request, err := readFrame(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Write(ctx, websocket.MessageBinary, frame{frameError, request.id, []byte(payload)}.encode()); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !IsExecutionUncertain(err) {
					t.Fatalf("lost execution uncertainty: %v", err)
				}
				known := map[string]error{"local_unavailable": ErrLocalUnavailable, "local_authentication": ErrLocalAuthentication, "local_http": ErrLocalHTTP, "invalid_models": ErrInvalidModels}
				if expected := known[payload]; expected != nil && !errors.Is(err, expected) {
					t.Fatalf("lost safe diagnostic %s: %v", payload, err)
				}
				if payload == "deadline" {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("lost deadline: %v", err)
					}
				} else if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "127.0.0.1") {
					t.Fatalf("peer error leaked: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("peer error did not finish request")
			}
		})
	}
}

func TestSessionReplacementAbortsWithoutReplay(t *testing.T) {
	m := NewManager()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = m.Accept(r.Context(), 1, w, r, AcceptOptions{Validate: func() bool { return true }})
	}))
	t.Cleanup(cloud.Close)
	t.Cleanup(func() { _ = m.Close() })
	first := dialRaw(t, cloud.URL)
	eventually(t, func() bool { return m.Status(1).Online })
	done := make(chan error, 1)
	go func() {
		_, err := m.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{}`))
		done <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := readFrame(ctx, first); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	testpeer.Start(t, "ws"+strings.TrimPrefix(cloud.URL, "http")+"/node/v1/connect", nil, 4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, "new-session") }))
	select {
	case err := <-done:
		if !IsExecutionUncertain(err) {
			t.Fatalf("replacement lost execution uncertainty: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("replaced request hung")
	}
	time.Sleep(30 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("pending request replayed on replacement")
	}
	resp, err := m.Client(1, time.Second).Do(modelRequest(t, context.Background(), "/responses", `{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "new-session" || calls.Load() != 1 {
		t.Fatalf("replacement request: %q %v calls=%d", body, err, calls.Load())
	}
}
