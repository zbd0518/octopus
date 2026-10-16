package openai

import (
	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

// ensureToolCallIDs 为历史中缺失 id 的工具调用合成稳定 id，并按位置重挂紧随其后的
// 工具结果。
//
// 背景：部分上游（Vercel AI Gateway、部分 DeepSeek/vLLM 部署）会返回不带 id 的
// tool_calls。客户端把这类回合原样回灌后，出站序列化的 `id,omitempty` 会直接省略
// 该字段，严格上游会整单拒绝：
//
//	400 messages.41.tool_calls.1.id: expected string, received undefined
//	400 Responses function_call 缺少 call_id 或 name
//
// 由于 id 在调用与结果两侧都为空，无法按值关联，只能按出现顺序（即 OpenAI 语义中
// 结果块的排列顺序）配对后赋予同一个合成 id。已有 id 的调用与结果不受影响。
//
// 调用方传入的 messages 会被就地修改，但 ToolCalls 采用写时复制，不会污染与重试
// 缓存/日志共享的底层数组。
func ensureToolCallIDs(messages []model.Message) {
	for i := 0; i < len(messages); i++ {
		msg := &messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		// 紧随其后的连续 tool 消息构成该回合的结果块。
		blockEnd := i + 1
		for blockEnd < len(messages) && messages[blockEnd].Role == "tool" {
			blockEnd++
		}

		// 空 tool_call_id 的结果按出现顺序排队，与空 id 的调用一一配对。
		pendingResults := make([]int, 0, blockEnd-(i+1))
		for k := i + 1; k < blockEnd; k++ {
			if messages[k].ToolCallID == nil || *messages[k].ToolCallID == "" {
				pendingResults = append(pendingResults, k)
			}
		}

		missing := false
		for _, tc := range msg.ToolCalls {
			if tc.ID == "" {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}

		// 写时复制：避免改写与内部请求共享的底层数组。
		msg.ToolCalls = append([]model.ToolCall(nil), msg.ToolCalls...)

		for j := range msg.ToolCalls {
			tc := &msg.ToolCalls[j]
			if tc.ID != "" {
				continue
			}
			tc.ID = syntheticToolCallID()
			if len(pendingResults) > 0 {
				k := pendingResults[0]
				pendingResults = pendingResults[1:]
				id := tc.ID
				messages[k].ToolCallID = &id
			}
		}
	}
}

// syntheticToolCallID 生成形如 call_xxxxxxxxxxxxxxxx 的合成 id，满足常见上游对
// `call_` 前缀与字符串类型的校验。
func syntheticToolCallID() string {
	return "call_" + lo.RandomString(16, lo.AlphanumericCharset)
}
