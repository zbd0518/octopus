---
kind: issue
title: "Responses 截断参数透传与渠道默认值"
type: ff
status: closed
created: 2026-11-06
---

修复 Responses 转换丢失 `truncation` 的问题，保留客户端明确的 `auto` / `disabled`，插件改写请求后也继续保留。客户端未设置时，渠道可通过 `param_override: {"truncation":"auto"}` 启用默认截断；只接受 `auto` / `disabled`，客户端明确值优先，双方未设置时不主动开启截断。

- 改动：`internal/transformer/model/model.go`、Responses 入站/出站结构与转换保存可选参数；`internal/relay/rewrite.go` 按现有客户端优先规则补齐渠道默认值。Chat 出站兼容清理移除该专用字段，沿用现有克隆机制隔离渠道尝试，不增加上下文超限重试。
- 验证：回归测试先复现丢参再通过，覆盖流式请求、改写开关、客户端优先、无效配置、渠道间隔离、Chat 转换及插件重编码；`go test ./...` 全部通过，`pnpm test:web` 的 i18n 与 399 项单测全部通过。
- codestable：已同步 `spec/index.md` 的 Responses 截断参数契约。此修复需要更新运行中的后端；未连接真实上游验证长上下文请求，自动截断仍取决于客户端/渠道设置及上游支持，会删除较早的历史项目。
