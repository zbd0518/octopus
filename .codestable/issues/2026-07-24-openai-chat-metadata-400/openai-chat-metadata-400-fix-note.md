---
doc_type: issue-fix-note
issue: 2026-07-24-openai-chat-metadata-400
status: completed
issue_path: fast-track
created: 2026-07-24
---

# 修复记录：Chat Completions 出站剥离 metadata

## 根因

Claude Code 的 Anthropic Messages 入站请求会携带 `metadata.user_id`，入站转换在 `internal/transformer/inbound/anthropic/messages.go` 中把它映射到内部请求 `InternalLLMRequest.Metadata`。

Responses 出站已经在 `internal/transformer/outbound/openai/response.go` 清空 `Metadata`，避免第三方 OpenAI-compatible Responses 上游返回 `400 Argument not supported: metadata`；但 Chat Completions 出站路径 `internal/transformer/outbound/openai/chat.go` 只清理了 reasoning / include / extra_body 等兼容字段，没有清空 `Metadata`，导致 `metadata` 被序列化进 `/v1/chat/completions` 请求体并被部分兼容上游拒绝。

## 改动

- 在 `SanitizeRequestForOpenAICompat` 中新增 `request.Metadata = nil`，让所有 OpenAI-compatible Chat Completions 出站请求默认不携带 `metadata`。
- 保留原始 `InternalLLMRequest.Metadata`：`ChatOutbound.TransformRequest` 已通过 `CloneRequestForOpenAICompat` 复制请求，清理只发生在出站副本上。
- 新增 `TestChatOutboundTransformRequest_StripsMetadata`，覆盖 Claude Code / grok-4.5 场景：带 `Metadata` 的内部请求经过 Chat 出站转换后，body 中不包含 `metadata` key。

## 验证

```text
go test ./internal/transformer/outbound/openai/ -count=1
ok  	github.com/lingyuins/octopus/internal/transformer/outbound/openai	1.050s
```

## 遗留风险

- 未发现需要产品判断的遗留风险。
- 本次只修 Chat Completions 出站兼容性；其它渠道日志中的超时 / 502 属于上游可用性问题，不在本 issue 范围。