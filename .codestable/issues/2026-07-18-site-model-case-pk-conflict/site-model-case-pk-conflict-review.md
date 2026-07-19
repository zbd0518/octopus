---
doc_type: issue-review
issue: 2026-07-18-site-model-case-pk-conflict
status: completed
commit: 112a42b
branch: fix/site-model-case-pk-conflict
reviewer: agent
outcome: pass
---

# Code Review：site-model-case-pk-conflict (B2)

## 范围

- 分支：`fix/site-model-case-pk-conflict`
- 提交：`112a42b` + follow-up（收紧 BeforeSave）
- 对比：`master...HEAD`
- 代码：`internal/model/site.go`、`internal/db/migrate/034*.go`、`internal/sitesync/{storage,sync_fetch,project}.go` + 测试

## 结论

**通过，可合并。**  
根因对症（MySQL CI unique vs 应用层大小写敏感身份），B2 实现完整，迁移放在 `BeforeAutoMigrate` 顺序正确，相关单测通过。

## Findings

### Blocking

无。

### Important

1. **`BeforeSave` 钩子面过宽**（`internal/model/site.go`）— **已在 follow-up 修复**  
   - 现改为：仅当 `ModelName` 非空时重算 key；空名 partial Save 不再改写 `ModelNameKey`。  
   - 并补充单测 `TestSiteModelBeforeSaveSkipsEmptyModelName`。

2. **升级后下游身份分裂（已知产品债，非本 PR 回归）**  
   - `llm_infos` 仍 ToLower；`group_items.model_name` 仍可能是 MySQL CI unique。  
   - 双存大小写后，价格合并 / 分组项在极端情况下仍可能撞车。  
   - fix-note 已记录；若生产出现分组双写冲突，需另开 issue。

### Nit

1. Git 把含中文注释的 Go 文件标成 Binary（`0 insertions`），不影响内容正确性；可考虑统一 LF 以改善 diff 可读性。  
2. `model_name_key` 空名回填用 `_empty_` 占位，仅迁移路径；运行时空名仍 key 为空，第二行空名会 unique 失败（可接受的边界）。  
3. migration 034 是目前唯一的 `BeforeAutoMigration`，与 version 记录机制兼容，OK。

## 验证核对

```text
go test ./internal/model ./internal/sitesync ./internal/db/migrate -count=1
# ok
```

- 大小写 key 不同：有单测  
- compact 保留变体：有单测  
- 迁移回填 + 双写 + 幂等：有单测  
- BeforeSave 空名不改 key：有单测  
- sitesync 既有 project/storage 回归：通过  

## 合并前建议

1. 在 **MySQL** 环境对含 `GLM-5.2`/`glm-5.2` 的站点账号点一次同步做冒烟  
2. 本 PR **未包含** 完整 `.codestable` 骨架（仅 issue 文档）
