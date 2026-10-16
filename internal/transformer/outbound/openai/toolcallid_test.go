package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer/model"
)

// ── ensureToolCallIDs：请求侧（历史回灌）补齐缺失的工具调用 id ──

func emptyIDToolCall(index int, name string) model.ToolCall {
	return model.ToolCall{
		Index: index,
		Type:  "function",
		Function: model.FunctionCall{
			Name:      name,
			Arguments: "{}",
		},
	}
}

func toolResultMessage(id string) model.Message {
	msg := model.Message{Role: "tool"}
	if id != "" {
		msg.ToolCallID = &id
	}
	return msg
}

func TestEnsureToolCallIDs_SynthesizesCallAndRelinksResult(t *testing.T) {
	messages := []model.Message{
		{Role: "user", Content: model.MessageContent{Content: strPtrTest("do it")}},
		{
			Role:      "assistant",
			ToolCalls: []model.ToolCall{emptyIDToolCall(0, "shell")},
		},
		toolResultMessage(""),
		{Role: "user", Content: model.MessageContent{Content: strPtrTest("thanks")}},
	}

	ensureToolCallIDs(messages)

	call := messages[1].ToolCalls[0]
	if !strings.HasPrefix(call.ID, "call_") {
		t.Fatalf("expected synthesized call_ id, got %q", call.ID)
	}
	if messages[2].ToolCallID == nil || *messages[2].ToolCallID != call.ID {
		t.Fatalf("expected tool result re-linked to %q, got %v", call.ID, messages[2].ToolCallID)
	}
}

func TestEnsureToolCallIDs_PreservesExistingIDs(t *testing.T) {
	existing := "call_real_1"
	messages := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{ID: existing, Index: 0, Type: "function", Function: model.FunctionCall{Name: "shell"}},
			},
		},
		toolResultMessage(existing),
	}

	ensureToolCallIDs(messages)

	if messages[0].ToolCalls[0].ID != existing {
		t.Fatalf("existing id must not be rewritten, got %q", messages[0].ToolCalls[0].ID)
	}
	if messages[1].ToolCallID == nil || *messages[1].ToolCallID != existing {
		t.Fatalf("existing result must not be rewritten, got %v", messages[1].ToolCallID)
	}
}

func TestEnsureToolCallIDs_ParallelEmptyCallsPairWithResultsInOrder(t *testing.T) {
	messages := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				emptyIDToolCall(0, "shell"),
				emptyIDToolCall(1, "update_plan"),
			},
		},
		toolResultMessage(""),
		toolResultMessage(""),
	}

	ensureToolCallIDs(messages)

	first := messages[0].ToolCalls[0].ID
	second := messages[0].ToolCalls[1].ID
	if first == "" || second == "" || first == second {
		t.Fatalf("expected two distinct synthesized ids, got %q / %q", first, second)
	}
	if got := messages[1].ToolCallID; got == nil || *got != first {
		t.Fatalf("result[0] should re-link to first call, got %v", got)
	}
	if got := messages[2].ToolCallID; got == nil || *got != second {
		t.Fatalf("result[1] should re-link to second call, got %v", got)
	}
}

func TestEnsureToolCallIDs_DoesNotTouchSharedUnderlyingArray(t *testing.T) {
	original := []model.Message{
		{
			Role:      "assistant",
			ToolCalls: []model.ToolCall{emptyIDToolCall(0, "shell")},
		},
	}
	// 模拟 CloneRequestForOpenAICompat 的浅拷贝：结构体切片是新的，
	// 但 ToolCalls 底层数组与原始消息共享。
	clone := append([]model.Message(nil), original...)

	ensureToolCallIDs(clone)

	if original[0].ToolCalls[0].ID != "" {
		t.Fatalf("original shared ToolCalls array was mutated: %q", original[0].ToolCalls[0].ID)
	}
	if clone[0].ToolCalls[0].ID == "" {
		t.Fatal("clone should carry the synthesized id")
	}
}

// ── ChatOutbound.TransformRequest：出站序列化不再产出无 id 的 tool_calls ──

func TestChatOutboundSynthesizesMissingToolCallIDsOnTheWire(t *testing.T) {
	request := &model.InternalLLMRequest{
		Model: "vmc/deepseek-v4p1-flash-contributor-fallbacks",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtrTest("run")}},
			{
				Role:      "assistant",
				ToolCalls: []model.ToolCall{emptyIDToolCall(0, "shell")},
			},
			toolResultMessage(""),
			{Role: "user", Content: model.MessageContent{Content: strPtrTest("continue")}},
		},
	}

	outbound := &ChatOutbound{}
	req, err := outbound.TransformRequest(t.Context(), request, "https://relay.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var payload struct {
		Messages []struct {
			Role      string `json:"role"`
			ToolCalls []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}

	// 复现线上故障的断言：messages[N].tool_calls[1].id 必须是字符串而不是 undefined。
	for mi, m := range payload.Messages {
		for ti, tc := range m.ToolCalls {
			if tc.ID == "" {
				t.Fatalf("messages[%d].tool_calls[%d].id is empty on the wire", mi, ti)
			}
			if !strings.HasPrefix(tc.ID, "call_") {
				t.Fatalf("messages[%d].tool_calls[%d].id = %q, want call_ prefix", mi, ti, tc.ID)
			}
		}
	}

	var callID string
	for _, m := range payload.Messages {
		if len(m.ToolCalls) > 0 {
			callID = m.ToolCalls[0].ID
		}
	}
	if callID == "" {
		t.Fatal("no tool call found in payload")
	}
	foundResult := false
	for _, m := range payload.Messages {
		if m.Role == "tool" {
			foundResult = true
			if m.ToolCallID != callID {
				t.Fatalf("tool result tool_call_id = %q, want %q", m.ToolCallID, callID)
			}
		}
	}
	if !foundResult {
		t.Fatal("tool result message missing from payload")
	}

	// 出站请求不应回写共享的内部请求（重试缓存/日志仍持有原切片）。
	if request.Messages[1].ToolCalls[0].ID != "" {
		t.Fatalf("internal request was mutated: %q", request.Messages[1].ToolCalls[0].ID)
	}
}

// ── ResponseOutbound.TransformRequest：Responses 出站不再产出无 call_id 的 function_call ──

func TestResponseOutboundSynthesizesMissingCallIDsOnTheWire(t *testing.T) {
	request := &model.InternalLLMRequest{
		Model: "gpt-5",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtrTest("run")}},
			{
				Role:      "assistant",
				ToolCalls: []model.ToolCall{emptyIDToolCall(0, "shell")},
			},
			toolResultMessage(""),
			{Role: "user", Content: model.MessageContent{Content: strPtrTest("continue")}},
		},
	}

	outbound := &ResponseOutbound{}
	req, err := outbound.TransformRequest(t.Context(), request, "https://relay.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var payload struct {
		Input []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		} `json:"input"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatalf("decode request body: %v", err)
	}

	var functionCalls, outputs int
	var callID string
	for _, item := range payload.Input {
		switch item.Type {
		case "function_call":
			functionCalls++
			if item.CallID == "" {
				t.Fatal("function_call without call_id on the wire (upstream relays reject: 缺少 call_id 或 name)")
			}
			callID = item.CallID
		case "function_call_output":
			outputs++
			if item.CallID == "" {
				t.Fatal("function_call_output without call_id on the wire")
			}
			if item.CallID != callID {
				t.Fatalf("function_call_output call_id = %q, want %q", item.CallID, callID)
			}
		}
	}
	if functionCalls != 1 || outputs != 1 {
		t.Fatalf("expected 1 function_call + 1 output, got %d/%d", functionCalls, outputs)
	}

	if request.Messages[1].ToolCalls[0].ID != "" {
		t.Fatalf("internal request was mutated: %q", request.Messages[1].ToolCalls[0].ID)
	}
}

func strPtrTest(s string) *string { return &s }
