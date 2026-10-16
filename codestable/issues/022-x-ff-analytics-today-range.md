---
kind: issue
title: "分析中心新增「今天」时间范围，1天改名24小时"
type: ff
status: closed
created: 2026-10-16
---

# 分析中心新增「今天」时间范围，1天改名24小时

> **读者：** 以后搜到这条时——「改了啥、怎么信、动没动制度记忆」。

分析中心顶部时间范围新增「今天」（今日 0 点起至当前），置于「24小时」之前；原「1天」按钮文案改为「24小时」，明确其为滚动 24 小时口径（`now-23h` 截到整点、跨两天），与「今天」的自然日口径区分开。

- 改动：
  - 后端 `internal/model/analytics.go` — 新增 `AnalyticsRangeToday = "today"` 并纳入 `ParseAnalyticsRange`。
  - 后端 `internal/op/analytics/analytics.go` — `analyticsStartTime` 增加 `today → 今日 0 点`；新增 `analyticsUsesRelayLogPrimary`（1d/today 均以 relay_logs 为主、stats_daily 补缺），替换 4 处 `r == AnalyticsRange1D` 判定；`analyticsStartDate` 经通用路径天然返回今天，无需特判。
  - 后端 `internal/op/ops/ops.go` — `opsRangeWindow` 增加 `today`：今日 0 点起、小时桶、桶数 = 已过小时数（含当前小时）。
  - 前端 `web/src/api/endpoints/analytics.ts` — `AnalyticsRange` 增加 `'today'`。
  - 前端 `web/src/components/modules/analytics/index.tsx` — `PRIMARY_RANGES` 改为 `['today','1d','7d','30d']`。
  - 前端 `web/src/components/modules/analytics/Utilization.tsx` — today 用缺省今日小时口径（`HourlyGet`，0 点起至当前小时），1d 仍走 `window=24h`。
  - 前端 `web/src/components/modules/ops/Cache.tsx` — today 按小时桶渲染趋势。
  - 前端 `web/src/components/modules/home/store.ts` + `home/analytics-overview.tsx` — 首页「使用概览」周期选择器新增「今天」（`OverviewRange`/`OVERVIEW_RANGES`/`RANGE_OPTIONS`）。运维中心无独立周期选择器（`ops/Cache` 嵌在分析中心内使用同一选择器）。
  - i18n `web/public/locale/{zh_hans,zh_hant,en}.json` — 新增 `analytics.range.today` 与 `home.overview.range.today`；`analytics.range.1d` 改为 24小时 / 24小時 / 24H。
- 验证：`go build ./internal/...`、`go vet` 通过；`go test ./internal/op/...` 全绿（含 `TestAnalyticsStartTime_Ranges`、`TestOpsRangeWindow_DayBucketsForMultiDayRanges` 新增 today 断言）；`npx tsc --noEmit` 仅剩既有测试文件报错（与本次无关）；`home/store.test.ts` 补充 today 断言。
- codestable：已同步 `spec/index.md`「当前已稳定的能力与保证」新增「分析中心时间口径」一条。
