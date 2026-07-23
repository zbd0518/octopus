---
doc_type: feature-review
slug: model-probe-prompt-setting
status: passed
reviewer: subagent
lane_a: completed
lane_a_ref: a18bf1a04dc27bec6
lane_b: unavailable
entry_source: feat
date: 2026-07-23
blocking: 0
important: 0
---

# 模型测活提示词 — 代码审查

## 范围

- Standard feature：`model-probe-prompt-setting`
- Spec：approved design + checklist S1–S4 / C1–C8
- Diff：Setting key、`resolveGroupProbePrompt`、Retry UI、i18n

## 环节

| 环节 | 状态 |
|------|------|
| A 独立 Task agent | completed（`a18bf1a04dc27bec6`）→ **passed** |
| B OCR | unavailable（不阻塞） |

## Findings

### blocking / important

无

### nit（不阻塞）

1. helper 测试全局 setting 缓存隔离可再加 `t.Cleanup` / seed
2. UI 存原文、后端 trim，展示与注入可能略不一致
3. 自由字符串无长度上限（与现网 free-string Setting 一致）

### praise

- 消费点单点正确；回退路径完整；Key 巡检隔离；成功判定未引入 content match；前后端 key 一致

## 验证

```text
go test ./internal/model -run 'DefaultSettings|GroupProbe' → ok
go test ./internal/helper → ok
pnpm --dir web test:i18n → passed
```

## 总评

| 项 | 结论 |
|----|------|
| **verdict** | **passed** |
| **下游** | accept-inline |
