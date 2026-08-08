package openai

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
)

// Release builds use -tags=jsoniter. jsoniter ConfigCompatibleWithStandardLibrary
// does not honor the encoding/json omitzero option, so pointer fields tagged
// omitzero used to serialize as explicit nulls (e.g. "store":null). Strict
// OpenAI-compat relays reject that with "store 类型错误".
func TestChatOutboundOmitsNilStoreFamilyFields(t *testing.T) {
	stream := true
	req := &model.InternalLLMRequest{
		Model:    "deepseek-v4-flash-0731",
		Messages: []model.Message{{Role: "user", Content: model.MessageContent{Content: strPtr("hi")}}},
		Stream:   &stream,
		// Explicit zero-value pointers must stay omitted on the wire.
		Store:            nil,
		TopLogprobs:      nil,
		PromptCacheKey:   nil,
		SafetyIdentifier: nil,
	}

	httpReq, err := (&ChatOutbound{}).TransformRequest(context.Background(), req, "https://api.jzgo.top/v1/", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	text := string(body)
	for _, key := range []string{`"store"`, `"top_logprobs"`, `"prompt_cache_key"`, `"safety_identifier"`} {
		if strings.Contains(text, key) {
			t.Fatalf("outbound body must omit unset %s, got %s", key, text)
		}
	}

	// Non-nil values still serialize (true/false), only nil is omitted.
	storeFalse := false
	req.Store = &storeFalse
	body2, err := transformer.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(body2), `"store":false`) {
		t.Fatalf("expected explicit false store, got %s", body2)
	}
}
