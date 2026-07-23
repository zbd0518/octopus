---
doc_type: feature-ff-note
slug: group-editor-channel-filter
status: implemented
execution_lane: quick
date: 2026-07-23
---

# 分组编辑器：禁用渠道过滤 / 标识 / 渠道分组筛选

## 目标

在「添加/编辑路由分组」时：

1. **候选模型列表默认隐藏** `enabled=false` 的渠道；可选「显示已禁用渠道」。
2. **已选成员列表**对禁用渠道的模型标明「渠道已禁用」，并汇总数量；**不自动移除**历史已选项。
3. 候选列表支持按 **渠道分组（ChannelGroup）** 快速过滤。

## 实现摘要

| 文件 | 变更 |
|------|------|
| `web/src/components/modules/group/editor-filters.ts` | 纯函数：过滤候选、同步 enabled、统计禁用成员 |
| `web/src/components/modules/group/editor-filters.test.ts` | 单元测试 6 条 |
| `web/src/components/modules/group/Editor.tsx` | 接入过滤 UI（分组下拉 + 显示禁用开关）；自动添加共用过滤口径；已选列表汇总条 |
| `web/src/components/modules/group/ItemList.tsx` | 已选成员展示 `渠道已禁用` 徽章 |
| `web/public/locale/{zh_hans,zh_hant,en}.json` | i18n keys |
| `web/package.json` | `test:unit` 加入 `editor-filters.test.ts` |

未改后端 API：`LLMChannel.enabled` 已有；渠道分组通过 `useChannelList` + `useChannelGroupList` 前端 join `channel_id → group_id`。

## 验收行为

- 站点账号禁用 → 投影渠道 `enabled=false` → 候选默认不再出现该渠道。
- 勾选「显示已禁用渠道」后可再看到，并带禁用徽章。
- 编辑已有分组时，历史成员若渠道已禁用：保留 + 琥珀色「渠道已禁用」徽章 + 顶部汇总。
- 渠道分组下拉与渠道页 `ChannelGroup` 一致；选「全部分组」不筛选。
- 自动添加只加入当前过滤后的（默认仅启用）渠道模型。

## 验证

```text
node --experimental-strip-types --test web/src/components/modules/group/editor-filters.test.ts  → 6 pass
pnpm test:i18n  → passed
```

## 未做（有意缩小范围）

- 未扩展 `LLMChannel` 的 `group_id` 字段（前端 join 足够）。
- 未在保存时自动剔除禁用成员。
- 未改运行时路由逻辑（仅编辑器 UX）。
