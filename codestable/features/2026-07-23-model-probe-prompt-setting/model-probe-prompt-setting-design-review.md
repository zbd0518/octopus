---
doc_type: feature-design-review
slug: model-probe-prompt-setting
status: passed
review_state: passed
reviewer_id: ae686b5fdb2b26410
review_mode: independent+focused-closure
date: 2026-07-23
blocking: 0
important_open: 0
---

# 模型测活提示词 — Design Review

## 首轮独立审查

- **reviewer**：Task agent `ae686b5fdb2b26410`
- **初判**：`changes-requested`
- **blocking**：0
- **important**：2（I1 B2 未映射；I2 C4↔E1 错位）

代码现状核对：硬编码 `hi`、渠道测活复用 group probe、Key 巡检 `/models`、Setting 扩展模式均与 design 一致；scope 守住 owner 决策。

## Focused closure（主 agent）

本轮修订**不改变**公开契约/范围（仍：全局一项、默认 hi、空回退、不校验回复、不改 Key 巡检），只补强验收映射与 steps 表述，适用 focused closure。

| Finding | 处置 | 证据 |
|---------|------|------|
| I1 B2 未落入 checklist/matrix | **closed** | design 场景表保留 B2；Coverage Matrix 增加「读配置失败仍可测活 → B2」；checklist 新增 **C7 maps_to B2**；S2 exit 写明默认/自定义/空白/**GetString err** |
| I2 C4↔E1 错位 | **closed** | 新增场景 **N3**（2xx+可解析即通过）；C4 改为 **maps_to: N3**；新增 **C8 maps_to: E1** 保留失败路径 |
| N1 S1 测试命令弱 | **closed（表述）** | S1 exit/verify 改为 `DefaultSettings\|GroupProbe` 或 diff_review，并建议专用小单测 |
| N2/N3 UI 形态与入口 | **closed（表述）** | S3 写明运维→维护→重试区 + hint 要点 |
| suggestions | **accepted as guidance** | resolve 小函数、i18n 文案、free-string Validate 对齐 — 实现时遵循，不阻塞 design |

## 修订后覆盖

| Check | maps_to | 状态 |
|-------|---------|------|
| C1 | N1 | ok |
| C2 | N2 | ok |
| C3 | B1 | ok |
| C4 | N3 | ok（不校验回复） |
| C5 | R1 | ok |
| C6 | X1 | ok |
| C7 | B2 | ok（读失败回退） |
| C8 | E1 | ok（失败路径） |

## 总评

| 项 | 结论 |
|----|------|
| **review_state** | **passed** |
| **blocking / important open** | 0 / 0 |
| **spec 覆盖** | 主路径 + B2 + 不校验回复 + Key 巡检边界已可证伪 |
| **下一步** | HumanCheckpoint **ConfirmDesign** — 等 owner 确认后 design → `approved` 再实现 |

## 用户确认时请核对的摘要

1. 全局 `group_probe_prompt`，默认 `hi`，空白/读失败回退 `hi`
2. 仅影响分组/渠道**模型测活**（chat + embedding 共用文案）
3. 不改 Key 巡检；不校验模型回复内容
4. UI 落在运维 → 维护 → 重试/运行时配置区
