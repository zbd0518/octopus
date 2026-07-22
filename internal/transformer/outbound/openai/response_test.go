package openai

import (
	"testing"

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
