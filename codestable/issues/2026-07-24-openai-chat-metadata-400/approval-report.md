---
doc_type: approval-report
issue: 2026-07-24-openai-chat-metadata-400
---

# Approvals

## issue-fast-path

- **status**: approved
- **ref**: approval-report.md#issue-fast-path
- **reason**: ConfirmFixPlan
- **summary**: Chat Completions 出站剥离 `metadata`，与 Responses 出站已有行为对齐

### 根因（file:line）

| 位置 | 说明 |
|------|------|
| `internal/transformer/inbound/anthropic/messages.go:58-59` | Claude Code 入站：`anthropicReq.Metadata.UserID` → `chatReq.Metadata["user_id"]` |
| `internal/transformer/outbound/openai/chat.go:90-118` | `SanitizeRequestForOpenAICompat` 未清空 `request.Metadata`，随后 `Marshal` 带入 body |
| `internal/transformer/outbound/openai/response.go:39` | Responses 出站已 `responsesReq.Metadata = nil`（对照：同类问题已修） |

### 修复方案

1. 在 `SanitizeRequestForOpenAICompat`（或 `ChatOutbound.TransformRequest` 内 sanitize 之后）将 `request.Metadata = nil`。
2. 新增 `TestChatOutboundTransformRequest_StripsMetadata`，构造带 `Metadata` 的请求，断言序列化 body 无 `"metadata"` key。
3. 不改入站结构、不改 `InternalLLMRequest` 字段定义；仅出站 wire 剥离。

### 风险

- **低**：官方 OpenAI Chat Completions 支持 `metadata`，省略后功能/计费不受影响；本字段对推理无作用。
- 若未来有依赖出站 `metadata` 的观测/计费上游，需另开 feature；当前 Claude Code 兼容场景优先。

### 验证

- `go test ./internal/transformer/outbound/openai/ -count=1`
- 可选：Claude Code 再打一次经 `default-Chat` 的请求，确认不再出现 `Argument not supported: metadata`。

### 请确认

请回复其一：

- **批准** → 进入 fix，直接改代码并写 fix-note  
- **拒绝** → 改走 standard analyze  
- **修订** → 说明要改的方案点，我更新 draft 后再确认

## issue-fix-completion

- **status**: approved
- **ref**: approval-report.md#issue-fix-completion
- **reason**: ConfirmFixCompletion
- **summary**: Chat Completions 出站已剥离 metadata；单测通过；独立 code review 通过（subagent+ocr）

### 完成摘要

| 项 | 状态 |
|---|---|
| 根因修复 | `SanitizeRequestForOpenAICompat` 末尾 `request.Metadata = nil` |
| 回归测试 | `TestChatOutboundTransformRequest_StripsMetadata` |
| 包测试 | `go test ./internal/transformer/outbound/openai/ -count=1` 通过 |
| code review | `openai-chat-metadata-400-review.md` status=passed, reviewer=subagent+ocr |

### residual-risk（不阻塞）

- 既有 `CloneRequestForOpenAICompat` 浅拷贝 Message 指针字段问题（另开 issue）
- 可选加强 stream 路径与 body 正向断言测试

### 请确认

请回复其一：

- **批准完成** → issue 闭环，可按需提交本 issue 归因改动
- **拒绝** → 说明原因，阻塞关闭
- **修订** → 说明要补的修复点，回 fix 再改