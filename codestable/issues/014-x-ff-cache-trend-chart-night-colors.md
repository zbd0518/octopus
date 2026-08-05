---
kind: issue
title: "缓存分析 24h 趋势图柱状图夜间显示为黑色"
type: ff
status: closed
created: 2026-08-02
---

# 缓存分析 24h 趋势图柱状图夜间显示为黑色

> **读者：** 以后搜到这条时——「改了啥、怎么信、动没动制度记忆」。

分析中心 → 缓存分析 → Provider Prompt 缓存视图的「24 小时趋势」柱状图，夜间时段柱体呈黑色、看不清读取/写入 Token 的分布。

- 改动：`web/src/components/modules/ops/Cache.tsx` — `buildChartConfig` 中 `cache_read_tokens` / `cache_write_tokens` 的 `color` 由 `hsl(var(--chart-1))` / `hsl(var(--chart-2))` 改为 `var(--chart-1)` / `var(--chart-2)`
- 根因：`--chart-1/--chart-2` 在 `globals.css` 中定义为 OKLCH 值（如 `oklch(0.62 0.08 160)`），`ChartStyle` 注入 `--color-*` 时原样拼接，`hsl(oklch(...))` 是无效 CSS，浏览器忽略后柱体回退为黑色
- 验证：`npx tsc --noEmit` 无新增错误（既有报错均在测试文件，与本次无关）；改动后柱体将使用 OKLCH 原色（绿/蓝），在深色模式下同样可辨
- codestable：无影响

顺手发现（可选）：`web/src/components/modules/remote-site/BalanceChart.tsx` 同样使用 `hsl(var(--chart-4, 45 96% 54%))`，但带 HSL 回退值，未出现此问题；不在本次范围。