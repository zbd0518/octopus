---
doc_type: audit-finding
audit: 2026-07-23-request-cli-channel-spoof
finding_id: "bug-01"
nature: bug
severity: P1
confidence: high
suggested_action: cs-issue
status: open
---

# Finding 01：Codex header profile 强制 Accept: application/json，可破坏流式

## 速答

启用 `request_rewrite.header_profile = "codex"` 时，会把上游请求的 `Accept` 固定为 `application/json`；流式场景下 outbound 适配器已设 `text/event-stream` 的话会被覆盖，可能导致上游不以 SSE 返回或客户端解析异常。

## 关键证据

- `internal/transformer/rewrite/config.go:39-44` / `49-55` — ExtraHeaders 含：

```go
"Accept": "application/json",
```

- `internal/relay/relay.go:804-807` — ExtraHeaders **最后** `Header.Set`，覆盖 outbound 与客户端：

```go
if effectiveRewrite != nil && len(effectiveRewrite.ExtraHeaders) > 0 {
    for key, value := range effectiveRewrite.ExtraHeaders {
        outboundRequest.Header.Set(key, value)
    }
}
```

- `internal/transformer/outbound/codex/outbound.go:107-111` — Codex 出站流式会设 `Accept: text/event-stream`（若同路径再叠 ExtraHeaders 同样会被盖掉；当前 UI 上 request_rewrite 主要面向 OpenAI Chat/Response/MiMo）
- `internal/transformer/outbound/openai/chat.go:53-54` — Chat 出站默认 `Accept: application/json`（非流式无差；流式仍依赖上游/客户端是否再改 Accept，但 profile 写死 json）

## 影响

- **触发条件**：渠道开启请求重写 + header_profile=Codex + 客户端发起 stream=true
- **表现**：上游可能拒绝 SSE、返回整包 JSON、或网关按 Accept 协商错误格式；用户表现为「开了 Codex 伪装后流式挂了」
- **范围**：所有启用该 header profile 的 OpenAI 兼容中转渠道

## 建议

- `cs-issue`：`header_profile=codex` 注入时**不要覆盖** `Accept`；或仅在非流式时设置；或流式强制保留 `text/event-stream`
- 回归：流式 + Codex header profile 的 outbound header 单测
