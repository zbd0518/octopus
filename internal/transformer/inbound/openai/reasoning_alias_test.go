package openai

import (
	"context"
	"strings"
	"testing"

	outboundopenai "github.com/lingyuins/octopus/internal/transformer/outbound/openai"
)

// Reproduces the "response" failure mode: an OpenAI-compatible upstream returns
// the chain of thought as `reasoning`, the Responses client must receive it as a
// reasoning summary item, otherwise the next turn cannot pass reasoning_content
// back to DeepSeek.
func TestResponseInboundStreamForwardsReasoningAlias(t *testing.T) {
	in := &ResponseInbound{}
	if _, err := in.TransformRequest(context.Background(),
		[]byte(`{"model":"deepseek-v4.1-flash","input":"hi","stream":true}`)); err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	out := &outboundopenai.ChatOutbound{}
	chunk := []byte(`{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":"prior chain of thought"}}]}`)
	stream, err := out.TransformStream(context.Background(), chunk)
	if err != nil {
		t.Fatalf("ChatOutbound.TransformStream() error = %v", err)
	}

	sse, err := in.TransformStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if !strings.Contains(string(sse), "reasoning_summary_text.delta") {
		t.Fatalf("reasoning alias was not forwarded to the Responses client: %s", sse)
	}
	if !strings.Contains(string(sse), "prior chain of thought") {
		t.Fatalf("reasoning delta payload missing: %s", sse)
	}
}
