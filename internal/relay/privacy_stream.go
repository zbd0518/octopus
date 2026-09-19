package relay

import (
	"strings"
	"unicode/utf8"

	tmodel "github.com/lingyuins/octopus/internal/transformer/model"
)

// privacyStreamRestorer 流式响应的占位符还原（issue 020）。
//
// 上游可能把占位符（⟪PII-N⟫）按 token 边界拆进多个 SSE 事件的 delta 文本里。
// 逐 chunk 独立替换会漏掉跨 chunk 的占位符。本还原器作用在内部 chunk 的
// Delta/Message 文本上（outAdapter 转换后、inAdapter 序列化前），持有尾部
// 「可能是占位符前缀」的 carry，与下一个 chunk 的文本拼接后再扫描。
//
// 已知边界：流结束时若 carry 仍有残留（上游发出被截断的占位符），残留字节
// 随尝试结束丢弃——只丢失几个占位符骨架字符，无隐私泄漏。仅当请求发生过
// 脱敏（privacyMap 非空）时启用，零脱敏请求零开销。
type privacyStreamRestorer struct {
	pm    *privacyPlaceholderMap
	carry []byte // 尾部扣留的部分占位符前缀，与下一 chunk 文本拼接
}

func newPrivacyStreamRestorer(pm *privacyPlaceholderMap) *privacyStreamRestorer {
	return &privacyStreamRestorer{pm: pm}
}

// placeholderLead 占位符首字符 ⟪（UTF-8 3 字节）。扫描以它为锚点。
const placeholderLead = "⟪"

// maxPlaceholderDigits 占位符序号最大位数（防御上限）。
const maxPlaceholderDigits = 10

// restoreChunk 对内部 chunk 做占位符还原（就地改写）。content 走 carry 语义
// （支持跨 chunk 拆分）；reasoning 与 tool arguments 只还原 chunk 内完整的
// 占位符（片段化 JSON 中跨 chunk 还原收益低，见方法注释）。
func (r *privacyStreamRestorer) restoreChunk(stream *tmodel.InternalLLMResponse) {
	if r == nil || r.pm == nil || stream == nil {
		return
	}
	for i := range stream.Choices {
		choice := &stream.Choices[i]
		var msg *tmodel.Message
		if choice.Message != nil {
			msg = choice.Message
		} else if choice.Delta != nil {
			msg = choice.Delta
		}
		if msg == nil {
			continue
		}
		if msg.Content.Content != nil && *msg.Content.Content != "" {
			*msg.Content.Content = r.restoreText(*msg.Content.Content)
		}
		for j := range msg.Content.MultipleContent {
			part := &msg.Content.MultipleContent[j]
			if part.Type == "text" && part.Text != nil && *part.Text != "" {
				*part.Text = r.restoreText(*part.Text)
			}
		}
		// reasoning 与 tool arguments：只还原 chunk 内完整占位符，不做 carry
		if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
			*msg.ReasoningContent = r.restoreCompleteOnly(*msg.ReasoningContent)
		}
		if msg.Reasoning != nil && *msg.Reasoning != "" {
			*msg.Reasoning = r.restoreCompleteOnly(*msg.Reasoning)
		}
		for j := range msg.ToolCalls {
			tc := &msg.ToolCalls[j]
			if tc.Function.Arguments != "" {
				tc.Function.Arguments = r.restoreCompleteOnly(tc.Function.Arguments)
			}
		}
	}
}

// restoreText 带 carry 的文本还原：还原完整占位符，尾部部分前缀扣留到下一 chunk。
func (r *privacyStreamRestorer) restoreText(text string) string {
	if len(r.carry) > 0 {
		text = string(r.carry) + text
		r.carry = r.carry[:0]
	}
	var out strings.Builder
	i := 0
	for i < len(text) {
		idx := strings.Index(text[i:], placeholderLead)
		if idx < 0 {
			// 没有完整的 ⟪。但尾部可能是 ⟪ 本身的前缀字节（它也可能被上游拆断），
			// 此时扣留这几个字节到下一 chunk。
			rest := text[i:]
			if hold := leadPrefixHoldLen(rest); hold > 0 {
				out.WriteString(rest[:len(rest)-hold])
				r.carry = append(r.carry[:0], rest[len(rest)-hold:]...)
				return out.String()
			}
			out.WriteString(rest)
			i = len(text)
			break
		}
		abs := i + idx
		out.WriteString(text[i:abs])
		i = abs

		if end := matchPlaceholder(text[i:]); end > 0 {
			out.WriteString(r.pm.restore(text[i : i+end]))
			i += end
			continue
		}
		// 不完整：可能是被拆断的占位符前缀 → 扣留到下一 chunk；否则放行锚点继续扫
		if isPatternPrefix(text[i:]) {
			r.carry = append(r.carry[:0], text[i:]...)
			return out.String()
		}
		out.WriteString(placeholderLead)
		i += len(placeholderLead)
	}
	return out.String()
}

// leadPrefixHoldLen 返回 s 末尾需要扣留的字节数：s 以 ⟪ 的真前缀（如 E2 或 E2 9F）结尾时，
// 这几字节可能被后续 chunk 补全成 ⟪。E2 是 UTF-8 前导字节，不会作为合法字符的末字节出现，
// 因此这种后缀判定不会误扣正常文本。
func leadPrefixHoldLen(s string) int {
	for k := len(placeholderLead) - 1; k >= 1; k-- {
		if len(s) >= k && strings.HasSuffix(s, placeholderLead[:k]) {
			return k
		}
	}
	return 0
}

// restoreCompleteOnly 无 carry 的还原：只替换 chunk 内完整占位符。
func (r *privacyStreamRestorer) restoreCompleteOnly(text string) string {
	if !strings.Contains(text, placeholderLead) {
		return text
	}
	var out strings.Builder
	i := 0
	for i < len(text) {
		idx := strings.Index(text[i:], placeholderLead)
		if idx < 0 {
			out.WriteString(text[i:])
			break
		}
		abs := i + idx
		out.WriteString(text[i:abs])
		i = abs
		if end := matchPlaceholder(text[i:]); end > 0 {
			out.WriteString(r.pm.restore(text[i : i+end]))
			i += end
			continue
		}
		out.WriteString(placeholderLead)
		i += len(placeholderLead)
	}
	return out.String()
}

// HasPending 流结束时 carry 是否仍有残留（被截断的占位符骨架，随尝试丢弃）。
func (r *privacyStreamRestorer) HasPending() bool {
	return r != nil && len(r.carry) > 0
}

// isPatternPrefix 判断 s 是否可能是占位符 ⟪PII-<digits>⟫ 的真前缀（可被后续数据补全）。
// 尾部若有被上游拆断的多字节字符（如 ⟫ 的前一两个字节），先剥离再判定：
// 那些字节不构成分歧，仍可能被补全。
func isPatternPrefix(s string) bool {
	s, _ = trimTrailingPartialRune(s)
	prefix := privacyPlaceholderPrefix // "⟪PII-"
	if len(s) < len(prefix) {
		return strings.HasPrefix(prefix, s)
	}
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	// 完整前缀之后只能跟数字（等待 ⟫ 或更多数字，未超长）
	rest := s[len(prefix):]
	if len(rest) >= maxPlaceholderDigits {
		return false
	}
	for k := 0; k < len(rest); k++ {
		if rest[k] < '0' || rest[k] > '9' {
			return false
		}
	}
	return true
}

// trimTrailingPartialRune 剥离 s 末尾不完整的多字节 UTF-8 序列，返回剩余部分与被剥离字节数。
// 剥离的部分可能被下一个 chunk 补全成完整字符（如 ⟫）。
func trimTrailingPartialRune(s string) (string, int) {
	trimmed := 0
	for len(s)-trimmed > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:len(s)-trimmed])
		if r != utf8.RuneError || size > 1 {
			break
		}
		// RuneError 且 size==1：末尾是不完整/非法字节，剥掉再看
		trimmed++
	}
	return s[:len(s)-trimmed], trimmed
}

// matchPlaceholder 在 text 开头匹配完整占位符 ⟪PII-<digits>⟫，返回长度，不匹配返回 -1。
func matchPlaceholder(text string) int {
	if !strings.HasPrefix(text, privacyPlaceholderPrefix) {
		return -1
	}
	i := len(privacyPlaceholderPrefix)
	start := i
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		i++
		if i-start > maxPlaceholderDigits {
			return -1
		}
	}
	if i == start {
		return -1 // 无数字
	}
	if !strings.HasPrefix(text[i:], "⟫") {
		return -1
	}
	return i + len("⟫")
}
