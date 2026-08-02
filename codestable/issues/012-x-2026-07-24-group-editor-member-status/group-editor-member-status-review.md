---
doc_type: feature-review
feature: 2026-07-24-group-editor-member-status
status: passed
reviewer: subagent
reviewed: 2026-07-24
round: 1
lane_a_state: completed
lane_a_ref: "a53ec65dad63856bc"
lane_a_reason: ""
lane_b_state: unavailable
lane_b_ref: ""
lane_b_reason: "ocr CLI present but ocr llm test timed out (context deadline exceeded)"
---

# group-editor-member-status 代码审查报告

## 1. Scope And Inputs

- Design: none（Quick / ff-note）
- Checklist: none
- Evidence pack: none
- Gate results: none
- DoD results: none
- Implementation evidence: `group-editor-member-status-ff-note.md` + brainstorm + review-fix 后的 diff
- Diff basis: 工作区 unstaged + untracked（本 feature 文件）
- Review mode: full-rereview after independent findings + review-fix
- Baseline dirty files: `.codestable/feedback/2026-07-24-cr-lane-a-delay/`（无关 baseline）

### Independent Review

- Detection: independent Task agent 可用；ocr CLI 未确认可用（llm test timeout）
- 环节 A 独立隔离 Task agent: independent-agent + completed（ref `a53ec65dad63856bc`）
- 环节 B OCR CLI: unavailable
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 环节 A findings 已本地核验；review-fix 后主 agent 复核关闭项
- Gate effect: none（lane A completed；OCR 不可用 → reviewer=subagent 可放行）

## 2. Diff Summary

- 新增：`editor-member-status.ts` / `editor-member-status.test.ts` / feature codestable 产物
- 修改：`Editor.tsx` / `ItemList.tsx` / `editor-filters.ts`(+test) / locale 三语 / `package.json`
- 删除：none
- 风险热点：UI 静态状态；无后端契约

## 3. Adversarial Pass

- 假设的生产 bug：编辑会话中 metrics 刷新导致左红右静
- 主动攻击：design 左右同口径 / channelList 未就绪假阴性 / 阈值边界 / 测活语义污染
- 结果：原 important #1/#2 已在 review-fix 关闭；残留为静态≠运行时（设计内）

## 4. Findings

### blocking

none

### important

- [x] REV-001 `editor-filters.ts` / `Editor.tsx` 右侧 `upstream_metrics` 不随 modelChannels 同步
  - Evidence: 独立 agent；`syncMembersChannelEnabled` 原只刷 enabled/name
  - Impact: 左右健康风险与汇总漂移
  - Status: **fixed** — `syncMembersChannelEnabled` 按 channel_id+name re-join metrics/price/balance；单测覆盖

- [x] REV-002 `Editor.tsx` channelList 未就绪时无 Key 假阴性
  - Evidence: 默认 `[]` + 空 map 无条目不当无 Key
  - Impact: 加载完成前漏标硬风险
  - Status: **fixed** — 仅 `isFetched` 后暴露 map；未就绪不展示 noKey 徽章/汇总

### nit

- [x] REV-003 `ItemList.tsx` noEnabledKey 内联判定
  - Status: **fixed** — 复用 `channelHasNoEnabledKey`

- [x] REV-004 阈值边界单测缺 NaN/Infinity
  - Status: **fixed** — 已补断言

- [ ] REV-005 `countDisabledMembers` 仍为旧路径
  - Evidence: 仍被 `editor-filters` 导出与测试使用；Editor 已改用 `countMemberRisks`
  - Impact: 低；双口径维护噪音
  - Expected fix scope: 可选删除或委托；**本轮延后**（不阻塞）

### suggestion

- [ ] REV-006 健康阈值与 StatusBars 双份实现 — 后续可抽共享 helper
- [ ] REV-007 汇总按成员行计数 — 产品可接受，QA 说清即可

### learning

- map 无条目 ≠ 无 Key 是正确防误报
- 静态风险与测活 `availability` 用 `showStaticRisks` 隔离方向正确

### praise

- 纯函数 + unit 接入干净；文案避开测活用语；范围克制

## 5. Test And QA Focus

- QA：左/右禁用、无 Key、成功率阈值矩阵；叠加三类汇总；不拦截添加/保存；Card 预览无静态徽章
- 验证命令：`editor-member-status` + `editor-filters` unit 12 pass；`test:i18n` pass
- 不能靠 review 完全确认：真实 channel list 权限下 keys mask 形态、长会话 refetch 频率

## 6. Residual Risk

- 静态风险 ≠ 运行时测活；无 metrics / 未加载 keys 时安静（设计明确不做拦截）
- OCR 未跑：行级扫描缺口，已用独立 agent + 本地核验覆盖

## 7. Verdict

- status: **passed**
- reviewer: **subagent**
- next: Quick 闭环可收尾；可询问是否 scoped-commit
