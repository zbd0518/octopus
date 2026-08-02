---
doc_type: feature-review
slug: channel-delete-purge-group-items
status: passed
reviewer: subagent
date: 2026-07-24
---

# 代码审查：删除渠道时同步清理分组已选渠道

## 审查结论

`review_state: passed`

首轮独立 review（worktree 隔离）曾指出：

1. `getAffectedGroupIDs` 仍返回空；
2. 必须在删除 `GroupItem` **之前**采集 affected groups；
3. HTTP / 过期任务绕过 `ChannelDel`；
4. 回归测试缺失。

上述问题均已在主工作区修复并复验。

## 复验清单

| 检查项 | 结论 |
|--------|------|
| DB 删除 `group_items` | `channel.Delete` 事务内 `WHERE channel_id = ?` 删除 |
| 缓存刷新时序 | `ChannelDel` 在 `channel.Delete` **之前** `Pluck` 受影响 `group_id`，删除后 `groupRefreshCacheByID` |
| 删除入口统一 | handler / channel_expire / managed / planprovider 均走 `op.ChannelDel` |
| 回归测试 | `TestChannelDelRemovesGroupItemsAndRefreshesGroupCache` 覆盖 DB + 缓存 |
| 无多余范围 | 未改路由语义、未改渠道分组（ChannelGroup）规则 |

## Findings

- **blocking**: 无
- **important**: 无
- **minor**: `ChannelDel` 末尾对 key cache 的二次清理与 `channel.Delete` 内清理略有冗余（无害，未改）

## 验证

```text
go test ./internal/op -run "TestChannel(GroupDeleteRules|DelRemovesGroupItemsAndRefreshesGroupCache)$"  → passed
go test ./internal/op ./internal/server/handlers ./internal/task  → passed
```
