---
doc_type: issue-review
issue: 2026-07-24-group-editor-no-key-blocks-site-channel
status: passed
reviewer: subagent
reviewed: 2026-07-24
round: 1
lane_a_state: completed
lane_a_ref: "a665212804bae1ea0"
lane_a_reason: ""
lane_b_state: unavailable
lane_b_ref: ""
lane_b_reason: "ocr optional lane skipped / not confirmed usable"
---

# group-editor-no-key-blocks-site-channel 代码审查报告

## 1. Scope And Inputs

- Report: `group-editor-no-key-blocks-site-channel-report.md`（confirmed, fast-track）
- Fix-note: `group-editor-no-key-blocks-site-channel-fix-note.md`
- Approval: `approval-report.md#issue-fast-path` Approved（方案 A）
- Diff: `Editor.tsx` + `ItemList.tsx`（hardRisk 收窄）
- Review mode: initial
- Baseline dirty: 无关 feedback / price 构建产物不在本轮归因

### Independent Review

- Detection: independent Task agent 可用；OCR 不可用
- 环节 A: independent-agent + completed（ref `a665212804bae1ea0`）
- 环节 B: unavailable
- Merge policy: 环节 A findings 已本地核验合并
- Gate effect: none（reviewer=subagent 可放行）

## 2. Diff Summary

- 修改：`Editor.tsx`（`hardRisk = channelDisabled` only）
- 修改：`ItemList.tsx`（`hasHardRisk = isDisabled` only）
- 风险热点：UI 灰化条件；无后端

## 3. Adversarial Pass

- 假设：无 Key 仍被当成不可选
- 攻击：硬/软风险是否仍共用视觉通道；禁用是否仍灰化；点击是否仍可用
- 结果：方案 A 四条均满足；无 blocking 升级

## 4. Findings

### blocking

none

### important

- [ ] REV-001 缺 hard/soft 分离的自动化契约测试
  - Evidence: 单测只覆盖 `editor-member-status` 聚合，不测 className 灰化条件
  - Impact: 可能再把 no-key 并回 hardRisk 而单测仍绿
  - Status: **延后**（owner 可接受；靠手工矩阵；不阻塞合入）

### nit

- [ ] REV-002 无 Key 与禁用徽章同为 amber，软/硬视觉权重仍接近
- [ ] REV-003 `hardRisk` 命名偏泛（可后续拆命名）

### suggestion

- QA 手工矩阵见独立 reviewer 报告（空 Key 可选不灰 / 禁用仍灰 / 汇总 / 健康徽章）

### learning

- 多信号勿共用单一 hardRisk 视觉通道

### praise

- 精准对齐方案 A；副作用面干净；注释点明意图

## 5. Test And QA Focus

- 必测：空 Key 投影渠道不灰可点选 + 徽章；禁用仍灰；禁用+无 Key 叠加；健康风险不灰
- 自动化：unit/i18n 通过，但不替代本 issue 手工验收

## 6. Residual Risk

- 投影 keys 仍可能为空；relay 仍需非空 Key（方案 A 明确接受）
- 缺 UI 契约测试（REV-001 延后）

## 7. Verdict

- status: **passed**
- reviewer: **subagent**
- next: ConfirmFixCompletion → 用户确认效果后可 commit / 再编译
