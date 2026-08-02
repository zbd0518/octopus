---
doc_type: issue-report
issue: 2026-07-26-codex-site-channel-key-and-client-disconnect
status: confirmed
issue_path: standard
severity: P1
summary: Codex 走 gpt-5.5 分组时，站点映射渠道 deepseek 报 no available key 502；换可用渠道后业务成功但每个请求日志出现 client disconnected
tags:
  - codex
  - relay
  - channel-key
  - site-channel
  - client-disconnect
  - responses
---

# Codex 站点映射渠道 no available key 与 client disconnected 日志 Issue Report

## 1. 问题现象

同一业务场景（Zed 下 Codex → 本机 octopus → 分组 `gpt-5.5`），观察到两条相关现象：

### 现象 A — Codex 502 / no available key

- 在**渠道测试详情页**对模型点「测试模型」→ **成功，HTTP 200**
- 在 **Cherry Studio** 调同一模型 → **成功**
- 用 **Codex** 连接本机 octopus（`http://localhost:3000/v1/responses`）→ **失败**，错误类似：

```text
unexpected status 502 Bad Gateway:
{"code":502,"message":"all channels failed: channel koyeb-免费/免费key/default-Chat: no available key (all keys in cooldown or disabled)"},
url: http://localhost:3000/v1/responses
```

- 涉及渠道名：`koyeb-免费/免费key/default-Chat`（站点映射渠道）
- 涉及模型：`deepseek-v4-flash`（用户复现时）
- 用户怀疑线索：该渠道从站点映射而来；账号以 API Key 方式保存；映射渠道 base URL 若以 `/v1` 结尾会报错，已去掉 `/v1`

### 现象 B — 业务成功但日志 client disconnected

- 在同一 `gpt-5.5` 分组下**移除其他模型，只保留另一个可工作渠道模型**后，**Codex 运行正常**
- 但 octopus 日志中**几乎每个请求**都出现 **`client disconnected`** 类信息
- 用户**未开启完整请求日志**；业务侧表现成功，日志仍像失败/异常，需要额外排查

两条现象属于**同一分组、同一 Codex 接入链路**；A 在特定站点映射渠道上失败，B 在换可用渠道后业务通但日志噪音。

## 2. 复现步骤

### 路径 A（502 / no available key）

1. 本机可执行文件启动 octopus（分支 `fix/site-model-case-pk-conflict`）
2. 使用分组 **`gpt-5.5`**；Codex（Zed）绑定该分组
3. 分组内挂上站点映射渠道 **`koyeb-免费/免费key/default-Chat`** 与模型 **`deepseek-v4-flash`**（用户描述每次只放一个模型）
4. 打开该渠道详情 → 点「测试模型」→ **成功 200**
5. Cherry Studio 调同一模型 → **成功**
6. Codex 经 `http://localhost:3000/v1/responses` 请求该模型
7. 观察到：502 + `no available key (all keys in cooldown or disabled)`，渠道名含 `koyeb-免费/免费key/default-Chat`

### 路径 B（client disconnected 日志）

1. 同一 `gpt-5.5` 分组，去掉其它模型，只保留「可工作」的渠道模型（当前库快照中分组成员为 `deepseek-数字站` / `deepseek-v4-pro`，以用户实际可工作配置为准）
2. Codex 正常跑任务，客户端侧成功
3. 观察 octopus 日志：每个请求都有 `client disconnected` 相关信息

复现频率：**A、B 均为稳定 100%**（用户确认）。

## 3. 期望 vs 实际

### 现象 A

**期望行为**：Codex 经 octopus 访问该分组/模型时能正常访问（与渠道测试、Cherry Studio 一致可用）。

**实际行为**：返回 502，消息为 `no available key (all keys in cooldown or disabled)`（渠道 `koyeb-免费/免费key/default-Chat`）。

### 现象 B

**期望行为**：成功完成的请求不应被记成错误/失败类日志，避免误导排障。

**实际行为**：Codex 业务成功，但每个请求日志仍出现 `client disconnected`，需要仔细排查才能判断是否真实故障。

## 4. 环境信息

- 涉及模块 / 功能：
  - 中继 `/v1/responses`（Codex）
  - 渠道测试 / 分组探测
  - 站点映射渠道（new-api 类站点）
  - Key 冷却 / 可用选择
  - 流式响应与 client disconnect 日志
- 相关文件 / 函数：**待定**（analyze 阶段定位；线索含 `GetChannelKeyWithCooldown`、`no available key` 跳过路径、`errClientDisconnected` / `clientDisconnectedLogMessage`）
- 运行环境：本机可执行文件启动（dev）
- 客户端：Zed 下 Codex；对照 Cherry Studio 成功
- 代码分支：`fix/site-model-case-pk-conflict`
- 分组：`gpt-5.5`（id=2），`endpoint_type=*`，`mode=3`；用户每次只放一个模型；系统默认设置
- 现象 A 渠道线索：
  - 名称：`koyeb-免费/免费key/default-Chat`
  - 站点映射：`site_channel_bindings` 有绑定（site_id=79 等）
  - 类型：new-api 系；1 个 key、启用；endpoint 用户认为偏 chat
  - base URL：`https://new-api.koyeb.app`（用户称曾去掉尾部 `/v1`）
  - DB 快照：channel id=99 enabled；channel_keys 有 1 条 enabled（remark「免费key1」）
- 现象 B 当前库快照（可能已与复现当时不同）：`group_items` 仅 `deepseek-数字站`(249) / `deepseek-v4-pro`
- 最近改动：用户称未为此问题专门改配置；可查本地启动配置与 DB；本机有历史相关 issue（如 empty key 投影、relay log 等，需 analyze 区分）
- 其他：未开完整请求日志

## 5. 严重程度

**P1 严重** — Codex 主路径对部分站点映射渠道直接 502；换渠道后虽可绕过，但 client disconnected 日志噪音影响排障。用户确认按 P1。

## 备注

- 渠道测试 / Cherry 成功 vs Codex `/v1/responses` 失败，说明**探测路径与 Codex 中继路径在 key 选择、冷却、endpoint 或请求形态上可能不一致**（仅现象线索，非根因结论）。
- 错误文案 `all keys in cooldown or disabled` 在中继跳过 key 时写入；DB 中 key 显示 enabled，是否运行时未加载、冷却内存态、或空 key 字符串等需 analyze 验证。
- 现象 B 日志文案可能对应 `client disconnected` / `client disconnected before response completed, continuing generation` 等；是否「客户端正常关流仍标失败」待 analyze。
- 快速通道判定：**不满足**——A 无已确认 file:line 根因；A+B 跨探测/中继/日志多路径；修复点预计 >2。走 **standard**（report → analyze → fix）。
