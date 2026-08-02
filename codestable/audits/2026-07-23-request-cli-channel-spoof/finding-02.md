---
doc_type: audit-finding
audit: 2026-07-23-request-cli-channel-spoof
finding_id: "security-01"
nature: security
severity: P2
confidence: medium
suggested_action: cs-issue
status: open
---

# Finding 02：CLI 伪装依赖可伪造的 User-Agent/Origin，无上游身份证明

## 速答

项目通过注入 Codex Desktop 风格的 `User-Agent` / `Origin` / `Referer`（及可选 `custom_header`）让 Claude Code 等客户端「看起来像」Codex；这只对**仅校验弱客户端指纹**的中转有效，不构成安全边界，也不能替代 OAuth / 设备绑定。

## 关键证据

- `internal/transformer/rewrite/config.go:39-55` — 硬编码 Desktop UA + chat.openai.com Origin/Referer
- `internal/relay/relay.go:788-808` — 客户端头透传后可被 custom_header / ExtraHeaders 覆盖；`User-Agent` **不在** hop-by-hop 过滤列表（`internal/relay/type.go:127-152`）
- `internal/transformer/outbound/codex/outbound.go:101-106` — 真 Codex 订阅路径另写 `originator: codex_cli_rs` + account 绑定；与 header profile 是两条线

## 影响

- **正面（产品能力）**：用户可用 Claude Code 调用「只认 Codex 客户端指纹」的第三方中转
- **风险**：
  1. 文档/UI 若暗示「完整兼容官方 Codex 限制」会误导
  2. 中转若升级为签名校验，现有配置会静默失败
  3. 伪装成功后可能违反部分上游 ToS（运营/合规风险，非代码漏洞）

## 建议

- 文档 / UI 标明：header profile 仅模拟 **HTTP 指纹**，不保证通过强校验
- 可选：`cs-issue` 在开启 Codex header profile 时加管理端提示文案
- 不建议把该能力包装成「安全绕过」
