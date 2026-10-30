package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dm "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/transformer/inbound"
	tm "github.com/lingyuins/octopus/internal/transformer/model"
)

func TestSharedFollowerReceivesCompleteErrorAndCanCancel(t *testing.T) {
	setupDiagnosticsRelay(t, true)
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			key := fmt.Sprint("shared-test-", cancelled)
			flight := &sharedRelayFlight{done: make(chan struct{})}
			sharedRelayFlights.Lock()
			sharedRelayFlights.entries[key] = flight
			sharedRelayFlights.Unlock()
			defer func() {
				sharedRelayFlights.Lock()
				delete(sharedRelayFlights.entries, key)
				sharedRelayFlights.Unlock()
			}()
			c, w := diagnosticsRequest("shared", false)
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			internal := &tm.InternalLLMRequest{Model: "shared"}
			req := &relayRequest{c: c, clientCtx: ctx, operationCtx: context.Background(), internalRequest: internal, inAdapter: inbound.Get(inbound.InboundTypeOpenAIChat), metrics: NewRelayMetrics(929001, "shared", "chat", "chat", "", internal)}
			done := make(chan error, 1)
			go func() { _, err := serveSharedRelay(req, dm.Group{}, "shared", key, 1, 1, 0, 10); done <- err }()
			if cancelled {
				cancel()
			} else {
				flight.result = &sharedRelayResponse{status: 200, header: http.Header{"Content-Type": []string{"application/json"}}, body: []byte(`{"error":{"code":"content_blocked"}}`)}
				close(flight.done)
			}
			select {
			case err := <-done:
				if cancelled && err != errClientDisconnected {
					t.Fatal(err)
				}
				if !cancelled && (w.Code != 200 || !strings.Contains(w.Body.String(), "content_blocked")) {
					t.Fatal(w.Code, w.Body.String())
				}
			case <-time.After(time.Second):
				t.Fatal("follower did not finish")
			}
		})
	}
}

func TestSharedResponseRecorderCapsBody(t *testing.T) {
	recorder := newSharedResponseRecorder(4)
	if _, err := recorder.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if !recorder.Overflow() || string(recorder.Body()) != "hell" {
		t.Fatalf("overflow=%t body=%q", recorder.Overflow(), recorder.Body())
	}
}

func TestRoutingContextAndProtectedHeader(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.Header.Set("Content-Type", "application/json; charset=utf-8")
	c.Request.Header.Set("X-Client", "mobile")
	c.Request.Header.Set("api-key", "client-secret")
	c.Set("api_key_name", "named")
	ctx := routingConditionContext(c, "model")
	if ctx.APIKeyName != "named" || ctx.ContentType != "application/json" || ctx.Headers["x-client"] != "mobile" {
		t.Fatalf("incomplete context %+v", ctx)
	}
	upstream := httptest.NewRequest("POST", "http://example.invalid", nil)
	upstream.Header.Set("api-key", "operator-secret")
	ra := &relayAttempt{relayRequest: &relayRequest{c: c}, channel: &dm.Channel{}}
	ra.copyHeaders(upstream, nil)
	if upstream.Header.Get("api-key") != "operator-secret" {
		t.Fatal("client overwrote protected credential")
	}
}

func TestFinishedFailedSessionRetriesAndMissingCursorFails(t *testing.T) {
	setupDiagnosticsRelay(t, false)
	if err := setting.SetString(dm.SettingKeyStreamSessionReplayEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	session, _, err := acquireRelayStreamSession("retry-session-diagnostics", 929001, 711)
	if err != nil {
		t.Fatal(err)
	}
	session.Finish(fmt.Errorf("private upstream details"))
	next, created, err := acquireRelayStreamSession("retry-session-diagnostics", 929001, 711)
	if err != nil || !created || next == session {
		t.Fatal("failed session reused", created, err)
	}
	defer next.Finish(nil)
	if _, _, err := acquireRelayStreamSessionForResume("missing-session-diagnostics", 929001, 712, 5); err != errRelayReplayWindowExpired {
		t.Fatal("resume restarted generation", err)
	}
}
