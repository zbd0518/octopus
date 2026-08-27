package openai

import (
	"context"
	"strings"
	"testing"

	outboundopenai "github.com/lingyuins/octopus/internal/transformer/outbound/openai"
)

// 临时复现：Codex(/v1/responses) → octopus → 上游 chat/completions 的完整链路。
// 验证请求侧 tools 是否保留、响应侧 tool_calls 是否转回 Responses function_call 事件。

func codexStyleResponsesRequest() []byte {
	return []byte(`{
	  "model": "gpt-5",
	  "instructions": "You are a coding agent.",
	  "input": [
	    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "list files"}]}
	  ],
	  "tools": [
	    {
	      "type": "function",
	      "name": "shell",
	      "description": "Runs a shell command",
	      "strict": false,
	      "parameters": {
	        "type": "object",
	        "properties": {"command": {"type": "array", "items": {"type": "string"}}},
	        "required": ["command"]
	      }
	    },
	    {"type": "web_search"}
	  ],
	  "tool_choice": "auto",
	  "parallel_tool_calls": false,
	  "reasoning": {"effort": "medium", "summary": "auto"},
	  "store": false,
	  "stream": true,
	  "include": ["reasoning.encrypted_content"]
	}`)
}

func TestCodexRepro_RequestSideTools(t *testing.T) {
	in := &ResponseInbound{}
	internalReq, err := in.TransformRequest(context.Background(), codexStyleResponsesRequest())
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	if len(internalReq.Tools) != 1 {
		t.Fatalf("internal tools = %d, want 1 (web_search dropped is ok)", len(internalReq.Tools))
	}
	if internalReq.Tools[0].Function.Name != "shell" {
		t.Fatalf("tool name = %q, want shell", internalReq.Tools[0].Function.Name)
	}

	out := &outboundopenai.ChatOutbound{}
	req, err := out.TransformRequest(context.Background(), internalReq, "https://upstream.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("ChatOutbound.TransformRequest() error = %v", err)
	}
	body := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, rerr := req.Body.Read(buf)
		body = append(body, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	s := string(body)
	t.Logf("upstream body: %s", s)
	if !strings.Contains(s, `"tools"`) {
		t.Fatalf("upstream chat body missing tools")
	}
	if !strings.Contains(s, `"name":"shell"`) && !strings.Contains(s, `"name": "shell"`) {
		t.Fatalf("upstream chat body missing shell tool name")
	}
}

// 模拟 Codex 多轮输入：reasoning + function_call + function_call_output 回灌。
func TestCodexRepro_MultiTurnRequest(t *testing.T) {
	multiTurn := []byte(`{
	  "model": "gpt-5",
	  "instructions": "You are a coding agent.",
	  "input": [
	    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "list files"}]},
	    {"type": "reasoning", "id": "rs_1", "summary": [{"type": "summary_text", "text": "thinking"}], "encrypted_content": "ENC"},
	    {"type": "function_call", "id": "fc_1", "call_id": "call_abc", "name": "shell", "arguments": "{\"command\":[\"ls\"]}"},
	    {"type": "function_call_output", "call_id": "call_abc", "output": "file.txt"},
	    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "now read file.txt"}]}
	  ],
	  "tools": [
	    {"type": "function", "name": "shell", "description": "Runs a shell command", "strict": false,
	     "parameters": {"type": "object", "properties": {"command": {"type": "array", "items": {"type": "string"}}}, "required": ["command"]}}
	  ],
	  "tool_choice": "auto",
	  "parallel_tool_calls": false,
	  "store": false,
	  "stream": true
	}`)

	in := &ResponseInbound{}
	internalReq, err := in.TransformRequest(context.Background(), multiTurn)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	for i, m := range internalReq.Messages {
		t.Logf("msg[%d] role=%s content=%v toolCalls=%d toolCallID=%v reasonSig=%v",
			i, m.Role, m.Content.Content, len(m.ToolCalls), m.ToolCallID, m.ReasoningSignature != nil && *m.ReasoningSignature != "")
	}

	out := &outboundopenai.ChatOutbound{}
	req, err := out.TransformRequest(context.Background(), internalReq, "https://upstream.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("ChatOutbound.TransformRequest() error = %v", err)
	}
	body := make([]byte, 0)
	buf := make([]byte, 8192)
	for {
		n, rerr := req.Body.Read(buf)
		body = append(body, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	s := string(body)
	t.Logf("upstream body: %s", s)

	if !strings.Contains(s, `"tools"`) || !strings.Contains(s, `"shell"`) {
		t.Errorf("multi-turn upstream body lost tools")
	}
	if !strings.Contains(s, `call_abc`) {
		t.Errorf("multi-turn upstream body lost call_id pairing")
	}
}

// 并行工具调用（两个 function_call 连续出现）经转换后是否保持配对完整。
func TestCodexRepro_ParallelToolCallsHistory(t *testing.T) {
	twoCalls := []byte(`{
	  "model": "gpt-5",
	  "input": [
	    {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "do two things"}]},
	    {"type": "function_call", "call_id": "call_A", "name": "shell", "arguments": "{}"},
	    {"type": "function_call", "call_id": "call_B", "name": "update_plan", "arguments": "{}"},
	    {"type": "function_call_output", "call_id": "call_A", "output": "ok-a"},
	    {"type": "function_call_output", "call_id": "call_B", "output": "ok-b"}
	  ],
	  "tools": [{"type": "function", "name": "shell", "parameters": {"type": "object"}}],
	  "stream": true
	}`)

	in := &ResponseInbound{}
	internalReq, err := in.TransformRequest(context.Background(), twoCalls)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	out := &outboundopenai.ChatOutbound{}
	req, err := out.TransformRequest(context.Background(), internalReq, "https://upstream.example.com/v1", "sk-test")
	if err != nil {
		t.Fatalf("ChatOutbound.TransformRequest() error = %v", err)
	}
	body := make([]byte, 0)
	buf := make([]byte, 8192)
	for {
		n, rerr := req.Body.Read(buf)
		body = append(body, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	s := string(body)
	t.Logf("upstream body: %s", s)

	for _, want := range []string{"call_A", "call_B", "ok-a", "ok-b"} {
		if !strings.Contains(s, want) {
			t.Errorf("parallel-call history lost %q after sanitize", want)
		}
	}
}

func TestCodexRepro_StreamToolCalls(t *testing.T) {
	in := &ResponseInbound{}
	// 初始化请求（建立 inbound 状态）
	if _, err := in.TransformRequest(context.Background(), codexStyleResponsesRequest()); err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	out := &outboundopenai.ChatOutbound{}
	chunks := []string{
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"shell","arguments":""}}]}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":"}}]}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"[\"ls\"]}"}}]}}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		`[DONE]`,
	}

	var all strings.Builder
	for _, c := range chunks {
		internalStream, err := out.TransformStream(context.Background(), []byte(c))
		if err != nil {
			t.Fatalf("ChatOutbound.TransformStream(%s) error = %v", c, err)
		}
		sse, err := in.TransformStream(context.Background(), internalStream)
		if err != nil {
			t.Fatalf("ResponseInbound.TransformStream(%s) error = %v", c, err)
		}
		if len(sse) > 0 {
			all.Write(sse)
		}
	}

	output := all.String()
	t.Logf("downstream SSE:\n%s", output)

	checks := []string{
		`"type":"response.output_item.added"`,
		`"type":"function_call"`,
		`"call_id":"call_abc"`,
		`"name":"shell"`,
		`response.function_call_arguments.delta`,
		`response.function_call_arguments.done`,
		`"arguments":"{\"command\":[\"ls\"]}"`,
		`response.output_item.done`,
		`"status":"completed"`,
	}
	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("downstream SSE missing %q", want)
		}
	}
}
