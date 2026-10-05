package model

import (
	"encoding/json"
	"testing"
)

func TestWithThinkingModeClearsConflictsWithoutMutatingClient(t *testing.T) {
	extra := []byte(`{"thinking":{"type":"enabled"},"reasoning":{"effort":"max"},"reasoning_effort":"max","enable_thinking":true,"thinking_budget":4096,"provider_option":"keep"}`)
	budget := int64(4096)
	enabled := true
	request := &InternalLLMRequest{ExtraBody: extra, ReasoningEffort: "max", ReasoningBudget: &budget, AdaptiveThinking: true, EnableThinking: &enabled}
	got := WithThinkingMode(request, "off")
	var payload map[string]any
	if err := json.Unmarshal(got.ExtraBody, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload["provider_option"] != "keep" {
		t.Fatalf("extra body = %s", got.ExtraBody)
	}
	if got.ReasoningBudget != nil || got.AdaptiveThinking || got.ReasoningEffort != "none" || got.EnableThinking == nil || *got.EnableThinking {
		t.Fatalf("conflicting controls survived: %+v", got)
	}
	if request.ReasoningBudget != &budget || !request.AdaptiveThinking || !*request.EnableThinking || string(request.ExtraBody) != string(extra) {
		t.Fatal("client request was mutated")
	}
	if WithThinkingMode(request, "auto") != request || WithThinkingMode(request, "") != request {
		t.Fatal("auto policy should leave request unchanged")
	}
}
