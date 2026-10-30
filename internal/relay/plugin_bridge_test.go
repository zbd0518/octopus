package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/plugin"
	"github.com/lingyuins/octopus/internal/transformer/inbound"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
)

func TestApplyBeforeRoutePluginSkipsWhenNoManager(t *testing.T) {
	req := &transmodel.InternalLLMRequest{Model: "gpt-4", Messages: []transmodel.Message{{Role: "user", Content: transmodel.MessageContent{Content: strPtr("hi")}}}}
	changed, err := applyBeforeRoutePlugin(context.Background(), &gin.Context{}, req)
	if err != nil || changed {
		t.Fatalf("expected no-op, got changed=%v err=%v", changed, err)
	}
}

func TestApplyRouteFilterPluginSkipsWhenNoManager(t *testing.T) {
	group := dbmodel.Group{ID: 1, Items: []dbmodel.GroupItem{{ChannelID: 1}, {ChannelID: 2}}}
	out, err := applyRouteFilterPlugin(context.Background(), group, "gpt-4", "rid")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(out.Items))
	}
}

func TestApplyBeforeUpstreamPluginSkipsWhenNoManager(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.example.com/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4"}`)))
	req.Header.Set("Authorization", "Bearer sk-secret")
	if err := applyBeforeUpstreamPlugin(context.Background(), req, 1, "a"); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer sk-secret" {
		t.Fatal("credential header was modified")
	}
}

func TestApplyBeforeSendPluginSkipsWhenNoManager(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := []byte(`{"id":"x"}`)
	out, err := applyBeforeSendPlugin(context.Background(), c, body, "rid")
	if err != nil || !bytes.Equal(out, body) {
		t.Fatalf("expected pass-through, got %q err=%v", out, err)
	}
}

func strPtr(s string) *string { return &s }

func TestPluginHooksWithoutSubscribersPreserveLargePayloads(t *testing.T) {
	old := plugin.DefaultManager
	plugin.DefaultManager = plugin.NewManager(t.TempDir())
	t.Cleanup(func() { plugin.DefaultManager.Close(); plugin.DefaultManager = old })
	body := []byte(`{"text":"` + strings.Repeat("x", 2<<20) + `"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Anthropic-Version", "2023-06-01")
	originalBody := req.Body
	if err := applyBeforeUpstreamPlugin(context.Background(), req, 1, "a"); err != nil {
		t.Fatal(err)
	}
	if req.Body != originalBody || req.Header.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatal("unsubscribed upstream hook modified request")
	}
	actual, _ := io.ReadAll(req.Body)
	if !bytes.Equal(actual, body) {
		t.Fatal("request body changed")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	out, err := applyBeforeSendPlugin(context.Background(), c, body, "a")
	if err != nil || !bytes.Equal(out, body) {
		t.Fatalf("large response changed: %v", err)
	}
	internal := &transmodel.InternalLLMRequest{Model: "gpt-4", Messages: []transmodel.Message{{Role: "user", Content: transmodel.MessageContent{Content: strPtr(string(body))}}}}
	if changed, err := applyBeforeRoutePlugin(context.Background(), c, internal); err != nil || changed {
		t.Fatalf("large request changed: %v", err)
	}
}

func TestPluginUpstreamHeadersPreserveProtocolAndCredentials(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", "feature")
	req.Header.Set("Authorization", "Bearer original")
	applyPluginUpstreamHeaders(req, map[string]string{"Anthropic-Version": "evil", "Authorization": "Bearer replacement", "Connection": "close", "X-Custom": "ignored", "Content-Type": "application/json", "X-Octopus-Test": "allowed"})
	for name, want := range map[string]string{"Anthropic-Version": "2023-06-01", "Anthropic-Beta": "feature", "Authorization": "Bearer original", "Content-Type": "application/json", "X-Octopus-Test": "allowed", "Connection": "", "X-Custom": ""} {
		if got := req.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestPluginModelOnlyEditPreservesOpaqueWireFields(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"model":"old","input":"hello","provider_extension":{"enabled":true},"previous_response_id":"resp-1"}`)
	original, err := inbound.Get(inbound.InboundTypeOpenAIResponse).TransformRequest(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	original.RawRequest = raw
	updated := *original
	updated.Model = "new"
	wire, err := encodePluginRequestWire(ctx, original, &updated)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["provider_extension"]) != `{"enabled":true}` || string(fields["previous_response_id"]) != `"resp-1"` || string(fields["input"]) != `"hello"` {
		t.Fatalf("model-only edit lost opaque wire fields: %s", wire)
	}
}

func TestPluginProcessingFailureDoesNotRetryUpstream(t *testing.T) {
	for _, status := range []int{0, http.StatusOK} {
		err := fmt.Errorf("response hook: %w: %w", errPluginProcessing, context.DeadlineExceeded)
		decision := ClassifyRelayError(status, err, false)
		if decision.Scope != ScopeAbortAll || !decision.SkipFailureAccounting || !decision.IsError {
			t.Fatalf("unexpected plugin failure decision: %+v", decision)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("plugin failure lost cause")
		}
	}
}

func TestPluginRequestWirePreservesInboundProtocol(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind inbound.InboundType
		body string
	}{
		{"responses", inbound.InboundTypeOpenAIResponse, `{"model":"old","input":"hello","instructions":"system prompt","max_output_tokens":42}`},
		{"anthropic", inbound.InboundTypeAnthropic, `{"model":"old","system":"system prompt","max_tokens":42,"messages":[{"role":"user","content":"hello"}]}`},
		{"chat", inbound.InboundTypeOpenAIChat, `{"model":"old","messages":[{"role":"system","content":"system prompt"},{"role":"user","content":"hello"}]}`},
		{"embedding", inbound.InboundTypeOpenAIEmbedding, `{"model":"old","input":"hello","dimensions":12}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			original, err := inbound.Get(tc.kind).TransformRequest(ctx, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			original.RawRequest = []byte(tc.body)
			for _, modelOnly := range []bool{true, false} {
				encoded, _ := json.Marshal(original)
				var updated transmodel.InternalLLMRequest
				if err := json.Unmarshal(encoded, &updated); err != nil {
					t.Fatal(err)
				}
				updated.RawAPIFormat = original.RawAPIFormat
				updated.Model = "new"
				if !modelOnly {
					temperature := 0.5
					updated.Temperature = &temperature
				}
				raw, err := encodePluginRequestWire(ctx, original, &updated)
				if err != nil {
					t.Fatal(err)
				}
				parsed, err := inbound.Get(tc.kind).TransformRequest(ctx, raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := parsed.Validate(); err != nil {
					t.Fatal(err)
				}
				if parsed.Model != "new" || len(parsed.Messages) != len(original.Messages) {
					t.Fatalf("lost request fields (modelOnly=%v): %s", modelOnly, raw)
				}
				if tc.kind == inbound.InboundTypeAnthropic {
					var wire map[string]json.RawMessage
					json.Unmarshal(raw, &wire)
					if len(wire["system"]) == 0 {
						t.Fatalf("missing top-level system: %s", raw)
					}
				}
				if tc.kind == inbound.InboundTypeOpenAIResponse && (parsed.MaxCompletionTokens == nil || *parsed.MaxCompletionTokens != 42) {
					t.Fatalf("lost Responses token limit: %s", raw)
				}
				if tc.kind == inbound.InboundTypeOpenAIEmbedding && (parsed.EmbeddingInput == nil || parsed.EmbeddingInput.Single == nil || *parsed.EmbeddingInput.Single != "hello" || parsed.EmbeddingDimensions == nil || *parsed.EmbeddingDimensions != 12) {
					t.Fatalf("lost embedding input/options: %s", raw)
				}
			}
		})
	}
}

func TestApplyBeforeRoutePluginDetectsNoChange(t *testing.T) {
	// No plugin manager: unchanged request should be a no-op even if payload
	// matches input.
	req := &transmodel.InternalLLMRequest{Model: "gpt-4"}
	changed, err := applyBeforeRoutePlugin(context.Background(), &gin.Context{}, req)
	if err != nil || changed {
		t.Fatalf("no-manager should not mark changed: %v %v", changed, err)
	}
}
