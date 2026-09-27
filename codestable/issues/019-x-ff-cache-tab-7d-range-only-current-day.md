---
kind: issue
title: "分析中心缓存页签 7d/30d 统计只看当天"
type: ff
status: closed
created: 2026-09-07
---

# 分析中心缓存页签 7d/30d 统计只看当天

> **读者：** 以后搜到这条时——「改了啥、怎么信、动没动制度记忆」。

分析中心 → 缓存页签，选中 7 天/30 天后，Provider Prompt 缓存趋势仍只显示最近 24 小时（甚至只有当天数据），「每天的统计没有保存」。

## 根因（三层都断）

1. **前端没传 range**：`analytics/index.tsx` 渲染 `<Cache />` 时未传 `range`，其他页签（Utilization/ChannelModel/Latency）都传了；`useOpsCacheStatus()` 也不接受 range 参数。
2. **后端接口不解析 range**：`GET /api/v1/ops/cache` 无 range 参数。
3. **后端硬编码 24h 窗口**：`buildOpsProviderPromptCacheSummary` 用 `opsHourlyWindowStart`（now−23h 截到整点）+ 24 个小时桶，`loadOpsProviderPromptCacheLogs` 只查 `time >= since`，永远只有当天。

语义缓存（SemanticCacheView）的 hits/misses 是进程内存累计计数（`semantic_cache.Stats()`），天然无按天历史——本次保持现状。

## 改了哪些

**后端**

- `internal/server/handlers/ops.go`：`getOpsCache` 解析 `range` 查询参数（复用 `parseAnalyticsRange`，缺省 7d），传给 `ops.OpsCacheStatusGet(ctx, r)`。
- `internal/op/ops/ops.go`：
  - `OpsCacheStatusGet(ctx, r model.AnalyticsRange)` 签名加 range。
  - `buildOpsProviderPromptCacheSummary(ctx, r)` 改为按 range 分桶：新增 `opsRangeWindow(now, loc, r)` 返回 `(start, bucketHours, bucketCount)`：
    - `1d` → 过去 24 小时，小时桶 ×24（与历史行为一致）
    - `7d/30d/90d` → 当天 0 点回溯 N−1 天，天桶
    - `ytd` → 今年 1/1 起按天，`all` → 上限 366 个天桶
  - 结果缓存改为按 range 分 key（`map[AnalyticsRange]entry`，60s TTL），避免不同周期互相污染。
  - `buildOpsProviderPromptCacheSummaryFromLogs(logs, start, bucketHours, bucketCount)` 加桶宽参数，日桶聚合按天。
- 内部调用方 `OpsHealthStatusGet`、telemetry 传 `AnalyticsRange1D`（它们只读语义缓存字段，不受影响）。
- `internal/op/ops.go` 旧包装函数签名同步加 range。
- `internal/op/ops/ops_test.go`：新增 `TestOpsRangeWindow_DayBucketsForMultiDayRanges`（1d/7d/30d/ytd/all 窗口断言）、`TestBuildOpsProviderPromptCacheSummaryFromLogs_DayBuckets`（同日日志聚合到同一个天桶）；原有用例传 `(start, 1, 24)` 保持小时桶语义。

**前端**

- `web/src/api/endpoints/ops.ts`：`useOpsCacheStatus(range = '7d')` 接受 range 并加入 queryKey。
- `web/src/components/modules/analytics/index.tsx`：`<Cache range={range} />` 传递 range。
- `web/src/components/modules/ops/Cache.tsx`：`Cache({ range })` 传给 hook；`ProviderPromptCacheView` 按 range 决定趋势 x 轴刻度——1d 小时桶显示 `HH:mm`，多日天桶只显示 `月/日`（新增 `formatUnixDay` 于 `analytics/shared.tsx`）。
- `hero.tsx` 等调 `useOpsCacheStatus()` 处默认 7d，今日缓存复用 Token 逻辑用 `timestamp >= 今日 0 点` 过滤，天桶下仍只命中今天的桶，语义不变。

## 怎样验证

- `go build ./internal/...`、`go vet ./internal/op/... ./internal/server/handlers/...` 通过
- `go test ./internal/op/...` 全绿；`go test ./internal/server/handlers` 绿
- `go test ./internal/server` 失败为**既有**（pool 账号批量删除/清空路由不在 audit 白名单），stash 后复现，与本次无关
- 前端 `npx tsc --noEmit` 新增文件无错误（既有报错全在测试文件，与本次无关）；eslint 仅 3 条既有警告（未使用 import 等）
- 前端单测 `cache-format.test.ts`、`time.test.ts` 通过

## 语义缓存说明（未改）

语义缓存 hits/misses 仍是进程累计值（重启清零），无按天持久化。若需「语义缓存每日命中率历史」，需另行设计按天聚合（如基于 `relay_logs.semantic_cache_hit`），不在本次范围。

## 对 codestable/ 的影响

- 无稳定业务规则变化；新增快改记录。
- `spec/index.md` 观测与告警段可补充「缓存页签支持按 range 查看多日趋势」，待下次规格维护时一并更新。