---
doc_type: approval-report
unit: 2026-07-18-site-model-case-pk-conflict
status: approved
reason: review-authorization
approvals:
  confirm-report: approved
  confirm-fix-plan: approved
approval_groups: {}
created_at: 2026-07-18
---

# Approval Report

## Decision History

- 2026-07-18 — `confirm-report` → **approved**
- 2026-07-18 — owner 补充 MD5 派生键设想 → 记入 analysis 方案 B2
- 2026-07-18 — `confirm-fix-plan` → **approved**（选定 **方案 B2**）

## Decision Needed

（无 pending；fix 进行中）

## Why Now

—

## Context

- 选定方案：**B2** — `model_name_key = md5(TrimSpace(原始 model_name))`，唯一索引 `(site_account_id, group_key, model_name_key)`
- `model_name` 保留原始大小写展示值

## Options

—

## Recommendation

—

## Risks And Tradeoffs

—

## Non-Automatic Actions

fix 阶段将改代码与 migration；不会自动 commit / 部署，除非 owner 另有指示。

## After You Answer

进入 fix：改 `SiteModel`、migration 034、sitesync 去重/写入、单测。
