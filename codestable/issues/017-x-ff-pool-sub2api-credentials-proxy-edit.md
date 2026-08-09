---
title: 修复号池 Sub2API OAuth 导入与账号编辑
status: closed
type: ff
---

## 做了什么

修复号池批量导入 Sub2API OpenAI OAuth 数据、账号编辑凭据展示，以及账号代理选择修改失效问题。

## 改了哪些

- 兼容 `credentials.chatgpt_account_id`，导入后转换为内部 OpenAI OAuth 所需的 `account_id`，并保留原始字段。
- `extra` 保持对象/字符串导入兼容，保留未知字段，避免 Sub2API 额度扩展字段丢失。
- 编辑账号时显示当前返回的脱敏凭据；未修改时不会把脱敏值写回。
- 修正编辑账号代理配置的清空/切换更新逻辑，`null` 可以真正落库。
- 保留并补充 OAuth 账号指纹兼容和 OpenAI 请求头兼容。

## 怎样验证

- `go test ./internal/model ./internal/op/pool ./internal/relay ./internal/server/handlers`
- `go test ./...`
- `pnpm --dir web test:i18n`
- `pnpm --dir web test:unit`
- ESLint 检查目标前端文件
- 前端生产构建及 Windows x64 可执行文件构建通过

产物：`build/bin/octopus-windows-x86_64.exe`

## 对 codestable/ 的影响

无稳定业务规则变化；新增快改记录。
