package openai

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer/model"
)

// OpenRouter-style OpenAI-compatible gateways return the chain of thought in the
// `reasoning` field. Normalizing it at the upstream parse boundary is what lets
// chat clients see `reasoning_content` (the field they replay next turn) and the
// Responses adapter emit reasoning items. Otherwise DeepSeek rejects the next
// request with 400 "reasoning_content ... must be passed back".
func TestChatOutboundStreamNormalizesReasoningAlias(t *testing.T) {
	out := &ChatOutbound{}
	chunk := []byte(`{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":"thinking"}}]}`)

	stream, err := out.TransformStream(context.Background(), chunk)
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if len(stream.Choices) != 1 || stream.Choices[0].Delta == nil {
		t.Fatalf("unexpected stream shape: %#v", stream)
	}
	if got := stream.Choices[0].Delta.GetReasoningContent(); got != "thinking" {
		t.Fatalf("reasoning_content = %q, want thinking", got)
	}
}

func TestChatOutboundResponseNormalizesReasoningAlias(t *testing.T) {
	resp := &model.InternalLLMResponse{
		Choices: []model.Choice{{
			Index:   0,
			Message: &model.Message{Role: "assistant"},
		}},
	}
	resp.Choices[0].Message.Reasoning = strPtr("thinking")

	normalizeOpenAICompatReasoningAlias(resp)

	if got := resp.Choices[0].Message.GetReasoningContent(); got != "thinking" {
		t.Fatalf("reasoning_content = %q, want thinking", got)
	}
}
