---
kind: issue
title: "隐私保护：请求侧敏感信息拦截/脱敏与响应占位符还原"
type: feature
status: open
created: 2026-09-18
---

<!--
对齐竞品能力：中转站「隐私保护」——请求先在本站检查一遍：
拦截请求 = 命中就不发给上游、直接报错；
自动过滤 = 把敏感内容替换成占位符再发给上游，上游回复里的占位符会在返回给你之前还原成原文。
关键词总开关关闭则请求原样发送，不做任何检查。
-->

# 隐私保护：请求侧敏感信息拦截/脱敏与响应占位符还原

> **读者：** 跨会话接手的人——「要做成什么、别碰什么、现状与方案是否还成立、怎么验、关了要回写哪里」。
> **自检：** 目标与范围 · 背景 · 现状怎么工作 · 影响面 · 方案/设计 · 风险与穿刺 · 验证 · 执行记录 · 关闭回写与结论。

---

## 做成以后是什么样

管理员在 Web 面板开启「隐私保护」总开关后，发往模型的**请求**先在本站做一遍敏感信息检测：

- **拦截请求（block）**：命中就不发给上游，直接返回 `content_filter` 风格错误（与现有输出拦截 `errResponseFilterBlocked` 同语义：不换 Key/渠道重试、不记渠道失败）。
- **自动过滤（filter）**：把命中的敏感内容替换为占位符（如 `⟪PII-1⟫`）再发给上游；上游回复中出现的占位符在返回客户端之前**还原成原文**。流式与非流式均支持。
- 总开关关闭时请求原样发送，不做任何检查。

检测类别（每类独立开关 + 独立动作 block/filter）：

| 类别 | 检测方式 |
|---|---|
| 密钥 / Token（`sk-`、`ghp_`、AKIA、Bearer 等） | 正则 |
| 账号密码等凭据（`password=` / `api_key: 递类` 等「字段名 + 值」写法） | 正则 |
| 手机号（中国大陆 + 国际 + 号格式） | 正则 |
| 邮箱地址 | 正则 |
| 身份证号（18 位，含校验位验证） | 正则 + GB11643 校验位 |
| 银行卡号（13–19 位数字 + Luhn 校验） | 正则 + Luhn |
| 高熵随机串（统计上像密钥的随机字符串） | 熵计算 + 长度/字符集启发式（误报最高，默认关或仅允许 filter） |
| 自定义敏感词 | 字面关键词或用户自定义正则 |

**范围：** 包含后端检测引擎、请求侧挂点、响应还原（含流式）、设置项与校验、前端设置组件（放置于设置页；运维中心可选复用）、三份 locale 词条。
**不包含：** 响应侧新增关键词（现有 `response_filter_*` 输出拦截保持原样不动）、图片/音频等多模态内容检测、`/v1/embeddings` 之外的非文本端点。

**归属：** 独立 feature issue。相关 spec：`codestable/spec/index.md`（中继链路）、现有输出拦截设计参照 `internal/relay/response_filter.go`。

## 为什么现在做 / 当前坏在哪

竞品中转站已内置请求侧隐私保护（见会话截图：总开关 + 8 类检测器 + 每类独立动作）。本项目目前只有**输出侧**关键词拦截（`response_filter_*`，4 个设置项 + `internal/relay/response_filter.go`），对**请求**中用户不小心带上的密钥、身份证、银行卡等敏感信息不做任何处理，明文发往上游。

## 现状怎么工作（设计时已确认）

- 输出拦截链路（可复用的地基，约 40%）：
  - 配置：4 个设置项 `response_filter_enabled/keywords/action/error_message`（`internal/model/setting.go:98-101`，默认值 `setting.go:202-205`，校验 `setting.go:436-478`）。
  - 引擎：`internal/relay/response_filter.go` — `extractResponseText`（提取 Choices 文本，含 Message/Delta/Content/MultipleContent）、`findMatchedKeyword`（大小写不敏感 Contains）、`replaceKeywordsInText`（等 rune 长度 `*` 替换，`EqualFold`）、`applyResponseFilter`（block/replace 两动作）。
  - 挂点：非流式 `handleResponse`（`relay.go:1307-1322`，返回 200 + `content_filter`/`content_blocked` JSON）；流式 `transformStreamData`（`relay.go:1283-1288`）→ 命中后 flush reasoning buffer、发错误 SSE 事件并终止流（`relay.go:1135-1161`）。
  - 重试语义：`errResponseFilterBlocked` → `ScopeAbortAll`，不重试不记渠道失败（`relay.go:411-420`）。
  - 配置缓存：`relayAttempt.getResponseFilterConfig()` 懒加载缓存（`type.go:279-292`），避免流式逐 chunk 读 setting/解析 JSON。
  - 前端：`web/src/components/modules/setting/ResponseFilter.tsx`（设置项读写），运维中心「维护策略」页签直接复用该组件（`web/src/components/modules/ops/Maintenance.tsx:31`）。
- 请求链路挂点候选：入站解析后 `internalRequest`（`InternalLLMRequest`，`transformer/model/model.go:35`）在 `forward()`（`relay.go:566`）前可按请求（跨 attempt）共享处理；`forward()` 内 `prepareInternalRequestForOutboundWithProvider`（`relay.go:586-598`）是 per-attempt 的改写点。**注意 passthrough/raw 适配器跳过 `prepareInternalRequestForOutbound` 原样转发**（`relay.go:586`）。
- 请求文本结构：`Message.Content.Content` / `MultipleContent`（`model.go:576-617`）、`EmbeddingInput`；现有 `extractResponseText` 只覆盖响应，**请求版提取需新写**（覆盖 system/user/tool 消息与 EmbeddingInput）。

## 动哪些、验哪些

- **必须改：**
  - `internal/model/setting.go`：新增 `privacy_protection_enabled` + `privacy_protection_config`（JSON：每类 `{enabled, action}` + 自定义条目 `{type: keyword|regex, pattern, action}`），`DefaultSettings()` 默认值、`Validate()` 校验（regex 必须可编译、action 枚举）。
  - 新建 `internal/relay/privacy/`：检测引擎（8 类检测器 + 自定义），接口统一为「文本 → 命中片段列表」；正则编译一次缓存。
  - `internal/relay/`：请求侧挂点（block 返回 `content_filter` 错误，语义对齐 `errResponseFilterBlocked`；filter 写占位符 + 映射表）、响应侧还原挂点、`type.go` 配置懒加载缓存。
  - 前端：新组件 `web/src/components/modules/setting/PrivacyProtection.tsx`（总开关 + 8 行类别 Switch + 动作下拉 + 自定义词/正则编辑区），`setting.ts` SettingKey，`zh_hans/zh_hant/en` locale。
- **需要验证：**
  - passthrough/raw 适配器：隐私过滤是否也跳过（建议跳过并在 UI 说明，避免改原始 body 路径）。
  - 语义缓存时序：请求脱敏后缓存 key 用脱敏文本；响应**入库前必须先还原**（`storeSemanticCacheResponse`，`relay.go:1339`），否则缓存污染。
  - 中继日志：`RequestContent` 应记脱敏后文本（写入点需在脱敏之后）；这也正是功能卖点。
  - 重试幂等：脱敏挂在请求级（非 per-attempt），换 Key/渠道重试复用已脱敏 `internalRequest`，占位符替换幂等。
  - 流式 reasoning buffer flush 前需过还原（`writeReasoningBuffer`）。
- **仍未知（穿刺项）：**
  - 流式跨 chunk 占位符还原：上游可能把占位符按 token 边界拆进多个 chunk，逐 chunk 替代会漏。需尾部滚动缓冲（每 chunk 只刷出「不可能是占位符前缀」的部分，攒齐跨 chunk 占位符再还原）。整个功能唯一高风险点。
  - 高熵检测误报阈值需调参。

## 方案与实现安排

数据/请求路径：

1. 请求进入、入站解析得到 `internalRequest` 后（`parseRequest` → `relay.go:545-562`），若隐私保护开启：提取全部文本（system/user/tool + EmbeddingInput）→ 逐类检测 → block 则直接返回 `content_filter` 错误（不产生 attempt/熔断记录）；filter 则把命中片段替换为占位符并把「占位符 ↔ 原文」映射表存**请求级共享结构**（不是 `relayAttempt`，跨重试共享）。
2. `forward()` 照常转发已脱敏的 `internalRequest`。
3. 响应回来：非流式在 `handleResponse` 现有 `applyResponseFilter` 调用点旁做占位符还原（含 `tool_calls` arguments JSON 里的占位符）；流式在 `transformStreamData` 输出侧用尾部滚动缓冲还原。
4. embeddings 只有请求侧脱敏，无还原问题。

不碰的边界：现有 `response_filter_*` 输出拦截代码不动、行为不变；passthrough/raw 透传语义保持（过滤跳过与否按「需要验证」结论定）。

实施顺序（风险驱动）：

1. 配置层 + 检测引擎 + 纯单测（无风险，一次到位）。
2. **穿刺**：非流式「请求脱敏 → 上游 → 响应还原」全链路端到端打通。
3. 加厚：8 类检测器调参、自定义正则、embeddings、语义缓存/日志时序。
4. 流式还原（最后做，先做尾部缓冲最小验证）。
5. 前端 UI。

## 验证

- 检测引擎单测：每类检测器 happy path + 边界（身份证校验位错误不算命中、Luhn 不过不算命中、高熵阈值边界、自定义 regex 非法配置被 `Validate` 拒绝）。
- 穿刺验证：真实上游一次非流式请求，请求中带 `sk-xxx`，观察上游收到占位符、客户端收到还原后的原文。
- block 动作：命中后客户端收到 `content_filter` 错误，日志无渠道失败记录、无重试。
- 流式：构造跨 chunk 占位符场景（mock 上游按 token 切分），客户端收到完整还原文本；客户端断开/异常路径不挂死。
- 回归：`response_filter_*` 输出拦截行为不变；隐私开关关闭时请求原样转发。

## 执行记录

### 第 1 步：配置层 + 检测引擎（2026-09-18）

- `internal/model/privacy.go`（新增）：`PrivacyCategory`（8 类）、`PrivacyAction`（block/filter）、`PrivacyRule`、`PrivacyProtectionConfig`（`categories` map + `rules` 数组）、`ValidatePrivacyProtectionConfig`。类别未配置默认开启、动作默认 `filter`。
- `internal/model/setting.go`：新增 `privacy_protection_enabled`（默认 `false`）与 `privacy_protection_config`（默认 `{"categories":{},"rules":[]}`），含 `Validate()` 校验。
- `internal/relay/privacy/`（新增包）：
  - `privacy.go`：`Matcher` 接口（`Find(text) []Match`，无状态可并发）、`Run` 聚合、通用 `regexMatcher`。
  - `builtin.go`：密钥/Token（sk-、ghp_、AKIA、xox、Bearer）、凭据写法（password=/api_key: 等）、手机号、邮箱正则检测器。
  - `validators.go`：身份证（GB11643 校验位）、银行卡（Luhn）、高熵随机串（Shannon 熵 ≥3.8 bits/char，长度 20–64，纯数字跳过）。
  - `custom.go`：自定义 keyword（大小写不敏感）/ regex 检测器。
- 测试：`internal/relay/privacy/privacy_test.go`，覆盖 8 类检测器 happy path + 边界（校验位错误不命中、Luhn 不过不命中、低熵不命中、大小写不敏感 keyword）+ 配置校验（非法 JSON/未知类别/非法 action/非法 regex）。`go build ./...`、`go test ./internal/model/ ./internal/relay/...` 全绿。
- 与方案偏差：无结构性偏差；凭据检测对 `(?i)` 分组用了非捕获组避免匹配污染（实现细节）。
- 下一步：第 2 步穿刺——非流式「请求脱敏 → 上游 → 响应还原」全链路。

### 第 2 步：非流式穿刺（2026-09-18）

- `internal/relay/privacy_filter.go`（新增）：
  - `loadPrivacyFilterConfig`：读总开关 + 解析 config JSON + 构建检测器（内置 + 自定义，类别开关过滤）。
  - `privacyPlaceholderMap`：双向映射（原文↔占位符 `⟪PII-N⟫`），同一原文幂等返回同一占位符（重试安全）。
  - `collectPrivacyTargets`：收集请求全部文本回写指针（system/user 消息、MultipleContent text、EmbeddingInput Single/Multiple）。
  - `applyPrivacyProtection`：两遍扫描——先 block（命中即拦截、请求保持原样），后 filter（就地替换为占位符）。
  - `restorePrivacyPlaceholders`：还原响应 Choices 的 Content/MultipleContent/ToolCalls.Arguments。
- 挂点：
  - `relay.go` Handler：`parseRequest` 后立即执行隐私处理；block 返回 200 + `content_filter`/`content_blocked` JSON（与输出拦截同形状），不进渠道选择/重试/熔断。占位符映射存 `relayRequest.privacyMap`（type.go 新增字段），跨重试共享。
  - `relay.go` handleResponse：`outAdapter.TransformResponse` 后、输出关键词拦截前，调用 `restorePrivacyPlaceholders`——输出拦截、入站序列化、metrics/日志拿到的都是还原后的文本。
- 测试：`privacy_filter_test.go` 10 个用例（block/filter/开关关闭/类别禁用/多消息+embedding/自定义词/幂等掩码/toolcall 还原/nil 映射/默认配置）全过；`go build ./...` + relay/privacy/model 三包测试全绿。
- 与方案偏差：
  1. 脱敏直接就地改写 `internalRequest`（原设计说「返回副本」）——请求级只处理一次且无并发读者，指针替换反而引入别名风险，就地更简单。
  2. 响应/日志记录的是**还原后**文本（设计阶段「需要验证」里曾倾向日志记脱敏后文本）——还原发生在入站序列化之前，自然保持一致；请求侧日志 `m.InternalRequest` 仍是脱敏后的（功能卖点保留）。
- 遗留到第 3 步：语义缓存命中路径（`buildSemanticCacheHitInternalResponse` 的响应未经还原）与流式路径；passthrough/raw 跳过行为确认。

### 第 3 步：加厚（语义缓存 + passthrough/raw）（2026-09-18）

- **语义缓存：确认无污染，未改代码**。查证链路：查缓存用 `req.internalRequest`（脱敏后）的向量做 lookup；写入用 `storeSemanticCacheResponse(ctx, ra.internalRequest, inResponse)`（relay.go:1375），而 `inResponse` 产生于 `inAdapter.TransformResponse`，此时还原已完成 → 缓存里存的是「脱敏请求 + 还原后响应」。命中时 `maybeServeSemanticCacheHit` 直接下发 payload，客户端拿到的即还原后文本。存/查同态（都用脱敏请求），一致性成立。
- **passthrough/raw 泄漏修复**：这两个适配器转发的是原始 body `InternalLLMRequest.RawRequest`（`passthrough.Outbound.TransformRequest` 用 `RawRequest` 而非结构体），`internalRequest` 上的脱敏对它不可见 → 真实泄漏点。修复：`forward()` 的 else 分支对 `requestForOutbound.RawRequest` 调 `privacyMap.maskRawBody()`，按已记录映射做字节替换。已知边界（代码注释已写）：值中含 JSON 转义字符（引号/换行）时原文匹配不到，不替换；内置类别极少含这些字符。

### 第 4 步：流式还原（2026-09-18）

- **关键取舍（与初始设想不同）**：最初设想在**入站序列化后的 SSE 字节**上做尾部滚动缓冲。实测不可行——跨 chunk 的占位符之间隔着 SSE 帧结构（`\n`、JSON 引号），字节层面不连续，且还原发生在序列化后时占位符已被 JSON 转义污染。改为在**内部 chunk 的 Delta/Message 文本**上还原（`outAdapter.TransformStream` 之后、`inAdapter.TransformStream` 序列化之前），挂点 `transformStreamData`。
- `internal/relay/privacy_stream.go`（新增）`privacyStreamRestorer`：
  - `restoreText`：带 carry 的文本还原。完整占位符就地还原；尾部若为占位符模式的真前缀（含被拆断的多字节字符）则扣留到下一 chunk 拼接后再扫。用 `isPatternPrefix` + `trimTrailingPartialRune` 处理 UTF-8 字节边界（如 `⟪` 的 3 字节被拆开），`leadPrefixHoldLen` 扣留被拆断的 `⟪` 前导字节。
  - `restoreCompleteOnly`：reasoning / tool_call arguments 只还原 chunk 内完整占位符（不做 carry——它们的 JSON 片段化还原收益低）。
  - 挂载：`relayAttempt.privacyStream`（attempt 级，换渠道重试新建 attempt，carry 不跨渠道污染），在 `relayAttempt{...}` 构造时由 `req.privacyMap.newStreamRestorer()` 初始化（无脱敏→nil→零开销）。
  - 流结束 carry 残留（上游发出被截断的占位符骨架）随尝试丢弃：只丢几个骨架字符，无隐私泄漏。
- 测试：`privacy_stream_test.go` 10 个用例，含**逐字节切分点**用例（穷举占位符每个切分位置，全部还原正确）。该用例在实现过程中抓出两个真实 bug：`⟪` 自身字节被拆断未扣留、尾部不完整 UTF-8 字节导致误判分歧——均已修复。

### 第 5 步：前端（2026-09-18）

- `web/src/components/modules/setting/PrivacyProtection.tsx`（新增）：总开关 + 8 类检测项（每行 Switch + 动作 Select：拦截请求/自动过滤）+ 自定义关键词/正则编辑区（前端先校验正则合法性）。状态管理范式和 `ResponseFilter.tsx` 一致（`intendedValuesRef` / `hasLocalIntentRef` / `inFlightValuesRef` 防抖回写）。
- `setting.ts` 加 `PrivacyProtectionEnabled` / `PrivacyProtectionConfig`；`Maintenance.tsx`（运维中心维护策略页签）挂载该组件，排在被熔断器之后的 `privacy-protection` 段。
- 三份 locale（zh_hans / zh_hant / en）补齐 `setting.privacyProtection.*`，文案与截图一致。
- 校验：`eslint` 目标文件无告警；`npm run test:i18n` 通过；`tsc` 对新增/改动文件无新增错误（仓库存在既有测试文件类型错误，已用 stash 对照确认为存量）。

### 当前状态（2026-09-18）

五步全部完成。`go build ./...`、`go test ./internal/relay/ ./internal/relay/privacy/ ./internal/model/`、`go vet` 全绿；前端 eslint 与 i18n 测试通过。

### 构建与端到端验证（2026-09-19）

- **本地可执行文件已构建**：`build/bin/octopus-windows-x86_64.exe`（69.4 MB，含内嵌前端，`static/out` 为 9-18 重建）。流程：`pnpm build` → `web/out` 移到 `static/out` → `go build -tags jsoniter -ldflags`（Version/BuildTime/Author/Commit 从 git 注入）。`version` 子命令验证元数据正确。
- **端到端冒烟**：起真实服务 + mock 上游，通过管理 API 建渠道/分组/API Key、开隐私保护，发中继请求。日志确认 `[隐私保护] 请求已脱敏: placeholders=1`——**脱敏在真实链路上生效**。但因 SSRF 护栏默认拦截 127.0.0.1 loopback，HTTP 冒烟未能打通（这是安全默认，不应在生产放行）。
- **改为 Go 集成测试**（新增 `internal/relay/privacy_e2e_test.go`），用官方测试开关 `xurl.SetSSRFAllowPrivateForTest(true)` 放行 loopback，走真实 outbound/inbound 适配器 + httptest 上游：
  - `TestPrivacyProtectionRequestMaskedUpstreamAndRestoredToClient`：断言上游收到占位符（无手机号泄漏）+ 客户端拿到还原后的原文。
  - `TestTransformStreamDataRestoresPlaceholder`：验证处理链路上实际调用点 `transformStreamData`（流式 chunk 转换入口）确实触发占位符还原。
  - 两个测试均通过。
- 说明：SSRF 护栏（`xurl/AssertSafeHost`）生产路径不放宽，注释明确「生产永远 false」——未改动它，符合安全默认原则。
- 验证累计覆盖：21 个单元/集成测试全绿（检测、脱敏、跨 chunk 还原、配置校验、端到端）。

收尾修正：`DefaultMatchers()` 原本在每次中继请求上调用 `regexp.Compile`（热路径重复编译），改为包级预编译 `*regexp.Regexp`，`DefaultMatchers()` 只做切片装配。

存量失败（与本改动无关，已用 stash 对照确认）：`internal/server` 的 `TestAllManagementWriteRoutesAreAudited` 因 issue 018 新增的 `POST /api/v1/pool/:id/account/clear` 未登记审计路由而失败。

**未做**（如需可继续）：真实上游端到端手测（issue「验证」章节的穿刺验证项）——本次以单元测试覆盖检测、脱敏、跨 chunk 还原、配置校验各层，未跑真实上游；运维中心概览字段填充（与本功能无关的既有遗留）。

## 关闭时

- 回写候选：project spec「当前已稳定的能力」加一条隐私保护语义；`internal/relay/` 链路说明如 spec 有中继细节则同步。
- 关闭判断：上述验证全过且主路径端到端可演示。
- 遗留：高熵阈值调参结论、运维中心概览字段（`OpsSystemSummary` 里 `ResponseFilter*` 预留字段未填充的问题如仍未处理，可顺带记录）。
