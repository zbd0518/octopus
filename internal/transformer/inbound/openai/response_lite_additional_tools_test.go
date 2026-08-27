package openai

import (
	"context"
	"strings"
	"testing"

	outboundopenai "github.com/lingyuins/octopus/internal/transformer/outbound/openai"
)

// Codex Response Lite 请求：gpt-5.6-luna 等模型把工具放到 input[].additional_tools，
// 顶层 tools 为 null。验证这些原生工具（namespace/custom）在
// Responses -> internal -> Responses 往返后原样保留，不再被静默丢弃。
func codexResponsesLiteRequest() []byte {
	return []byte(`{
	  "model": "gpt-5.6-luna",
	  "instructions": "You are Codex...",
	  "input": [
	    {
	      "type": "additional_tools",
	      "role": "developer",
	      "tools": [
	        {
	          "type": "namespace",
	          "name": "functions",
	          "description": "tasks",
	          "tools": [
	            {"type": "custom", "name": "exec", "description": "run shell", "strict": false},
	            {"type": "custom", "name": "wait", "strict": false}
	          ]
	        }
	      ]
	    },
	    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "list files"}]}
	  ],
	  "tools": null,
	  "tool_choice": "auto",
	  "parallel_tool_calls": false,
	  "store": false,
	  "stream": true
	}`)
}

func TestCodexResponsesLite_AdditionalToolsRoundTrip(t *testing.T) {
	in := &ResponseInbound{}
	internalReq, err := in.TransformRequest(context.Background(), codexResponsesLiteRequest())
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	// 原请求顶层 tools 为 null，additional_tools 携带工具。内部 Tools 允许为空，
	// 但 additional_tools 原生工具 JSON 必须被暂存以便出站复原。
	raw, ok := internalReq.TransformerMetadata[transformerMetadataResponsesLiteAdditionalTools]
	if !ok || raw == "" {
		t.Fatalf("additional_tools raw JSON not stashed in TransformerMetadata")
	}
	if !strings.Contains(raw, "namespace") || !strings.Contains(raw, "custom") || !strings.Contains(raw, "exec") {
		t.Fatalf("stashed additional_tools missing native tools: %s", raw)
	}

	// 出站必须把这些原生工具重新放回 input[].additional_tools。
	out := &outboundopenai.ResponseOutbound{}
	httpReq, err := out.TransformRequest(context.Background(), internalReq, "https://upstream.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("ResponseOutbound.TransformRequest() error = %v", err)
	}
	body := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, rerr := httpReq.Body.Read(buf)
		body = append(body, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	s := string(body)
	t.Logf("outbound responses body: %s", s)

	if !strings.Contains(s, `"additional_tools"`) {
		t.Fatalf("outbound responses body missing additional_tools item")
	}
	if !strings.Contains(s, `"namespace"`) || !strings.Contains(s, `"custom"`) || !strings.Contains(s, `"exec"`) {
		t.Fatalf("outbound responses body lost native tools: %s", s)
	}
}

// Codex Response Lite 响应侧：模型调用 custom exec 工具时，上游 SSE 产生
// custom_tool_call item + custom_tool_call_input.delta。验证 Octopus
// outbound TransformStream 能把它解析成带 namespace 的内部 ToolCall，再经
// inbound TransformStream 重新输回 custom_tool_call SSE 事件给 Codex 客户端。
func TestCodexResponsesLite_CustomToolCallResponseRoundTrip(t *testing.T) {
	in := &ResponseInbound{}
	// 初始化请求（建立 inbound 状态）
	if _, err := in.TransformRequest(context.Background(), codexResponsesLiteRequest()); err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	out := &outboundopenai.ResponseOutbound{}
	upstreamSSE := []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.6-luna"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"item_exec_1","call_id":"call_exec_1","name":"exec","namespace":"functions","status":"in_progress"}}`,
		`{"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"item_exec_1","delta":"{\"command\":[\"ls\"]}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"item_exec_1","call_id":"call_exec_1","name":"exec","namespace":"functions","status":"completed","input":"{\"command\":[\"ls\"]}"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-luna","status":"completed"}}`,
	}

	var all strings.Builder
	for _, ev := range upstreamSSE {
		internal, err := out.TransformStream(context.Background(), []byte(ev))
		if err != nil {
			t.Fatalf("ResponseOutbound.TransformStream(%s) error = %v", ev, err)
		}
		if internal == nil {
			continue
		}
		sse, err := in.TransformStream(context.Background(), internal)
		if err != nil {
			t.Fatalf("ResponseInbound.TransformStream(%s) error = %v", ev, err)
		}
		if len(sse) > 0 {
			all.Write(sse)
		}
	}

	output := all.String()
	t.Logf("downstream SSE:\n%s", output)

	checks := []string{
		`"type":"response.output_item.added"`,
		`"type":"custom_tool_call"`,
		`"call_id":"call_exec_1"`,
		`"name":"exec"`,
		`"namespace":"functions"`,
		`response.custom_tool_call_input.delta`,
		`response.custom_tool_call_input.done`,
		`"status":"completed"`,
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("downstream SSE missing %q", want)
		}
	}
}

// 多轮工具输出回灌：Codex 客户端在下一次请求把 custom exec 的执行结果作为
// custom_tool_call_output item 回传。验证 Octopus 入站识别它 -> 内部 tool message
// 带 ToolCallType=custom -> 出站重新生成 custom_tool_call_output（而非 function_call_output）。
func TestCodexResponsesLite_CustomToolCallOutputRoundTrip(t *testing.T) {
	req := []byte(`{
	  "model": "gpt-5.6-luna",
	  "instructions": "You are Codex...",
	  "input": [
	    {
	      "type": "custom_tool_call",
	      "id": "item_exec_1",
	      "call_id": "call_exec_1",
	      "name": "exec",
	      "namespace": "functions",
	      "input": "{\"command\":[\"ls\"]}"
	    },
	    {
	      "type": "custom_tool_call_output",
	      "call_id": "call_exec_1",
	      "output": "file1.txt\nfile2.txt"
	    },
	    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "next"}]}
	  ],
	  "tool_choice": "auto",
	  "parallel_tool_calls": false,
	  "store": false,
	  "stream": true
	}`)

	in := &ResponseInbound{}
	internalReq, err := in.TransformRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	// 检查内部请求：应有一个 custom tool call + 一个 tool 输出消息（ToolCallType=custom）。
	var sawCustomCall, sawCustomOutput bool
	for _, msg := range internalReq.Messages {
		if len(msg.ToolCalls) > 0 && msg.ToolCalls[0].Type == "custom" && msg.ToolCalls[0].Namespace == "functions" {
			sawCustomCall = true
		}
		if msg.Role == "tool" && msg.ToolCallType == "custom" && msg.ToolCallID != nil && *msg.ToolCallID == "call_exec_1" {
			sawCustomOutput = true
		}
	}
	if !sawCustomCall {
		t.Fatalf("internal request missing custom_tool_call; messages=%+v", internalReq.Messages)
	}
	if !sawCustomOutput {
		t.Fatalf("internal request missing custom_tool_call_output tool message; messages=%+v", internalReq.Messages)
	}

	// 出站应重新生成 custom_tool_call + custom_tool_call_output。
	out := &outboundopenai.ResponseOutbound{}
	httpReq, err := out.TransformRequest(context.Background(), internalReq, "https://upstream.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("ResponseOutbound.TransformRequest() error = %v", err)
	}
	body := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, rerr := httpReq.Body.Read(buf)
		body = append(body, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	s := string(body)
	t.Logf("outbound responses body: %s", s)

	if !strings.Contains(s, `"custom_tool_call"`) {
		t.Errorf("outbound missing custom_tool_call")
	}
	if !strings.Contains(s, `"custom_tool_call_output"`) {
		t.Errorf("outbound missing custom_tool_call_output")
	}
	if !strings.Contains(s, `"namespace":"functions"`) {
		t.Errorf("outbound missing namespace")
	}
}
