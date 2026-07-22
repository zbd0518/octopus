package openai

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
)

func TestConvertToResponsesRequest_OmitsNoneReasoningEffort(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:           "mimo-v2.5-pro",
		ReasoningEffort: "none",
	}

	got := ConvertToResponsesRequest(req)
	if got.Reasoning != nil {
		t.Fatalf("expected reasoning to be omitted, got %#v", got.Reasoning)
	}
}

func TestConvertToResponsesRequest_PreservesValidReasoningEffort(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:           "o3",
		ReasoningEffort: "high",
	}

	got := ConvertToResponsesRequest(req)
	if got.Reasoning == nil {
		t.Fatalf("expected reasoning to be present")
	}
	if got.Reasoning.Effort != "high" {
		t.Fatalf("expected reasoning effort high, got %q", got.Reasoning.Effort)
	}
}

func TestConvertToResponsesRequest_PreservesMaxAndXHighReasoningEffort(t *testing.T) {
	for _, effort := range []string{"max", "xhigh", "minimal"} {
		req := &model.InternalLLMRequest{
			Model:           "gpt-5.5",
			ReasoningEffort: effort,
		}
		got := ConvertToResponsesRequest(req)
		if got.Reasoning == nil {
			t.Fatalf("effort %q: expected reasoning to be present", effort)
		}
		if got.Reasoning.Effort != effort {
			t.Fatalf("effort %q: got %q", effort, got.Reasoning.Effort)
		}
	}
}

func TestNormalizeOpenAICompatReasoningEffort_PreservesExtendedLevels(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"none":    "",
		"NONE":    "",
		"minimal": "minimal",
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
		"xhigh":   "xhigh",
		"max":     "max",
		"MAX":     "max",
		"bogus":   "",
	}
	for in, want := range cases {
		if got := normalizeOpenAICompatReasoningEffort(in); got != want {
			t.Fatalf("normalizeOpenAICompatReasoningEffort(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestConvertToResponsesRequest_PreservesMetadata(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "grok-4.5",
		Metadata: map[string]string{
			"user_id": `{"device_id":"abc","session_id":"def"}`,
		},
	}
	got := ConvertToResponsesRequest(req)
	if got.Metadata == nil || got.Metadata["user_id"] == "" {
		t.Fatalf("expected ConvertToResponsesRequest to preserve metadata for callers that need it")
	}
}

func TestResponseOutbound_TransformRequest_StripsMetadata(t *testing.T) {
	var o ResponseOutbound
	req := &model.InternalLLMRequest{
		Model: "grok-4.5",
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					Content: strPtr("hello"),
				},
			},
		},
		Metadata: map[string]string{
			"user_id": `{"device_id":"439c66ca","account_uuid":"","session_id":"1f38f2da"}`,
		},
		ReasoningEffort: "high",
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://example.com", "test-key")
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	defer httpReq.Body.Close()

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), `"metadata"`) {
		t.Fatalf("expected metadata to be stripped from outbound body, got: %s", body)
	}

	var payload map[string]any
	if err := transformer.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := payload["metadata"]; ok {
		t.Fatalf("expected no metadata key in payload, got: %#v", payload["metadata"])
	}
	if payload["model"] != "grok-4.5" {
		t.Fatalf("expected model preserved, got: %#v", payload["model"])
	}
}
