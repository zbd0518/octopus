---
doc_type: feature-review
slug: site-overview-platform-checkin-filters
status: passed
reviewer: subagent
lane_a: completed
lane_a_ref: ac3dd8bbb95e21548
lane_b: unavailable
entry_source: feat-ff
date: 2026-07-23
blocking: 0
important: 0
---

# 站点总览筛选 — 代码审查

## 范围

- Quick feature：`site-overview-platform-checkin-filters`
- Spec：`site-overview-platform-checkin-filters-ff-note.md`
- Diff：`web/src/components/modules/site/*` 平台筛选 + 签到筛选 UI；`web/package.json` test:unit

## 环节

| 环节 | 状态 | 说明 |
|------|------|------|
| A 独立 Task agent | completed | ref `ac3dd8bbb95e21548`，verdict passed |
| B OCR 行级扫描 | unavailable | CLI 启动后无有效输出；不阻塞 gate |

## Design fit

对照 ff-note 验收点均已实现：平台多选 chip、仅展示实际出现平台、与签到/搜索叠加、签到标签保留、清空三处联动、forced 跳转旁路。未越界（无后端 API / 无 i18n / 未统一全站 PLATFORM_LABELS）。

单元测试：`site-filters.test.ts` → 6 pass。

## Findings

### blocking

无

### important

无

### nit

1. **`platformFilters` / label map 未绑定 `SitePlatform` 类型**  
   - 证据：`ui-store.ts` 为 `string[]`；`site-filters.ts` 为 `Record<string, string>`，与枚举值手工对齐。  
   - 影响：新增平台时编译器不会提示 label 漏改。  
   - 边界：可接受的 node:test 可测性权衡；不阻塞。

2. **失效平台筛选可残留**  
   - 证据：chip 列表来自当前 `sites`，选中值存 zustand；平台站点全消失时 chip 没了但 filter 仍非空。  
   - 影响：边缘 UX；「清空/全部」可恢复。可选 prune。

3. **引号风格不一致**（`ui-store` 单引号 vs 新文件双引号）— 无行为影响。

### suggestion

1. 平台展示名在 `site-filters` / `SiteEditDialog` / `site-channel/utils` 三处并存，后续可抽公共 labels。  
2. 可把 `visibleSites` 组合过滤抽纯函数再测；本轮非必须。

### residual-risk

- 组合场景（平台 ∩ 签到 ∩ 搜索、forced 跳转）建议手测。  
- OCR 未产出结果，未能合并 OCR High findings；本地对抗审查未发现正确性/安全问题。

## 总评

| 项 | 结论 |
|----|------|
| **verdict** | **passed** |
| **blocking** | 0 |
| **important** | 0 |
| **spec fit** | 满足 |
| **下游** | 可收尾提交；无需 review-fix |

## 验证命令

```text
node --no-warnings --experimental-strip-types --test web/src/components/modules/site/site-filters.test.ts  → 6 pass
```
