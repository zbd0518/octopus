package openai

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer/model"
)

func TestChatInboundGetInternalResponsePreservesSparseChoiceIndexes(t *testing.T) {
	inbound := &ChatInbound{}
	if _, err := inbound.TransformStream(context.Background(), makeChunkWithChoice(2, "second")); err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if _, err := inbound.TransformStream(context.Background(), makeChunkWithChoice(1, "first")); err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}

	resp, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse() error = %v", err)
	}

	assertChoiceOrderAndContent(t, resp, []int{1, 2}, []string{"first", "second"})
}

func TestResponseInboundGetInternalResponsePreservesSparseChoiceIndexes(t *testing.T) {
	inbound := &ResponseInbound{}
	if _, err := inbound.TransformStream(context.Background(), makeChunkWithChoice(3, "third")); err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if _, err := inbound.TransformStream(context.Background(), makeChunkWithChoice(1, "first")); err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}

	resp, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse() error = %v", err)
	}

	assertChoiceOrderAndContent(t, resp, []int{1, 3}, []string{"first", "third"})
}

func TestChatInboundOnlineAggregationDoesNotRetainChunkList(t *testing.T) {
	inbound := &ChatInbound{}
	// Many tiny chunks should not leave a growing chunk slice behind.
	for i := 0; i < 100; i++ {
		if _, err := inbound.TransformStream(context.Background(), makeChunkWithChoice(0, "x")); err != nil {
			t.Fatalf("TransformStream() error = %v", err)
		}
	}
	if inbound.streamChoices == nil || len(inbound.streamChoices) != 1 {
		t.Fatalf("streamChoices size = %v, want 1 choice", len(inbound.streamChoices))
	}
	resp, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse() error = %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("aggregated choices = %v", resp)
	}
	if got := *resp.Choices[0].Message.Content.Content; len(got) != 100 {
		t.Fatalf("aggregated content length = %d, want 100", len(got))
	}
	if inbound.streamResponse != nil || inbound.streamChoices != nil {
		t.Fatalf("aggregation state should be cleared after GetInternalResponse")
	}
	// Second call must return nil after state is cleared (same as old streamChunks path).
	resp2, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("second GetInternalResponse() error = %v", err)
	}
	if resp2 != nil {
		t.Fatalf("second GetInternalResponse() = %+v, want nil", resp2)
	}
}

func TestChatInboundOnlineAggregationMergesToolCallsUsageReasoning(t *testing.T) {
	inbound := &ChatInbound{}
	reasoning1 := "think-"
	reasoning2 := "ing"
	finish := "tool_calls"

	chunks := []*model.InternalLLMResponse{
		{
			ID:      "resp-id",
			Object:  "chat.completion.chunk",
			Created: 1,
			Model:   "gpt-test",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						Index: 0,
						ID:    "call_1",
						Type:  "function",
						Function: model.FunctionCall{
							Name:      "get_",
							Arguments: `{"c`,
						},
					}},
					ReasoningContent: &reasoning1,
				},
			}},
		},
		{
			ID:      "resp-id",
			Object:  "chat.completion.chunk",
			Created: 1,
			Model:   "gpt-test",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					ToolCalls: []model.ToolCall{{
						Index: 0,
						Function: model.FunctionCall{
							Name:      "weather",
							Arguments: `ity":"SF"}`,
						},
					}},
					ReasoningContent: &reasoning2,
				},
			}},
		},
		{
			ID:      "resp-id",
			Object:  "chat.completion.chunk",
			Created: 1,
			Model:   "gpt-test",
			Usage: &model.Usage{
				PromptTokens:     11,
				CompletionTokens: 22,
			},
			Choices: []model.Choice{{
				Index:        0,
				FinishReason: &finish,
				Delta:        &model.Message{},
			}},
		},
	}
	for _, chunk := range chunks {
		if _, err := inbound.TransformStream(context.Background(), chunk); err != nil {
			t.Fatalf("TransformStream() error = %v", err)
		}
	}

	resp, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse() error = %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Message == nil {
		t.Fatalf("aggregated response incomplete: %+v", resp)
	}
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls len = %d, want 1", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "get_weather" || tc.Function.Arguments != `{"city":"SF"}` {
		t.Fatalf("merged tool call = %+v", tc)
	}
	if msg.GetReasoningContent() != "think-ing" {
		t.Fatalf("reasoning = %q, want think-ing", msg.GetReasoningContent())
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != finish {
		t.Fatalf("finish_reason = %v, want %q", resp.Choices[0].FinishReason, finish)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 22 {
		t.Fatalf("usage = %+v, want prompt=11 completion=22", resp.Usage)
	}
}

func TestResponseInboundOnlineAggregationClearsState(t *testing.T) {
	inbound := &ResponseInbound{}
	for i := 0; i < 20; i++ {
		if _, err := inbound.TransformStream(context.Background(), makeChunkWithChoice(0, "y")); err != nil {
			t.Fatalf("TransformStream() error = %v", err)
		}
	}
	resp, err := inbound.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatalf("GetInternalResponse() error = %v", err)
	}
	if resp == nil || len(resp.Choices) != 1 {
		t.Fatalf("aggregated choices = %v", resp)
	}
	if got := *resp.Choices[0].Message.Content.Content; len(got) != 20 {
		t.Fatalf("aggregated content length = %d, want 20", len(got))
	}
	if inbound.streamResponse != nil || inbound.streamChoices != nil {
		t.Fatalf("aggregation state should be cleared after GetInternalResponse")
	}
}

func makeChunkWithChoice(index int, content string) *model.InternalLLMResponse {
	return &model.InternalLLMResponse{
		ID:      "resp-id",
		Object:  "chat.completion.chunk",
		Created: 1,
		Model:   "gpt-test",
		Choices: []model.Choice{
			{
				Index: index,
				Delta: &model.Message{
					Role: "assistant",
					Content: model.MessageContent{
						Content: &content,
					},
				},
			},
		},
	}
}

func assertChoiceOrderAndContent(t *testing.T, resp *model.InternalLLMResponse, wantIndexes []int, wantContents []string) {
	t.Helper()

	if resp == nil {
		t.Fatal("GetInternalResponse() response = nil")
	}
	if len(resp.Choices) != len(wantIndexes) {
		t.Fatalf("GetInternalResponse() choices len = %d, want %d", len(resp.Choices), len(wantIndexes))
	}

	for i, wantIndex := range wantIndexes {
		if resp.Choices[i].Index != wantIndex {
			t.Fatalf("GetInternalResponse() choices[%d].Index = %d, want %d", i, resp.Choices[i].Index, wantIndex)
		}
		if resp.Choices[i].Message == nil || resp.Choices[i].Message.Content.Content == nil {
			t.Fatalf("GetInternalResponse() choices[%d].Message.Content = nil", i)
		}
		if got := *resp.Choices[i].Message.Content.Content; got != wantContents[i] {
			t.Fatalf("GetInternalResponse() choices[%d].Message.Content = %q, want %q", i, got, wantContents[i])
		}
	}
}
