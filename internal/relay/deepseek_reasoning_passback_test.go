package relay

import (
	"context"
	"io"
	"strings"
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

// DeepSeek requires `reasoning_content` on every previous assistant turn when the
// request carries `tools`; otherwise the API returns 400
// "The `reasoning_content` in the thinking mode must be passed back to the API".
// Clients that store Octopus' own upstream response verbatim replay it as the
// OpenRouter-style `reasoning` field, so the deepseek provider rewrite must fold
// the alias into `reasoning_content` instead of deleting it.
func TestRewriteConversationRequestByProvider_FoldsReasoningAliasForDeepSeek(t *testing.T) {
	reasoning := "prior chain of thought"
	req := &transmodel.InternalLLMRequest{
		Messages: []transmodel.Message{
			{Role: "assistant", Reasoning: &reasoning},
		},
	}

	got := rewriteConversationRequestByProvider(
		appmodel.Group{EndpointType: appmodel.EndpointTypeChat, EndpointProvider: "deepseek"}, req)

	if got.Messages[0].Reasoning != nil {
		t.Fatal("deepseek rewrite should clear the reasoning alias field")
	}
	if got.Messages[0].ReasoningContent == nil || *got.Messages[0].ReasoningContent != reasoning {
		t.Fatalf("reasoning alias was dropped instead of folded: %#v", got.Messages[0].ReasoningContent)
	}
	if req.Messages[0].Reasoning == nil || req.Messages[0].ReasoningContent != nil {
		t.Fatal("provider rewrite must not mutate the original request")
	}
}

func TestRewriteConversationRequestByProvider_KeepsReasoningContentOverAlias(t *testing.T) {
	alias := "alias"
	content := "canonical"
	req := &transmodel.InternalLLMRequest{
		Messages: []transmodel.Message{
			{Role: "assistant", Reasoning: &alias, ReasoningContent: &content},
		},
	}

	got := rewriteConversationRequestByProvider(
		appmodel.Group{EndpointType: appmodel.EndpointTypeChat, EndpointProvider: "deepseek"}, req)

	if got.Messages[0].Reasoning != nil {
		t.Fatal("deepseek rewrite should clear the reasoning alias field")
	}
	if got.Messages[0].ReasoningContent == nil || *got.Messages[0].ReasoningContent != content {
		t.Fatalf("canonical reasoning_content should win, got %#v", got.Messages[0].ReasoningContent)
	}
}

// End-to-end: an assistant turn replayed with only the OpenRouter-style
// `reasoning` alias must still land on the wire as `reasoning_content`, because
// DeepSeek rejects requests that keep tools but lose the chain of thought.
func TestDeepSeekOutboundCarriesReasoningContentFromAlias(t *testing.T) {
	reasoning := "the prior turn chain of thought"
	callID := "call_weather_1"
	answer := "answer"
	weatherPrompt := "weather?"
	followUp := "and tomorrow?"
	weather := "sunny"

	req := &transmodel.InternalLLMRequest{
		Model: "deepseek-v4.1-flash",
		Messages: []transmodel.Message{
			{Role: "user", Content: transmodel.MessageContent{Content: &weatherPrompt}},
			{
				Role:      "assistant",
				Content:   transmodel.MessageContent{Content: &answer},
				Reasoning: &reasoning,
				ToolCalls: []transmodel.ToolCall{{ID: callID, Type: "function", Function: transmodel.FunctionCall{Name: "get_weather", Arguments: `{}`}}},
			},
			{Role: "tool", ToolCallID: &callID, Content: transmodel.MessageContent{Content: &weather}},
			{Role: "user", Content: transmodel.MessageContent{Content: &followUp}},
		},
		Tools:               []transmodel.Tool{{Type: "function", Function: transmodel.Function{Name: "get_weather"}}},
		TransformerMetadata: map[string]string{},
	}

	group := &appmodel.Group{EndpointType: appmodel.EndpointTypeChat, EndpointProvider: "deepseek"}
	rewritten := rewriteConversationRequestByProvider(*group, req)
	prepared, _, err := prepareInternalRequestForOutbound(
		&appmodel.Channel{Type: outbound.OutboundTypeOpenAIChat}, rewritten, group)
	if err != nil {
		t.Fatalf("prepareInternalRequestForOutbound() error = %v", err)
	}

	httpReq, err := outbound.Get(outbound.OutboundTypeOpenAIChat).TransformRequest(
		context.Background(), prepared, "https://openrouter.ai/api/v1", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if !strings.Contains(string(body), `"reasoning_content":"`+reasoning+`"`) {
		t.Fatalf("DeepSeek outbound body lost reasoning_content: %s", body)
	}
	if strings.Contains(string(body), `"reasoning":`) {
		t.Fatalf("DeepSeek outbound body must not carry the reasoning alias: %s", body)
	}
}
