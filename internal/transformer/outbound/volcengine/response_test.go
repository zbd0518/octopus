package volcengine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer"
	"github.com/lingyuins/octopus/internal/transformer/model"
)

func stringPtr(s string) *string { return &s }

func readRequestBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	if r.Body == nil {
		return nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

// TestTransformRequest_OmitsThinkingWhenNoReasoningEffort (issue #181):
// 未设置 reasoning_effort 时，序列化输出绝不能包含 thinking 字段，
// 否则火山引擎收到 {"thinking":{"type":""}} 返回 400。
func TestTransformRequest_OmitsThinkingWhenNoReasoningEffort(t *testing.T) {
	o := &ResponseOutbound{}
	stream := false
	req := &model.InternalLLMRequest{
		Model: "doubao-seed-1-6-251015",
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: stringPtr("hi"),
			},
		}},
		Stream: &stream,
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://ark.cn-beijing.volces.com/api/v3", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	bodyStr := string(readRequestBody(t, httpReq))

	if strings.Contains(bodyStr, "thinking") {
		t.Fatalf("issue #181: expected thinking field omitted, but body contains it: %s", bodyStr)
	}
}

// TestTransformRequest_SetsThinkingDisabledForMinimalEffort (issue #181 回归):
// reasoning_effort=minimal → thinking.type=disabled。
func TestTransformRequest_SetsThinkingDisabledForMinimalEffort(t *testing.T) {
	o := &ResponseOutbound{}
	stream := false
	req := &model.InternalLLMRequest{
		Model:           "doubao-seed-1-6-251015",
		ReasoningEffort: "minimal",
		Messages:        []model.Message{{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}}},
		Stream:          &stream,
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://ark.cn-beijing.volces.com/api/v3", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	bodyStr := string(readRequestBody(t, httpReq))

	if !strings.Contains(bodyStr, `"thinking":{"type":"disabled"}`) {
		t.Fatalf("expected thinking.type=disabled, got: %s", bodyStr)
	}
}

// TestTransformRequest_SetsThinkingEnabledForHighEffort (issue #181 回归):
// reasoning_effort=high → thinking.type=enabled。
func TestTransformRequest_SetsThinkingEnabledForHighEffort(t *testing.T) {
	o := &ResponseOutbound{}
	stream := false
	req := &model.InternalLLMRequest{
		Model:           "doubao-seed-1-6-251015",
		ReasoningEffort: "high",
		Messages:        []model.Message{{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}}},
		Stream:          &stream,
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://ark.cn-beijing.volces.com/api/v3", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	bodyStr := string(readRequestBody(t, httpReq))

	if !strings.Contains(bodyStr, `"thinking":{"type":"enabled"}`) {
		t.Fatalf("expected thinking.type=enabled, got: %s", bodyStr)
	}
}

// TestTransformRequest_SetsThinkingEnabledForExtendedEffort (issue #185 同型回归):
// 扩展思考档位 xhigh/max 也应启用 thinking，不能落入 default 而缺失 thinking 字段。
func TestTransformRequest_SetsThinkingEnabledForExtendedEffort(t *testing.T) {
	for _, effort := range []string{"xhigh", "max", "XHIGH", "MAX"} {
		o := &ResponseOutbound{}
		stream := false
		req := &model.InternalLLMRequest{
			Model:           "doubao-seed-1-6-251015",
			ReasoningEffort: effort,
			Messages:        []model.Message{{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}}},
			Stream:          &stream,
		}

		httpReq, err := o.TransformRequest(context.Background(), req, "https://ark.cn-beijing.volces.com/api/v3", "sk-test")
		if err != nil {
			t.Fatalf("TransformRequest error: %v", err)
		}

		bodyStr := string(readRequestBody(t, httpReq))

		if !strings.Contains(bodyStr, `"thinking":{"type":"enabled"}`) {
			t.Fatalf("effort=%q: expected thinking.type=enabled, got: %s", effort, bodyStr)
		}
	}
}

// TestResponsesRequest_ThinkingSerialization 直接验证序列化层行为，
// 覆盖 jsoniter 对 *Thinking + omitempty 的处理：
//   - nil 时 thinking 字段不出现在 JSON 中（issue #181 核心断言）
//   - 非 nil 时输出 {"type":"..."}
func TestResponsesRequest_ThinkingSerialization(t *testing.T) {
	text := "hi"

	// nil Thinking → 不含 thinking 字段
	r1 := ResponsesRequest{
		Input: ResponsesInput{Text: &text},
	}
	b1, err := transformer.Marshal(r1)
	if err != nil {
		t.Fatalf("marshal r1: %v", err)
	}
	if strings.Contains(string(b1), "thinking") {
		t.Fatalf("issue #181: nil Thinking must be omitted, got %s", b1)
	}

	// non-nil Thinking → 输出 thinking.type
	r2 := ResponsesRequest{
		Input:    ResponsesInput{Text: &text},
		Thinking: &Thinking{Type: ThinkingTypeEnabled},
	}
	b2, err := transformer.Marshal(r2)
	if err != nil {
		t.Fatalf("marshal r2: %v", err)
	}
	if !strings.Contains(string(b2), `"thinking":{"type":"enabled"}`) {
		t.Fatalf("expected thinking.type=enabled, got %s", b2)
	}
}
