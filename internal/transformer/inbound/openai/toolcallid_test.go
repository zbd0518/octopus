package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
)

// ── A1：Responses 入站流式 —— 上游 tool_call 缺 id 时合成 call_id ──

// TestResponseInboundSynthesizesCallIDForIDlessUpstreamToolCall 复现线上 #2 的
// 污染源：上游（Vercel/部分 DeepSeek 部署）返回不带 id 的 tool_call，Octopus 若
// 原样发给 Codex，客户端回灌的 function_call 将缺失 call_id，被中转站的
// Responses 协议转换整单拒绝（"缺少 call_id 或 name"）。
func TestResponseInboundSynthesizesCallIDForIDlessUpstreamToolCall(t *testing.T) {
	adapter := &ResponseInbound{}
	chunks := []*model.InternalLLMResponse{
		makeStreamChunk("r", toolCallDelta(0, "", "shell", "{\"command\":[\"ls\"]}"), nil, nil),
		makeStreamChunk("r", nil, strPtrDone("tool_calls"), nil),
		{Object: "[DONE]"},
	}

	var addedItem, doneItem *ResponsesItem
	for _, chunk := range chunks {
		wire, err := adapter.TransformStream(context.Background(), chunk)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range collectCompletedEvents(t, wire) {
			switch ev.Type {
			case "response.output_item.added":
				addedItem = ev.Item
			case "response.output_item.done":
				doneItem = ev.Item
			}
		}
	}

	if addedItem == nil || doneItem == nil {
		t.Fatalf("missing tool call items: added=%v done=%v", addedItem, doneItem)
	}
	for label, item := range map[string]*ResponsesItem{"added": addedItem, "done": doneItem} {
		if !strings.HasPrefix(item.CallID, "call_") {
			t.Fatalf("%s item call_id = %q, want synthesized call_ prefix", label, item.CallID)
		}
		if addedItem.CallID != doneItem.CallID {
			t.Fatalf("added/done call_id mismatch: %q vs %q", addedItem.CallID, doneItem.CallID)
		}
	}

	// 回灌验证：客户端把该 function_call 原样回灌后，入站转换应能配对成功。
	messages, err := convertInputToMessages(&ResponsesInput{
		Items: []ResponsesItem{
			*addedItem,
			{Type: "function_call_output", CallID: addedItem.CallID},
		},
	})
	if err != nil {
		t.Fatalf("echoed history conversion failed: %v", err)
	}
	var sawAssistantCall, sawToolResult bool
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 && msg.ToolCalls[0].ID == addedItem.CallID {
			sawAssistantCall = true
		}
		if msg.Role == "tool" && msg.ToolCallID != nil && *msg.ToolCallID == addedItem.CallID {
			sawToolResult = true
		}
	}
	if !sawAssistantCall || !sawToolResult {
		t.Fatalf("echoed history lost pairing: assistant=%v result=%v", sawAssistantCall, sawToolResult)
	}
}

// TestResponseInboundKeepsUpstreamCallIDWhenPresented：上游携带真实 id 时不得覆盖。
func TestResponseInboundKeepsUpstreamCallIDWhenPresented(t *testing.T) {
	adapter := &ResponseInbound{}
	chunks := []*model.InternalLLMResponse{
		makeStreamChunk("r", toolCallDelta(0, "call_real_1", "shell", "{}"), nil, nil),
		makeStreamChunk("r", nil, strPtrDone("tool_calls"), nil),
		{Object: "[DONE]"},
	}

	var doneItem *ResponsesItem
	for _, chunk := range chunks {
		wire, err := adapter.TransformStream(context.Background(), chunk)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range collectCompletedEvents(t, wire) {
			if ev.Type == "response.output_item.done" {
				doneItem = ev.Item
			}
		}
	}

	if doneItem == nil || doneItem.CallID != "call_real_1" {
		t.Fatalf("upstream call_id must be preserved, got %+v", doneItem)
	}
}

// ── A3：Chat 入站流式 —— (choice, index) 维度的规范 id ──

// TestChatInboundStreamSynthesizesStableToolCallIDs：第一个 chunk 缺 id 时合成，
// 后续 arguments chunk 复用同一 id；真实 id 后到时以真实 id 为准且后续 chunk 一致。
func TestChatInboundStreamSynthesizesStableToolCallIDs(t *testing.T) {
	adapter := &ChatInbound{}

	// chunk1：无 id → 合成
	wire1, err := adapter.TransformStream(context.Background(),
		makeStreamChunk("r", toolCallDelta(0, "", "shell", "{\"command\":"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	id1 := extractWireToolCallID(t, wire1)
	if !strings.HasPrefix(id1, "call_") {
		t.Fatalf("first chunk id = %q, want synthesized call_ prefix", id1)
	}

	// chunk2：后续 arguments chunk 无 id → 复用同一合成 id
	wire2, err := adapter.TransformStream(context.Background(),
		makeStreamChunk("r", toolCallDelta(0, "", "", "[\"ls\"]}"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := extractWireToolCallID(t, wire2); got != id1 {
		t.Fatalf("second chunk id = %q, want stable %q", got, id1)
	}

	// 聚合结果与 chunk 一致
	internal, err := adapter.GetInternalResponse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var aggregatedID string
	for _, ch := range internal.Choices {
		if ch.Message != nil {
			for _, tc := range ch.Message.ToolCalls {
				aggregatedID = tc.ID
			}
		}
	}
	if aggregatedID != id1 {
		t.Fatalf("aggregated id = %q, want %q", aggregatedID, id1)
	}
}

func TestChatInboundStreamRealIDOverridesSyntheticPlaceholder(t *testing.T) {
	adapter := &ChatInbound{}

	// chunk1：无 id → 合成占位
	wire1, err := adapter.TransformStream(context.Background(),
		makeStreamChunk("r", toolCallDelta(0, "", "shell", "{"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	id1 := extractWireToolCallID(t, wire1)

	// chunk2：携带真实 id → 规范值切到真实 id
	wire2, err := adapter.TransformStream(context.Background(),
		makeStreamChunk("r", toolCallDelta(0, "call_real_9", "", "}"), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := extractWireToolCallID(t, wire2); got != "call_real_9" {
		t.Fatalf("chunk2 id = %q, want real id call_real_9", got)
	}

	// chunk3：后续无 id chunk → 复用真实 id（而非旧的合成占位）
	wire3, err := adapter.TransformStream(context.Background(),
		makeStreamChunk("r", toolCallDelta(0, "", "", ""), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := extractWireToolCallID(t, wire3); got != "call_real_9" {
		t.Fatalf("chunk3 id = %q, want call_real_9 (not stale %q)", got, id1)
	}
}

// ── A4：Chat 入站非流式 —— 响应中缺失 id 的 tool_call 补齐 ──

func TestChatInboundNonStreamSynthesizesMissingToolCallIDs(t *testing.T) {
	adapter := &ChatInbound{}
	response := &model.InternalLLMResponse{
		ID:     "r1",
		Object: "chat.completion",
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{
						{Index: 0, Type: "function", Function: model.FunctionCall{Name: "shell", Arguments: "{}"}},
					},
				},
			},
		},
	}

	wire, err := adapter.TransformResponse(context.Background(), response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), `"id":"call_`) {
		t.Fatalf("non-stream response should carry synthesized tool_call id, body: %s", string(wire))
	}
}

// ── helpers ──

func extractWireToolCallID(t *testing.T, wire []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(wire), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						ID string `json:"id"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := transformer.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			for _, tc := range ch.Delta.ToolCalls {
				if tc.ID != "" {
					return tc.ID
				}
			}
		}
	}
	t.Fatalf("no tool_call id found in wire: %s", string(wire))
	return ""
}
