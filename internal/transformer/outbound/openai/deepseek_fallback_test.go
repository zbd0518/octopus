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

func TestDeepSeekThinkingFallbackDisablesWhenHistoryLacksReasoning(t *testing.T) {
	parsed := deepSeekFallbackBody(t, deepSeekFallbackRequest("", true))

	if got := deepSeekThinkingType(parsed); got != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled; body=%v", got, parsed)
	}
	if _, ok := parsed["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort must be removed when thinking is disabled: %v", parsed)
	}
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

	if got := deepSeekThinkingType(parsed); got != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled", got)
	}
	if _, ok := parsed["reasoning_effort"]; ok {
		t.Fatalf("nested extra_body reasoning_effort must be removed: %v", parsed)
	}
	if parsed["foo"] != "bar" {
		t.Fatalf("unrelated extra_body keys must be preserved: %v", parsed)
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

// The Responses adapter must degrade the same way: reasoning effort "none"
// disables thinking on DeepSeek's Responses endpoint.
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
	if parsed.Reasoning == nil || parsed.Reasoning.Effort != "none" {
		t.Fatalf("reasoning effort should be none, body=%s", body)
	}
	if strings.Contains(string(body), `"effort":"high"`) {
		t.Fatalf("stale effort leaked into Responses body: %s", body)
	}
}
