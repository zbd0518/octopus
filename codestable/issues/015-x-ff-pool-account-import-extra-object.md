---
title: 号池账号导入兼容 object extra
status: closed
type: ff
---

## 做了什么

修复号池账号批量导入时 `extra` 为 JSON 对象导致解析失败的问题，并让导出格式与导入格式保持兼容。

## 改了哪些

- `internal/op/pool/credentials.go`
  - 将导入临时结构的 `extra` 从 `string` 改为 `json.RawMessage`。
  - 同时接受字符串、对象及其他合法 JSON 值；对象以 JSON 字符串形式写入 `PoolAccount.Extra`。
- `internal/server/handlers/pool.go`
  - 导出时将合法的 `extra` JSON 还原为对象，避免导出后再次导入触发类型冲突。
- `web/src/api/endpoints/pool.ts`
  - 同步导出类型，允许 `extra` 为任意 JSON 值。
- `internal/op/pool/credentials_import_test.go`
  - 增加对象、字符串和非对象 JSON `extra` 的导入回归测试。

## 怎样验证

- `go test ./internal/op/pool ./internal/server/handlers ./internal/model`
- `git diff --check`

两项均通过。工作树中原有的 relay、transformer 等未提交改动未修改。

## 对 codestable/ 的影响

无稳定业务规则变化；新增快改记录。
