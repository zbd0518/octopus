---
kind: issue
title: "Codex Desktop Header 预设版本更新"
type: ff
status: closed
created: 2026-11-06
---

渠道新增、编辑继续使用 `header_profile: codex`；统一后端预设的 User-Agent 从引擎 `0.131.0` / Desktop `26.519.21041` 更新为同一官方 Windows 安装包中的引擎 `0.166.0-alpha.6` / Desktop `26.1029.61003`。

- 改动：`internal/transformer/rewrite/config.go` 更新两个版本号并注明核验日期，沿用 Desktop 标识及其余 Header；已有渠道无需迁移数据，更新后端后生效，渠道自定义 Header 仍可覆盖预设。
- 来源：2026-11-06 核验 [OpenAI Windows 部署文档](https://developers.openai.com/codex/enterprise/windows-deployment) 指向的 [官方 x64 安装包](https://persistent.oaistatic.com/codex-app-prod/ChatGPT-x64.msix)。`AppxManifest.xml` 的 Windows 包版本为 `26.1029.7803.0`；`app/resources/app.asar` 内 `package.json` 与 `owl-app.ini` 的应用版本为 `26.1029.61003`；包内 `app/resources/codex.exe` 的 CLI、App Server 和 User-Agent 构建版本字符串均为 `0.166.0-alpha.6`。通过 HTTP Range 读取包元数据与版本字符串，未安装或运行该包。
- 验证：`go test ./internal/transformer/rewrite ./internal/relay ./internal/helper` 三个包全部通过；`gofmt` 与 `git diff --check` 通过。
- codestable：无影响；当前稳定 spec 无此预设版本约束。本次只更新预设版本，不代表已验证上游客户端白名单放行。
