---
doc_type: feature-acceptance
slug: model-probe-prompt-setting
status: passed
mode: accept-inline
date: 2026-07-23
execution_lane: standard
---

# 模型测活提示词 — 验收（Inline）

## 范围核对

对照 approved design：全局 `group_probe_prompt`、默认/回退 `hi`、仅模型测活、不校验回复、不改 Key 巡检 — 实现与 review 均对齐。

## Inline Verification Matrix

| 场景 | 期望 | 证据 | 结果 |
|------|------|------|------|
| N1 默认 hi | 请求文本 hi | DefaultSettings + resolve/单测 | pass |
| N2 自定义 | 注入自定义文案 | `TestBuildGroupProbeRequest_UsesCustomPrompt` | pass |
| B1 空白 | 回退 hi | resolve blank 单测 | pass |
| B2 读失败 | 回退 hi 不崩 | Del key + resolve 单测 | pass |
| N3 不校验回复 | Passed 不看 content | sendGroupProbeRequest 语义 + review | pass |
| E1 非 2xx | 仍失败 | 既有路径未改 + C8 | pass |
| R1 Key 巡检 | 仍 /models | channel_probe/key_health 无提示词耦合 | pass |
| X1 不做项 | 无渠道覆盖/期望回复 | grep/UI 仅全局一项 | pass |

## DoD

- [x] Setting key 默认 hi，前后端 key 一致
- [x] 模型测活读配置；空/失败回退 hi
- [x] 设置页可配且文案说明范围
- [x] helper/model 单测 + i18n 通过
- [x] code review passed（0 blocking / 0 important）

## 命令证据

```text
go test ./internal/model/ -count=1 -run 'DefaultSettings|GroupProbe' → ok
go test ./internal/helper/ -count=1 → ok
pnpm --dir web test:i18n → passed
```

## 残留风险（可接受）

- 工作区可能混有无关 site-filters / price 改动：合入时请按 feature 拆分提交
- UI 不 trim 展示、后端 trim 消费：体验 nit，不挡验收

## 结论

**acceptance: passed** — 可标 feature 完成。Owner 若需浏览器手测：运维→维护→重试区改提示词后跑一次分组/渠道模型测活即可。
