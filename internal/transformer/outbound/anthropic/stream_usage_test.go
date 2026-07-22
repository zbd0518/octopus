package anthropic

import (
	"context"
	"testing"
)

func TestTransformStream_MessageDeltaAttachesUsageToChunk(t *testing.T) {
	o := &MessageOutbound{}

	// message_start with input tokens only
	start, err := o.TransformStream(context.Background(), []byte(`{
		"type":"message_start",
		"message":{
			"id":"msg_1",
			"model":"claude-test",
			"usage":{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":20}
		}
	}`))
	if err != nil {
		t.Fatalf("message_start: %v", err)
	}
	if start == nil || start.Usage == nil || start.Usage.PromptTokens != 100 {
		t.Fatalf("message_start usage = %#v", start)
	}

	// message_delta carries output tokens — must appear on this chunk for inbound aggregation
	delta, err := o.TransformStream(context.Background(), []byte(`{
		"type":"message_delta",
		"delta":{"stop_reason":"end_turn"},
		"usage":{"output_tokens":42}
	}`))
	if err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	if delta == nil || delta.Usage == nil {
		t.Fatal("message_delta must attach Usage to chunk")
	}
	if delta.Usage.PromptTokens != 100 {
		t.Fatalf("PromptTokens=%d, want 100 (merged from message_start)", delta.Usage.PromptTokens)
	}
	if delta.Usage.CompletionTokens != 42 {
		t.Fatalf("CompletionTokens=%d, want 42", delta.Usage.CompletionTokens)
	}
	if delta.Usage.PromptTokensDetails == nil || delta.Usage.PromptTokensDetails.CachedTokens != 20 {
		t.Fatalf("cached tokens not preserved: %#v", delta.Usage.PromptTokensDetails)
	}
}

func TestTransformStream_MessageStopStillCarriesUsage(t *testing.T) {
	o := &MessageOutbound{}
	_, _ = o.TransformStream(context.Background(), []byte(`{
		"type":"message_start",
		"message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":10}}
	}`))
	_, _ = o.TransformStream(context.Background(), []byte(`{
		"type":"message_delta",
		"usage":{"output_tokens":7}
	}`))
	stop, err := o.TransformStream(context.Background(), []byte(`{"type":"message_stop"}`))
	if err != nil {
		t.Fatalf("message_stop: %v", err)
	}
	if stop == nil || stop.Usage == nil {
		t.Fatal("message_stop should still carry usage")
	}
	if stop.Usage.PromptTokens != 10 || stop.Usage.CompletionTokens != 7 {
		t.Fatalf("stop usage = prompt=%d completion=%d", stop.Usage.PromptTokens, stop.Usage.CompletionTokens)
	}
}
