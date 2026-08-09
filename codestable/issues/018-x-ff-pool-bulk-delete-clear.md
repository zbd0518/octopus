---
title: 增加号池账号批量删除与一键清空
status: closed
type: ff
---

## 做了什么

为号池账号增加“批量删除”和“一键清空”操作。

## 改了哪些

- 后端新增 `POST /api/v1/pool/:id/account/batch-delete`，按勾选账号删除。
- 后端新增 `POST /api/v1/pool/:id/account/clear`，清空指定账号池的全部账号。
- 删除操作复用账号删除钩子，清理调度器中的账号状态。
- 前端批量操作栏增加批量删除；账号池工具栏增加一键清空。
- 两种删除操作均需要浏览器确认，并在成功后清除选择、刷新列表。
- 补充中英文及繁体中文文案。

## 怎样验证

- `go test ./internal/op/pool ./internal/server/handlers`
- `go test ./...`
- `pnpm --dir web test:i18n`
- `pnpm --dir web test:unit`
- ESLint 检查号池相关前端文件
- `git diff --check`

## 对 codestable/ 的影响

无稳定业务规则变化；新增快改记录。
