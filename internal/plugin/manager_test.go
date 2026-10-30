package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func lifecycleGeneration(t *testing.T, id string, events ...string) *generation {
	t.Helper()
	g := &generation{
		manifest: Manifest{ID: id, Events: events},
		pool:     make(chan *instance, 1), refsDone: make(chan struct{}),
	}
	t.Cleanup(func() {
		g.Retire()
		select {
		case _, ok := <-g.pool:
			if ok {
				t.Error("generation pool was not closed")
			}
		case <-time.After(time.Second):
			t.Error("generation did not close")
		}
	})
	return g
}

func TestManagerSwapKeepsReusedGeneration(t *testing.T) {
	m := NewManager(t.TempDir())
	t.Cleanup(m.Close)
	kept := lifecycleGeneration(t, "kept", "relay.completed")
	old := lifecycleGeneration(t, "replaced")
	m.swap([]*generation{kept, old})
	if !old.beginRequest() {
		t.Fatal("initial generation is not accepting requests")
	}
	defer old.endRequest()

	replacement := lifecycleGeneration(t, "replaced")
	m.replace(replacement)
	if !kept.beginRequest() {
		t.Fatal("replacing another plugin retired a reused generation")
	}
	kept.endRequest()
	if old.beginRequest() {
		old.endRequest()
		t.Fatal("replaced generation still accepts new requests")
	}
	select {
	case <-old.pool:
		t.Fatal("replaced generation closed while a request is in flight")
	default:
	}
	if len(m.retired) != 1 || m.retired[0] != old {
		t.Fatal("retired list contains a reused generation")
	}

	m.remove("replaced")
	if !kept.beginRequest() {
		t.Fatal("removing another plugin retired a reused generation")
	}
	kept.endRequest()
	if replacement.beginRequest() {
		replacement.endRequest()
		t.Fatal("removed generation still accepts new requests")
	}
	if len(m.retired) != 2 || m.retired[1] != replacement {
		t.Fatal("retired list does not contain only removed generations")
	}
	m.swap([]*generation{kept})
	if len(m.retired) != 2 {
		t.Fatal("unchanged swap added reused generation to retired list")
	}
}

func TestManagerInvokeWithoutSubscribersBypassesPayloadLimit(t *testing.T) {
	payload := json.RawMessage(bytes.Repeat([]byte(" "), MaxPayloadBytes+1))
	for _, state := range []string{"uninitialized", "empty", "unrelated"} {
		t.Run(state, func(t *testing.T) {
			m := NewManager(t.TempDir())
			t.Cleanup(m.Close)
			switch state {
			case "empty":
				m.swap(nil)
			case "unrelated":
				m.swap([]*generation{lifecycleGeneration(t, "observer", "relay.completed")})
			}
			got, err := m.Invoke(context.Background(), "relay.request.before_route", "", payload)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("unsubscribed invocation did not preserve payload: %v", err)
			}
		})
	}
}

func TestManagerInvokeWithSubscriberEnforcesPayloadLimit(t *testing.T) {
	m := NewManager(t.TempDir())
	t.Cleanup(m.Close)
	m.swap([]*generation{lifecycleGeneration(t, "observer", "relay.completed")})
	_, err := m.Invoke(context.Background(), "relay.completed", "", make(json.RawMessage, MaxPayloadBytes+1))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized subscribed payload error = %v", err)
	}
}

func TestHandlesTracksActiveSnapshot(t *testing.T) {
	previous := DefaultManager
	t.Cleanup(func() { DefaultManager = previous })
	DefaultManager = nil
	if Handles("relay.completed") {
		t.Fatal("nil default manager reports a subscriber")
	}
	m := NewManager(t.TempDir())
	t.Cleanup(m.Close)
	DefaultManager = m
	if m.Handles("relay.completed") || Handles("relay.completed") {
		t.Fatal("empty manager reports a subscriber")
	}
	g := lifecycleGeneration(t, "observer", "relay.completed")
	m.swap([]*generation{g})
	if !m.Handles("relay.completed") || !Handles("relay.completed") || m.Handles("http.rpc") {
		t.Fatal("subscription check does not match the active manifest")
	}
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for range 1000 {
				m.Handles("relay.completed")
				Handles("http.rpc")
			}
		})
	}
	for range 100 {
		m.swap([]*generation{g})
	}
	readers.Wait()
	m.remove(g.manifest.ID)
	if m.Handles("relay.completed") || Handles("relay.completed") {
		t.Fatal("removed plugin still reports a subscriber")
	}
}

type rejectedPayload struct{ calls *int }

func (p rejectedPayload) MarshalJSON() ([]byte, error) {
	(*p.calls)++
	return nil, errors.New("payload must not be marshaled")
}

func TestInvokeWithoutSubscribersSkipsSerialization(t *testing.T) {
	previous := DefaultManager
	t.Cleanup(func() { DefaultManager = previous })
	for _, state := range []string{"nil", "empty", "unrelated"} {
		t.Run(state, func(t *testing.T) {
			DefaultManager = nil
			if state != "nil" {
				DefaultManager = NewManager(t.TempDir())
				t.Cleanup(DefaultManager.Close)
				if state == "unrelated" {
					DefaultManager.swap([]*generation{lifecycleGeneration(t, "observer", "relay.completed")})
				}
			}
			calls := 0
			result, err := Invoke(context.Background(), "relay.request.before_route", rejectedPayload{calls: &calls})
			if err != nil || result.Payload != nil || calls != 0 {
				t.Fatalf("unsubscribed invocation serialized payload: calls=%d, err=%v", calls, err)
			}
			if _, err := Invoke(context.Background(), "relay.request.before_route", string(bytes.Repeat([]byte("x"), MaxPayloadBytes+1))); err != nil {
				t.Fatalf("unsubscribed large payload failed: %v", err)
			}
		})
	}
}
