package relay

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

func TestGroupThinkingModeOutboundBody(t *testing.T) {
	tests := []struct {
		name     string
		adapter  outbound.OutboundType
		provider string
		baseURL  string
		path     []string
		off, on  any
	}{
		{"deepseek provider", outbound.OutboundTypeOpenAIChat, "deepseek", "https://proxy.example/v1", []string{"thinking", "type"}, "disabled", "enabled"},
		{"deepseek URL", outbound.OutboundTypeOpenAIChat, "", "https://api.deepseek.com/v1", []string{"thinking", "type"}, "disabled", "enabled"},
		{"mimo", outbound.OutboundTypeMimo, "", "https://proxy.example/v1", []string{"thinking", "type"}, "disabled", "enabled"},
		{"openai chat", outbound.OutboundTypeOpenAIChat, "", "https://api.openai.com/v1", []string{"reasoning_effort"}, "none", "high"},
		{"openai responses", outbound.OutboundTypeOpenAIResponse, "", "https://api.openai.com/v1", []string{"reasoning", "effort"}, "none", "high"},
		{"anthropic", outbound.OutboundTypeAnthropic, "", "https://api.anthropic.com", []string{"thinking", "type"}, "disabled", "adaptive"},
		{"gemini", outbound.OutboundTypeGemini, "", "https://generativelanguage.googleapis.com", []string{"generationConfig", "thinkingConfig", "thinkingBudget"}, float64(0), float64(-1)},
		{"volcengine", outbound.OutboundTypeVolcengine, "", "https://ark.example/api/v3", []string{"thinking", "type"}, "disabled", "enabled"},
	}
	for _, tt := range tests {
		for _, mode := range []string{"off", "on"} {
			for _, clientControls := range []bool{false, true} {
				name := tt.name + "/" + mode
				if clientControls {
					name += "/conflicting client controls"
				}
				t.Run(name, func(t *testing.T) {
					text := "complete the code"
					base := &transmodel.InternalLLMRequest{
						Model:    "test-model",
						Messages: []transmodel.Message{{Role: "user", Content: transmodel.MessageContent{Content: &text}}},
					}
					if clientControls {
						budget := int64(4096)
						enabled := mode == "off"
						base.ReasoningBudget = &budget
						base.AdaptiveThinking = true
						base.EnableThinking = &enabled
						base.ReasoningEffort = "high"
						base.ExtraBody = []byte(`{"thinking":{"type":"enabled"},"reasoning_effort":"high"}`)
						if mode == "on" {
							base.ReasoningEffort = "none"
							base.ExtraBody = []byte(`{"thinking":{"type":"disabled"},"reasoning_effort":"none"}`)
						}
					}
					before, _ := json.Marshal(base)
					channel := &appmodel.Channel{Type: tt.adapter}
					group := &appmodel.Group{EndpointType: "chat", EndpointProvider: tt.provider, ThinkingMode: mode}
					prepared, _, err := prepareInternalRequestForOutbound(channel, base, group)
					if err != nil {
						t.Fatal(err)
					}
					req, err := outbound.Get(tt.adapter).TransformRequest(context.Background(), prepared, tt.baseURL, "test-key")
					if err != nil {
						t.Fatal(err)
					}
					body, _ := io.ReadAll(req.Body)
					var payload map[string]any
					if err := json.Unmarshal(body, &payload); err != nil {
						t.Fatal(err)
					}
					var value any = payload
					for _, key := range tt.path {
						obj, ok := value.(map[string]any)
						if !ok {
							t.Fatalf("missing %v in %s", tt.path, body)
						}
						value = obj[key]
					}
					want := tt.off
					if mode == "on" {
						want = tt.on
					}
					if !reflect.DeepEqual(value, want) {
						t.Fatalf("%v = %v, want %v; body=%s", tt.path, value, want, body)
					}
					if _, ok := payload["extra_body"]; ok && tt.adapter == outbound.OutboundTypeMimo {
						t.Fatalf("MiMo thinking must be top-level: %s", body)
					}
					after, _ := json.Marshal(base)
					if string(before) != string(after) || base.ThinkingMode != "" {
						t.Fatal("original request mutated across attempts")
					}
				})
			}
		}
	}
}

func TestGroupThinkingModeAutoPreservesClientRequest(t *testing.T) {
	for _, mode := range []string{"", "auto"} {
		for _, effort := range []string{"", "none", "high"} {
			request := &transmodel.InternalLLMRequest{ReasoningEffort: effort, ExtraBody: []byte(`{"thinking":{"type":"disabled"}}`)}
			got, _, err := prepareInternalRequestForOutbound(&appmodel.Channel{Type: outbound.OutboundTypeOpenAIChat}, request, &appmodel.Group{ThinkingMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			if got.ReasoningEffort != effort || string(got.ExtraBody) != string(request.ExtraBody) || got.ThinkingMode != "" {
				t.Fatalf("auto policy changed client controls: %+v", got)
			}
		}
	}
}
