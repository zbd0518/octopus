package openai

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
)

// decodedOutboundMessage is a minimal projection of the outbound chat message used
// by the tests below to assert tool pairing behavior.
type decodedOutboundMessage struct {
	Role        string `json:"role"`
	ToolCallID  string `json:"tool_call_id"`
	ToolCalls   []struct {
		ID string `json:"id"`
	} `json:"tool_calls"`
	Content json.RawMessage `json:"content"`
}

func decodeOutboundMessages(t *testing.T, request *model.InternalLLMRequest, baseURL string) []decodedOutboundMessage {
	t.Helper()
	outbound := &ChatOutbound{}
	httpReq, err := outbound.TransformRequest(context.Background(), request, baseURL, "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	var got struct {
		Messages []decodedOutboundMessage `json:"messages"`
	}
	if err := transformer.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to unmarshal outbound body: %v\nbody=%s", err, string(body))
	}
	return got.Messages
}

func toolCall(id string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionCall{
			Name:      "lookup_weather",
			Arguments: `{"city":"Shanghai"}`,
		},
	}
}

func toolResult(id string) model.Message {
	content := "sunny"
	return model.Message{
		Role:       "tool",
		ToolCallID: &id,
		Content:    model.MessageContent{Content: &content},
	}
}

func TestSanitizeToolPairingForOpenAICompat_WellFormedHistoryUntouched(t *testing.T) {
	text := "let me check"
	request := &model.InternalLLMRequest{
		Model: "deepseek-v4-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &text}},
			{
				Role:      "assistant",
				ToolCalls: []model.ToolCall{toolCall("call_a"), toolCall("call_b")},
			},
			toolResult("call_a"),
			toolResult("call_b"),
			{Role: "user", Content: model.MessageContent{Content: &text}},
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %#v", len(msgs), msgs)
	}
	if len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls preserved, got %d", len(msgs[1].ToolCalls))
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_a" {
		t.Fatalf("expected tool call_a result preserved, got %#v", msgs[2])
	}
	if msgs[3].Role != "tool" || msgs[3].ToolCallID != "call_b" {
		t.Fatalf("expected tool call_b result preserved, got %#v", msgs[3])
	}
}

func TestSanitizeToolPairingForOpenAICompat_DropsMidConversationUnresolvedToolCalls(t *testing.T) {
	content := "I will call two tools"
	user := "please"
	next := "go on"
	request := &model.InternalLLMRequest{
		Model: "deepseek-v4-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &user}},
			{
				Role:      "assistant",
				Content:   model.MessageContent{Content: &content},
				ToolCalls: []model.ToolCall{toolCall("call_a"), toolCall("call_b")},
			},
			// The next message is a user turn, not the tool results for the two
			// calls above -> upstream would reject with
			// "assistant message appears before tool results".
			{Role: "user", Content: model.MessageContent{Content: &next}},
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %#v", len(msgs), msgs)
	}
	if len(msgs[1].ToolCalls) != 0 {
		t.Fatalf("expected unresolved mid-conversation tool calls to be dropped, got %d", len(msgs[1].ToolCalls))
	}
	if msgs[1].Role != "assistant" {
		t.Fatalf("expected assistant text message to remain, got role %q", msgs[1].Role)
	}
}

func TestSanitizeToolPairingForOpenAICompat_TrailingContinuationKept(t *testing.T) {
	// DeepSeek tool continuation: a trailing assistant message with tool_calls has
	// its results supplied on the next request, so it must be preserved verbatim
	// (and keep its reasoning_content for the continuation echo).
	reasoning := "need one more tool round"
	content := ""
	request := &model.InternalLLMRequest{
		Model: "deepseek-v4-pro",
		Messages: []model.Message{
			{
				Role:             "assistant",
				Content:          model.MessageContent{Content: &content},
				ReasoningContent: &reasoning,
				ToolCalls:        []model.ToolCall{toolCall("call_1")},
			},
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %#v", len(msgs), msgs)
	}
	if len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].ID != "call_1" {
		t.Fatalf("expected trailing continuation tool_calls preserved, got %#v", msgs[0].ToolCalls)
	}
}

func TestSanitizeToolPairingForOpenAICompat_DropsOnlyUnfulfilledCalls(t *testing.T) {
	user := "please"
	request := &model.InternalLLMRequest{
		Model: "deepseek-v4-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &user}},
			{
				Role:      "assistant",
				ToolCalls: []model.ToolCall{toolCall("call_a"), toolCall("call_b")},
			},
			toolResult("call_a"), // call_b result is missing
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d: %#v", len(msgs), msgs)
	}
	if len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "call_a" {
		t.Fatalf("expected only fulfilled call_a preserved, got %#v", msgs[1].ToolCalls)
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_a" {
		t.Fatalf("expected call_a result preserved, got %#v", msgs[2])
	}
}

func TestSanitizeToolPairingForOpenAICompat_DropsOrphanToolResult(t *testing.T) {
	text := "hi"
	request := &model.InternalLLMRequest{
		Model: "deepseek-v4-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &text}},
			toolResult("call_orphan"), // no preceding assistant tool_calls
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 1 {
		t.Fatalf("expected orphan tool result dropped, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" {
		t.Fatalf("expected only user message to remain, got %#v", msgs)
	}
}

func TestSanitizeToolPairingForOpenAICompat_DropsEmptyAssistantWithOnlyUnfulfilledCalls(t *testing.T) {
	text := "hi"
	next := "go on"
	request := &model.InternalLLMRequest{
		Model: "deepseek-v4-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &text}},
			{ // empty assistant whose only content was the unresolved tool calls
				Role:      "assistant",
				ToolCalls: []model.ToolCall{toolCall("call_a")},
			},
			{Role: "user", Content: model.MessageContent{Content: &next}},
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 2 {
		t.Fatalf("expected empty assistant message dropped, got %d: %#v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[1].Role != "user" {
		t.Fatalf("expected only the two user messages to remain, got %#v", msgs)
	}
}

func TestSanitizeToolPairingForOpenAICompat_NonStreamingTrailingContinuationKeptOnGeneric(t *testing.T) {
	// The tool pairing sanitize applies to every OpenAI-compat chat outbound, not
	// only reasoning targets. A non-streaming trailing assistant with tool_calls is
	// a valid continuation (arguments arrive next request) regardless of provider,
	// so it is preserved verbatim.
	content := "I will call tools"
	user := "please"
	request := &model.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &user}},
			{
				Role:      "assistant",
				Content:   model.MessageContent{Content: &content},
				ToolCalls: []model.ToolCall{toolCall("call_a"), toolCall("call_b")},
			},
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.openai.com/v1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(msgs), msgs)
	}
	if len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("expected non-streaming trailing tool calls kept verbatim, got %d", len(msgs[1].ToolCalls))
	}
}

func TestSanitizeToolPairingForOpenAICompat_StreamingTrailingUnresolvedDropped(t *testing.T) {
	// Streaming requests reject a trailing assistant message that carries
	// tool_calls with no results (the exact "messages[4]: assistant message appears
	// before tool results" case). The unresolved calls must be stripped, keeping the
	// assistant's text content.
	stream := true
	content := "I will call tools"
	user := "please"
	request := &model.InternalLLMRequest{
		Model:  "oc/deepseek-v4-flash-0731",
		Stream: &stream,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &user}},
			{
				Role:      "assistant",
				Content:   model.MessageContent{Content: &content},
				ToolCalls: []model.ToolCall{toolCall("call_7100"), toolCall("call_a3af"), toolCall("call_7ba8")},
			},
		},
	}

	msgs := decodeOutboundMessages(t, request, "https://api.deepseek.com/v1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %#v", len(msgs), msgs)
	}
	if len(msgs[1].ToolCalls) != 0 {
		t.Fatalf("expected streaming trailing unresolved tool calls dropped, got %d", len(msgs[1].ToolCalls))
	}
	if msgs[1].Role != "assistant" {
		t.Fatalf("expected assistant text message to remain, got role %q", msgs[1].Role)
	}
}