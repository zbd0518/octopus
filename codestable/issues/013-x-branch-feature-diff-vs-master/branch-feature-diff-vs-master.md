---
kind: issue
title: "分支 fix/site-model-case-pk-conflict vs master 功能差异清单与合并评估"
type: explore
status: closed
created: 2026-08-02
migrated_from: codestable/compound/branch-feature-diff-vs-master.md
---

# 分支 `fix/site-model-case-pk-conflict` vs `master` 功能差异清单

> 生成日期：2026-08-02
> 对比基线：`origin/master` (6b4bd5ba)
> 统计：166 文件变更 / 97 新增 / 67 修改 / 1 重命名 / 1392 净增加行
> 分支共 53 个提交，除去 merge + chore(price) 后的有效功能提交见下文。

---

## 一、站点模型 System (site model)

> 核心主题：解决站点模型名大小写唯一性冲突（MySQL utf8mb4 默认 collation 对大小写不敏感，导致 `GLM-5.2` 与 `glm-5.2` 在唯一索引上冲突）

- **model_name_key 新唯一索引** — `SiteModel` 表增加 `ModelNameKey` 列（model_name 的 MD5 十六进制指纹），唯一索引从 `(site_account_id, group_key, model_name)` 改为 `(site_account_id, group_key, model_name_key)`，避免同一字母大小写变体冲突。
  - `internal/model/site.go`: 新增 `SiteModelNameKey()` / `SiteModelIdentityKey()` / `EnsureModelNameKey()` 工具方法；`ModelNameKey` 列定义（`size:32;uniqueIndex:idx_site_account_group_model_key;not null;default:''`）
  - `internal/sitesync/storage.go` + `sync_fetch.go` + `project.go`: 去重 / upsert / 同步流程统一调用 `EnsureModelNameKey()` 替换原 "group_key + model_name" 拼接 key 的逻辑
  - 迁移：`041` 为 `RegisterBeforeAutoMigration`，先建新索引 `idx_site_account_group_model_key`、回填空 `model_name_key`、再删旧索引 `idx_site_account_group_model`；`042` (`RegisterAfterAutoMigration`) 兜底清理 041 半成功残留（旧 CI 唯一索引仍在）

---

## 二、号池 (Pool Account)

> 参照 sub2api 差距补充号池生命周期与调度能力

- **号池账号扩展字段**（`migration 044`）— `pool_accounts` 增加：`platform`(平台类型)、`type`(账号类型)、`models`(关联模型)、`quota`(额度)、`token_expires_at`/`last_used_at`/`error_message`/`notes`；存量账号自动回填默认值
- **号池生命周期调度字段**（`migration 045`）— `pool_accounts` 增加：`temp_unsched_until/reason`、`auth_error_count/window_start`、`expires_at`、`auto_pause_on_expired`、`extra`、`weight`、`load_factor`
- **号池 OAuth 回调去重** — 重复授权复用已有账号
- **号池账号编辑修复** — 编辑时代理配置无法清空

---

## 三、中继/推理流 (Relay & Reasoning Stream)

> 优化长思考请求的流式推送，防止客户端/反代因无数据超时断开

- **长思考请求默认 immediate 推流** — `getReasoningBufferStrategy()` 新增长思考检测 `prefersImmediateReasoningStream()`，对于 `reasoning_effort` 请求（如 o1/o3/o4-mini）默认 immediate 策略，避免 reasoning-only 阶段长时间无 SSE 输出导致断开
- **客户端主动断开识别** — `AttemptStatus` 增加 `AttemptClientClosed` 状态，区分"客户端断开"与"渠道故障"，不计入 RequestFailed/熔断
- **Channel.DescribeNoAvailableKey()** — 细化"无可用 Key"错误文案（区分：渠道无 key / 全部禁用 / 全部冷却 / 混合过滤）
- **批量 Key 导入** — 渠道上游 Key 支持批量粘贴导入（`bulk-keys.ts` / 前端组件）
- **key 冷却感知测活** — 分组探测 `group_probe.go` 用 `GetChannelKeyWithCooldown()` 替换 `GetChannelKey()`，测活 200 与实际请求 502 不再认知差
- Responses API 兼容 — 兼容 upstream/responses metadata，修复 Anthropic 流 content block 状态
- Chat Completions 出站剥离 metadata — 避免兼容上游 400
- 流式 inbound 在线聚合 — 避免 streamChunks 内存冲高

---

## 四、统计 (Stats)

- **直方图列名一致性修复**（`migration 043`）— `StatsMetrics` 直方图字段增加显式 `gorm column` tag，修复 GORM 默认蛇形命名 `histogram_lt100` 与 UPSERT/Redis/前端的 `histogram_lt_100` 不一致。迁移自动 RENAME/DROP 错误列
- **Latency/FTUT 补齐** — 补齐缺失的 `latency`、`FTUT` 列
- **已删除渠道 FK 1452 修复** — 统计落盘前用 `filterLiveChannelIDs()` 剔除已删除/不存在的 channel，避免整批事务撞外键约束后 requeue 死循环。使用 `sync.Map` 追踪进程内删除操作

---

## 五、分组编辑器 (Group Editor)

- **路由分组所属展示** — 渠道详情展示所属路由分组列表并支持跳转编辑（`route-groups.ts` + 跳转系统扩展）
- **成员静态风险状态** — 分组编辑器展示成员上游成功率健康风险（`editor-member-status.ts`，`HealthRiskLevel: none/moderate/low`）
- **渠道启用/禁用过滤** — 分组编辑器过滤禁用渠道，支持按渠道分组筛选（`editor-filters.ts`）
- **跳转系统扩展** — `jump.ts` 新增 `GroupJumpTarget` 类型（`kind: 'group-editor'`），支持从渠道详情直接跳转到对应分组编辑
- **修复：候选模型项 hover 底边抖动**
- **修复：编辑弹窗关闭后被 opener 立刻重开**
- **修复：已选成员 hover 底边抖动并收窄 jump 类型**
- **无可用 Key 降为软风险提示** — 避免误判不可选

---

## 六、渠道管理 (Channel)

- **渠道上游 Key 批量粘贴导入** — 新增前端组件 `bulk-keys.ts`，支持解析 `key:remark` / 每行一 Key 格式，去重合并保留已存在的 Key
- **渠道详情展示所属路由分组**（`route-groups.ts`）— 只读 join 展示，支持跳转编辑
- **删除渠道时同步清理分组已选成员** — 避免残留引用
- **渠道过期检测** — `task/channel_expire.go` 修改

---

## 七、仪表盘/总览 (Site Dashboard)

- **总览支持按平台类型筛选站点** — `site-filters.ts` 前端组件，支持 New API / AnyRouter 等平台类型筛选

---

## 八、配置与设置 (Settings)

- **自定义模型测活提示词** — 新增设置项 `SettingKeyGroupProbePrompt`（默认 "hi"），支持管理员自定义分组/渠道模型测活时的 chat content / embedding input
- **测活提示词解析** — `resolveGroupProbePrompt()` 安全回退

---

## 九、构建与版本

- **版本自动注入** — 构建脚本 `scripts/build.sh` 用最新 git tag 作为 `GIT_VERSION`，通过 `ldflags` 注入到 `internal/conf.Version`；`version.go` 中硬编码版本号更新至 `v2.5.4`
- **前端版本** — `NEXT_PUBLIC_APP_VERSION` 同步 tag 构建
- **模型价格预设** — `presets.go` 通过 `updatePrice.py` 定期刷新（含 237 个模型）
- CodeStable 工作区资产接入

---

## 十、其他修复 (Others)

- **网络受限环境代理配置说明** — `README_zh.md` 补充说明
- **夜间模式渠道 Badge 文字看不清** — 修复 dark mode 下渠道名称/类型 Badge 对比度
- **连接池空闲生命周期缩短** — 缓解非 SQLite 数据库的 `driver: bad connection`
- **迁移注释修正** — `migration 040` 注释中迁移版本引用修正
- **依赖更新** — `go.mod` 新增 `klauspost/compress` 间接依赖

---

## 十一、迁移总览 (Post-040)

| 版本 | 功能 | 注册时机 | 备注 |
|------|------|---------|------|
| 041 | 站点模型唯一索引改建：先建 `idx_site_account_group_model_key` + 回填 `model_name_key` + 删旧 `idx_site_account_group_model` | BeforeAutoMigration | HEAD 独有，核心功能 |
| 042 | 清理 041 半成功残留的旧 CI 唯一索引 + plan_providers 增加 team_organization_id / team_project_id | AfterAutoMigration | HEAD 独有 |
| 043 | 统计表直方图列名修复 (`histogram_lt100`→`histogram_lt_100`) & latency/FTUT 补齐 | AfterAutoMigration | HEAD 独有 |
| 044 | 号池账号扩展字段 platform/type/models/quota/token_expires_at/last_used_at/error_message/notes；存量回填默认值 | AfterAutoMigration | HEAD 独有 |
| 045 | 号池生命周期字段 temp_unsched_until/reason、auth_error_count/window_start、expires_at、weight、load_factor 等 | AfterAutoMigration | HEAD 独有 |
| 046 | plan_provider 增加自动刷新与增量快照字段 (RefreshIntervalMin/LastBalance/LastQuotaUsed) | AfterAutoMigration | 来自上游重排 |
| 047 | plan_provider 累计已用额度 TotalUsed | AfterAutoMigration | 来自上游重排 |
| 048 | plan_provider 商汤日日新自动登录字段 (LoginUsername/LoginPasswordEnc/RefreshTokenEnc) | AfterAutoMigration | 来自上游重排 |
| 049 | 模型广场归一化去重默认开启 (model_normalize_market_dedupe_default false→true) | AfterAutoMigration | 来自上游重排 |

> **注意**：046-049 来自 upstream master 重排，实际功能已在上游上线。041-045 为本分支独有。

---

## 十二、前端新增组件/库一览

| 文件 | 功能 |
|------|------|
| `bulk-keys.ts` + `bulk-keys.test.ts` | 渠道 Key 批量导入解析器 |
| `route-groups.ts` + `route-groups.test.ts` | 渠道所属路由分组聚合展示 |
| `editor-filters.ts` + `editor-filters.test.ts` | 分组编辑器渠道过滤 |
| `editor-member-status.ts` + `editor-member-status.test.ts` | 分组成员健康风险状态 |
| `site-filters.ts` + `site-filters.test.ts` | 站点平台类型筛选 |
| `brand-badge-style.ts` + `brand-badge-style.test.ts` | 品牌色 Badge 对比度安全文字色计算 |

---

## 十三、修改必要性评估

> 按"是否应随分支进入 master"评估，分三档：**✅ 必要**（核心 bug/功能修复）、**⚠️ 锦上添花**（增益有限，非必须）、**❌ 可省略**（影响小或可由上游覆盖）。

### 一、站点模型 — ✅ 全部必要

**分支命名由来，必须保留。** `model_name_key` 索引方案解决了实打实的数据库层级 bug（MySQL collation 导致 `GLM-5.2` 与 `glm-5.2` 冲突），是生产数据完整性修正。

### 二、号池 — ⚠️ 部分非核心

| 项 | 评估 | 理由 |
|---|------|------|
| 044 扩展字段 | ✅ 必要 | 缺少 platform/type 等基本分类无法实现账号分类调度 |
| 045 生命周期字段 | ⚠️ 锦上添花 | 自动暂停/错误计数/weight 属于"高级调度"，基础版不用也能跑 |
| OAuth 回调去重 | ✅ 必要 | 重复授权导致数据混乱，正确性修复 |
| 代理配置清空修复 | ✅ 必要 | 编辑 bug 影响基本操作 |

### 三、中继/推理流 — ✅ 大部分必要

| 项 | 评估 | 理由 |
|---|------|------|
| 长思考 immediate 推流 | ✅ 必要 | 解决用户实际遇到的首字 0ms 断开，生产问题 |
| Responses API 兼容 | ❌ 可省略 | **已被上游修复，合并 master 会覆盖本分支改动** |
| Chat Completions 出站剥离 metadata | ❌ 可省略 | **同上，已被上游修复** |
| `DescribeNoAvailableKey` | ⚠️ 锦上添花 | 日志更好排查，但旧文案也能用 |
| `AttemptClientClosed` | ✅ 必要 | 区分客户端断开 vs 渠道故障，影响熔断计数正确性 |
| 冷却感知测活 | ✅ 必要 | 避免 UI 显示 200 / 实际 502 的认知偏差 |
| 流式 inbound 在线聚合 | ✅ 必要 | 修复 streamChunks 内存冲高 |

### 四、统计 — ✅ 全部必要

直方图列名 bug 和 FK 1452 都是生产事故级别的 bug：前者导致统计数据写入失败/丢数据，后者导致整批事务 requeue 死循环。

### 五、分组编辑器 — ✅ 大部分必要

| 项 | 评估 | 理由 |
|---|------|------|
| 路由分组展示+跳转 | ✅ 必要 | 渠道详情看不到所属分组，影响日常运维效率 |
| 成员风险状态 | ⚠️ 锦上添花 | 信息有用但不影响操作 |
| 渠道过滤 | ✅ 必要 | 编辑时看到禁用渠道容易误操作 |
| 4 个 UI 抖动修复 | ✅ 必要 | 使用中会明显感知 |
| 无可用 Key 降为软风险 | ⚠️ 锦上添花 | 行为变化微妙，主要改善 UX |

### 六、渠道管理 — ⚠️ 部分锦上添花

| 项 | 评估 | 理由 |
|---|------|------|
| 批量 Key 导入 | ⚠️ 锦上添花 | 方便但没它也能一条条加 |
| 所属路由分组 | ✅ 必要 | 运维查看时需要 |
| 删除时清理成员 | ✅ 必要 | 防止孤儿引用 |
| 渠道过期检测 | ✅ 必要 | 过期渠道未检测上线后会出错 |

### 七、仪表盘站点筛选 — ⚠️ 锦上添花

无损原有功能，方便查看但非必须。

### 八、自定义测活提示词 — ⚠️ 锦上添花

默认与以前硬编码一致，不改不影响功能。

### 九、构建与版本 — ✅ 全部必要

版本注入和价格预设刷新是持续集成的必要组件。

### 十、其他修复

| 项目 | 评估 | 理由 |
|---|------|------|
| 代理配置文档 | ⚠️ 锦上添花 | 完善 README，可保留 |
| 夜间模式 Badge 对比度 | ❌ 可省略 | UI 微调，不影响功能 |
| 连接池空闲生命周期 | ✅ 必要 | 修复 `driver: bad connection` 生产 bug |
| migration 040 注释修正 | ❌ 可省略 | 格式/缩进调整，无功能影响 |
| `klauspost/compress` 依赖 | ✅ 必要 | master 新引入的间接依赖，合入后自动保留 |

### 十一、迁移 046-049 — ⚠️ 依赖 master

已在 master 上线，回归 master 后自动保持。仅需在合并时确认 SQL 一致性。

---

### 汇总

| 分类 | ✅ 必要 | ⚠️ 锦上添花 | ❌ 可省略（与 master 无冲突） | ❌ 已被上游修复 |
|------|:-------:|:-----------:|:----------------------------:|:----------------:|
| 站点模型 | 3 | 0 | 0 | 0 |
| 号池 | 3 | 1 | 0 | 0 |
| 中继/推理 | 3 | 1 | 0 | **2** |
| 统计 | 3 | 0 | 0 | 0 |
| 分组编辑器 | 5 | 2 | 0 | 0 |
| 渠道管理 | 3 | 1 | 0 | 0 |
| 仪表盘 | 0 | 1 | 0 | 0 |
| 设置 | 0 | 1 | 0 | 0 |
| 构建/版本 | 3 | 0 | 0 | 0 |
| 其他修复 | 1 | 1 | **2** (Badge / 040注释) | 0 |
| **合计** | **24** | **8** | **2** | **2** |

> **结论**：34 项修改中 **24 项必要（70%）**、**8 项锦上添花**、**4 项可考虑放弃**。其中 2 项中继兼容修复已被 upstream master 覆盖，合并时自然丢弃；夜间模式 Badge 和 040 注释修正是纯 UI 和格式调整，回归 master 前可考虑跳过以避免产生不必要的冲突。