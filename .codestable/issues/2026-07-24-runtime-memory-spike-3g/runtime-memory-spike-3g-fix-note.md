---
doc_type: issue-fix-note
issue: 2026-07-24-runtime-memory-spike-3g
status: confirmed
related:
  - runtime-memory-spike-3g-report.md
  - runtime-memory-spike-3g-analysis.md
chosen_plan: A
tags: [memory, stream, transformer, inbound]
---

# 运行时内存冲高至约 3GB Fix Note

## 1. 根因（复述）

流式 inbound 把每个 SSE chunk 的完整 `*InternalLLMResponse` 都 `append` 到 `streamChunks`，请求结束才聚合清空。长流 / 大图 / 多并发时，请求内堆峰值可冲到 GB 级。

## 2. 改动

按已批准方案 **A：流式增量聚合**。

| 文件 | 改动 |
|---|---|
| `internal/transformer/inbound/openai/chat.go` | `streamChunks` → `streamResponse` + `streamChoices`；`TransformStream` 调 `foldStreamChunk` 在线聚合；`GetInternalResponse` 返回已聚合结果并清空状态 |
| `internal/transformer/inbound/openai/response.go` | 同上 |
| `internal/transformer/inbound/anthropic/messages.go` | 同上 |
| `internal/transformer/inbound/openai/aggregate_test.go` | 改为经 `TransformStream` 注入；新增 100 帧在线聚合测试 |
| `internal/transformer/inbound/openai/chat_format_test.go` | 断言改为检查输入 chunk 未被 mutate（不再读 `streamChunks`） |
| `internal/transformer/inbound/anthropic/messages_test.go` | 稀疏 index 聚合测试改为经 `TransformStream` 注入 |

语义：与旧版「缓存全部 chunks 再一次性聚合」一致，但内存只保留最终 content / reasoning / tool_calls / usage 等结果，不再按帧数线性放大对象图。

未在本 issue 内改（分析中的次要护栏，记为顺手发现）：

> 顺手发现：`internal/relay/type.go:25` 默认 `maxSSEEventSize=32MB` 仍偏大；`RawRequest` 整 body 拷贝、stream session / 语义缓存仍会贡献峰值与基线。不在方案 A 主范围，可另开 issue。

## 3. 验证

```text
go test ./internal/transformer/inbound/openai/ ./internal/transformer/inbound/anthropic/ ./internal/relay/ -count=1
# ok
```

补充测试覆盖：稀疏 index、100 帧 content 拼接、tool_calls 分片合并、reasoning 拼接、usage/finish_reason、二次 GetInternalResponse 清空、ResponseInbound clear。

本地未对用户 3GB 现场做 pprof 复测；建议修后在「约 4 并发流式」下对比 RSS / heap top。

Code review：`runtime-memory-spike-3g-review.md` → `passed`（`reviewer: subagent+ocr`）。

## 4. 遗留风险

- 三处 inbound 的 `foldStreamChunk` 逻辑与旧循环对齐，但 OpenAI chat 对 multipart / images 的合并比 response/anthropic 更完整；若某协议后续新增 delta 字段，需同步改 fold。
- 请求结束后 Go RSS 可能仍不立刻回落（runtime 保留堆），需用 pprof inuse 与「是否持续爬升」区分。
- 方案 B 硬上限未做；极端单帧大图仍可能瞬时很高。
