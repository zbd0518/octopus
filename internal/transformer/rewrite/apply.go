package rewrite

import (
	"fmt"
	"strings"

	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
)

func Apply(req *transmodel.InternalLLMRequest, cfg *EffectiveConfig) (*transmodel.InternalLLMRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if cfg == nil {
		return req, nil
	}

	cloned := cloneRequest(req)
	if !cloned.IsChatRequest() {
		return cloned, nil
	}

	rewritten, err := rewriteMessages(cloned.Messages, cfg)
	if err != nil {
		return nil, err
	}
	cloned.Messages = rewritten
	return cloned, nil
}

func cloneRequest(req *transmodel.InternalLLMRequest) *transmodel.InternalLLMRequest {
	cloned := *req

	if len(req.Messages) > 0 {
		cloned.Messages = make([]transmodel.Message, len(req.Messages))
		for i, msg := range req.Messages {
			cloned.Messages[i] = cloneMessage(msg)
		}
	}

	if req.StreamOptions != nil {
		streamOptions := *req.StreamOptions
		cloned.StreamOptions = &streamOptions
	}

	return &cloned
}

func cloneMessage(msg transmodel.Message) transmodel.Message {
	cloned := msg

	if len(msg.ToolCalls) > 0 {
		cloned.ToolCalls = append([]transmodel.ToolCall(nil), msg.ToolCalls...)
	}
	if len(msg.Content.MultipleContent) > 0 {
		cloned.Content.MultipleContent = append([]transmodel.MessageContentPart(nil), msg.Content.MultipleContent...)
	}

	return cloned
}

func rewriteMessages(messages []transmodel.Message, cfg *EffectiveConfig) ([]transmodel.Message, error) {
	rewritten := append([]transmodel.Message(nil), messages...)
	if cfg.SystemMessageStrategy == "merge" {
		rewritten = mergeSystemMessages(rewritten)
	}

	for i := range rewritten {
		msg := rewritten[i]

		if cfg.FlattenTextBlockArrays {
			if text, ok := flattenTextOnlyContent(msg.Content.MultipleContent); ok {
				msg.Content = transmodel.MessageContent{
					Content: &text,
				}
			}
		}

		if len(cfg.PromptRules) > 0 && isSystemLikeRole(msg.Role) {
			var err error
			msg.Content, err = rewritePromptContent(msg.Content, cfg.PromptRules)
			if err != nil {
				return nil, err
			}
		}

		if cfg.ToolRoleStrategy == "stringify_to_user" && msg.Role == "tool" {
			msg = stringifyToolMessage(msg)
		}

		if cfg.EnsureAssistantContentWithToolCalls && msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			ensureNonNilContent(&msg)
		}

		if cfg.NilContentAsEmptyString {
			ensureNonNilContent(&msg)
		}

		rewritten[i] = msg
	}

	return rewritten, nil
}

func rewritePromptContent(content transmodel.MessageContent, rules []EffectivePromptRewriteRule) (transmodel.MessageContent, error) {
	rewritten := content
	if content.Content != nil {
		text, err := applyPromptRules(*content.Content, rules)
		if err != nil {
			return content, err
		}
		rewritten.Content = &text
	}
	if len(content.MultipleContent) == 0 {
		return rewritten, nil
	}

	rewritten.MultipleContent = append([]transmodel.MessageContentPart(nil), content.MultipleContent...)
	for index, part := range rewritten.MultipleContent {
		if part.Type != "text" || part.Text == nil {
			continue
		}
		text, err := applyPromptRules(*part.Text, rules)
		if err != nil {
			return content, err
		}
		rewritten.MultipleContent[index].Text = &text
	}
	return rewritten, nil
}

func applyPromptRules(text string, rules []EffectivePromptRewriteRule) (string, error) {
	rewritten := text
	for _, rule := range rules {
		var err error
		rewritten, err = rule.Pattern.Replace(rewritten, rule.Replacement, -1, -1)
		if err != nil {
			return "", fmt.Errorf("apply prompt rewrite rule: %w", err)
		}
	}
	return rewritten, nil
}

func mergeSystemMessages(messages []transmodel.Message) []transmodel.Message {
	systemCount := 0
	firstIndex := -1
	systemParts := make([]string, 0)
	merged := make([]transmodel.Message, 0, len(messages))

	for _, msg := range messages {
		if isSystemLikeRole(msg.Role) {
			if firstIndex == -1 {
				firstIndex = len(merged)
			}
			systemCount++
			if text := extractTextContent(msg.Content); text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}

		merged = append(merged, msg)
	}

	if systemCount <= 1 || firstIndex == -1 {
		return messages
	}

	joined := strings.Join(systemParts, "\n\n")
	mergedSystem := transmodel.Message{
		Role: "system",
		Content: transmodel.MessageContent{
			Content: &joined,
		},
	}
	if joined == "" {
		ensureNonNilContent(&mergedSystem)
	}

	if firstIndex >= len(merged) {
		return append(merged, mergedSystem)
	}

	result := make([]transmodel.Message, 0, len(merged)+1)
	result = append(result, merged[:firstIndex]...)
	result = append(result, mergedSystem)
	result = append(result, merged[firstIndex:]...)
	return result
}

func flattenTextOnlyContent(parts []transmodel.MessageContentPart) (string, bool) {
	if len(parts) == 0 {
		return "", false
	}

	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type != "text" || part.Text == nil {
			return "", false
		}
		textParts = append(textParts, *part.Text)
	}

	return strings.Join(textParts, "\n"), true
}

func stringifyToolMessage(msg transmodel.Message) transmodel.Message {
	text := extractTextContent(msg.Content)
	if msg.ToolCallID != nil && *msg.ToolCallID != "" {
		label := "Tool result"
		if msg.ToolCallIsError != nil && *msg.ToolCallIsError {
			label = "Tool error"
		}
		if text == "" {
			text = fmt.Sprintf("%s (%s)", label, *msg.ToolCallID)
		} else {
			text = fmt.Sprintf("%s (%s):\n%s", label, *msg.ToolCallID, text)
		}
	}

	msg.Role = "user"
	msg.Content = transmodel.MessageContent{Content: &text}
	msg.MessageIndex = nil
	msg.ToolCallID = nil
	msg.ToolCallName = nil
	msg.ToolCallIsError = nil
	msg.ToolCalls = nil

	ensureNonNilContent(&msg)
	return msg
}

func extractTextContent(content transmodel.MessageContent) string {
	if content.Content != nil {
		return *content.Content
	}

	textParts := make([]string, 0, len(content.MultipleContent))
	for _, part := range content.MultipleContent {
		if part.Type == "text" && part.Text != nil {
			textParts = append(textParts, *part.Text)
		}
	}

	return strings.Join(textParts, "\n")
}

func ensureNonNilContent(msg *transmodel.Message) {
	if msg.Content.Content != nil || len(msg.Content.MultipleContent) > 0 {
		return
	}

	empty := ""
	msg.Content.Content = &empty
}

func isSystemLikeRole(role string) bool {
	return role == "system" || role == "developer"
}
