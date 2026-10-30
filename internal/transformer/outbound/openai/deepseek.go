package openai

import (
	"github.com/lingyuins/octopus/internal/transformer"
	"strings"

	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/utils/log"
)

func isDeepSeekCompatRequest(baseURL string, request *model.InternalLLMRequest) bool {
	return isProviderReasoningCompatRequest(baseURL, request, false, "deepseek")
}

func isMimoCompatRequest(baseURL string, request *model.InternalLLMRequest) bool {
	return isProviderReasoningCompatRequest(baseURL, request, true, "mimo")
}

func isReasoningCompatRequest(baseURL string, request *model.InternalLLMRequest, isMimoChannel bool) bool {
	return isProviderReasoningCompatRequest(baseURL, request, isMimoChannel, "deepseek") ||
		isProviderReasoningCompatRequest(baseURL, request, isMimoChannel, "mimo")
}

func isProviderReasoningCompatRequest(baseURL string, request *model.InternalLLMRequest, isMimoChannel bool, provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return false
	}

	if request != nil && strings.EqualFold(
		strings.TrimSpace(request.TransformerMetadata[model.TransformerMetadataGroupEndpointProvider]),
		provider,
	) {
		return true
	}

	if provider == "mimo" && isMimoChannel {
		return true
	}

	// DeepSeek/MiMo thinking controls are wire-format features of the *upstream*,
	// not of the client-facing model alias. Matching only on model name causes
	// Octopus to inject reasoning_effort / thinking into strict OpenAI-compat
	// proxies (e.g. FuturePPO) that expose deepseek-* aliases but reject those
	// fields with HTTP 400.
	lowerBaseURL := strings.ToLower(strings.TrimSpace(baseURL))
	if lowerBaseURL != "" && strings.Contains(lowerBaseURL, provider) {
		return true
	}
	if request == nil {
		return false
	}

	if strings.EqualFold(
		strings.TrimSpace(request.TransformerMetadata[model.TransformerMetadataGroupEndpointType]),
		provider,
	) {
		return true
	}

	// MiMo keeps model-name fallback (xiaomi / mimo) because many MiMo gateways
	// omit "mimo" from the host while still speaking the MiMo thinking schema.
	// DeepSeek does not: require base URL or explicit endpoint_type=deepseek.
	if provider == "mimo" {
		lowerModelName := strings.ToLower(strings.TrimSpace(request.Model))
		return strings.Contains(lowerModelName, "mimo") || strings.Contains(lowerModelName, "xiaomi")
	}

	return false
}

func looksLikeDeepSeekModelName(modelName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "deepseek")
}

func normalizeDeepSeekReasoningCompat(request *model.InternalLLMRequest, baseURL string, isMimoChannel bool) {
	if request == nil || !isReasoningCompatRequest(baseURL, request, isMimoChannel) {
		return
	}

	if request.ThinkingMode == "off" || request.ThinkingMode == "on" {
		thinkingType := "enabled"
		request.ReasoningEffort = "high"
		if request.ThinkingMode == "off" {
			thinkingType = "disabled"
			request.ReasoningEffort = ""
		}
		request.ExtraBody = mergeDeepSeekThinkingExtraBody(request.ExtraBody, thinkingType)
		return
	}

	thinkingType, hasThinkingType := extractDeepSeekThinkingType(request.ExtraBody)
	normalizedEffort := normalizeDeepSeekReasoningEffort(request.ReasoningEffort)
	originalEffort := strings.ToLower(strings.TrimSpace(request.ReasoningEffort))

	if hasThinkingType && thinkingType == "disabled" {
		request.ReasoningEffort = ""
	} else {
		request.ReasoningEffort = normalizedEffort
	}

	switch {
	case hasThinkingType && (thinkingType == "enabled" || thinkingType == "disabled"):
		request.ExtraBody = mergeDeepSeekThinkingExtraBody(request.ExtraBody, thinkingType)
	case originalEffort == "none":
		request.ExtraBody = mergeDeepSeekThinkingExtraBody(request.ExtraBody, "disabled")
	case request.ReasoningEffort != "":
		request.ExtraBody = mergeDeepSeekThinkingExtraBody(request.ExtraBody, "enabled")
	}
}

func normalizeDeepSeekReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "none":
		return ""
	case "low", "medium", "high":
		return "high"
	case "xhigh", "max":
		return "max"
	default:
		return strings.TrimSpace(effort)
	}
}

func extractDeepSeekThinkingType(raw transformer.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}

	var payload map[string]any
	if err := transformer.Unmarshal(raw, &payload); err != nil {
		return "", false
	}

	thinkingValue, ok := payload["thinking"]
	if !ok {
		return "", false
	}

	thinking, ok := thinkingValue.(map[string]any)
	if !ok {
		return "", false
	}

	typeValue, ok := thinking["type"]
	if !ok {
		return "", false
	}

	typeString, ok := typeValue.(string)
	if !ok {
		return "", false
	}

	normalized := strings.ToLower(strings.TrimSpace(typeString))
	return normalized, normalized != ""
}

func mergeDeepSeekThinkingExtraBody(raw transformer.RawMessage, thinkingType string) transformer.RawMessage {
	payload := map[string]any{}
	if len(raw) > 0 {
		if err := transformer.Unmarshal(raw, &payload); err != nil {
			log.Warnf("failed to unmarshal deepseek extra body: %v", err)
		}
	}

	payload["thinking"] = map[string]any{
		"type": thinkingType,
	}

	merged, err := transformer.Marshal(payload)
	if err != nil {
		return raw
	}
	return merged
}

// applyDeepSeekThinkingFallback downgrades DeepSeek thinking mode when the
// replayed history cannot satisfy its passback rule. DeepSeek requires the
// reasoning_content of every previous assistant turn when the request carries
// `tools`; a client that stored only the final answer (or dropped reasoning)
// would otherwise get a hard 400 on every follow-up turn. Disabling thinking
// keeps tool calling usable at the cost of that turn's chain of thought.
//
// It must run after message cleanup (`attachStandaloneDeepSeekReasoningMessages`
// / `sanitizeToolPairingForOpenAICompat`) so merged or dropped messages are not
// counted.
func applyDeepSeekThinkingFallback(request *model.InternalLLMRequest, baseURL string, isMimoChannel bool) {
	if request == nil || len(request.Tools) == 0 {
		return
	}
	if !isProviderReasoningCompatRequest(baseURL, request, isMimoChannel, "deepseek") {
		return
	}

	index, missing := deepSeekMissingReasoningTurn(request.Messages)
	if !missing || !deepSeekThinkingEnabled(request) {
		return
	}

	request.ThinkingMode = "off"
	request.ReasoningEffort = ""
	request.ExtraBody = mergeDeepSeekThinkingExtraBody(request.ExtraBody, "disabled")
	// reasoning_effort in extra_body is flattened to the top level on the wire and
	// would contradict thinking.type=disabled.
	request.ExtraBody = removeExtraBodyKey(request.ExtraBody, "reasoning_effort")
	log.Warnf("deepseek thinking disabled: assistant message #%d lacks reasoning_content while tools are present (model=%s)", index, request.Model)
}

func deepSeekMissingReasoningTurn(messages []model.Message) (int, bool) {
	for i := range messages {
		msg := &messages[i]
		if msg.Role != "assistant" || msg.GetReasoningContent() != "" {
			continue
		}
		if !assistantTurnNeedsReasoning(*msg) {
			continue
		}
		return i, true
	}
	return 0, false
}

// assistantTurnNeedsReasoning reports whether an assistant message represents a
// real model turn that DeepSeek expects to carry reasoning_content. Empty
// placeholder messages (e.g. a dropped assistant turn) are ignored.
func assistantTurnNeedsReasoning(msg model.Message) bool {
	if len(msg.ToolCalls) > 0 || msg.Refusal != "" {
		return true
	}
	if msg.Content.Content != nil && strings.TrimSpace(*msg.Content.Content) != "" {
		return true
	}
	return len(msg.Content.MultipleContent) > 0
}

func deepSeekThinkingEnabled(request *model.InternalLLMRequest) bool {
	if request.ThinkingMode == "off" {
		return false
	}
	if request.ThinkingMode == "on" {
		return true
	}
	effort := strings.ToLower(strings.TrimSpace(request.ReasoningEffort))
	if effort != "" && effort != "none" {
		return true
	}
	if thinkingType, ok := extractDeepSeekThinkingType(request.ExtraBody); ok {
		return thinkingType == "enabled"
	}
	// DeepSeek enables thinking by default, so an unspecified request still
	// carries the passback requirement.
	return true
}

func removeExtraBodyKey(raw transformer.RawMessage, key string) transformer.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	payload := map[string]any{}
	if err := transformer.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	if _, ok := payload[key]; !ok {
		return raw
	}
	delete(payload, key)
	merged, err := transformer.Marshal(payload)
	if err != nil {
		return raw
	}
	return merged
}
