---
kind: issue
title: "Claude Code Anthropic Header Profile"
type: ff
status: closed
created: 2026-11-05
---

在渠道请求重写高级设置中新增 `claude_code` Header Profile，仅允许 Anthropic 渠道，注入 `User-Agent: claude-cli` 与 `X-App: cli`；不改请求体或 Anthropic 协议/认证头，且渠道 `custom_header` 优先级更高。

- 改动：后端校验、Header resolver、relay 与分组模型探测的合并顺序及回归测试；前端 Anthropic-only 选项、stale 值归一化、三语文案；中英文 README；`codestable/issues/025-x-ff-claude-code-header-profile.md`
- 验证：`go test ./...`、`pnpm test`（397/397）、分组探测定向测试、channel helper、i18n、`tsc --noEmit` 与改动文件 ESLint 通过；`git diff --check` 通过。全仓 `pnpm --dir web lint` 仍有既有 `analytics/Utilization.tsx` 3 处 `no-explicit-any` 错误。
- codestable：无影响；当前稳定 spec 无 Header Profile 约束，本次仅新增快改记录。

顺手发现：全仓 lint 的既有错误位于 `web/src/components/modules/analytics/Utilization.tsx:162,177,190`，不在本次范围。
