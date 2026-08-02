---
doc_type: feature-design
slug: model-probe-prompt-setting
status: approved
execution_lane: standard
execution_lane_reason: 新增 Setting 契约 + 后端读配置改测活请求构造 + 设置页 UI；非单点前端补丁
date: 2026-07-23
requirement: ""
summary: 全局可配置模型测活提示词，默认 hi，成功判定仍只看 HTTP/可解析
tags:
  - setting
  - probe
  - group-test
---

# 模型测活提示词可配置

## 0. 术语

| 术语 | 含义 |
|------|------|
| **模型测活** | 分组/草稿/渠道单模型测试：对上游发一次最小 LLM 请求，判断模型是否可用 |
| **Key 巡检** | `helper.TestChannel` / `key_health_check`：`GET /models` 连通性，**不走 chat 提示词** |
| **测活提示词** | 模型测活请求中的用户文本（对话 `messages[].content` 或 embedding `input`） |
| **Setting** | 全局 KV 配置（`model.SettingKey` + 设置页） |

## 1. 目标与约束

### 用户目标

许多上游拦截极短/像探测的输入（如 `hi`）。希望在**设置**中自定义测活提示词，使分组测模型、渠道测模型等走同一可配置文案。

### 已确认决策

1. **范围**：全局 Setting **一项**（对话 + embedding 共用同一字符串）。
2. **成功判定**：保持现状——HTTP 2xx 且响应可被 outbound 解析即通过；**不**校验回复内容。
3. **默认值**：`hi`；空字符串 / 仅空白 → 回退 `hi`（行为与现网一致）。

### 明确不做

- 不改 Key 巡检（`/models`）。
- 不做按渠道 / 按模型覆盖。
- 不做「期望回复子串」校验。
- 不新增 `max_tokens` 等其它测活参数（本轮）。
- 不改测活并发、进度 API 路径、成功/失败落库语义。

### 复杂度档位

默认档位：普通应用内配置扩展，无新对外 SDK / 无迁移表结构。

### 放在哪儿

- **配置权威**：现有 Setting 子系统（`SettingKey` + `DefaultSettings` + 设置 API）。
- **消费点**：`helper.buildGroupProbeRequest`（及由其驱动的分组/草稿/渠道模型测活）。
- **UI**：设置页与「重试 / Key 巡检」同一运行时配置区域（`SettingRetry` 或同级设置卡片）。

### Top 3 风险

| 风险 | 缓解 |
|------|------|
| 改提示词后旧单测仍写死 `"hi"` | checklist 强制更新 `group_probe_test`；默认回退路径单测保留 `"hi"` |
| 设置未 seed / 读失败导致 panic 或空 body | 读失败或空值统一回退默认；不阻断测活 |
| 用户以为改了提示词也改了 Key 巡检 | UI 文案标明仅作用于「模型测活」，不写 Key 巡检 |

### 关键假设

- 假设：一项字符串同时用于 chat content 与 embedding input 足够（owner 已选）。
- 假设：不需要 DB migration（Setting 缺 key 时走默认 seed / 读默认即可，与现有 Setting 模式一致）。

### 必跑验证

```text
go test ./internal/helper/ -count=1
# 或更窄：group_probe 相关测试
pnpm --dir web test:i18n   # 若改 locale
```

基线：仓库其它包可能已有无关红灯；本 feature 归因以 helper + setting 相关测试为准。

### 清洁度

禁止：调试 `fmt.Print`、临时 TODO、注释掉的旧硬编码块残留。默认常量可保留为 `defaultGroupProbePrompt = "hi"`。

---

## 2. 方案

### 2.1 名词层

**现状**

- `buildGroupProbeRequest` 对话与 embedding 均硬编码 `"hi"`（`internal/helper/group_probe.go`）。
- Setting 无测活提示词 key；已有 `key_health_check_*` 仅管 Key 巡检。
- 前端 `SettingKey` / `Retry.tsx` 已展示 Key 巡检配置。

**变化**

| 名 | 定义 |
|----|------|
| `SettingKeyGroupProbePrompt` | 字符串 key，建议值：`group_probe_prompt` |
| 默认值 | `"hi"`，写入 `DefaultSettings()` |
| 解析规则 | `strings.TrimSpace(value)`；空则 `"hi"` |
| 公开契约 | 既有 `GET/PUT` Setting API，无新 REST 资源 |

示例：

```text
PUT setting group_probe_prompt = "请用一句话介绍你自己"
→ 随后任意模型测活请求 user content / embedding input 使用该文案

PUT group_probe_prompt = "   "
→ 行为等同 "hi"
```

### 2.2 编排层

线性流程（免图：无分支状态机）：

1. 用户在设置页保存 `group_probe_prompt`。
2. 启动模型测活（分组 / 草稿 / 渠道单模型）→ 现有 `Start*GroupModelTest` / `runGroupModelTest`。
3. `sendGroupProbeRequest` → `buildGroupProbeRequest`。
4. **变化点**：构造 messages/embedding 文本时读取 Setting 并 apply 回退规则，不再字面量 `"hi"`。
5. 成功判定逻辑不变。

Key 巡检路径不进入本编排。

### 2.3 挂载点（删则 feature 消失）

1. Setting key 常量 + 默认值 seed。
2. `buildGroupProbeRequest`（或紧邻的 prompt 解析函数）读配置并注入文本。
3. 设置页控件 + i18n 文案。
4. 前端 `SettingKey` 枚举镜像（与现有 key 同步方式一致）。

### 2.4 推进策略

| Step | 交付 | 退出信号 |
|------|------|----------|
| 1 | Setting key / 默认（无严格 Validate，与 free-string Setting 对齐） | `DefaultSettings` 含新 key 且默认 `hi`（建议专用小单测，勿依赖泛化 `-run Setting`） |
| 2 | `resolveGroupProbePrompt` + 注入 `buildGroupProbeRequest`；单测覆盖默认 / 自定义 / 空白 / GetString err | `go test ./internal/helper` 绿；四类 resolve 断言 + 请求体注入断言 |
| 3 | 运维 → 维护 → 重试区 UI + i18n | 可编辑保存；hint 标明仅模型测活、不影响 Key 巡检；空白回退说明 |
| 4 | 回归：Key 巡检与默认 `hi` 行为 | Key 巡检代码路径无 diff 行为变化；默认仍 hi |

### 2.5 结构健康度

- **文件级**：`group_probe.go` 已偏大，但本改动是单点字符串注入。结论：**不做**整文件拆分；若实现时加 `resolveGroupProbePrompt()` 小函数，可同文件或同包内极小辅助，避免再塞 UI。
- **目录级**：Setting 与 helper 目录不摊平；新逻辑不另起子系统。
- **超出范围观察**：未来若要做渠道级覆盖，再开 feature，不在本 design 扩 scope。

---

## 3. 验收契约

### 场景

| # | 触发 | 期望 | 证据 |
|---|------|------|------|
| N1 | 未改设置，跑分组/渠道模型测活 | 请求体文本仍为 `hi` | 单测 / 抓包或 mock 断言 |
| N2 | 设置自定义非空字符串后测活 | 请求体使用该字符串（trim 后） | 单测 |
| N3 | 上游 2xx 且响应可被 outbound 解析，assistant content 任意 | `Passed=true`，不匹配回复文案 | 现状 `sendGroupProbeRequest` 语义 + code review / 既有单测 |
| B1 | 设置为空或纯空白 | 回退 `hi` | 单测 |
| B2 | Setting 读失败（missing key / GetString err） | 回退 `hi`，测活不崩溃 | 单测（resolve 函数） |
| E1 | 上游仍 4xx/5xx | 仍判定失败（与现网一致） | 既有失败路径 / 手工 |
| R1 | 改测活提示词后跑 Key 巡检 | Key 巡检仍走 `/models`，不受该设置影响 | diff review + 可选手工 |
| X1（不做） | 设置「期望回复」 | UI/API 不存在该能力 | grep 反向 |

### Acceptance Coverage Matrix

| 需求 | 场景 |
|------|------|
| 可自定义测活提示词 | N2 |
| 默认兼容 | N1, B1 |
| 读配置失败仍可测活 | B2 |
| 仅模型测活 | R1 |
| 不校验回复内容 | N3 + 不做 X1 |
| 失败路径不变 | E1 |

### DoD Contract

- [ ] 新 Setting key 有默认值且前后端 key 字符串一致
- [ ] 模型测活路径使用配置文案；空回退 `hi`
- [ ] 设置页可配置且文案说明范围
- [ ] helper 相关单测更新并通过
- [ ] 无 Key 巡检行为变更

---

## 4. 自我批判（起草后已修）

1. **可证伪**：验收表为 yes/no 场景，避免「体验更好」类表述。
2. **原子步**：Setting → 构造 → UI → 回归四步，可独立验证。
3. **最弱依赖**：构造层读 Setting；读失败必须回退，避免测活整体不可用。
4. **证据**：以 helper 单测为主；UI 为页面保存 + i18n。
5. **不做项**已可 grep 反向核对。
