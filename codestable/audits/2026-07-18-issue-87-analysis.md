# Issue #87 代码核对报告：优化数据的组织与展示

> 基于 [lingyuins/octopus#87](https://github.com/lingyuins/octopus/issues/87) 的功能建议，对照当前代码逐条核对。
> 核对范围：前端 `web/src/components/modules/*`、后端 `internal/op/analytics/*`、`internal/server/handlers/analytics.go`。

## 一、当前导航与页面结构

顶级导航（`web/src/route/config.tsx`）共 12 个入口：

| ID | 页面 | 模块路径 |
|----|------|----------|
| home | 首页 | `modules/home` |
| hub | Hub（远程站点） | `modules/remote-site` |
| channel | 渠道 | `modules/channel` |
| group | 分组 | `modules/group` |
| model | 模型广场 | `modules/model` |
| analytics | 分析中心 | `modules/analytics` |
| log | 日志 | `modules/log` |
| alert | 告警 | `modules/alert` |
| ops | 运维中心 | `modules/ops` |
| apikey | API 密钥及端点 | `modules/apikey` |
| setting | 设置 | `modules/setting` |
| user | 用户 | `modules/user` |

分析中心 6 个 Tab（`analytics/index.tsx:38-43`，默认 Tab 为 `cache`）：
`缓存 → 利用率 → 路由健康 → 渠道×模型 → 评估 → 延迟`

运维中心 5 个 Tab（`ops/index.tsx:24-30`，默认 `telemetry`）：
`遥测 → 配额 → 健康 → 系统 → 审计`

---

## 二、逐条核对

### 1. 分析中心第一页「缓存」默认展示，非高频关注 ✅ 确认

- **代码位置**：`analytics/index.tsx:26` `useState<AnalyticsTab>('cache')`
- **现状**：默认 Tab 是 `cache`，且 `Cache` 组件直接复用运维中心的 `modules/ops/Cache`（`analytics/index.tsx:14`）。
- **评价**：缓存命中率确实属于低频观测项。把最低频的数据放在首屏首位，与用户预期相反。
- **建议**：默认 Tab 改为 `channel-model`（用户公认最重要的页）；或调整 Tab 顺序，把 `渠道×模型`/`利用率` 前置，`缓存` 后置。

### 2. 「利用率」页数据令人困惑 ✅ 确认

- **代码位置**：`analytics/Utilization.tsx`
- **现状**：三张 `BreakdownCard`（供应商/模型/密钥），每项展示：请求次数、成功率、消耗金额、Tokens。
- **问题根因**：
  - 「利用率」名称与内容不符——页面实为「按维度（供应商/模型/密钥）的用量明细」，并非真正的"利用率"（如配额消耗比例、渠道负载占比）。
  - 成功率只显示 `0%/100%` 的可能原因：`formatPercent(item.success_rate)` 当 `success_rate` 为 0 或 100 时只显示整数值；若后端在所选 `range` 内无失败请求则为 100%，无成功请求则为 0%，缺少中间状态时观感像"非 0 即 100"。
  - 金额全为 0：当所选时间窗内该维度无计费记录（或渠道未回填 cost）时，`total_cost` 为 0，而首页消耗金额走的是 `stats` 接口（`home/chart.tsx` 用 `useStatsDaily/Hourly`），两者数据源不同，会出现"首页有钱、利用率页为 0"的不一致。
- **建议**：重命名为「用量明细 / 维度分布」；金额为 0 时给出"无计费数据"提示而非空显示；或直接与「渠道×模型」合并为统一的分布分析页。

### 3. 「路由健康」其实是分组状况，且有 Bug ⚠️ 确认（含 Bug）

- **代码位置**：`analytics/GroupHealth.tsx`；后端 `internal/op/analytics/analytics.go:294` `AnalyticsGroupHealthGet` / `:617` `buildGroupHealth`。
- **现状**：每个分组卡片展示健康分、失败数、启用/禁用成员数、失败渠道下钻、Auto 组的「实时表现」。
- **🔴 确认的 Bug——「实时表现出现不相关模型」**：
  - 前端 `GroupHealth.tsx:238` 调用 `useAnalyticsAutoStrategy()`（**不带 groupID，拉全量**），再在 `autoItemsForGroup`（`:248-260`）里**仅按 `item.channel_ids` 过滤**。
  - 后端 `buildGroupHealth`（`analytics.go:736-744`）收集的 `channelIDs` 只是「该组涉及的所有渠道 ID 集合」，**不含 model/endpoint_type 维度**。
  - 而 Auto 策略快照（`balancer.GetAutoStatsSnapshot`）是按 `(channel_id, model_name)` 全局聚合的，跨所有分组、所有 endpoint。
  - **后果**：同一渠道若同时属于分组 A（chat，模型 X）和分组 B（embeddings，模型 Y），则分组 A 的「实时表现」会把模型 Y 也显示出来——即 issue 反馈的"不相关的模型"。
  - **修复方向**：后端 `AnalyticsAutoStrategyGet` 接收 `groupID` 时，应按该组 `group.Items` 的 `(channel_id, model_name)` 对精确过滤快照（而非只按 channel_id）；或 `AnalyticsGroupHealthItem` 直接带上本组的 `(channel,model)` 列表，前端据此过滤。前端也应改为按组请求 `useAnalyticsAutoStrategy(groupId)` 而非拉全量客户端过滤。
- **整合建议**：路由健康本质是分组运行态，与 `modules/group` 页面强相关。可在分组卡片内嵌健康分/失败数（复用 `AnalyticsGroupHealthItem`），利用分组页已有的筛选能力，避免与分析中心重复。

### 4. 「评估」页四项中三项为 AI 路由相关 ✅ 确认

- **代码位置**：`analytics/Evaluation.tsx`
- **现状**：2 张 `EntryCard` + 2 张汇总卡 = 4 卡片。其中：可用性（分组测试）、AI 路由、AI 路由汇总、分组测试汇总。AI 路向占比偏高，且「可用性」卡按钮只是 `setActiveItem('group')` 跳转（`Evaluation.tsx:123,143`）。
- **建议**：换名为「AI 路由与测试」或「任务中心」；纯跳转入口可合并进分组页操作区，减少无信息量的卡片。

### 5. 「渠道×模型」是最重要页，数据分散 ✅ 确认（核心痛点）

- **代码位置**：`analytics/ChannelModel.tsx`；首页 `home/chart.tsx`、`home/rank.tsx`。
- **现状核对**：
  - **渠道数据分散**：查看某渠道的请求次数/输入输出需去渠道页点开具体渠道（`modules/channel`）；站点渠道模型的历史小图在 `modules/site-channel`；非站点渠道无模型级调用记录入口。
  - **首页图表**：`home/chart.tsx:195-198` 四个切换为 `cost / count / tokens / success-rate`，其中 cost、count、tokens 三条曲线高度同向（都反映调用频率），与 issue 描述完全一致。
  - **首页排行**：`home/rank.tsx` 只按 **渠道**（cost/count/tokens）和 **API 密钥**（usage）排行，**没有按模型或分组的排行/占比图**——这正是用户想替代 Newapi「模型数据分析」却找不到的功能。
- **建议**（与 issue 诉求一致）：
  1. 新增「按模型（分组）的用量分布图 + 占比」；
  2. 新增「按渠道的用量分布图」；
  3. 分组内各模型、渠道内各模型的数据图（`ChannelModel.tsx` 已支持按 `groupId` 过滤，`ChannelModel.tsx:80`，可在此基础上加趋势图）。
  4. 首页 cost/count/tokens 三条同向曲线可合并为一个多指标切换，腾出位置给"模型/分组占比"。

### 6. 模型广场筛选选项少 ✅ 确认

- **代码位置**：`model/index.tsx:31-45`
- **现状**：筛选仅有 `priced`（有定价）/ `free`（免费）/ 全部 + 名称搜索 + 排序。**无按分组、按能力（endpoint）、按渠道等筛选**。
- **建议**：参考 Newapi 增加分组维度筛选；既然 Octopus 以「分组」为核心路由单元，模型广场可提供"按分组查看模型及其组内状况"视图。

### 7. 运维中心「遥测」供应商健康：列表无图、无排序 ✅ 确认

- **代码位置**：`ops/Telemetry.tsx:238-287` `ProviderHealthTable`
- **现状**：供应商健康为纯 `<table>`，列：名称/状态/延迟/请求数/成功率。**无按列排序**，**无图表**。
- **建议**：与渠道总览合并；表格增加列排序；延迟/请求数可用迷你条形图增强直观性。

### 8. 运维中心「配额」与 API 密钥详情数据分散 ✅ 确认

- **代码位置**：`ops/Quota.tsx` vs `apikey/index.tsx`
- **现状核对**：
  - 配额页（`Quota.tsx:107-136`）每个密钥展示：请求次数、总消耗、最大消耗、RPM、TPM、支持模型数、是否有按模型配额。
  - API 密钥详情（`apikey/index.tsx:203-215`，点 Info 展开）展示：请求次数、**成功率**、**Tokens**、消耗。
  - **两处数据重叠但字段不一致**：配额页缺成功率与 Tokens，密钥详情缺 RPM/TPM/配额状态。issue 反馈完全属实。
- **建议**：合并为统一的密钥详情视图，同时呈现配额状态 + 成功率 + Tokens + 限额。

### 9. 运维中心「健康」的「需关注的路由分组」与分析中心「路由健康」重复 ✅ 确认

- **代码位置**：`ops/Health.tsx:125-174`（`failing_groups`）vs `analytics/GroupHealth.tsx`
- **现状**：两页都展示分组健康分/失败数/状态，数据同源（`AnalyticsGroupHealthGet` / `OpsHealthStatus` 均基于分组健康聚合）。`ops/Health` 是"仅失败分组"子集，`analytics/GroupHealth` 是全量。
- **建议**：保留一处（建议留在分组页或分析中心），另一处改为跳转入口。

### 10. 运维中心定位模糊 ✅ 确认

- **现状**：`ops` 5 个 Tab 中，遥测/配额/健康偏"分析展示"，与 `analytics` 重叠；仅系统/审计体现"运维"差异。issue 评价"运维中心并不能起到操作然后实现维护的效果"属实——当前基本只读。
- **建议**：把可操作维护项（见下条）并入运维中心，使其名副其实。

### 11. 设置页多项可归入运维/对应业务页 ✅ 确认

- **代码位置**：`setting/index.tsx:38-57`（18 项设置均为弹窗）
- **核对结果**：

  | 设置项 | 文件 | issue 建议 | 评价 |
  |--------|------|-----------|------|
  | 输出关键词拦截 | `ResponseFilter.tsx` | → 运维中心 | ✅ 合理，属内容治理/维护 |
  | 熔断器设置 | `CircuitBreaker.tsx` | → 运维中心 | ✅ 合理，典型运维项 |
  | 重试配置 | `Retry.tsx` | → 运维中心 | ✅ 合理 |
  | 站点自动化 | `SiteAutomation.tsx` | → 站点管理 | ✅ 合理，当前仅弹窗 |
  | 清理分组内不可用模型 | `PurgeUnavailableModels.tsx` | → 分组页 | ✅ 合理，单功能操作 |
  | 路由分组（全部删除） | `RouteGroupDanger.tsx` | → 分组页 | ✅ 合理，确认 issue 吐槽"点开只有一个全部删除"属实——`RouteGroupDanger` 是危险操作弹窗，独立成项过重 |
  | 自动任务 | `LLMSync.tsx` + `AutoStrategy.tsx` | → 运维/合并 | ✅ 合理，两项且无详细说明 |

### 12. API 密钥及端点：「可用端点」与密钥同页不合理 ✅ 确认

- **代码位置**：`apikey/index.tsx:271`（`view: 'keys' | 'endpoints'`）、`apikey/EndpointsPanel.tsx`
- **现状**：`EndpointsPanel` 用 `useModelCapabilities` 按 endpoint 类型对模型做只读分组展示，**与 API 密钥无直接关系**，本质是"模型能力分组视图"。
- **建议**：迁移至分组页或模型广场（与分组设计对齐），密钥页专注密钥管理。

---

## 三、确认的 Bug 汇总

| # | Bug | 位置 | 严重度 | 状态 |
|----|-----|------|--------|------|
| 1 | 路由健康「实时表现」显示不相关模型（仅按 channel_id 过滤，未按 model/endpoint_type） | 前端 `analytics/GroupHealth.tsx:248-260`；后端 `internal/op/analytics/analytics.go:218-292, 736-744` | 🟠 中（数据误导） | ✅ 已修复（commit `b354e3c36`） |

> 其余条目为信息架构/UX 优化建议，非功能性 Bug。

### Bug 修复说明（commit `b354e3c36`，已推送 origin/master）

- **model** `AnalyticsGroupHealthItem` 新增 `AutoItems []AutoStrategySnapshotItem` 字段。
- **后端** `AnalyticsGroupHealthGet` 拉全量 Auto 快照传给 `buildGroupHealth`，后者对每个 Auto 组按本组 `(channel_id, model_name)` 精确过滤后填入 `AutoItems`；`AnalyticsAutoStrategyGet(groupID)` 同步改为按组 `(channel, model)` 精确过滤（抽取 `buildGroupAutoScope` / `filterAutoSnapshot` / `buildAutoStrategyItems` 三个辅助函数）。
- **前端** `GroupHealth.tsx` 删除独立 `useAnalyticsAutoStrategy()` 调用与客户端过滤逻辑，直接用 `item.auto_items`。
- **测试** 新增 4 个用例（跨组渠道回归、非 Auto 组空 AutoItems、filterAutoSnapshot 精确匹配、端到端 AnalyticsAutoStrategyGet 按 groupID 过滤）。
- **校验** `go build` / `go test ./internal/op/analytics/...` / `balancer` 测试全过；前端 lint 零新增问题，单测 111/111 全过；pre-commit hook（gofmt + eslint）通过。

---

## 四、改进优先级与执行计划

> P0 已完成。P1 已完成。P2 已完成。P3 已完成。

### P1 — 高价值数据展示（建议本轮优先）

| # | 任务 | 涉及文件 | 预估改动 | 依赖 | 状态 |
|----|------|----------|----------|------|------|
| 1 | 分析中心默认 Tab 改为 `channel-model`，Tab 顺序调整（渠道×模型前置、缓存后置） | `analytics/index.tsx` | 极小（改默认 state + Tab 顺序） | 无 | ✅ 已完成 |
| 2 | 新增「按模型/分组的用量占比图」（对标 Newapi 模型数据分析） | 复用 `useAnalyticsUtilization`/`useAnalyticsChannelModel`；前端新增 `analytics/UsageDistribution.tsx` | 中（前端图表组件，复用现有接口） | 无 | ✅ 已完成 |
| 3 | 合并「配额」与「API 密钥详情」字段，消除数据分散 | 后端 `model/ops.go` + `op/ops/ops.go`；前端 `ops.ts` + `ops/Quota.tsx` | 中（后端增补字段 + 前端统一展示 + 跳转） | 无 | ✅ 已完成 |

### P1 修复说明

- **#1 默认 Tab 与顺序**：`analytics/index.tsx` 默认 state 由 `'cache'` 改为 `'channel-model'`；Tab 与 TabsContent 顺序统一调整为 `渠道×模型 → 利用率 → 路由健康 → 延迟 → 评估 → 缓存`，高频数据前置、缓存后置。
- **#2 用量分布占比图**：新增 `analytics/UsageDistribution.tsx`，置于「渠道×模型」页顶部。支持「按模型 / 按渠道×模型」两个维度与「请求次数 / 费用 / Token」三个指标切换，取 Top 8 + 「其它」聚合，用水平条形图展示占比。复用现有后端接口（`model_breakdown` + `channel-model`），无需新增后端接口。三语 i18n 已补齐（`usageDistribution` 命名空间）。
- **#3 配额与密钥详情合并**：后端 `OpsQuotaKeyItem` 增补 `TotalTokens`/`SuccessRate` 字段，从 `stats.APIKeyList()` 聚合填充；前端配额页卡片由 4 格扩展为 6 格（累计成本 / 成本上限 / 总 Token / 成功率 / RPM / TPM），并在卡片底部新增「查看密钥详情」跳转按钮直达 API 密钥页。配额页成为统一密钥详情视图。三语 i18n 已补齐。
- **校验**：`go build` / `go test ./internal/op/ops/... ./internal/model/...` 通过；前端 `pnpm test:i18n` 通过、`pnpm test:unit` 111/111 全过、lint 零新增问题。

### P2 — 信息架构整合

| # | 任务 | 涉及文件 | 预估改动 | 依赖 | 状态 |
|----|------|----------|----------|------|------|
| 4 | 路由健康整合进分组页卡片；运维中心「健康」失败分组改为跳转入口 | `group/GroupListItem.tsx`、`ops/Health.tsx` | 中 | 无 | ✅ 已完成 |
| 5 | 「可用端点」迁移至分组/模型广场，密钥页专注密钥管理 | 新增 `model/EndpointsView.tsx`；移除 `apikey/EndpointsPanel.tsx` 及 apikey 视图切换 | 中（迁移组件 + 路由调整） | #4 | ✅ 已完成 |
| 6 | 运维中心纳入熔断器/重试/输出拦截等可操作维护项 | `ops/index.tsx` + `setting/CircuitBreaker.tsx` 等 | 中（设置项迁移为运维 Tab） | 无 | ✅ 已完成 |
| 7 | 设置页瘦身：站点自动化→站点管理；清理不可用模型/路由分组→分组页 | `setting/index.tsx` + 对应业务页 | 小～中（逐项迁移弹窗） | #4 #6 | ✅ 已完成 |

#### P2 已完成项实施说明

- **#4 路由健康整合**
  - `group/GroupListItem.tsx`：接入 `useAnalyticsGroupHealth()`，按 `group_id` 匹配当前分组健康状态，在折叠行右侧新增健康徽章（`down` 红色 / `degraded|warning` 琥珀色），展示失败次数或「风险」，悬浮显示健康分与状态文案。
  - `ops/Health.tsx`：「需关注的路由分组」详情区头部新增「查看路由健康详情」跳转按钮，点击跳转至分析中心（analytics 模块），实现运维侧 → 分析侧的联动入口。
  - 新增 i18n：`group.card.healthScore`、`group.card.healthLow`、`group.healthStatus.{healthy,warning,degraded,down,empty}`、`ops.health.actions.viewRouteHealth`（三语）。

- **#5 「可用端点」迁移至模型广场**
  - 新增 `model/EndpointsView.tsx`：复用 `useModelCapabilities()` 与 `apikey/endpoint-grouping.ts`（保留对话族规约逻辑与单测），独立于密钥模块。
  - `model/index.tsx`：顶部新增「模型广场 / 可用端点」视图切换，默认 Market。
  - `apikey/index.tsx`：移除 `EndpointsPanel` import、`endpoints` 视图切换按钮与分支，密钥页专注密钥管理。
  - 删除 `apikey/EndpointsPanel.tsx`（已无引用）；`endpoint-grouping.ts` 与 `endpoint-grouping.test.ts` 保留原位供 `EndpointsView` 复用。
  - 新增 i18n：`model.marketTitle`（三语）。

- **#6 运维中心纳入可操作维护项**
  - 新增 `ops/Maintenance.tsx`：直接复用 `SettingCircuitBreaker` / `SettingRetry` / `SettingResponseFilter` 三个自包含组件，垂直堆叠于同一 Tab，每项保留原表单/标题/图标。
  - `ops/index.tsx`：新增 `maintenance` Tab，顺序为 `遥测 → 配额 → 健康 → 维护 → 系统 → 审计`（维护紧随健康，形成"观察→处置"流程）；默认 Tab 仍为 `telemetry`。
  - `setting/index.tsx`：从 `SETTING_ITEM_DEFS` 移除 `retry` / `circuit-breaker` / `response-filter` 三项入口及对应 import（`SettingRetry` / `SettingCircuitBreaker` / `SettingResponseFilter` / `Zap` / `ShieldAlert` / `RotateCcw`），设置页瘦身。
  - `setting/SettingOrder.tsx`：`SettingItemId` 类型与 `DEFAULT_SETTING_ORDER`、`titleByKey` 同步移除这三项；已存 localStorage 中的旧顺序由 `loadStoredOrder` 的 `DEFAULT_SETTING_ORDER.includes` 校验自动丢弃，无需迁移。
  - 组件文件（`CircuitBreaker.tsx` / `Retry.tsx` / `ResponseFilter.tsx` / `runtime-settings.ts`）保留原位供 ops 复用；i18n 键仍归属 `setting.*` 命名空间，跨模块复用无副作用。
  - 新增 i18n：`ops.tabs.maintenance` + `ops.maintenance.description`（三语）。
  - 顺带修复 P2 #4 遗留的 i18n 缺失：补齐 `en.ops.health.actions.viewRouteHealth`、`zh_hant.group.card.healthScore/healthLow` 与 `group.healthStatus.*`。
  - **校验**：`pnpm lint`（改动文件零新增问题）、`pnpm test:i18n` 通过、`pnpm test:unit` 111/111 全过。

- **#7 设置页瘦身：迁移站点自动化、清理不可用模型、路由分组到对应业务页**
  - **站点自动化 → Hub 新增「自动化」Tab**：
    - `remote-site/hub-tab-store.ts`：`HubTab` 类型新增 `'automation'`。
    - `remote-site/index.tsx`：新增 `automation` Tab，内嵌 `SettingSiteAutomation` 组件（`SiteAutomation.tsx` 文件保留供 Hub 复用）。
    - 新增 i18n：`hub.tabs.automation`（三语）。
  - **清理不可用模型 + 全部删除分组 → 分组页顶部「维护」下拉按钮**：
    - 新增 `group/MaintenanceButton.tsx`：基于 Popover 实现下拉菜单，含两个选项卡片（「清理不可用模型」「删除全部路由分组」），点击后打开对应 `AlertDialog` 二次确认；直接复用 `usePurgeUnavailableGroupItems` / `useDeleteAllGroups` hooks 与 `setting.purgeUnavailable.*` / `setting.routeGroups.*` i18n 键，无需重命名。
    - `group/index.tsx`：emptyState 按钮行新增 MaintenanceButton（与 AutoGroupButton/AIRouteButton 并列）；有数据时在 VirtualizedGrid 上方新增轻量顶部条容纳右对齐的 MaintenanceButton。
    - 新增 i18n：`group.actions.maintenance`（三语）。
  - **设置页清理**：
    - `setting/index.tsx`：从 `SETTING_ITEM_DEFS` 移除 `purge-unavailable` / `site-automation` / `route-group-danger` 三项及对应 import（`SettingPurgeUnavailableModels` / `SettingSiteAutomation` / `SettingRouteGroupDanger` / `Eraser` / `Globe2` / `FolderX`）。
    - `setting/SettingOrder.tsx`：`SettingItemId` 类型与 `DEFAULT_SETTING_ORDER`、`titleByKey` 同步移除三项。
    - 删除孤立组件文件：`setting/PurgeUnavailableModels.tsx`、`setting/RouteGroupDanger.tsx`（确认零引用）；`setting/SiteAutomation.tsx` 保留供 Hub 复用。
    - 顺带清理 `remote-site/index.tsx` 中预先存在的 `useEffect` 未使用 import warning。
  - **设置页规模变化**：从 18 项 → 12 项（P2 #6 移除 3 项 + #7 移除 3 项），保留核心：信息、外观、AI 路由、自动策略、账号、语义缓存、日志、系统、模型同步、备份、WebDAV、WebAuthn。
  - **校验**：`pnpm lint`（改动文件零新增问题，仍剩仓库已有 `_patch2.mjs` 1 error 与 10 个预先存在 warning）、`pnpm test:i18n` 通过、`pnpm test:unit` 111/111 全过。

### P3 — 体验增强（已完成）

- **#8 供应商健康表列排序 + 迷你条形图**（`ops/Telemetry.tsx`）
  - `ProviderHealthTable` 新增 `useState<ProviderSortKey, ProviderSortOrder>` 排序状态，表头改为可点击按钮（带 `ArrowUp/ArrowDown/ArrowUpDown` 图标），支持 name/status/latency/requests/success_rate 五列升降序。
  - `ProviderRow` 在 latency / requests / success_rate 单元格内嵌 16px 宽的 mini 横向条形图（按本列最大值归一），延迟用琥珀色、请求数用 primary、成功率用绿色，数值与条形并排。
  - status 列排序按 `down > degraded > warning > disabled > healthy` 的严重度排名。

- **#9 模型广场多维筛选 + 归一化去重**（`model/normalize.ts` + `model/useModelFilters.ts` + `model/index.tsx`）
  - **超出原 issue 范围**：用户要求把同一基础模型的不同命名变体（如 `kimi-k2.5`、`@cf/moonshotai/kimi-k2.5`、`dmxapi-kimi-k2.5`、`moonshotai/kimi-k2.5`、`agent/kimi-k2.5`、`kimi-k2.5-cc`）统一聚合。
  - 新增 `normalize.ts`：`normalizeModelName(name)` 剥离路径前缀（`provider/`、`@cf/org/`、`agent/`）→ 路由商前缀（`dmxapi-`/`agent-`/`openai-`/`anthropic-`）→ 功能后缀（`-cc`/`-fast`/`-thinking`/`-preview`/`-beta`/`-latest`，可叠加）→ 小写归一。配套 `normalize.test.ts` 7 个用例覆盖路径/前缀/后缀/叠加/组合场景。
  - 新增 `useModelFilters.ts`：基于 `useModelCapabilities`（按模型名 join 能力）+ `getModelIcon(name).label`（推断模型原始厂商），提供「能力筛选（chat/embeddings/rerank/image/audio/video/music/search）+ 厂商筛选 + 名称搜索 + 归一化去重」筛选链。
  - `model/index.tsx`：Market 视图顶部新增独立筛选条（不污染全局 Toolbar 的 all/priced/free 单选 popover）：能力 chips + 厂商 chips + 「归一化去重」开关 + 提示文案；保留原 Toolbar 筛选作为第一层。
  - 新增 i18n：`modelFilter` 命名空间（`capability` / `provider` / `all` / `dedupe` / `dedupeHint` / `capability.*`，三语）。

- **#10 「利用率」重命名为「用量明细」+ 空数据提示**（`analytics/Utilization.tsx` + i18n）
  - i18n 标题 `analytics.cards.utilization.title` 由 `Utilization`/`利用率`/`利用率` 改为 `Usage Breakdown`/`用量明细`/`用量明細`；描述文案澄清「金额来自计费请求聚合，与首页图表的实时统计来源不同」。
  - `BreakdownCard` 新增 `noBillingHint`：当卡片内所有 items 的 `total_cost` 均为 0 时，在卡片底部展示琥珀色提示「此时间窗内无计费数据」，消除「首页有钱、利用率页为 0」的困惑。
  - 新增 i18n：`analytics.utilization.noBilling`（三语）；优化 `analytics.utilization.empty` 文案。

- **#11 首页多指标叠加图**（`home/store.ts` + `home/chart.tsx`）
  - `store.ts`：新增 `chartMetrics: ChartMetricType[]` 多选状态（默认 `['cost']`）与 `toggleChartMetric` action（点击已选则取消，至少保留一个）；`setChartMetricType` 同步重置为单选以保向后兼容；`merge` 兼容旧持久化的 `chartMetricType` 单值。
  - `chart.tsx`：原单选 `Tabs` 改为多选按钮组（带指标色点 `getChartStroke`），数据构造遍历 `chartMetrics` 同时产出多个 dataKey；单指标时渲染 `Area`（带渐变填充），多指标时渲染多条 `Line`（避免 Area 互相覆盖）；YAxis 刻度按主指标（`chartMetrics[0]`）格式化。
  - 满足 issue「合并同向曲线」诉求：用户可同时勾选 cost/count/tokens 看走势叠加，也可只看成功率。
  - **校验**：`pnpm lint`（改动文件零新增问题）、`pnpm test:i18n` 通过、`pnpm test:unit` 118/118 全过（含新增 7 个 normalize 用例）。

### P3 — 体验增强

| # | 任务 | 涉及文件 | 预估改动 | 依赖 | 状态 |
|----|------|----------|----------|------|------|
| 8 | 供应商健康表加列排序与迷你条形图 | `ops/Telemetry.tsx` | 小 | 无 | ✅ 已完成 |
| 9 | 模型广场增加分组/能力筛选 | `model/index.tsx` | 小 | 无 | ✅ 已完成（扩展为能力 + 厂商 + 归一化去重） |
| 10 | 「利用率」重命名为「用量明细 / 维度分布」+ 空数据提示优化 | `analytics/Utilization.tsx` + i18n | 小 | 无 | ✅ 已完成 |
| 11 | 首页 cost/count/tokens 三同向曲线合并为多指标切换，腾位给占比图 | `home/chart.tsx` | 小 | #2 | ✅ 已完成（改为多指标叠加） |

### 建议执行顺序

```
本轮（P1）：#1 → #2 → #3   ✅ 已完成
  ↓
下轮（P2）：#4 ✅ → #5 ✅ → #6 ✅ → #7 ✅
  ↓
收尾（P3）：#8 ✅ #9 ✅ #10 ✅ #11 ✅
```

> Issue #87 全部改进项（P0/P1/P2/P3）已闭环。

> 其中 #1（默认 Tab）改动极小可立即落地；#2（占比图）是 issue 最核心诉求，建议作为本轮重点。
