---
doc_type: feature-ff-note
slug: channel-delete-purge-group-items
status: implemented
execution_lane: quick
date: 2026-07-24
---

# 删除渠道时同步清理分组已选渠道

## 目标

删除渠道时，路由分组中已选中的该渠道模型项需要一并删除，避免分组缓存继续保留已删除渠道的成员。

## 实现摘要

| 文件 | 变更 |
|------|------|
| `internal/op/channel.go` | `ChannelDel` 删除前查询受影响 `group_id`，删除后刷新这些分组缓存；`getAffectedGroupIDs` 改为真实 DB 查询。 |
| `internal/server/handlers/channel.go` | HTTP 删除入口改为 `op.ChannelDel`，不再直接 `ch.Delete` + 仅清 stats。 |
| `internal/task/channel_expire.go` | 过期删除入口改为 `op.ChannelDel`，保证缓存 / hooks 与手动删除一致。 |
| `internal/op/channel_group_test.go` | 新增回归测试：删除渠道后 DB 中对应 `GroupItem` 被删除，`GroupGet` 缓存同步为空。 |

## 验收行为

- 删除渠道时，`group_items.channel_id = 被删渠道 ID` 的成员会被删除。
- 删除后受影响分组缓存会刷新，页面 / 路由读取分组时不再看到被删渠道成员。
- HTTP 删除、过期任务删除、`ChannelDelManaged` / planprovider 均统一走 `op.ChannelDel`。
- 既有渠道分组删除规则不变。

## 验证

```text
go test ./internal/op -run "TestChannel(GroupDeleteRules|DelRemovesGroupItemsAndRefreshesGroupCache)$"  → passed
go test ./internal/op ./internal/server/handlers ./internal/task  → passed
```
