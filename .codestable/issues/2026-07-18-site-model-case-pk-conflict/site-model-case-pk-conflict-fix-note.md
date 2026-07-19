---
doc_type: issue-fix-note
issue: 2026-07-18-site-model-case-pk-conflict
status: confirmed
selected_fix: B2
related:
  - site-model-case-pk-conflict-report.md
  - site-model-case-pk-conflict-analysis.md
tags:
  - sitesync
  - mysql
  - model-name-key
  - migration-034
---

# 站点同步模型名大小写主键冲突 Fix Note

## 1. 根因（确认）

`site_models` 唯一索引原为 `(site_account_id, group_key, model_name)`。MySQL `utf8mb4` 默认 collation 大小写不敏感，同组 `GLM-5.2` 与 `glm-5.2` 在库端视为同一键；应用层按原始大小写去重并批量插入 → `Duplicate entry` / 主键冲突，站点同步失败。

## 2. 选定方案

**B2**：新增 `model_name_key = md5(TrimSpace(原始 model_name))`（hex，32 字符，**不 ToLower**），唯一索引改为 `(site_account_id, group_key, model_name_key)`；`model_name` 继续保存原始大小写展示值。

## 3. 改动清单

| 文件 | 改动 |
|---|---|
| `internal/model/site.go` | `SiteModel` 增加 `ModelNameKey`；去掉 `model_name` 上的旧 unique tag；新增 `SiteModelNameKey` / `EnsureModelNameKey` / `SiteModelIdentityKey`；`BeforeCreate`/`BeforeSave` 自动填 key |
| `internal/db/migrate/034.go` | **BeforeAutoMigrate**：加列、回填、删旧唯一索引、建新唯一索引（避免 AutoMigrate 先建新唯一索引时空 key 撞车） |
| `internal/sitesync/storage.go` | persist / compact / merge 身份键改用 `SiteModelIdentityKey`，写入前 `EnsureModelNameKey` |
| `internal/sitesync/sync_fetch.go` | `buildSiteModels` / `buildGlobalSiteModels` / `syncSiteModelsByGroup` 填充与按 key 去重 |
| `internal/sitesync/project.go` | `compactSiteModels` 按 key 去重 |
| `internal/model/site_model_name_key_test.go` | key 大小写敏感单测 |
| `internal/sitesync/storage_model_key_test.go` | compact / build 保留大小写变体 |
| `internal/db/migrate/034_test.go` | 迁移回填 + 双写变体 + 幂等 |

## 4. 验证

```text
go test ./internal/model ./internal/sitesync ./internal/db/migrate -count=1
# ok model / sitesync / migrate
```

覆盖：
- MD5 key 对 `GLM-5.2` / `glm-5.2` 不同
- compact 保留两条大小写变体、去掉完全重复
- 迁移后旧行回填 key，新变体可插入，二次迁移幂等
- 既有 sitesync project/storage 回归通过（GORM hook 保证测试插入也填 key）

## 5. 遗留风险 / 不在本次范围

1. **价格表 `llm_infos`** 仍 `ToLower` 主键：两条站点模型可能共享一条价格记录（预期可接受）。
2. **`group_items.model_name`** 若 MySQL CI 唯一，未来若同组同渠道双写大小写变体仍可能冲突；本次未改。
3. **投射渠道 `channel.Model`** 会同时列出两种大小写（符合「都保留」）；自动分组等若大小写不敏感匹配，行为需观察。
4. 未在真实 MySQL 实例上手工跑一次站点同步（单测以 SQLite 为主 + 迁移 dialect 分支代码）。

## 6. 回滚要点

- 回滚代码 + 如需回滚库：恢复旧唯一索引需先处理已存在的大小写双行，否则 MySQL 无法重建 CI 唯一索引。
