---
doc_type: feature-ff-note
slug: site-overview-platform-checkin-filters
status: complete
execution_lane: quick
date: 2026-07-23
---

# 站点总览：平台类型筛选 + 签到状态过滤

## 目标

站点管理「总览」筛选能力补齐：

1. **按平台类型筛选**站点列表（多选 chip，空 = 全部）。
2. 保留并明确展示既有 **签到执行状态** 筛选（全部 / 成功 / 失败 / 未执行 / 禁用），用于过滤是否已执行过签到。
3. 「清空筛选」同时清掉搜索、平台、签到三类条件。

## 实现摘要

| 文件 | 变更 |
|------|------|
| `web/src/components/modules/site/site-filters.ts` | 纯函数：平台匹配、去重收集、计数、展示名 |
| `web/src/components/modules/site/site-filters.test.ts` | 单元测试 6 条 |
| `web/src/components/modules/site/ui-store.ts` | 新增 `platformFilters` 状态与 setter |
| `web/src/components/modules/site/CheckinPanel.tsx` | 总览 UI：平台 chip 行 + 签到 chip 行（加「平台/签到」标签） |
| `web/src/components/modules/site/index.tsx` | `visibleSites` 接入平台过滤；清空/徽章联动；平台标签共用 `platformLabel` |
| `web/package.json` | `test:unit` 加入 `site-filters.test.ts` |

未改后端 API：站点列表已有 `platform` / 签到字段，全部前端本地过滤。

## 验收行为

- 总览出现「平台」chip：仅展示当前站点列表中实际出现的平台；可多选；选「全部」清空平台筛选。
- 选中平台后，列表只保留对应平台站点；与签到状态筛选、搜索可叠加。
- 「签到」chip：`未执行` = 今日未签到成功路径（既有 `deriveCheckinStatus` idle）；`成功/失败/禁用` 语义不变。
- 「清空筛选」清除搜索词 + 平台 + 签到筛选。
- 跳转强制展示目标站点时，不受平台/签到过滤隐藏。

## 验证

```text
node --no-warnings --experimental-strip-types --test web/src/components/modules/site/site-filters.test.ts  → 6 pass
```

`tsc --noEmit` 仓库内已有无关测试文件类型错误（alert/forms、group-progress 等），本次站点改动未新增 site 相关报错。

## 未做（有意缩小范围）

- 未改后端 list API / 查询参数。
- 未做平台筛选的 i18n（总览既有文案为中文硬编码，与签到 chip 一致）。
- 未把 `PLATFORM_LABELS` 从 `SiteEditDialog` 等处统一抽取（仅列表/总览路径共用 `site-filters`）。
