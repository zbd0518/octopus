---
doc_type: feature-ff-note
slug: group-editor-member-status
status: implemented
execution_lane: quick
date: 2026-07-24
---

# 分组编辑器：成员静态风险状态

## 做了什么

在「添加/编辑路由分组」左右两侧展示静态成员风险：渠道已禁用、无可用 Key、上游健康风险（成功率标签），右侧汇总条扩展三类计数。不发测活、不禁添加、不改运行时。

## 改了哪些

- `web/src/components/modules/group/editor-member-status.ts` — 纯函数：Key 判定、健康阈值、风险汇总
- `web/src/components/modules/group/editor-member-status.test.ts` — 单测 5 条
- `web/src/components/modules/group/Editor.tsx` — 左右徽章 + join `useChannelList.keys` + 汇总
- `web/src/components/modules/group/ItemList.tsx` — `showStaticRisks` / 无 Key / 健康徽章
- `web/public/locale/{zh_hans,zh_hant,en}.json` — i18n
- `web/package.json` — `test:unit` 纳入新单测

## 怎么验证的

```text
node --experimental-strip-types --test web/src/components/modules/group/editor-member-status.test.ts  → pass
node --experimental-strip-types --test web/src/components/modules/group/editor-filters.test.ts  → pass
pnpm --dir web test:i18n  → passed
```

## review-fix

- `syncMembersChannelEnabled` 按 channel_id+name 同步 `upstream_metrics` 等，避免左右健康风险漂移
- channel list 未 fetch 完成前不展示「无可用 Key」（区分未知与已确认）
- ItemList 复用 `channelHasNoEnabledKey`；补 NaN/Infinity 阈值单测

## 顺手发现

- 详情页测活 `availability` 与本静态风险并存；文案刻意避开「可用/不可用」。
