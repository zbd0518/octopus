---
doc_type: issue-review
issue: 2026-07-24-openai-chat-metadata-400
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-24
round: 1
lane_a_state: completed
lane_a_ref: "a3e8cfb6b3415d714"
lane_a_reason: ""
lane_b_state: completed
lane_b_ref: ""
lane_b_reason: "ocr review returned 0 comments"
---

# openai-chat-metadata-400 代码审查报告

## 1. Scope And Inputs

- Design: none（fast-track issue，无 design）
- Checklist: none
- Evidence pack: none
- Gate results: none
- DoD results: none
- Implementation evidence:
  - `.codestable/issues/2026-07-24-openai-chat-metadata-400/openai-chat-metadata-400-report.md`
  - `.codestable/issues/2026-07-24-openai-chat-metadata-400/approval-report.md#issue-fast-path`（approved）
  - `.codestable/issues/2026-07-24-openai-chat-metadata-400/openai-chat-metadata-400-fix-note.md`
  - 验证：`go test ./internal/transformer/outbound/openai/ -count=1` 通过
- Diff basis: 仅本 issue 归因文件
  - `internal/transformer/outbound/openai/chat.go`（`SanitizeRequestForOpenAICompat` 末尾 `request.Metadata = nil` + 注释）
  - `internal/transformer/outbound/openai/chat_test.go`（新增 `TestChatOutboundTransformRequest_StripsMetadata`）
- Review mode: initial
- Baseline dirty files（非本 issue）:
  - `internal/op/channel.go`
  - `internal/op/channel_group_test.go`
  - `internal/price/presets.go`
  - `internal/server/handlers/channel.go`
  - `internal/task/channel_expire.go`
  - `.codestable/features/2026-07-24-channel-delete-purge-group-items/`

### Independent Review

- Detection: independent Task agent 可用；OCR CLI 可用
- 环节 A 独立隔离 Task agent: independent-agent + completed（agent `a3e8cfb6b3415d714`）
- 环节 B OCR CLI: completed（0 comments）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 各环节结果已逐条本地核验后合并
- Gate effect: none

## 2. Diff Summary

- 新增：none（仅改现有文件）
- 修改：
  - `internal/transformer/outbound/openai/chat.go`
  - `internal/transformer/outbound/openai/chat_test.go`
- 删除：none
- 未跟踪 / staged：issue 产物目录 `.codestable/issues/2026-07-24-openai-chat-metadata-400/`
- 风险热点：none（定点兼容性修复，与 Responses 既有行为对齐）

## 3. Adversarial Pass

- 假设的生产 bug：出站仍把 Claude Code 注入的 `metadata.user_id` 发给兼容 Chat 上游，继续 400
- 主动攻击过的反例：
  - design 一致性：与 `response.go:39` 对照，Chat 路径现已同样剥离
  - 原始请求污染：`TransformRequest` 先 `CloneRequestForOpenAICompat` 再 sanitize；`Metadata` 是 map 字段值赋值 nil，不共享 map 写回
  - 测试假阳性：测试同时做字符串检查 + unmarshal map key 检查 + 原始 request.Metadata 保留断言
  - 既有浅拷贝：`CloneRequestForOpenAICompat` 对 Message 内指针字段浅拷贝（pre-existing，非本 diff 引入）
- 结果：
  - 本次 fix 本身无 blocking
  - 既有浅拷贝问题记 residual-risk / 顺手发现
  - stream 覆盖与正向断言记 important（可选加强，不阻塞本 issue）

## 4. Findings

### blocking

none

### important

- [ ] REV-001 `internal/transformer/outbound/openai/chat_test.go:14-57` 新增测试未覆盖 `Stream=true` 路径
  - Evidence: independent-agent；`TransformRequest` stream 分支会设置 `StreamOptions`，sanitize 路径相同但缺回归保护
  - Impact: 未来若 stream 分支误重新注入 metadata，测试不捕获
  - Expected fix scope: 可选追加 stream 子测试；不阻塞本次 metadata 400 修复

- [ ] REV-002 `internal/transformer/outbound/openai/chat_test.go:47-56` 缺少 body 正向字段断言
  - Evidence: independent-agent；Responses 侧 `StripsMetadata` 会检查 `payload["model"]`，Chat 测试只断言 metadata 不存在
  - Impact: 若 sanitize 异常清空 payload，测试仍可能通过
  - Expected fix scope: 可选增加 `model` / `messages` 正向断言

### nit

- [ ] REV-003 `chat_test.go:43-45` `strings.Contains(..., '"metadata"')` 有假阳性风险；可靠断言已在后续 map key check
- [ ] REV-004 `chat_test.go:57-58` 新测试与下一函数间缺空行（风格）

### suggestion

- [ ] REV-005 可将 `request.Metadata = nil` 提到 `ChatOutbound.TransformRequest` 中 sanitize 之后、Marshal 之前，与 `response.go` 的 caller 层剥离模式更一致（当前实现功能正确，属结构偏好）

### learning

- Mimo Chat 通过调用 `SanitizeRequestForOpenAICompat` 自动获得本次剥离，复用传播良好
- Responses 路径早已剥离 metadata；本次是补齐 Chat 路径对称行为

### praise

- 注释准确描述 Claude Code `metadata.user_id` → 400 根因链，并显式对齐 Responses 行为
- 测试命名与 Responses 侧 `TestResponseOutbound_TransformRequest_StripsMetadata` 一致
- 测试数据复用相同 `user_id` JSON 形态，覆盖真实 Claude Code 场景
- 原始 request metadata 保留断言比 Responses 测试更完整

## 5. Test And QA Focus

- QA 必须重点复核：
  1. 真实 Claude Code → `default-Chat` 渠道，确认不再出现 `400 Argument not supported: metadata`
  2. 可选：`Stream=true` Chat Completions 同样无 metadata 400
- Evidence pack residual risks / gate warnings：none
- 建议新增或加强的测试：
  1. stream 版 `StripsMetadata`
  2. 正向断言 `model` / `messages`
  3. （另开 issue）非 DeepSeek 路径下 clone 不污染原始 Message help 字段
- 不能靠 review 完全确认的点：
  1. VolcEngine / Cloudflare / Codex 等其它 Chat 出站是否都走 `SanitizeRequestForOpenAICompat`
  2. 是否还有非 Anthropic 入站也会设置 `Metadata`

## 6. Residual Risk

- **既有浅拷贝**（非本 diff 引入）：`CloneRequestForOpenAICompat` 只拷贝 Messages slice，Message 内 `*string` 等指针字段共享；`sanitizeMessageForOpenAICompat` 在非 DeepSeek 路径调用 `ClearHelpFields()` 可能回写原始 request 的 reasoning help 字段。与本次 metadata 修复无关，但依赖 clone 隔离假设的后续逻辑需另开 issue。
- 上游超时 / 502（截图中的 subgrok）不在本 issue 范围。
- baseline dirty 文件（channel delete / price presets 等）未纳入本轮审查。

## 7. Verdict

- Status: **passed**
- Next: 回到 `cs-issue` fix 收尾 → 请求 owner 确认 fix completion → 按需提交本 issue 归因改动

## 8. Focused Closure（无则写 none）

none

## 本地核验摘要

| 独立审查 finding | 本地结论 |
|---|---|
| Metadata 剥离位置正确 | 确认：`chat.go:19` clone → `chat.go:24` sanitize → `chat.go:126` Metadata=nil |
| 原始 Metadata 未污染 | 确认：map 字段在 clone struct 上置 nil；测试断言原始 request.Metadata 保留 |
| 与 Responses 对齐 | 确认：`response.go:39` 已有同等剥离 |
| 浅拷贝 Message 指针 | 确认为**既有缺陷**，非本 diff 引入；不升为本次 blocking |
| OCR | 0 comments，Looks good |
| 单测 | `go test ./internal/transformer/outbound/openai/ -count=1` 通过 |