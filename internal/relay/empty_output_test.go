package relay

import (
	"testing"

	dbmodel "github.com/lingyuins/octopus/internal/model"
	tmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

// --- isEmptyOutputResponse ---

func TestIsEmptyOutputResponse_NilResponse(t *testing.T) {
	if isEmptyOutputResponse(nil) {
		t.Fatal("nil response should not be empty")
	}
}

func TestIsEmptyOutputResponse_NoChoices(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{Choices: []tmodel.Choice{}}
	if isEmptyOutputResponse(resp) {
		t.Fatal("response with no choices should not be detected as empty output (embedding fallback)")
	}
}

// issue #155 core: model consumed reasoning tokens (CompletionTokens > 0) but no visible content.
func TestIsEmptyOutputResponse_ReasoningTokensOnly(t *testing.T) {
	empty := ""
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role:             "assistant",
				Content:          tmodel.MessageContent{Content: &empty},
				ReasoningContent: lo.ToPtr("I thought about this deeply but produced no answer."),
			},
		}},
		Usage: &tmodel.Usage{
			CompletionTokens: 19700,
			CompletionTokensDetails: &tmodel.CompletionTokensDetails{
				ReasoningTokens: 19700,
			},
		},
	}
	if !isEmptyOutputResponse(resp) {
		t.Fatal("reasoning tokens with no visible content should be detected as empty output (issue #155)")
	}
}

func TestIsEmptyOutputResponse_ReasoningFieldOnly(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role:      "assistant",
				Reasoning: lo.ToPtr("thinking..."),
			},
		}},
		Usage: &tmodel.Usage{CompletionTokens: 100},
	}
	if !isEmptyOutputResponse(resp) {
		t.Fatal("reasoning field with no visible content should be detected as empty output")
	}
}

func TestIsEmptyOutputResponse_WithTextContent(t *testing.T) {
	content := "Hello!"
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role:             "assistant",
				Content:          tmodel.MessageContent{Content: &content},
				ReasoningContent: lo.ToPtr("I thought about what to say."),
			},
		}},
		Usage: &tmodel.Usage{CompletionTokens: 50},
	}
	if isEmptyOutputResponse(resp) {
		t.Fatal("response with visible text content should not be detected as empty")
	}
}

func TestIsEmptyOutputResponse_WithWhitespaceContent(t *testing.T) {
	ws := "   \n  "
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role:    "assistant",
				Content: tmodel.MessageContent{Content: &ws},
			},
		}},
		Usage: &tmodel.Usage{CompletionTokens: 10},
	}
	if !isEmptyOutputResponse(resp) {
		t.Fatal("whitespace-only content should be detected as empty output")
	}
}

func TestIsEmptyOutputResponse_WithToolCalls(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role:      "assistant",
				ToolCalls: []tmodel.ToolCall{{ID: "call_1", Type: "function"}},
			},
		}},
	}
	if isEmptyOutputResponse(resp) {
		t.Fatal("response with tool calls should not be detected as empty")
	}
}

func TestIsEmptyOutputResponse_WithMultipleContent(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role: "assistant",
				Content: tmodel.MessageContent{
					MultipleContent: []tmodel.MessageContentPart{{Type: "text", Text: lo.ToPtr("hi")}},
				},
			},
		}},
	}
	if isEmptyOutputResponse(resp) {
		t.Fatal("response with multimodal content should not be detected as empty")
	}
}

func TestIsEmptyOutputResponse_WithAudio(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Message: &tmodel.Message{
				Role: "assistant",
				Audio: &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{Data: "base64audio"},
			},
		}},
	}
	if isEmptyOutputResponse(resp) {
		t.Fatal("response with audio should not be detected as empty")
	}
}

func TestIsEmptyOutputResponse_NilMessage(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{Index: 0, Message: nil}},
		Usage:   &tmodel.Usage{CompletionTokens: 5},
	}
	if !isEmptyOutputResponse(resp) {
		t.Fatal("choice with nil message should be detected as empty output")
	}
}

// --- streamChunkHasVisibleContent ---

func TestStreamChunkHasVisibleContent_NilResponse(t *testing.T) {
	if streamChunkHasVisibleContent(nil) {
		t.Fatal("nil response should not have visible content")
	}
}

func TestStreamChunkHasVisibleContent_ReasoningOnly(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Delta: &tmodel.Message{
				Role:             "assistant",
				ReasoningContent: lo.ToPtr("thinking..."),
			},
		}},
	}
	if streamChunkHasVisibleContent(resp) {
		t.Fatal("reasoning-only chunk should not have visible content (issue #155)")
	}
}

func TestStreamChunkHasVisibleContent_TextDelta(t *testing.T) {
	content := "Hello"
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Delta: &tmodel.Message{
				Role:    "assistant",
				Content: tmodel.MessageContent{Content: &content},
			},
		}},
	}
	if !streamChunkHasVisibleContent(resp) {
		t.Fatal("text delta chunk should have visible content")
	}
}

func TestStreamChunkHasVisibleContent_ToolCallDelta(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Delta: &tmodel.Message{
				Role:      "assistant",
				ToolCalls: []tmodel.ToolCall{{ID: "call_1", Type: "function"}},
			},
		}},
	}
	if !streamChunkHasVisibleContent(resp) {
		t.Fatal("tool call delta chunk should have visible content")
	}
}

func TestStreamChunkHasVisibleContent_EmptyDelta(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{
		Choices: []tmodel.Choice{{
			Index: 0,
			Delta: &tmodel.Message{Role: "assistant"},
		}},
	}
	if streamChunkHasVisibleContent(resp) {
		t.Fatal("empty delta chunk should not have visible content")
	}
}

func TestStreamChunkHasVisibleContent_NoChoices(t *testing.T) {
	resp := &tmodel.InternalLLMResponse{Choices: []tmodel.Choice{}}
	if streamChunkHasVisibleContent(resp) {
		t.Fatal("response with no choices should not have visible content")
	}
}

// --- prefersImmediateReasoningStream / getReasoningBufferStrategy ---

func TestPrefersImmediateReasoningStream(t *testing.T) {
	budgetHigh := int64(8000)
	budgetLow := int64(1024)

	tests := []struct {
		name string
		req  *tmodel.InternalLLMRequest
		want bool
	}{
		{name: "nil request", req: nil, want: false},
		{name: "empty effort", req: &tmodel.InternalLLMRequest{}, want: false},
		{name: "medium effort", req: &tmodel.InternalLLMRequest{ReasoningEffort: "medium"}, want: false},
		{name: "high effort", req: &tmodel.InternalLLMRequest{ReasoningEffort: "high"}, want: true},
		{name: "HIGH effort case", req: &tmodel.InternalLLMRequest{ReasoningEffort: "HIGH"}, want: true},
		{name: "xhigh effort", req: &tmodel.InternalLLMRequest{ReasoningEffort: "xhigh"}, want: true},
		{name: "max effort", req: &tmodel.InternalLLMRequest{ReasoningEffort: "max"}, want: true},
		{name: "adaptive thinking", req: &tmodel.InternalLLMRequest{AdaptiveThinking: true}, want: true},
		{name: "large reasoning budget", req: &tmodel.InternalLLMRequest{ReasoningBudget: &budgetHigh}, want: true},
		{name: "small reasoning budget", req: &tmodel.InternalLLMRequest{ReasoningBudget: &budgetLow}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prefersImmediateReasoningStream(tt.req); got != tt.want {
				t.Fatalf("prefersImmediateReasoningStream() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetReasoningBufferStrategy_GroupOverride(t *testing.T) {
	// 分组显式 buffer 时，即使 high 思考也不自动改成 immediate。
	group := &dbmodel.Group{ReasoningBufferStrategy: "buffer"}
	req := &tmodel.InternalLLMRequest{ReasoningEffort: "high"}
	if got := getReasoningBufferStrategy(group, req); got != "buffer" {
		t.Fatalf("group explicit buffer should win, got %q", got)
	}

	group.ReasoningBufferStrategy = "immediate"
	if got := getReasoningBufferStrategy(group, req); got != "immediate" {
		t.Fatalf("group explicit immediate should win, got %q", got)
	}
}

func TestGetReasoningBufferStrategy_LongThinkingDefaultImmediate(t *testing.T) {
	// 分组未配置时，长思考默认 immediate，避免 client disconnected。
	req := &tmodel.InternalLLMRequest{ReasoningEffort: "high"}
	if got := getReasoningBufferStrategy(nil, req); got != "immediate" {
		t.Fatalf("long-thinking without group override should be immediate, got %q", got)
	}
	if got := getReasoningBufferStrategy(&dbmodel.Group{}, req); got != "immediate" {
		t.Fatalf("empty group strategy should still prefer immediate for high effort, got %q", got)
	}
}
