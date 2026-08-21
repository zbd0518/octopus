package openai

import "github.com/lingyuins/octopus/internal/transformer/model"

// sanitizeToolPairingForOpenAICompat keeps the OpenAI-compatible messages array
// well-formed for strict upstreams (DeepSeek / vLLM, Mimo, FuturePPO-style relays)
// that reject a conversation where an assistant message carrying tool_calls is not
// immediately followed by the tool results for those calls:
//
//	400 messages[4]: assistant message appears before tool results for call_...
//
// It performs two defensive passes that leave a normal history untouched:
//
//  1. For every assistant message with tool_calls, keep only the calls fulfilled by
//     the consecutive block of tool-result messages that immediately follows it
//     before the next non-tool message. An assistant whose tool results were lost
//     mid-history (e.g. after a client reconnect / truncated history replay) no
//     longer trips the upstream. If removing the calls leaves an assistant message
//     with no visible content, the empty message is dropped entirely.
//
//     For non-streaming requests, a *trailing* assistant message that carries
//     tool_calls is a valid DeepSeek continuation (its results arrive on the next
//     turn and reasoning_content must be echoed back), so it is kept verbatim. For
//     streaming requests a trailing unresolved tool_calls is invalid, so it is
//     stripped like any other position.
//
//  2. Drop orphan tool results whose tool_call_id matches no retained assistant tool
//     call.
func sanitizeToolPairingForOpenAICompat(messages []model.Message, streaming bool) []model.Message {
	if len(messages) == 0 {
		return messages
	}

	// Pass 1: retain only the tool calls that have a result in the immediately
	// following tool-result block; drop empty assistant messages left behind.
	//
	// A *trailing* assistant message that carries tool_calls is a valid DeepSeek
	// continuation only for non-streaming requests: its results arrive on the next
	// turn and DeepSeek requires reasoning_content (if present) to be echoed back.
	// For streaming requests the trailing unresolved tool_calls is invalid and must
	// be stripped.
	rebuilt := make([]model.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			rebuilt = append(rebuilt, msg)
			continue
		}

		// Trailing continuation (non-streaming only): preserve tool_calls verbatim.
		if !streaming && i == len(messages)-1 {
			rebuilt = append(rebuilt, msg)
			continue
		}

		fulfilled := make(map[string]struct{})
		for j := i + 1; j < len(messages); j++ {
			next := messages[j]
			if next.Role != "tool" {
				break
			}
			if next.ToolCallID != nil && *next.ToolCallID != "" {
				fulfilled[*next.ToolCallID] = struct{}{}
			}
		}

		kept := make([]model.ToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			if tc.ID == "" {
				continue
			}
			if _, ok := fulfilled[tc.ID]; ok {
				kept = append(kept, tc)
			}
		}
		msg.ToolCalls = kept

		if len(msg.ToolCalls) == 0 && len(msg.GetReasoningContent()) == 0 &&
			msg.Content.Content == nil && len(msg.Content.MultipleContent) == 0 &&
			msg.Refusal == "" {
			continue // drop the empty assistant message
		}
		rebuilt = append(rebuilt, msg)
	}

	// Pass 2: keep only tool results that reference a retained assistant tool call.
	keptCallIDs := make(map[string]struct{})
	for _, msg := range rebuilt {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					keptCallIDs[tc.ID] = struct{}{}
				}
			}
		}
	}

	out := make([]model.Message, 0, len(rebuilt))
	for _, msg := range rebuilt {
		if msg.Role == "tool" {
			if msg.ToolCallID == nil || *msg.ToolCallID == "" {
				continue
			}
			if _, ok := keptCallIDs[*msg.ToolCallID]; !ok {
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}