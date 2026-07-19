---
doc_type: issue-report
issue: 2026-07-18-site-model-case-pk-conflict
status: confirmed
issue_path: standard
severity: P1
summary: 站点/Hub 同步时，同一分组内仅大小写不同的模型名（如 GLM-5.2 与 glm-5.2）触发主键/唯一约束冲突导致同步失败
tags:
  - sitesync
  - model-name
  - mysql
  - unique-constraint
  - case-sensitivity
---

# 站点同步模型名大小写主键冲突 Issue Report

## 1. 问题现象

在站点 / Hub 同步渠道（站点账号同步）过程中，若上游同一分组返回的模型列表里存在**仅大小写不同**的模型名（例如 `GLM-5.2` 与 `glm-5.2`），同步会失败，错误表现为数据库**主键相同 / 唯一约束冲突**（unique constraint / primary key 相关错误），同步无法正常完成。

## 2. 复现步骤

1. 使用 **MySQL** 作为主库部署 Octopus。
2. 配置一个站点 / Hub 账号，其上游模型列表在同一 `group` 内同时包含仅大小写不同的模型名（例如 `GLM-5.2` 与 `glm-5.2`）。
3. 在站点管理中触发该账号的同步（同步渠道 / 同步模型）。
4. 观察到：同步失败，日志或接口返回主键 / 唯一约束冲突类错误。

复现频率：**稳定**（只要上游同组存在大小写变体模型名即可复现）

## 3. 期望 vs 实际

**期望行为**：同步不因「仅大小写不同的模型名」而报主键错误；用户希望**两种大小写形式都保留**，且同步成功。

**实际行为**：同步写入失败，报主键相同 / 唯一约束冲突，同步中断。

## 4. 环境信息

- 涉及模块 / 功能：站点 / Hub 同步（`internal/sitesync`）、站点模型持久化（`SiteModel`）
- 相关文件 / 函数：
  - `internal/model/site.go` — `SiteModel` 唯一索引 `idx_site_account_group_model`（`site_account_id` + `group_key` + `model_name`）
  - `internal/sitesync/storage.go` — `persistSyncSnapshot` / `preparePersistedSyncModels` / `compactPersistedSiteModels`
- 运行环境：主库 **MySQL**
- 其他上下文：MySQL 在常见 `utf8mb4` / 默认 collation 下，字符串唯一索引对大小写往往**不敏感**；应用层 `compactPersistedSiteModels` 的去重键使用原始大小写 `model_name`（仅 `TrimSpace`），与库端唯一约束语义不一致。用户明确期望「都保留且不报错」，该期望与 MySQL 默认唯一索引行为可能冲突，需在分析阶段做方案取舍。

## 5. 严重程度

**P1** — 站点 / Hub 同步失败会阻断模型列表更新与后续投射，影响日常运维；有一定触发条件（上游同组大小写重复），非全用户必现，但一旦触发无法绕过完成同步。

## 备注

- 快速通道判定：**不走 fast-path**。虽有明确现象与高相关代码线索，但「都保留」与 MySQL 唯一索引语义存在产品 / 方案取舍，且修复可能跨持久化去重、索引策略、投射渠道与模型匹配等多处，超出「1–2 处小修」。
- 分析阶段需核对：冲突究竟落在 `site_models` 还是后续投射写入 `llm_infos` / `group_items` / `channels`；以及「都保留」在 MySQL 上是否需要显式 binary/ci 索引策略或换存法。
