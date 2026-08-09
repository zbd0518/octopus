---
title: 修复路由分组推理内容缓冲策略编辑回显
status: closed
type: ff
---

## 做了什么

修复路由分组编辑页未回填“推理内容缓冲策略”，导致保存后重新打开始终显示第一个选项的问题。

## 改了哪些

- `web/src/components/modules/group/GroupListItem.tsx`
  - 编辑器初始化时传入 `reasoning_buffer_strategy`。
  - 编辑提交时比较并提交策略字段。
  - 在相关回调依赖中加入该字段。
- `internal/op/group/group_category_test.go`
  - 增加策略写入数据库及缓存返回值的回归断言。

## 怎样验证

- `go test ./internal/op/group ./internal/relay`
- `pnpm --dir web test:unit`
- `pnpm --dir web exec eslint src/components/modules/group/GroupListItem.tsx`
- `git diff --check`

均通过。

## 对 codestable/ 的影响

无稳定业务规则变化；新增快改记录。
