---
kind: issue
title: "分支 fix/site-model-case-pk-conflict 相对 master 的功能差异与合并评估"
type: explore
status: closed
created: 2026-08-02
related_issue: "001-x-2026-07-18-site-model-case-pk-conflict"
---

# 分支功能差异 Explore

> **读者：** 要合并 `fix/site-model-case-pk-conflict` 到 master 前，想知道分支带来了什么、哪些值得合入的人。

---

## 要弄清什么、怎样算够

- 支持的决策：合并该分支回 master 前，判断 34 项修改哪些必要（✅24）、哪些锦上添花（⚠️8）、哪些可放弃（❌4）
- 本轮边界：只评估功能差异与合并价值；不含合并冲突的具体解决步骤
- 停止条件：分支合并完成后此探索失效，可关闭归档

## 一句话：触发怎样变成结果

该分支（53 个提交、166 文件变更）围绕「站点模型名大小写冲突修复」展开，同时携带号池生命周期、中继推流优化、统计修复、分组编辑器增强等 11 个主题的改动；合并前需逐项评估必要性与上游覆盖情况。

## 先读哪里

1. `branch-feature-diff-vs-master.md` — 完整差异清单（11 个主题 + 迁移 041-049 总览 + 34 项必要性评估）。读完能带走「该合入什么、可放弃什么」的结论。

## 已经确认的主叙述

- **站点模型**（分支命名由来）✅ 全部必要：`model_name_key` MD5 指纹唯一索引解决 MySQL collation 大小写冲突（001-x issue 的历史修复）
- **号池** ⚠️ 部分：扩展字段/OAuth 去重/清空修复必要，生命周期调度字段锦上添花
- **中继/推理流** ✅ 大部分：长思考 immediate 推流、AttemptClientClosed 区分、冷却感知测活必要；2 项 Responses/metadata 兼容已被 upstream 修复（合并时自然丢弃）
- **统计** ✅ 全部必要：直方图列名修复（migration 043）+ FK 1452 死循环修复，均为生产级 bug
- **构建/版本** ✅ 必要：版本注入 + 价格预设刷新
- 迁移 041-045 为分支独有；046-049 来自 upstream 重排（合并后自动保持）

有具体变化时：合并前无需改动本文档；合并后此 Explore 结论过期。

## 曾排除的理解 / 仍未知

- 合并冲突的具体解决方式未评估（超出本轮边界）
- 046-049 迁移的 SQL 一致性需在合并时确认

## 关掉时材料去哪

- 分支合并回 master 后：结论作为合并依据记录在 git merge commit 中；本 Explore 归档至 `done/` 或保持 closed 状态
- 站点模型机制（`model_name_key`）已体现在 spec 与 001-x issue