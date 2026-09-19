package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/transformer/inbound"
	tmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

// TestPrivacyProtectionRequestMaskedUpstreamAndRestoredToClient 端到端验证 issue 020 主路径：
// 真实 outbound 适配器 + httptest 上游（回显请求内容），断言
//   1) 上游收到的请求体里手机号已被替换为占位符（请求脱敏生效）
//   2) 客户端最终拿到的是还原后的原文（响应还原生效）
//
// 走的是与线上一致的适配器路径（TransformRequest/TransformResponse），
// 仅 SSRF 校验用官方测试开关临时放行 loopback。
func TestPrivacyProtectionRequestMaskedUpstreamAndRestoredToClient(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	const phone = "13812345678"
	var upstreamSaw string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		upstreamSaw = string(raw)

		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &payload)
		echo := ""
		if len(payload.Messages) > 0 {
			echo = payload.Messages[0].Content
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-mock", "object": "chat.completion", "created": 1, "model": "mock-model",
			"choices": []map[string]any{{
				"index": 0, "message": map[string]any{"role": "assistant", "content": echo}, "finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()

	// --- 请求侧：与 Handler 中 parseRequest 之后的处理一致 ---
	cfg, pcfg := privacyTestConfig(map[appmodel.PrivacyCategory]appmodel.PrivacyCategoryConfig{
		appmodel.PrivacyCategoryPhone: {Enabled: boolPtr(true), Action: appmodel.PrivacyActionFilter},
	}, nil)

	content := "记录一下我的手机号 " + phone + " 谢谢"
	original := content // applyPrivacyProtection 就地改写 content，先留存原文用于断言
	internalReq := &tmodel.InternalLLMRequest{
		Model:    "mock-model",
		Messages: []tmodel.Message{{Role: "user", Content: tmodel.MessageContent{Content: &content}}},
	}
	pm := newPrivacyPlaceholderMap()
	if blocked, _ := applyPrivacyProtection(internalReq, cfg, pcfg, pm); blocked {
		t.Fatal("filter action must not block")
	}

	// --- 真实出站适配器：构造并发送请求 ---
	out := outbound.Get(outbound.OutboundTypeOpenAIChat)
	if out == nil {
		t.Fatal("openai chat outbound adapter not registered")
	}
	httpReq, err := out.TransformRequest(context.Background(), internalReq, upstream.URL, "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	res, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("upstream request failed: %v", err)
	}
	defer res.Body.Close()

	// --- 断言 1：上游收到的是脱敏后的文本 ---
	if !strings.Contains(upstreamSaw, "⟪PII-") {
		t.Fatalf("upstream request should contain placeholder, got: %s", upstreamSaw)
	}
	if strings.Contains(upstreamSaw, phone) {
		t.Fatalf("upstream request leaked phone number: %s", upstreamSaw)
	}

	// --- 响应侧：outbound 解析 → 还原 → 客户端可见内容 ---
	internalResp, err := out.TransformResponse(context.Background(), res)
	if err != nil {
		t.Fatalf("TransformResponse() error = %v", err)
	}
	if got := *internalResp.Choices[0].Message.Content.Content; !strings.Contains(got, "⟪PII-") {
		t.Fatalf("upstream echo should still contain placeholder before restore, got %q", got)
	}

	restorePrivacyPlaceholders(internalResp, pm)
	got := *internalResp.Choices[0].Message.Content.Content
	if got != original {
		t.Fatalf("client-visible content = %q, want restored %q", got, original)
	}
	if !strings.Contains(got, phone) {
		t.Fatalf("restored content should contain original phone, got %q", got)
	}
}

// TestTransformStreamDataRestoresPlaceholder 验证处理链路上的实际调用点：
// transformStreamData（流式 chunk 转换入口）确实会触发占位符还原。
func TestTransformStreamDataRestoresPlaceholder(t *testing.T) {
	const phone = "13812345678"
	pm := newPrivacyPlaceholderMap()
	ph := pm.mask(phone)

	// transformStreamData 接收的是 SSE reader 已剥离 "data: " 前缀的裸 JSON 事件体
	sse := `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m",` +
		`"choices":[{"index":0,"delta":{"role":"assistant","content":"号码 ` + ph + ` 已记录"}}]}`

	ra := &relayAttempt{
		relayRequest: &relayRequest{
			internalRequest: &tmodel.InternalLLMRequest{Model: "m"},
			inAdapter:       inbound.Get(inbound.InboundTypeOpenAIChat),
		},
		outAdapter:    outbound.Get(outbound.OutboundTypeOpenAIChat),
		privacyStream: pm.newStreamRestorer(),
	}
	if ra.privacyStream == nil {
		t.Fatal("expected non-nil stream restorer when placeholders exist")
	}

	out, _, err := ra.transformStreamData(context.Background(), sse)
	if err != nil {
		t.Fatalf("transformStreamData() error = %v", err)
	}
	if strings.Contains(string(out), "⟪PII-") {
		t.Fatalf("stream output leaked placeholder: %s", out)
	}
	if !strings.Contains(string(out), phone) {
		t.Fatalf("stream output should contain restored phone, got: %s", out)
	}
}
