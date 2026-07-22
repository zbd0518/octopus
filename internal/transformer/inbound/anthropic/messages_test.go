package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

func TestMessagesInboundGetInternalResponsePreservesSparseChoiceIndexes(t *testing.T) {
	first := "first"
	second := "second"
	inbound := &MessagesInbound{
		streamChunks: []*model.InternalLLMResponse{
			{
				ID:      "resp-id",
				Object:  "chat.completion.chunk",
				Created: 1,
				Model:   "claude-test",
				Choices: []model.Choice{
					{
						Index: 2,
						Delta: &model.Message{
							Role: "assistant",
							Content: model.MessageContent{
								Content: &second,
							},
						},
					},
				},
			},
			{
				ID:      "resp-id",
				Object:  "chat.completion.chunk",
				Created: 1,
				Model:   "claude-test",
				Choices: []model.Choice{
					{
						Index: 1,
						Delta: &model.Message{
							Role: "assistant",
							Content: model.MessageContent{
								Content: &first,
							},
						},
					},
				},
			},
		},
	}

	resp, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GetInternalResponse() response = nil")
	}
	if len(resp.Choices) != 2 {
		t.Fatalf("GetInternalResponse() choices len = %d, want 2", len(resp.Choices))
	}
	if resp.Choices[0].Index != 1 || resp.Choices[1].Index != 2 {
		t.Fatalf("GetInternalResponse() indexes = [%d %d], want [1 2]", resp.Choices[0].Index, resp.Choices[1].Index)
	}
	if resp.Choices[0].Message == nil || resp.Choices[0].Message.Content.Content == nil || *resp.Choices[0].Message.Content.Content != first {
		t.Fatalf("GetInternalResponse() first content = %+v, want %q", resp.Choices[0].Message, first)
	}
	if resp.Choices[1].Message == nil || resp.Choices[1].Message.Content.Content == nil || *resp.Choices[1].Message.Content.Content != second {
		t.Fatalf("GetInternalResponse() second content = %+v, want %q", resp.Choices[1].Message, second)
	}
}

// TestMessagesInboundTransformStream_ToolIndexStartsAtOne simulates OpenAI
// Responses API function_call output_index starting at 1 (common when a
// reasoning item occupies index 0). The Anthropic client must see
// content_block_start before any content_block_stop/delta for that block.
func TestMessagesInboundTransformStream_ToolIndexStartsAtOne(t *testing.T) {
	inbound := &MessagesInbound{}
	ctx := context.Background()

	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_1",
			Object: "chat.completion.chunk",
			Model:  "grok-4.5",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{Role: "assistant"},
			}},
		},
		{
			ID:     "resp_1",
			Object: "chat.completion.chunk",
			Model:  "grok-4.5",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role:             "assistant",
					ReasoningContent: lo.ToPtr("planning tool use"),
				},
			}},
		},
		{
			ID:     "resp_1",
			Object: "chat.completion.chunk",
			Model:  "grok-4.5",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						Index: 1, // Responses output_index starts at 1 after reasoning
						ID:    "call_abc",
						Type:  "function",
						Function: model.FunctionCall{
							Name: "Bash",
						},
					}},
				},
			}},
		},
		{
			ID:     "resp_1",
			Object: "chat.completion.chunk",
			Model:  "grok-4.5",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						Index: 1,
						ID:    "call_abc",
						Type:  "function",
						Function: model.FunctionCall{
							Arguments: `{"command":"ls"}`,
						},
					}},
				},
			}},
		},
		{
			ID:     "resp_1",
			Object: "chat.completion.chunk",
			Model:  "grok-4.5",
			Choices: []model.Choice{{
				Index:        0,
				FinishReason: lo.ToPtr("tool_calls"),
			}},
			Usage: &model.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		},
	}

	var combined strings.Builder
	for _, chunk := range chunks {
		out, err := inbound.TransformStream(ctx, chunk)
		if err != nil {
			t.Fatalf("TransformStream() error = %v", err)
		}
		if len(out) > 0 {
			combined.Write(out)
			combined.WriteByte('\n')
		}
	}

	sse := combined.String()
	startIdx := strings.Index(sse, `event:content_block_start`)
	if startIdx < 0 {
		t.Fatalf("expected content_block_start in SSE, got:\n%s", sse)
	}
	starts := strings.Count(sse, "event:content_block_start")
	stops := strings.Count(sse, "event:content_block_stop")
	if stops > starts {
		t.Fatalf("content_block_stop (%d) exceeds content_block_start (%d); SSE:\n%s", stops, starts, sse)
	}
	if !strings.Contains(sse, "tool_use") {
		t.Fatalf("expected tool_use block in SSE, got:\n%s", sse)
	}
	if !strings.Contains(sse, "input_json_delta") {
		t.Fatalf("expected input_json_delta in SSE, got:\n%s", sse)
	}
}

func TestMessagesInboundTransformStream_SkipsOrphanSignatureDelta(t *testing.T) {
	inbound := &MessagesInbound{}
	ctx := context.Background()

	out, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{
		ID:     "resp_2",
		Object: "chat.completion.chunk",
		Model:  "grok-4.5",
		Choices: []model.Choice{{
			Index: 0,
			Delta: &model.Message{
				Role:               "assistant",
				ReasoningSignature: lo.ToPtr("sig-only"),
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if strings.Contains(string(out), "signature_delta") {
		t.Fatalf("expected orphan signature_delta to be skipped, got:\n%s", out)
	}
	if strings.Contains(string(out), "content_block_delta") {
		t.Fatalf("expected no content_block_delta without open block, got:\n%s", out)
	}
}

func TestMessagesInboundTransformStream_MultipleToolsWithoutOrphanStop(t *testing.T) {
	inbound := &MessagesInbound{}
	ctx := context.Background()

	chunks := []*model.InternalLLMResponse{
		{
			ID: "r", Object: "chat.completion.chunk", Model: "m",
			Choices: []model.Choice{{Index: 0, Delta: &model.Message{Role: "assistant"}}},
		},
		{
			ID: "r", Object: "chat.completion.chunk", Model: "m",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						Index: 2,
						ID:    "call_1",
						Type:  "function",
						Function: model.FunctionCall{
							Name:      "Read",
							Arguments: `{"path":"a"}`,
						},
					}},
				},
			}},
		},
		{
			ID: "r", Object: "chat.completion.chunk", Model: "m",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						Index: 5,
						ID:    "call_2",
						Type:  "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"command":"pwd"}`,
						},
					}},
				},
			}},
		},
		{
			ID: "r", Object: "chat.completion.chunk", Model: "m",
			Choices: []model.Choice{{Index: 0, FinishReason: lo.ToPtr("tool_calls")}},
			Usage:   &model.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		},
	}

	var combined strings.Builder
	for _, chunk := range chunks {
		out, err := inbound.TransformStream(ctx, chunk)
		if err != nil {
			t.Fatalf("TransformStream() error = %v", err)
		}
		combined.Write(out)
	}
	sse := combined.String()
	starts := strings.Count(sse, "event:content_block_start")
	stops := strings.Count(sse, "event:content_block_stop")
	if starts != 2 {
		t.Fatalf("expected 2 content_block_start, got %d; SSE:\n%s", starts, sse)
	}
	if stops != 2 {
		t.Fatalf("expected 2 content_block_stop, got %d; SSE:\n%s", stops, sse)
	}
	if !strings.Contains(sse, "call_1") || !strings.Contains(sse, "call_2") {
		t.Fatalf("expected both tool call ids in SSE:\n%s", sse)
	}
}

