package model

import (
	"encoding/json"
	"strings"
)

// WithThinkingMode returns an attempt-local request. Client reasoning controls
// must not survive a forced policy or leak into another channel's retry.
func WithThinkingMode(request *InternalLLMRequest, mode string) *InternalLLMRequest {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if request == nil || (mode != "off" && mode != "on") {
		return request
	}

	cloned := *request
	cloned.ThinkingMode = mode
	cloned.ReasoningBudget = nil
	cloned.AdaptiveThinking = false
	cloned.EnableThinking = nil
	cloned.ReasoningEffort = "high"
	if mode == "off" {
		cloned.ReasoningEffort = "none"
	}
	if request.EnableThinking != nil {
		enabled := mode == "on"
		cloned.EnableThinking = &enabled
	}

	if len(request.ExtraBody) > 0 {
		var extra map[string]json.RawMessage
		if json.Unmarshal(request.ExtraBody, &extra) == nil {
			for _, key := range []string{"thinking", "reasoning", "reasoning_effort", "enable_thinking", "thinking_budget"} {
				delete(extra, key)
			}
			cloned.ExtraBody, _ = json.Marshal(extra)
		} else {
			cloned.ExtraBody = nil
		}
	}
	return &cloned
}
