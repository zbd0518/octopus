package relay

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

func TestOutboundAttemptTypesChatOnChatChannelAutoPrefersChat(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesChatOnResponseChannelAutoPrefersChat(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIResponse, req, "")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesResponsesOnChatChannelAutoPrefersChat(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIResponse}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesResponsesOnResponseChannelAutoPrefersChat(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIResponse}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIResponse, req, "")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesResponsesLiteAdditionalToolsForcesResponses(t *testing.T) {
	req := &model.InternalLLMRequest{
		RawAPIFormat: model.APIFormatOpenAIResponse,
		TransformerMetadata: map[string]string{
			model.TransformerMetadataResponsesLiteAdditionalTools: `[{"type":"namespace","name":"functions"}]`,
		},
	}

	for _, format := range []string{"", "chat", "chat_only", "messages"} {
		got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, format)
		want := []outbound.OutboundType{outbound.OutboundTypeOpenAIResponse}
		if len(got) != len(want) || got[0] != want[0] {
			t.Fatalf("format %q: attempt types = %#v, want %#v", format, got, want)
		}
	}
}

func TestOutboundAttemptTypesResponsesLiteCustomHistoryForcesResponses(t *testing.T) {
	req := &model.InternalLLMRequest{
		RawAPIFormat: model.APIFormatOpenAIResponse,
		Messages: []model.Message{{
			Role: "assistant",
			ToolCalls: []model.ToolCall{{
				Type:     "custom",
				Function: model.FunctionCall{Name: "exec"},
			}},
		}},
	}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	if len(got) != 1 || got[0] != outbound.OutboundTypeOpenAIResponse {
		t.Fatalf("attempt types = %#v, want Responses only", got)
	}
}

func TestResponsesLiteRelaySelectionPreservesAdditionalTools(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:        "gpt-5.6-luna",
		RawAPIFormat: model.APIFormatOpenAIResponse,
		TransformerMetadata: map[string]string{
			model.TransformerMetadataResponsesLiteAdditionalTools: `[{"type":"namespace","name":"functions"}]`,
		},
		Messages: []model.Message{{
			Role:    "user",
			Content: model.MessageContent{Content: stringPtr("list files")},
		}},
	}

	attemptTypes := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	if len(attemptTypes) != 1 || attemptTypes[0] != outbound.OutboundTypeOpenAIResponse {
		t.Fatalf("attempt types = %#v, want Responses only", attemptTypes)
	}

	upstreamRequest, err := outbound.Get(attemptTypes[0]).TransformRequest(
		context.Background(), req, "https://upstream.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("Responses TransformRequest() error = %v", err)
	}
	body, err := io.ReadAll(upstreamRequest.Body)
	if err != nil {
		t.Fatalf("read outbound body: %v", err)
	}
	if !strings.Contains(string(body), `"additional_tools"`) || !strings.Contains(string(body), `"namespace"`) {
		t.Fatalf("Responses outbound body lost native tools: %s", body)
	}
}

func stringPtr(value string) *string {
	return &value
}

func TestOutboundAttemptTypesEmbeddingNoFallback(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIEmbedding}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	if len(got) != 1 || got[0] != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("attempt types = %#v, want single channel type", got)
	}
}

func TestOutboundAttemptTypesNilRequest(t *testing.T) {
	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, nil, "")
	if len(got) != 1 || got[0] != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("attempt types = %#v, want single channel type", got)
	}
}

func TestOutboundAttemptTypesChatFormatPrefersChatFirst(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "chat")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesResponsesFormatPrefersResponseFirst(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "responses")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeOpenAIChat}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesChatOnlyDisablesFallback(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "chat_only")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesResponsesOnlyDisablesFallback(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "responses_only")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesMessagesFormatPrefersAnthropicFirst(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "messages")
	want := []outbound.OutboundType{outbound.OutboundTypeAnthropic, outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesMessagesOnlyDisablesFallback(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "messages_only")
	want := []outbound.OutboundType{outbound.OutboundTypeAnthropic}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesPassthroughDisablesFallback(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "passthrough")
	want := []outbound.OutboundType{outbound.OutboundTypePassthrough}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesAnthropicMessagesOnlyUsesAnthropic(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatAnthropicMessage}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "messages_only")
	want := []outbound.OutboundType{outbound.OutboundTypeAnthropic}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesAnthropicMessagesPrefersAnthropicFirst(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatAnthropicMessage}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "messages")
	want := []outbound.OutboundType{outbound.OutboundTypeAnthropic, outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesAnthropicPassthroughDisablesFallback(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatAnthropicMessage}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "passthrough")
	want := []outbound.OutboundType{outbound.OutboundTypePassthrough}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesRawPassthroughUsesRawAdapter(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "raw")
	want := []outbound.OutboundType{outbound.OutboundTypeRaw}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesAnthropicRawPassthroughUsesRawAdapter(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatAnthropicMessage}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "raw")
	want := []outbound.OutboundType{outbound.OutboundTypeRaw}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestOutboundAttemptTypesAnthropicAutoStillPrefersChatFallbackChain(t *testing.T) {
	// Anthropic inbound + auto format on an OpenAI-compatible channel should still
	// enter the LLM adapter-selection path instead of hard-coding channel type.
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatAnthropicMessage}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

// Unknown format values fall back to the default auto behavior so a stale or
// mistyped setting never disables routing entirely.
func TestOutboundAttemptTypesUnknownFormatFallsBackToAuto(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}

	got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "bogus")
	want := []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}

	if len(got) != len(want) {
		t.Fatalf("attempt types len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt types = %#v, want %#v", got, want)
		}
	}
}

func TestShouldTryAdapterFallbackSkipsSameChannelFailures(t *testing.T) {
	result := attemptResult{
		Success:  false,
		Written:  false,
		Decision: RetryDecision{Scope: ScopeSameChannel, Reason: "unauthorized", Code: 401, IsError: true},
	}

	if shouldTryAdapterFallback(result, 0, 2) {
		t.Fatal("expected key-scoped failure to skip adapter fallback")
	}
}

func TestShouldTryAdapterFallbackAllowsNextChannelFailures(t *testing.T) {
	result := attemptResult{
		Success:  false,
		Written:  false,
		Decision: RetryDecision{Scope: ScopeNextChannel, Reason: "gateway error", Code: 503, IsError: true},
	}

	if !shouldTryAdapterFallback(result, 0, 2) {
		t.Fatal("expected route-scoped failure to allow adapter fallback")
	}
}

func TestShouldTryAdapterFallbackSkipsClientErrorScopeNone(t *testing.T) {
	result := attemptResult{
		Success:  false,
		Written:  false,
		Decision: RetryDecision{Scope: ScopeNone, Reason: "bad request, client error", Code: 400, IsError: true},
	}

	if shouldTryAdapterFallback(result, 0, 2) {
		t.Fatal("expected client error ScopeNone to skip adapter fallback so upstream body can be returned immediately")
	}
}
