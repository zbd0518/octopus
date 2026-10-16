package openai

import (
	"context"
	"fmt"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

type ChatInbound struct {
        // streamResponse / streamChoices 在流式路径上在线聚合，避免把每个 SSE chunk
        // 完整对象都 append 进切片（长流/大图/并发下堆峰值可冲到 GB 级）。
        streamResponse *model.InternalLLMResponse
        streamChoices  map[int]*model.Choice
        // storedResponse stores the non-stream response
        storedResponse *model.InternalLLMResponse
        // canonicalToolCallIDs 记录 (choice.Index:toolCall.Index) 的规范化 id：
        // 上游缺失 id 时写入合成值，后续携带真实 id 的 chunk 覆盖为真实值，
        // 使后续空 id 的增量与客户端聚合结果始终引用同一 id。
        canonicalToolCallIDs map[string]string
}

func (i *ChatInbound) TransformRequest(ctx context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var request model.InternalLLMRequest
	if err := transformer.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	request.RawAPIFormat = model.APIFormatOpenAIChatCompletion
	return &request, nil
}

func (i *ChatInbound) TransformResponse(ctx context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	        ensureNonStreamToolCallIDs(response)
	        // Store the response for later retrieval
	        i.storedResponse = response

	body, err := transformer.Marshal(response)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (i *ChatInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
        if stream.Object == "[DONE]" {
                return []byte("data: [DONE]\n\n"), nil
        }

        i.ensureStreamToolCallIDs(stream)

        // 在线聚合：只保留最终文本/工具调用等结果，不缓存完整 chunk 列表。
        i.foldStreamChunk(stream)

	var body []byte
	var err error

	// OpenAI Chat Completion stream chunks must always carry a "choices" array,
	// and every choice must carry a "delta" field — even an empty object on the
	// final finish_reason chunk. Strict clients (e.g. RikkaHub) treat a missing
	// or null choices[0].delta as a protocol violation and throw. Since
	// model.Choice.Delta is omitempty, stream chunks are serialized through a
	// dedicated type that always emits both fields per the OpenAI SSE spec.
	if stream.Object == "chat.completion.chunk" {
		body, err = marshalChatChunk(stream)
	} else {
		body, err = transformer.Marshal(stream)
	}

	if err != nil {
		return nil, err
	}
	return []byte("data: " + string(body) + "\n\n"), nil
}

// streamChoice renders a chat.completion.chunk choice. The Delta field shadows
// model.Choice.Delta (which is omitempty) so that "delta" is always emitted,
// matching the OpenAI streaming spec — including an empty {} on the terminal
// finish_reason chunk.
type streamChoice struct {
	model.Choice
	Delta *model.Message `json:"delta"`
}

// marshalChatChunk serializes an OpenAI chat completion stream chunk, ensuring
// "choices" is always an array (possibly empty) and each choice always carries
// a non-null "delta" field. It does not mutate the input chunk.
func marshalChatChunk(stream *model.InternalLLMResponse) ([]byte, error) {
	choices := make([]streamChoice, 0, len(stream.Choices))
	for _, choice := range stream.Choices {
		delta := choice.Delta
		if delta == nil {
			delta = &model.Message{}
		}
		choices = append(choices, streamChoice{Choice: choice, Delta: delta})
	}

	type alias model.InternalLLMResponse
	aux := &struct {
		*alias
		Choices []streamChoice `json:"choices"`
	}{
		alias:   (*alias)(stream),
		Choices: choices,
	}
	return transformer.Marshal(aux)
}

// foldStreamChunk merges one stream chunk into the running aggregation.
// 语义与旧版「缓存全部 chunks 再一次性聚合」一致，但内存只保留最终结果。
func (i *ChatInbound) foldStreamChunk(chunk *model.InternalLLMResponse) {
	if chunk == nil {
		return
	}

	if i.streamResponse == nil {
		i.streamResponse = &model.InternalLLMResponse{
			ID:                chunk.ID,
			Object:            "chat.completion",
			Created:           chunk.Created,
			Model:             chunk.Model,
			SystemFingerprint: chunk.SystemFingerprint,
			ServiceTier:       chunk.ServiceTier,
		}
		i.streamChoices = make(map[int]*model.Choice)
	}

	result := i.streamResponse
	if chunk.ID != "" {
		result.ID = chunk.ID
	}
	if chunk.Model != "" {
		result.Model = chunk.Model
	}
	if chunk.Usage != nil {
		result.Usage = chunk.Usage
	}

	for _, choice := range chunk.Choices {
		existingChoice, exists := i.streamChoices[choice.Index]
		if !exists {
			existingChoice = &model.Choice{
				Index:   choice.Index,
				Message: &model.Message{},
			}
			i.streamChoices[choice.Index] = existingChoice
		}

		if choice.Delta != nil {
			delta := choice.Delta

			if delta.Role != "" {
				existingChoice.Message.Role = delta.Role
			}

			if delta.Content.Content != nil {
				if existingChoice.Message.Content.Content == nil {
					existingChoice.Message.Content.Content = new(string)
				}
				*existingChoice.Message.Content.Content += *delta.Content.Content
			}

			if len(delta.Content.MultipleContent) > 0 {
				existingChoice.Message.Content.MultipleContent = append(
					existingChoice.Message.Content.MultipleContent,
					delta.Content.MultipleContent...,
				)
			}

			if len(delta.Images) > 0 {
				existingChoice.Message.Content.MultipleContent = append(
					existingChoice.Message.Content.MultipleContent,
					delta.Images...,
				)
			}

			if delta.GetReasoningContent() != "" {
				if existingChoice.Message.ReasoningContent == nil {
					existingChoice.Message.ReasoningContent = new(string)
				}
				*existingChoice.Message.ReasoningContent += delta.GetReasoningContent()
			}

			if delta.ReasoningSignature != nil {
				signature := *delta.ReasoningSignature
				if delta.ReasoningSignatureFormat != model.APIFormatOpenAIResponse && existingChoice.Message.ReasoningSignature != nil {
					signature = *existingChoice.Message.ReasoningSignature + signature
				}
				existingChoice.Message.ReasoningSignature = &signature
				existingChoice.Message.ReasoningSignatureFormat = delta.ReasoningSignatureFormat
			}

			for _, toolCall := range delta.ToolCalls {
				existingChoice.Message.ToolCalls = mergeToolCall(existingChoice.Message.ToolCalls, toolCall)
			}

			if delta.Refusal != "" {
				// Append（而非覆盖）：拒答可能跨多个 chunk 到达（上游 fix(relay): preserve refusals）。
				existingChoice.Message.Refusal += delta.Refusal
			}
		}

		if choice.FinishReason != nil {
			existingChoice.FinishReason = choice.FinishReason
		}

		if choice.Logprobs != nil {
			if existingChoice.Logprobs == nil {
				existingChoice.Logprobs = &model.LogprobsContent{}
			}
			existingChoice.Logprobs.Content = append(existingChoice.Logprobs.Content, choice.Logprobs.Content...)
		}
	}
}

// GetInternalResponse returns the complete internal response for logging, statistics, etc.
// For streaming: returns the online-aggregated response
// For non-streaming: returns the stored response
func (i *ChatInbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	if i.storedResponse != nil {
		return i.storedResponse, nil
	}

	if i.streamResponse == nil || len(i.streamChoices) == 0 {
		// 仅有元数据、无 choice 的流（极少见）也返回已聚合壳，避免与旧行为在
		// “有 chunk 但无 choice”时返回非 nil 不一致；旧逻辑在 len(chunks)>0 时返回 result。
		if i.streamResponse != nil {
			result := i.streamResponse
			i.streamResponse = nil
			i.streamChoices = nil
			return result, nil
		}
		return nil, nil
	}

	result := i.streamResponse
	result.Choices = model.SortedChoicesByIndex(i.streamChoices)
	i.streamResponse = nil
	i.streamChoices = nil
	return result, nil
}

// ensureStreamToolCallIDs 为上游流式 chunk 中缺失 id 的 tool_call 增量补一个
// (choice, toolCall index) 维度稳定的合成 id；后续携带真实 id 的 chunk 会把规范值
// 覆盖为真实 id。避免客户端聚合出无 id 的 tool_calls 后原样回灌，被严格上游
// （Vercel Zod 校验、中转站协议转换）整单拒绝。
func (i *ChatInbound) ensureStreamToolCallIDs(chunk *model.InternalLLMResponse) {
	if chunk == nil {
		return
	}
	for ci := range chunk.Choices {
		choice := &chunk.Choices[ci]
		if choice.Delta == nil {
			continue
		}
		for ti := range choice.Delta.ToolCalls {
			tc := &choice.Delta.ToolCalls[ti]
			key := fmt.Sprintf("%d:%d", choice.Index, tc.Index)
			if tc.ID != "" {
				// 携带真实 id：覆盖规范值（可能覆盖此前的合成占位）。
				if i.canonicalToolCallIDs == nil {
					i.canonicalToolCallIDs = make(map[string]string)
				}
				i.canonicalToolCallIDs[key] = tc.ID
				continue
			}
			if id, ok := i.canonicalToolCallIDs[key]; ok {
				tc.ID = id
			} else {
				id := "call_" + lo.RandomString(16, lo.AlphanumericCharset)
				if i.canonicalToolCallIDs == nil {
					i.canonicalToolCallIDs = make(map[string]string)
				}
				i.canonicalToolCallIDs[key] = id
				tc.ID = id
			}
		}
	}
}

// ensureNonStreamToolCallIDs 为非流式响应中缺失 id 的 tool_call 合成 id，避免
// 客户端回灌无 id 的 tool_calls 后被严格上游整单拒绝。
func ensureNonStreamToolCallIDs(response *model.InternalLLMResponse) {
	if response == nil {
		return
	}
	for ci := range response.Choices {
		choice := &response.Choices[ci]
		if choice.Message == nil {
			continue
		}
		for ti := range choice.Message.ToolCalls {
			if choice.Message.ToolCalls[ti].ID == "" {
				choice.Message.ToolCalls[ti].ID = "call_" + lo.RandomString(16, lo.AlphanumericCharset)
			}
		}
	}
}

func mergeToolCall(toolCalls []model.ToolCall, delta model.ToolCall) []model.ToolCall {
	// Find existing tool call by index
	for i, tc := range toolCalls {
		if tc.Index == delta.Index {
			// Merge the delta into existing tool call
			if delta.ID != "" {
				toolCalls[i].ID = delta.ID
			}
			if delta.Type != "" {
				toolCalls[i].Type = delta.Type
			}
			if delta.Function.Name != "" {
				toolCalls[i].Function.Name += delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				toolCalls[i].Function.Arguments += delta.Function.Arguments
			}
			if delta.Namespace != "" {
				toolCalls[i].Namespace = delta.Namespace
			}
			return toolCalls
		}
	}

	// New tool call, add it
	return append(toolCalls, delta)
}
