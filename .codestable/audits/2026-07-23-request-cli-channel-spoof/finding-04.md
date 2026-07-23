---
doc_type: audit-finding
audit: 2026-07-23-request-cli-channel-spoof
finding_id: "maintainability-02"
nature: maintainability
severity: P2
confidence: medium
suggested_action: cs-refactor
status: open
---

# Finding 04：前端 ChannelType 未暴露 Codex 出站类型，与后端枚举不一致

## 速答

后端已有 `OutboundTypeCodex`（ChatGPT Codex 订阅出站，写死 `originator` 等），前端 `ChannelType` 枚举只到 Cloudflare=7，未见 Codex；用户若期望在 UI 里建「真 Codex 订阅渠道」与「header 伪装」可能混淆，且类型面不一致。

## 关键证据

- 后端：`internal/transformer/outbound/register.go:27` — `OutboundTypeCodex`
- 后端出站：`internal/transformer/outbound/codex/outbound.go:101-106` — `originator: codex_cli_rs` 等
- 前端：`web/src/api/endpoints/channel.ts:11-20` — `ChannelType` 0–7，无 Codex
- 前端 request_rewrite 支持类型：`Form.tsx:105-106` — 仅 OpenAIChat / MiMoChat / OpenAIResponse
- 前端 header profile：`channel.ts:37-40` — `Codex = 'codex'`（这是 **header 伪装**，不是渠道类型）

## 影响

- 产品语义：`header_profile=codex` vs `channel type=codex` 两条能力，UI 只突出前者
- 若后端 API 已支持创建 type=Codex 渠道，前端无法对称选择（需确认 handlers；本 finding 置信度 medium）
- 用户文档若只说「选 Codex」易歧义

## 建议

- 澄清文档：  
  - **Header Codex** = 中转指纹伪装  
  - **Channel type Codex** = ChatGPT 订阅 OAuth 出站  
- 若产品需要 UI 创建订阅渠道：补齐前端枚举与表单；否则在 API 层限制创建避免孤儿类型
