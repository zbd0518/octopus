package openai

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
)

func deepSeekFallbackRequest(reasoning string, withTools bool) *model.InternalLLMRequest {
	empty := ""
	callID := "call_1"
	req := &model.InternalLLMRequest{
		Model:           "deepseek-v4.1-flash",
		ReasoningEffort: "high",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("weather?")}},
			{
				Role:    "assistant",
				Content: model.MessageContent{Content: &empty},
				ToolCalls: []model.ToolCall{{
					ID:       callID,
					Type:     "function",
					Function: model.FunctionCall{Name: "get_weather", Arguments: `{}`},
				}},
			},
			{Role: "tool", ToolCallID: &callID, Content: model.MessageContent{Content: strPtr("sunny")}},
			{Role: "user", Content: model.MessageContent{Content: strPtr("thanks")}},
		},
	}
	if reasoning != "" {
		req.Messages[1].ReasoningContent = &reasoning
	}
	if withTools {
		req.Tools = []model.Tool{{Type: "function", Function: model.Function{Name: "get_weather"}}}
	}
	return req
}

func deepSeekFallbackBody(t *testing.T, req *model.InternalLLMRequest) map[string]any {
	t.Helper()
	httpReq, err := (&ChatOutbound{}).TransformRequest(context.Background(), req, "https://api.deepseek.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var parsed map[string]any
	if err := transformer.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return parsed
}

func deepSeekThinkingType(parsed map[string]any) string {
	thinking, ok := parsed["thinking"].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := thinking["type"].(string)
	return value
}

// A client that replays a tool-call turn without its chain of thought used to
// force thinking off. The placeholder keeps the request valid instead, so the
// follow-up turn can still think.
func TestDeepSeekThinkingFallbackDisablesWhenHistoryLacksReasoning(t *testing.T) {
	parsed := deepSeekFallbackBody(t, deepSeekFallbackRequest("", true))

	if got := deepSeekThinkingType(parsed); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled (placeholder satisfies passback); body=%v", got, parsed)
	}
	if got, _ := parsed["reasoning_effort"].(string); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high", got)
	}
	if !bodyCarriesPlaceholderReasoning(parsed) {
		t.Fatalf("assistant tool-call turn must carry placeholder reasoning_content; body=%v", parsed)
	}
}

// bodyCarriesPlaceholderReasoning reports whether any replayed assistant turn
// carries the synthesized placeholder instead of a real chain of thought.
func bodyCarriesPlaceholderReasoning(parsed map[string]any) bool {
	messages, ok := parsed["messages"].([]any)
	if !ok {
		return false
	}
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		if reasoning, ok := msg["reasoning_content"].(string); ok && reasoning == reasoningPlaceholder {
			return true
		}
	}
	return false
}

func TestDeepSeekThinkingFallbackKeepsThinkingWhenHistoryHasReasoning(t *testing.T) {
	parsed := deepSeekFallbackBody(t, deepSeekFallbackRequest("prior chain of thought", true))

	if got := deepSeekThinkingType(parsed); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled; body=%v", got, parsed)
	}
	if got, _ := parsed["reasoning_effort"].(string); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high", got)
	}
}

func TestDeepSeekThinkingFallbackSkipsWithoutTools(t *testing.T) {
	parsed := deepSeekFallbackBody(t, deepSeekFallbackRequest("", false))

	if got := deepSeekThinkingType(parsed); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled without tools; body=%v", got, parsed)
	}
}

func TestDeepSeekThinkingFallbackRemovesExtraBodyReasoningEffort(t *testing.T) {
	req := deepSeekFallbackRequest("", true)
	req.ExtraBody = []byte(`{"reasoning_effort":"max","foo":"bar"}`)

	parsed := deepSeekFallbackBody(t, req)

	if got := deepSeekThinkingType(parsed); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled", got)
	}
	if parsed["foo"] != "bar" {
		t.Fatalf("unrelated extra_body keys must be preserved: %v", parsed)
	}
	if !bodyCarriesPlaceholderReasoning(parsed) {
		t.Fatalf("assistant tool-call turn must carry placeholder reasoning_content; body=%v", parsed)
	}
}

func TestDeepSeekThinkingFallbackKeepsExplicitlyDisabledThinking(t *testing.T) {
	req := deepSeekFallbackRequest("", true)
	req.ReasoningEffort = ""
	req.ThinkingMode = "off"

	parsed := deepSeekFallbackBody(t, req)

	if got := deepSeekThinkingType(parsed); got != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled", got)
	}
}

// The Responses adapter must stay consistent with the Chat adapter: the same
// placeholder keeps the follow-up turn's reasoning effort intact.
func TestResponseOutboundDeepSeekThinkingFallback(t *testing.T) {
	req := deepSeekFallbackRequest("", true)

	httpReq, err := (&ResponseOutbound{}).TransformRequest(context.Background(), req, "https://api.deepseek.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var parsed struct {
		Reasoning *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := transformer.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	// Responses cannot express reasoning_content, so thinking must stay disabled
	// there while the Chat adapter keeps it enabled with a placeholder.
	if parsed.Reasoning == nil || parsed.Reasoning.Effort != "none" {
		t.Fatalf("reasoning effort should be none, body=%s", body)
	}
	if strings.Contains(string(body), `"effort":"high"`) {
		t.Fatalf("stale effort leaked into Responses body: %s", body)
	}
}
