---
doc_type: feature-ff-note
feature: channel-detail-route-groups
date: 2026-07-25
requirement:
tags:
  - channel
  - detail
  - group
  - jump
execution_lane: quick
status: implemented
---

## 做了什么

渠道详情查看态新增「所属路由分组」：按路由 `Group` 聚合展示组名 + 本渠道贡献的模型；未加入时明确空态。点击组名跳转到分组页并自动打开该分组编辑器（不在详情内改成员）。

## 改了哪些

- `web/src/components/modules/channel/route-groups.ts` — `collectChannelRouteGroups` 纯 join
- `web/src/components/modules/channel/route-groups.test.ts` — 3 条单测
- `web/src/components/modules/channel/CardContent.tsx` — 查看态 section + jump
- `web/src/stores/jump.ts` — `group-editor` jump target
- `web/src/components/modules/group/index.tsx` — 消费 jump：切卡片视图、置顶、滚动
- `web/src/components/modules/group/GroupListItem.tsx` — autoOpen 编辑器
- `web/public/locale/{zh_hans,zh_hant,en}.json` — i18n
- `web/package.json` — `test:unit` 纳入 route-groups 测试

## 怎么验证的

```text
node --experimental-strip-types --test web/src/components/modules/channel/route-groups.test.ts  → 3 pass
pnpm test:i18n  → passed
```

## 顺手发现（可选，不阻塞）

- `channel-card` jump target 在 `jump.ts` 已定义且有多处 request，但渠道列表页尚未消费（打开指定渠道详情）；与本次无关。
