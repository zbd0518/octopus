---
doc_type: audit-index
audit: 2026-07-23-request-cli-channel-spoof
scope: 出站请求特殊处理（header 转发/改写、Codex CLI 伪装、跨客户端使用限制渠道）
created: 2026-07-23
status: active
total_findings: 4
dimensions: [bug, security, maintainability]
---

# request-cli-channel-spoof 审计报告

## 范围

用户问题（审计视角）：**数据请求时是否有特殊处理？若渠道限制只能在 Codex CLI 使用，用 Claude Code 时如何走该渠道？**

扫描目录 / 关键文件：

- `internal/relay/relay.go` — 转发、`copyHeaders`
- `internal/relay/rewrite.go` / `internal/transformer/rewrite/*` — 请求体与 header 重写
- `internal/model/channel.go` — `CustomHeader`、`RequestRewriteConfig`
- `internal/transformer/outbound/codex/outbound.go` — Codex 订阅出站
- `internal/transformer/outbound/openai/{chat,response}.go`、`anthropic/messages.go` — 各协议默认 header
- `web/src/components/modules/channel/Form.tsx`、`web/src/api/endpoints/channel.ts` — 渠道配置 UI / 枚举

**未扫**：全仓库、arch-drift（无 ADR）、性能热点路径细节。

## 总评（速答优先）

**有，而且分三层。** 用 Claude Code 打「只允许 Codex CLI / Codex Desktop 特征」的中转站时，核心靠：

1. **客户端头默认透传**（`User-Agent` 等不在 hop-by-hop 黑名单里）
2. **渠道 `custom_header`** 强制覆盖
3. **`request_rewrite.header_profile = "codex"`** 注入 Codex Desktop 风格 `User-Agent` / `Origin` / `Referer`

其中 **`header_profile=codex` 是现成「伪装成 Codex」开关**；覆盖优先级为：

`客户端原头` → `custom_header` → `request_rewrite.ExtraHeaders`（最后写的赢）

另有 **Codex 渠道类型**（`OutboundTypeCodex`）会写死 `originator: codex_cli_rs` 等，那是 ChatGPT 订阅专用，不是「第三方限制 CLI 的中转」主路径。

发现 4 条：1×bug、1×security、2×maintainability。最值得关注的是 **流式时 Codex header profile 会把 `Accept` 盖成 `application/json`**，以及 **伪装依赖 header 字符串、无协议级校验**。

## 发现清单

| # | 性质 | 严重度 | 置信度 | 标题 | 文件 |
|---|---|---|---|---|---|
| 1 | bug | P1 | high | Codex header profile 强制 Accept: application/json，可破坏流式 | [finding-01.md](finding-01.md) |
| 2 | security | P2 | medium | CLI 伪装依赖可伪造的 User-Agent/Origin，无上游身份证明 | [finding-02.md](finding-02.md) |
| 3 | maintainability | P2 | high | Codex Desktop UA 硬编码版本，上游升级后可能失效 | [finding-03.md](finding-03.md) |
| 4 | maintainability | P2 | medium | 前端 ChannelType 未暴露 Codex 出站类型，与后端枚举不一致 | [finding-04.md](finding-04.md) |

## 按维度分布

| 性质 | P0 | P1 | P2 | 合计 |
|---|---|---|---|---|
| bug | 0 | 1 | 0 | 1 |
| security | 0 | 0 | 1 | 1 |
| performance | 0 | 0 | 0 | 0 |
| maintainability | 0 | 0 | 2 | 2 |
| arch-drift | 0 | 0 | 0 | 0 |
| **合计** | **0** | **1** | **3** | **4** |

## 机制说明（回答用户问题）

### 1. 出站请求会做哪些「特殊处理」

| 层级 | 位置 | 行为 |
|------|------|------|
| 协议适配 | `outbound/*/TransformRequest` | 写协议必需头（`Authorization`、`Anthropic-Version`、`Content-Type`…） |
| 客户端透传 | `copyHeaders` | 复制客户端请求头，去掉 auth / hop-by-hop / 部分代理头 |
| 渠道自定义头 | `channel.CustomHeader` | `Header.Set` 覆盖同名头 |
| 请求重写 | `request_rewrite` | body 兼容策略 + 可选 `header_profile` 注入 ExtraHeaders |
| 参数覆盖 | `param_override` | 仅补客户端未设的 max_tokens / temperature 等 |
| Codex 渠道 | `outbound/codex` | OAuth JSON、`originator=codex_cli_rs`、`chatgpt-account-id`、强制 `store=false` 等 |

### 2. 「只能 Codex CLI 用」的渠道，Claude Code 怎么用

典型配置路径（中转站校验 UA / Origin 类限制）：

1. 渠道类型用 **OpenAI Chat / OpenAI Response / MiMo**（前端 `request_rewrite` 仅支持这三类）
2. 打开 **请求重写** → **Headers 请求重写** 选 **Codex**
3. 必要时再用 **自定义 Header** 补 `originator` 等 profile 未覆盖的字段

注入内容（`internal/transformer/rewrite/config.go`）：

- `User-Agent: Codex Desktop/0.131.0 (Windows 10.0.19045; x86_64) unknown (Codex Desktop; 26.519.21041)`
- `Origin: https://chat.openai.com`
- `Referer: https://chat.openai.com/`
- `Accept: application/json`

若中转只认 **真实 Codex CLI 的 `originator: codex_cli_rs`**，当前 profile **不会**自动加该头——需 `custom_header` 自行配置。

若中转按 **Claude Code 的 UA / anthropic 特征** 拦截：只要最终 `User-Agent` 被 profile / custom_header 覆盖，Claude Code 原 UA 不会原样到达上游（见覆盖顺序）。

### 3. 不在范围内的事

- 无法保证绕过**服务端签名、设备绑定、OAuth 账号绑定**类限制（header 伪装不够）
- **官方 Anthropic / OpenAI 直连**的客户端限制通常不靠本项目改 UA 解决

## 下一步建议

- **P1 本迭代修**：finding-01（流式 Accept 被 Codex profile 覆盖）→ 建议 `cs-issue`
- **P2 有空再看**：finding-02 / 03 / 04 → 文档澄清或小 refactor
- **非缺陷使用说明**：用户场景主要是配置指引；若要把「Claude Code → Codex 限制渠道」写成正式能力，建议补文档 + 可选增加 `originator` 到 header profile

## Checkpoint

- Phase 1 范围：用户描述已足够可执行，**视为已确认**（request 出站特殊处理 + CLI 渠道兼容）
- arch-drift：未请求 / 无 ADR，未纳入
- 本审计**只发现不定修**
