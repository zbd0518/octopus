---
doc_type: issue-analysis
issue: 2026-07-24-runtime-memory-spike-3g
status: confirmed
root_cause_type: missing-guard
related: [runtime-memory-spike-3g-report.md]
tags: [memory, stream, transformer, relay, concurrency]
---

# 运行时内存冲高至约 3GB 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `internal/transformer/inbound/openai/chat.go:12,43` | `ChatInbound.streamChunks` 每个 SSE chunk 原样 `append` 整颗 `*InternalLLMResponse`，仅在 `GetInternalResponse` 聚合后清空 |
| `internal/transformer/inbound/openai/response.go:49,93` | `ResponseInbound` 同样无上限累积 stream chunks |
| `internal/transformer/inbound/anthropic/messages.go:35,491` | Anthropic inbound 同样无上限累积 stream chunks |
| `internal/relay/relay.go:493-494,557,1198-1222` | 请求结束才 `collectResponse()` → `GetInternalResponse()`，流式过程中 chunks 全程驻留 |
| `internal/relay/type.go:21-25` | `maxSSEEventSize` 默认 **32MB**，单帧（尤其 base64 图像）可非常大 |
| `internal/relay/request_session.go:17` | `RawRequest` 整份请求体再拷一份（上限默认 64MB，`request_body.go:14-18`） |
| `internal/relay/stream_session.go:66-73,285-348,408-432` | 流会话 replay 缓冲默认单会话 **4MB** / 4096 events；done 后最多保留 2 分钟（issue #46 已缓解，但活跃会话仍占峰） |
| `internal/utils/semantic_cache/cache.go:28-72,159-196` | 语义缓存默认最多 1000 条完整 `ResponseJSON`（稳态放大器，不是 4 请求瞬时 3GB 的主因） |
| `internal/client/http.go:30-38,219-238` | 历史 issue #124：每请求新建 Transport/连接池；当前已按 proxy 缓存并设连接池上限 |

历史相关修复（说明仓库已多次打过内存相关补丁，但**流式 chunk 全量缓存**这条仍在）：

- issue #46：流会话 done 后大缓冲驻留过久、多处 map 无界增长 → 已加短保留与 purge
- issue #124：HTTP client 不复用导致内存/连接累积 → 已缓存
- relay log 队列 drop policy、analytics 大字段 SELECT 等 OOM 防护 → 已有

## 2. 失败路径还原

**正常路径（期望）**：

1. 启动后空闲约 40~80MB  
2. 少量并发聊天/转发请求  
3. 流式转发时只短暂持有当前帧 + 必要聚合结果  
4. 请求结束后内存回到百 MB 级稳态  

**失败路径（代码实际）**：

1. 请求进入 `parseRequest`：`readLimitedRequestBody` 读整 body → `inAdapter.TransformRequest` → `populateRelayRequestSessionFields` **再拷一份** `RawRequest`（`request_session.go:17`）  
2. 流式上游响应：`handleStreamResponse` 按 SSE 读帧（`relay.go:828+`），单帧上限默认 **32MB**（`type.go:25`）  
3. 每帧：`outAdapter.TransformStream` → `inAdapter.TransformStream`  
4. **Inbound 把每个 `*InternalLLMResponse` 指针都 append 到 `streamChunks`**（`chat.go:43` 等），**全程不释放**  
5. 同时：写出 SSE 给客户端；若开启 conversation stream session，再 `AddPayload` 复制进 replay 缓冲（默认再最多 4MB/会话）  
6. 仅在 attempt 结束 `collectResponse` → `GetInternalResponse` 时才聚合并 `streamChunks = nil`  
7. 4 路并发长流 / 含大图或长 reasoning 时：  
   - 每路持有「全部历史 chunk 对象图」  
   - 再加 RawRequest、SSE 读缓冲、写出缓冲、可选 stream session  
   - 瞬时堆可冲到 GB 级  
8. 结束后即使 `streamChunks` 清空，Go 堆/OS 常不立刻把 RSS 还回，表现为「跑一阵后内存很高」

**分叉点**：

- `internal/transformer/inbound/openai/chat.go:43`（及 response/anthropic 对等处）— **为日志/统计做全量 chunk 留存，却无大小上限、无增量聚合**  
- 与「只保留聚合结果 + 有界缓冲」的期望路径在此分叉  

## 3. 根因

**根因类型**：`missing-guard`（缺少有界缓存/增量聚合防护），叠加大载荷协议假设

**根因描述**：

流式路径为了在结束后构造完整 `InternalLLMResponse`（日志、usage、语义缓存），在 inbound adapter 里**按帧完整保存所有 stream chunk 对象**，直到请求结束才聚合清空。这不是「泄漏到全局 map 永不删」的经典 leak，而是：

1. **请求内无界放大**：chunk 数量 × 每帧对象开销（甚至 base64 大图）线性涨；  
2. **并发叠加**：约 4 个并发流式请求即可把峰值推到 GB 级；  
3. **单帧上限过高**：默认 32MB SSE 事件 + 默认 64MB 请求体拷贝进一步放大；  
4. **观测混淆**：RSS 回落滞后，容易被当成「泄漏到 3GB」。

**是否有多个根因**：是。

| 主次 | 项 | 说明 |
|---|---|---|
| **主** | inbound `streamChunks` 全量缓存 | 流式热路径上最大、最直接的无界堆占用 |
| 次 | `maxSSEEventSize=32MB` + 大图/多模态 | 单帧即可数十 MB，再被 chunk 列表与写出路径复制 |
| 次 | `RawRequest` 整 body 拷贝 | 每请求额外一份，默认上限 64MB |
| 次 | stream session replay 缓冲 | 默认 4MB/会话；已有 trim 与 done 2min 清理，贡献峰值但通常单独到不了 3GB |
| 稳态放大器 | 语义缓存最多 1000 条完整响应 | 长时间运行抬高基线，不是「4 请求瞬时」主因 |
| 已修/低嫌疑 | HTTP client 每请求 Transport（#124）、stream session 30min 驻留（#46） | 代码已缓解 |

**置信度说明**：未在本机对你的 3GB 现场做 pprof 取证，故以代码路径与历史 issue 证据为主。修复前应用 `pprof heap` 在复现窗口验证 `streamChunks` / `InternalLLMResponse` / `[]byte` 占比。

## 4. 影响面

- **影响范围**：所有走 transformer inbound 的**流式**聊天/补全（OpenAI chat、OpenAI responses、Anthropic）；非流式只持一份完整响应，峰值通常小得多  
- **潜在受害模块**：relay 主路径、语义缓存入库（依赖聚合后的完整响应）、带 `X-Conversation-ID` 的 stream session 重连、图像/多模态模型  
- **数据完整性风险**：无（内存问题，不直接损坏业务数据）  
- **严重程度复核**：**维持 P1** — 本地已可到约 3GB，有 OOM/卡顿风险；若生产并发更高应视为 P0 风险  

## 5. 修复方案

### 方案 A：流式增量聚合（推荐）

- **做什么**：  
  - 改 `ChatInbound` / `ResponseInbound` / `MessagesInbound`：`TransformStream` 时**在线合并** content/reasoning/tool_calls/usage，**不再** `append` 全量 `streamChunks`（或仅保留有界「最近 N 帧」供调试）  
  - `GetInternalResponse` 直接返回已聚合结果  
  - 补充单测：稀疏 choice index、tool call 分片、reasoning-only 后再可见内容、usage 落在最后帧等现有 `aggregate_test` 场景  
- **优点**：从根上消除「chunk 数 × 对象图」放大；请求结束后内存曲线更接近期望；与现有「聚合后清空」语义兼容  
- **缺点 / 风险**：三处 inbound 聚合逻辑要仔细对齐；图像多 part、tool call 合并边界易回归  
- **影响面**：  
  - `internal/transformer/inbound/openai/chat.go`  
  - `internal/transformer/inbound/openai/response.go`  
  - `internal/transformer/inbound/anthropic/messages.go`  
  - 相关 aggregate 测试  

### 方案 B：有界 streamChunks + 硬上限（止血）

- **做什么**：  
  - 给 `streamChunks` 加 max count / max bytes；超限后丢弃旧帧或只保留聚合态  
  - 同步下调默认 `maxSSEEventSize`（例如 4~8MB）与评估 `RawRequest` 是否必须全量拷贝  
  - stream session：确认默认 replay 是否可关，或进一步降低 `stream_session_max_bytes_mb`  
- **优点**：改动面相对小、能快速限制峰值  
- **缺点 / 风险**：超限后日志/语义缓存可能不完整；不解决「每帧全对象」的放大系数  
- **影响面**：inbound 三处 + `relay/type.go` 常量/配置 + 可能 setting 文档  

### 方案 C：先取证再定点改（诊断优先）

- **做什么**：  
  - 加/启用 `net/http/pprof`（若尚未暴露）  
  - 复现 4 并发流式时抓 `heap`/`allocs`，确认 top 是否为 `streamChunks` / `[]byte` / SSE  
  - 根据 profile 再选 A 或 B，并记录「请求结束后 RSS 是否回落」  
- **优点**：证据最硬，避免误修  
- **缺点 / 风险**：不直接降低内存；本地需可稳定复现  
- **影响面**：诊断配置与复现脚本；代码改动可延后  

### 推荐方案

**推荐方案 A**，理由：

1. 直接对准主分叉点（全量 `streamChunks`）  
2. 改动集中在 transformer inbound，不改变对外 API 协议  
3. 方案 B 可作 A 的配套护栏（尤其 `maxSSEEventSize`）  

建议落地顺序：**A 为主 + B 中的硬上限作防御**；若你更希望先确认现场，可先做 **C 一轮 pprof**，再按同一方案修。

## 备注：建议验证命令（修复前后）

```text
# 运行中进程
curl -s http://127.0.0.1:<pprof-port>/debug/pprof/heap > heap.pb
go tool pprof -top heap.pb

# 观察 RSS 是否在请求结束后回落（Windows）
Get-Process <name> | Select-Object WorkingSet64
```
