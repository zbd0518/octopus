---
doc_type: issue-analysis
issue: 2026-07-26-codex-site-channel-key-and-client-disconnect
status: confirmed
root_cause_type: logic
related:
  - codex-site-channel-key-and-client-disconnect-report.md
tags:
  - codex
  - relay
  - key-cooldown
  - client-disconnect
  - site-channel
  - responses
---

# Codex 站点映射渠道 no available key 与 client disconnected 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `internal/helper/group_probe.go:406-410` | 渠道/分组「测试模型」取 key 用 `GetChannelKey()`（model 名为空），**不走模型级冷却** |
| `internal/model/channel.go:417-472` | `GetChannelKeyWithCooldown(modelName, …)`：`modelName != ""` 时通过 `KeyCooldownFunc` 过滤冷却中的 key；候选为空则返回空 `ChannelKey` |
| `internal/relay/balancer/key_cooldown.go:54-59` | `IsKeyOnCooldown`：**modelName 为空直接放行**（与探测路径对齐） |
| `internal/relay/balancer/key_cooldown.go:91-96` | `RecordKeyCooldown`：仅 `statusCode >= 400` 且 model 非空时写入冷却；默认 TTL 来自 `ratelimit_cooldown`（本机 DB=**300s**） |
| `internal/relay/relay.go:1424-1438` | 中继 key 循环：用 **resolvedModelName** 调 `GetChannelKeyWithCooldown`；空 key 时统一 Skip 文案 **`no available key (all keys in cooldown or disabled)`** 并最终 502 |
| `internal/relay/relay.go:492-495` | 转发失败且 `statusCode >= 400` 时 `RecordKeyCooldown(channel, key, model, statusCode)`，为后续「无可用 key」埋下状态 |
| `internal/relay/relay.go:89-134` | Codex 入站 `/v1/responses` 后，OpenAI 系渠道默认出站仍 **Chat → Responses** 回退；上游 4xx/5xx 会冷却该 (channel,key,model) |
| `internal/relay/relay.go:34,452-458` | 客户端断开 → `errClientDisconnected` → attempt **`AttemptFailed`**，`Success=false`，`ScopeAbortAll` |
| `internal/relay/relay.go:1503-1507` / `1305-1308` | disconnect 路径 `metrics.Save(**false**, errClientDisconnected, …)` |
| `internal/relay/relay.go:881-886,937-942` | 有 `streamSession` 时：客户端断开只 `markClientDisconnected` + **WARN** `client disconnected before response completed, continuing generation`，上游继续生成 |
| `internal/relay/context.go:16` | 上述 WARN 文案常量 |
| `internal/relay/request_session.go:91-96` | `streamSession` 仅在 `Stream=true` 且 `ConversationID != ""` 时启用 |
| `internal/relay/metrics.go:95-116,365-366` | `success=false` 记 `RequestFailed`；`err != nil` 写入 `relayLog.Error`（界面可见「错误」） |
| `internal/server/handlers/channel.go:333-360` | 渠道详情「测试模型」→ `StartChannelModelTest` → 与 group probe **同源**（绕过冷却） |

本机相关快照（analyze 时）：

- 渠道 `koyeb-免费/免费key/default-Chat` id=99，enabled，`base_urls=https://new-api.koyeb.app`（无 `/v1`），1 条 key enabled（`channel_keys.id=20`，key 非空）
- 设置：`ratelimit_cooldown=300`，`reasoning_buffer_strategy=buffer`，`relay_retry_count=3`
- 分组 `gpt-5.5` mode=3（Failover）；当前 `group_items` 可能已换成 `deepseek-数字站`（与复现时 koyeb 配置可能不同，不影响代码路径结论）

## 2. 失败路径还原

### 现象 A — 测试 200 / Cherry 通，Codex 502 no available key

**正常路径（探测）**

1. UI「测试模型」→ `testChannelModel` → `StartChannelModelTest` → `testGroupModelItem`
2. `channel.GetChannelKey()` → `GetChannelKeyWithCooldown("", 300)`  
3. model 名为空 → **不检查冷却** → 只要 key enabled 且非空即可发出短探测请求  
4. 上游 200 → UI 显示成功  

**失败路径（Codex `/v1/responses`）**

1. Codex → `localhost:3000/v1/responses` → 匹配分组 `gpt-5.5` → `executeRelay`
2. `ch.Get` 取渠道（含 Keys 缓存）→ `resolvedModelName`（如 `deepseek-v4-flash`）
3. `GetChannelKeyWithCooldown(resolvedModelName, 300)`  
4. 若该 `(channelID, keyID, model)` 已在冷却（或无 enabled key）→ 返回空 key  
5. `routeIter.Skip(..., "no available key (all keys in cooldown or disabled)")`  
6. 单渠道组耗尽 → `502 all channels failed: channel koyeb-…: no available key…`  

**冷却如何产生（与探测不对称的前置）**

1. 某次 Codex/中继曾成功选到 key 并真实转发  
2. 上游返回 ≥400（免费站常见 401/403/429/502，或 Responses/Chat 路径不匹配）  
3. `RecordKeyCooldown(channel, key, model, statusCode)`，默认 **300s**  
4. 窗口内后续 Codex 请求直接「无可用 key」；同窗口内 UI 测试仍因空 model **绕过冷却** 显示 200  

**分叉点**

- **主分叉**：`GetChannelKeyWithCooldown` 是否带 model 名  
  - 探测：`group_probe.go:406` 空 model → 无视冷却  
  - 中继：`relay.go:1426` 带 resolved model → 受冷却约束  
- **文案分叉**：空 key 一律报「cooldown or disabled」，无法区分「真无 key / 全禁用 / 全在冷却」  
- **可能的上游失败诱因**（次要、需日志确认首次 ≥400）：免费 new-api 站、base URL 去 `/v1` 后路径、Codex Responses 入站与默认 Chat 优先出站的组合；**不解释「测试成功 vs 中继无 key」本身**，只解释 key 为何进入冷却  

### 现象 B — Codex 业务成功，但每请求日志 client disconnected

**正常路径（期望）**

上游完整生成 → 客户端收齐 → 日志 `success=true`，无失败/错误感日志。

**实际路径（有 streamSession）**

1. Codex 流式且带 `conversation_id` → `acquireRelayStreamSession`  
2. 生成过程中客户端关闭连接 / 取消读 → `clientCtx.Done()`  
3. `markClientDisconnected` + **WARN** `client disconnected before response completed, continuing generation`  
4. 服务端**继续**读上游并缓冲，会话可完成；客户端侧可能已通过先前 chunk 或重放认为「成功」  
5. 用户在日志中看到每条请求的 disconnect 文案，像失败  

**实际路径（无 streamSession）**

1. 流式但无 conversation id → 客户端一断开，`forward` 直接 `return errClientDisconnected`  
2. attempt 记 **`AttemptFailed` / `client disconnected`**  
3. `metrics.Save(false, …)` → `RequestFailed` + `relayLog.Error=client disconnected`  
4. 若断开发生在 body 已基本写完之后，客户端仍可能显示成功，服务端记失败  

**分叉点**

- `relay.go:452-458`：把 **客户端主动断开** 与 **渠道故障** 一样标成 `AttemptFailed` + `Save(false)`（注释已说不记熔断，但仍失败落库/展示）  
- `streamSession` 路径用 WARN「continuing generation」——业务可成功，日志仍像异常  
- 全局 `reasoning_buffer_strategy=buffer` 在长空闲推理时更容易触发客户端超时断开（相关设计注释见 `type.go:94-97`），加重 B  

## 3. 根因

**根因类型**：逻辑错误（路径不对称 + 失败语义过粗）+ 状态污染（模型级 key 冷却）

**根因描述**

1. **现象 A（直接）**：中继按 `(channel, key, model)` 冷却选 key，而渠道/分组测试取 key **故意不带 model、不查冷却**。因此 key 一旦因真实 ≥400 进入冷却，Codex 稳定 502「no available key…」，UI 测试仍可 200。文案把「冷却中」和「disabled/无 key」混在一句，排障误判为「key 没配好」。DB 显示 koyeb 渠道 key 存在且 enabled，与「冷却态」一致，与「库里没 key」不一致。  
2. **现象 A（诱因）**：冷却通常来自更早的中继 ≥400（免费站/格式/路径等）。探测不写 `RecordKeyCooldown`，无法复现「测通但仍冷却」。  
3. **现象 B**：客户端断开被当作 attempt 失败（及/或 WARN 级「client disconnected…」），成功流式仍刷错误感日志；`streamSession` 下业务可继续成功，日志与成功态不一致。  

**是否有多个根因**：是。

| 优先级 | 根因 | 对应现象 |
|---|---|---|
| 主 | 探测 vs 中继对 key 冷却的不对称 + 空 key 错误文案过粗 | A |
| 主 | client disconnect 记失败/WARN，与「客户端成功」观感冲突 | B |
| 次 | 上游真实 ≥400（免费站/URL/出站格式）导致进入冷却 | A 的前置 |
| 次 | 全局 reasoning buffer 等加剧空闲断开 | B 的加剧因素 |

## 4. 影响面

- **影响范围**  
  - A：所有「测试通、中继因冷却跳过」的渠道，尤其单 key 公益站/站点映射；不限 Codex，任何带 model 的中继都受冷却，但 Codex 高频失败更易踩中。  
  - B：所有流式客户端（Codex/Zed、部分 IDE），有无 `conversation_id` 表现不同。  
- **潜在受害模块**：relay 选 key、渠道测试、分组测试、relay 日志 UI、熔断/冷却运维认知、站点投影渠道（单 key 更脆）。  
- **数据完整性风险**：无业务数据损坏；可能抬高 `RequestFailed`、污染渠道/模型失败统计（B 与 A 的 skip 日志）。A 的 Skip 不记熔断（设计如此），但 502 影响可用性。  
- **严重程度复核**：**维持 P1**。A 阻塞 Codex 用部分映射渠道；B 不挡业务但严重干扰排障。  

## 5. 修复方案

### 方案 A：对齐冷却语义 + 拆分错误文案 + 降噪 disconnect（推荐组合）

**做什么**

1. **A1 — 测试路径可选/默认尊重冷却，或在结果中暴露冷却态**  
   - 最小：探测仍可发请求，但若 key 对「待测 model」处于冷却，结果标记 `passed` 时附带 warning，或提供「忽略冷却」开关；更严：探测对指定 model 走 `GetChannelKeyWithCooldown(modelName, …)`，与中继一致。  
2. **A2 — 拆分 Skip 原因**  
   - `no keys` / `all keys disabled` / `all keys in cooldown (model=…, retry_after≈Ns)` 三选一，替换笼统 `cooldown or disabled`。  
3. **B1 — disconnect 语义**  
   - attempt 状态改为独立状态（如 `AttemptClientClosed`）或成功路径下不把 Error 写成失败红字；  
   - `metrics.Save`：若已有完整上游响应/已写完可见内容，记 success 或独立 client_abort 计数，不进 `RequestFailed`；  
   - WARN 降为 INFO，文案区分「中途取消」与「完成后半截关闭」。  

**优点**：同时修 A 的可观测性/一致性与 B 的日志噪音；改动集中在 probe + key 选择错误分类 + metrics/attempt 状态。  
**缺点 / 风险**：探测「变严」后 UI 测试可能从绿变黄/红，需产品接受；新 attempt 状态要前后端约定。  
**影响面**：`group_probe.go`、`channel.go` 选 key 诊断、`relay.go` skip 文案、`metrics.go` / log UI、可能 `model.ChannelAttempt` 状态枚举。

### 方案 B：仅修中继冷却策略（缩小冷却触发面）

**做什么**

- 仅对 429/401/403 等可恢复限流/鉴权记冷却，或对 adapter fallback 的中间失败不冷却，直到所有 adapter 失败再冷却；  
- 或冷却 TTL 对公益站可配置更短；  
- 不改探测路径。  

**优点**：减少「误冷却」导致的 A。  
**缺点 / 风险**：不解决「测试绕过冷却」的认知差；B 完全不覆盖；错误限流策略可能让坏 key 被更频繁打。  
**影响面**：`RecordKeyCooldown` 调用点、`attempt` 失败分类。

### 方案 C：仅文档/运维绕过（不改代码）

**做什么**

- 文档说明：502 no available key 时先查冷却/等 300s/清冷却；client disconnected 流式可忽略。  
- 运维清 `cooldown:*` 或重启（内存冷却）。  

**优点**：零代码。  
**缺点**：不修根因；P1 体验仍在。  
**影响面**：无代码。

### 推荐方案

**推荐方案 A**（A1 可先做「结果暴露冷却」弱一致，A2 必做，B1 必做），理由：

1. 直接对准「测试 200 vs 中继 no available key」的代码分叉与误导文案；  
2. 直接对准「成功但仍像失败」的 disconnect 记账；  
3. 方案 B 可作 A 的增强（减少误冷却），不宜单独作为唯一修复。  

建议实现顺序：

1. **A2** 拆分空 key 原因（小、高排障价值）  
2. **B1** disconnect 不记渠道失败 / 降噪（对应用户「每条都是错误」）  
3. **A1** 探测与中继冷却对齐或明确提示  
4. 可选 **B 方案** 收紧 RecordKeyCooldown 触发条件（需对照 koyeb 首次 ≥400 日志）
