---
doc_type: issue-review
issue: 2026-07-24-runtime-memory-spike-3g
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-24
round: 1
lane_a_state: completed
lane_a_ref: a6c135ed3e49bc78c
lane_a_reason: ""
lane_b_state: completed
lane_b_ref: 6da465a5-2ec4-4471-878d-dd624e5cd1e3
lane_b_reason: "ocr review workspace succeeded; earlier ocr scan hung and was stopped"
---

# runtime-memory-spike-3g 代码审查报告

## 1. Scope And Inputs

- Design: runtime-memory-spike-3g-analysis.md（confirmed，方案 A）
- Checklist: none（issue 路径）
- Evidence pack: none
- Gate results: none
- DoD results: none
- Implementation evidence: runtime-memory-spike-3g-fix-note.md
- Diff basis: 工作区 unstaged diff，范围 `internal/transformer/inbound/{openai,anthropic}/*`
- Review mode: initial
- Baseline dirty files: `internal/price/presets.go`（与本 issue 无关，排除）

### Independent Review

- Detection: Task agent 可用；ocr CLI 可用
- 环节 A 独立隔离 Task agent: independent-agent + completed（ref=`a6c135ed3e49bc78c`，verdict=passed）
- 环节 B OCR CLI: completed（`ocr review --audience agent --format json`，session=`6da465a5-2ec4-4471-878d-dd624e5cd1e3`，0 comments；先前 `ocr scan` 挂起已停止）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 各环节结果已逐条本地核验后合并
- Gate effect: none

## 2. Diff Summary

- 新增：none
- 修改：
  - `internal/transformer/inbound/openai/chat.go`
  - `internal/transformer/inbound/openai/response.go`
  - `internal/transformer/inbound/anthropic/messages.go`
  - `internal/transformer/inbound/openai/aggregate_test.go`
  - `internal/transformer/inbound/openai/chat_format_test.go`
  - `internal/transformer/inbound/anthropic/messages_test.go`
- 删除：none
- 未跟踪 / staged：issue 产物目录 `codestable/issues/005-x-2026-07-24-runtime-memory-spike-3g/`
- 风险热点：流式聚合语义 / 内存峰值；无权限/数据迁移

## 3. Adversarial Pass

- 假设的生产 bug：fold 漏合并 tool_calls / reasoning / usage，导致日志或语义缓存不完整
- 主动攻击过的反例：稀疏 choice index、tool call 分片、reasoning 拼接、usage 落最后帧、二次 GetInternalResponse、images/multipart 历史不一致
- 结果：
  - 稀疏 index / 在线 content / 清空状态：测试覆盖
  - tool_calls / reasoning / usage / 二次 GetInternalResponse：补测后通过
  - images/multipart 与 response/anthropic reasoning 完整度：历史债，进 residual-risk

## 4. Findings

### blocking

none

### important

none（审查时的 important「缺 tool/usage/reasoning/二次调用测试」已在同轮补测关闭）

### nit

1. **三处 `foldStreamChunk` / `mergeToolCall` 近似复制** — source: `heterogeneous-agent`  
   - 证据：`chat.go` / `response.go` / `messages.go` 各自一份  
   - 影响：后续字段同步成本高  
   - 建议：另开重构抽共享 helper，不阻塞本修复

### suggestion

1. **为 Anthropic 补对称「在线聚合 + clear」轻量测试** — source: `heterogeneous-agent`  
2. **方案 B 硬上限（maxSSEEventSize / RawRequest）另开 issue** — source: `heterogeneous-agent` + analysis residual

### learning

1. 本问题是请求内无界峰值，不是全局 map 泄漏；RSS 不立刻回落不能单独当 leak 证据。 — source: `heterogeneous-agent`

### praise

1. 主修复对准根因：全仓库去掉 `streamChunks`，在线 fold 只保留最终结果。 — source: `heterogeneous-agent`  
2. OCR review：No comments generated. Looks good to me. — source: `ocr`

### residual-risk

1. **无 pprof 前后对比**：3GB 现场未复测；代码路径支持显著降峰值，证据链未闭环。  
2. **最终响应体仍 O(完整内容)**：长文/多图聚合结果本身仍占内存，这是日志/缓存所需。  
3. **response/anthropic fold 仍不如 chat 完整（images/multipart / GetReasoningContent）**：与旧实现一致，非本次回归。  
4. **工作区噪音**：`internal/price/presets.go` 与本 issue 无关，提交时勿混入。

## 5. Verdict

`status: passed`，`reviewer: subagent+ocr`。

方案 A 实现正确，关键回归测试已补强并通过：

```text
go test ./internal/transformer/inbound/openai/ ./internal/transformer/inbound/anthropic/ ./internal/relay/ -count=1
# ok
```

下一步：owner 确认 fix completion；确认后可提交（勿夹带 `internal/price/presets.go`）。
