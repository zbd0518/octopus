package openai

import "strings"

// normalizeOpenAICompatReasoningEffort 规范化发往 OpenAI 兼容上游的 reasoning_effort。
// 官方当前支持 none / minimal / low / medium / high / xhigh / max（以模型为准）。
// none 表示关闭思考；其余合法档位原样透传，避免把 max/xhigh 错误压成 high。
func normalizeOpenAICompatReasoningEffort(effort string) string {
	normalized := strings.ToLower(strings.TrimSpace(effort))

	switch normalized {
	case "", "none":
		return ""
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return normalized
	default:
		// 未知值静默丢弃，避免部分上游因非法枚举直接 400。
		return ""
	}
}
