---
doc_type: feature-review
feature: 2026-07-23-group-editor-channel-filter
status: passed
reviewer: subagent+ocr
reviewed: 2026-07-23
round: 1
lane_a_state: completed
lane_a_ref: a10ff2f152e7f76a5
lane_a_reason: "independent Task agent review returned verdict=passed; locally verified"
lane_b_state: completed
lane_b_ref: "bbuc3x7k1"
lane_b_reason: "ocr review completed exit=0; scope mixed with unrelated files; feature-relevant findings verified below"
---

# group-editor-channel-filter 代码审查报告

## 1. Scope And Inputs

- Design: none（Quick / ff-note）
- Checklist: none
- Evidence pack: none
- Gate results: none
- DoD results: none
- Implementation evidence: `codestable/issues/007-x-2026-07-23-group-editor-channel-filter/group-editor-channel-filter-ff-note.md`
- Diff basis: 可归因文件见下；忽略 `internal/*`、migrate 038、`.codestable` 基础设施
- Review mode: initial
- Baseline dirty files: `internal/conf/version.go`, `internal/model/stats.go`, `internal/price/presets.go`, migrate 038 等（本轮不审）

### Independent Review

- Detection: Task agent 可用；`ocr` CLI 可用
- 环节 A 独立隔离 Task agent: independent-agent + completed（ref=a10ff2f152e7f76a5）
- 环节 B OCR CLI: completed（ref=bbuc3x7k1；27 files reviewed，scope 混入无关 untracked）
- OCR severity mapping: High→blocking/important, Medium→nit/suggestion, Low→discarded
- Merge policy: 环节 A + OCR 均已本地核验；OCR 误挂到 `.workflow/*` / `migrate/038.go` 的注释已丢弃，仅保留可落到本 feature 文件的项
- Gate effect: `reviewer: subagent+ocr`

## 2. Diff Summary

- 新增：`web/src/components/modules/group/editor-filters.ts`, `editor-filters.test.ts`
- 修改：`Editor.tsx`, `ItemList.tsx`, `web/public/locale/{zh_hans,zh_hant,en}.json`, `web/package.json`
- 删除：none
- 未跟踪：`editor-filters.ts` / `editor-filters.test.ts`
- 风险热点：UI 状态同步（enabled 刷新）、候选/自动添加过滤口径；无后端/协议变更

## 3. Adversarial Pass

- 假设的生产 bug：已选成员 `enabled` 不同步；自动添加仍塞入禁用渠道；分组筛选因 map 未就绪暂时全空
- 主动攻击过的反例：
  - `syncMembersChannelEnabled` 在 `modelChannels=[]` 时 early-return（保留 initial）
  - 候选默认 `showDisabled=false` + `filterModelChannelsForPicker`
  - auto-add 基于已过滤的 `pickerModelChannels`
  - 渠道分组用 `useChannelList` join `group_id`
- 结果：无 blocking；加载竞态与「仅 UX、不改路由」记入 residual-risk

## 4. Findings

### blocking

none

### important

none

### nit

- [ ] REV-001 `web/src/components/modules/group/Editor.tsx:427-430` `filterMatchedForAutoAdd` 在当前接线中偏冗余
  - Evidence: `matchedModelChannels` 已来自 `pickerModelChannels`（已按 showDisabled 过滤），再包一层无行为差
  - Impact: 可读性；无功能错误
  - Expected fix scope: 可选删除冗余层或加注释说明防御意图
  - Source: independent-agent

- [ ] REV-002 `web/src/components/modules/group/Editor.tsx:168-172` 渠道分组下拉未与渠道页排序对齐
  - Evidence: 直接 `channelGroups.map`；渠道页有默认组等排序
  - Impact: 下拉顺序可能不一致；功能正确
  - Source: independent-agent

- [ ] REV-003 `web/src/components/modules/group/Editor.tsx` 过滤结果为空时无 empty copy
  - Impact: 勾选过滤后全空可能被误认为数据丢失
  - Source: independent-agent

- [ ] REV-004 `web/public/locale/en.json` 英文 ICU 文案略生硬
  - Evidence: `disabledMembersCount` 使用 “member channel disabled”
  - Source: independent-agent

- [ ] REV-007 `web/src/components/modules/group/editor-filters.ts:57` 使用 `!= null` 而非 `!== null`
  - Evidence: OCR 提示项目规范倾向 `===`/`!==`；此处类型为 `number | null`，`!== null` 语义足够
  - Impact: 风格一致性；当前无类型强制 bug
  - Source: ocr (medium → nit)

- [ ] REV-008 `web/src/components/modules/group/editor-filters.ts:75-76` `countDisabledMembers` 可用 `filter().length`
  - Evidence: OCR style；`enabled === false` 显式写法更贴合「仅 false 算禁用」
  - Impact: 可读性偏好；`!enabled` 会把 `undefined` 也算进去，现写法更稳
  - Source: ocr (low → discard-ish / 保留为 nit 说明为何不改)

### suggestion

- [ ] REV-005 候选折叠头 `enabled` 取 first model 行（`Editor.tsx` 聚合 channels 时）
  - 后端 `LLMList` 同行 `Enabled: ch.Enabled`，生产一致；若未来模型级 enabled 需改聚合策略

- [ ] REV-006 单测可补「`groupIdByChannelId` 缺 key 时分组筛选剔除」

### learning

- 纯函数下沉 `editor-filters.ts` + `test:unit` 注册，与 group 模块既有 capabilities / grouped-route-view 模式一致
- 已选成员不剔除、仅 sync `enabled`/`channel_name` 准确落了「历史保留 + 标识」
- 自动添加与 picker 共用 `pickerModelChannels`，避免「列表看不见却被自动加进来」

### praise

- 目标 1–4（默认隐藏 / 可选显示+徽章 / 已选徽章+汇总 / 不自动移除 / 分组过滤 / 自动添加口径一致）闭环清晰
- `ItemList` 徽章复用：Card / 列表预览共用 `MemberList`，禁用态从仅 grayscale 变为可文案识别
- i18n 三语同步；`package.json` 已挂测试

### residual-risk

- 仅编辑器 UX：不改变保存 payload 剔除策略，禁用成员仍可提交（ff-note 有意范围）
- 本 PR 不保证运行时「禁用渠道必不参与 relay」
- `channelList` 加载慢于 `channelGroups` 时，已选分组筛选可能短暂空列表
- 孤儿成员（渠道已从 model/channel 消失）不同步 `enabled`（sync skip missing），与 Card `?? true` 默认可能略不一致——预存边界

## 5. Verdict

- status: **passed**
- reviewer: **subagent+ocr**
- 验证：`editor-filters.test.ts` 6/6 pass；`pnpm test:i18n` pass；本轮 TypeScript 改动文件无新增错误；OCR 无 High/已核实 blocking
- 下一步：Quick feature 可收尾提交（由 owner 决定是否 commit）；nit 不阻塞
