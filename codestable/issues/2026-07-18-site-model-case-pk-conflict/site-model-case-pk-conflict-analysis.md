---
doc_type: issue-analysis
issue: 2026-07-18-site-model-case-pk-conflict
status: confirmed
selected_fix: B2
root_cause_type: data-format
related:
  - site-model-case-pk-conflict-report.md
tags:
  - sitesync
  - mysql
  - unique-constraint
  - case-sensitivity
  - model-name
---

# 站点同步模型名大小写主键冲突 根因分析

## 1. 问题定位

| 关键位置 | 说明 |
|---|---|
| `internal/model/site.go:219-223` | `SiteModel` 唯一索引 `idx_site_account_group_model` = `(site_account_id, group_key, model_name)`。`model_name` 按普通字符串入库，无 binary / case-sensitive 标注。 |
| `internal/db/db.go:464-470` | MySQL 连接默认补 `charset=utf8mb4`。未指定 collation 时，库/表通常落在 **大小写不敏感** collation（如 `utf8mb4_unicode_ci` / `utf8mb4_0900_ai_ci`）。唯一索引比较时 `GLM-5.2` ≡ `glm-5.2`。 |
| `internal/sitesync/http.go:400-415` | `normalizeModelNames` 仅 `TrimSpace` 后按**原始大小写**去重；`GLM-5.2` 与 `glm-5.2` 都会保留。 |
| `internal/sitesync/sync_fetch.go:566-573` | `buildSiteModels` 用 `normalizeModelNames` 的结果直接生成 `SiteModel` 列表，大小写变体全部进入 snapshot。 |
| `internal/sitesync/storage.go:442-470` | `compactPersistedSiteModels` 去重键 = `groupKey + "\x00" + TrimSpace(ModelName)`，**大小写敏感**；与 MySQL 唯一索引语义不一致。 |
| `internal/sitesync/storage.go:134-140` | `persistSyncSnapshot` 先按账号删除再 `tx.Create(&finalModels)` 批量插入；同组大小写变体在应用层仍是两行 → MySQL **Error 1062 / Duplicate entry**（用户看到的「主键相同」）。 |
| `internal/sitesync/core.go:47-48` | `SyncAccount` 在抓取成功后调用 `persistSyncSnapshot`；此处失败则整次同步失败，后续投射不执行。 |
| `internal/sitesync/project.go:516-533` | 投射阶段 `compactSiteModels` 同样大小写敏感去重；若仅修投射而不修持久化，仍会在入库阶段失败。 |
| `internal/op/llm/llm.go:110-149` | 模型价格表 `LLMInfo.Name` 主键路径会 `strings.ToLower` 并在内存去重；投射后的 `syncProjectedModelPrices` 通常**不会**因大小写双写而炸。主炸点在 `site_models`，不是 `llm_infos`。 |

## 2. 失败路径还原

**正常路径**：

站点账号同步 → `SyncAccount` → 上游拉取模型名 → `normalizeModelNames` 去重 → `buildSiteModels` 写入 snapshot → `persistSyncSnapshot` 合并/压缩 → 批量 `Create` 成功 → `ProjectAccount` 更新投射渠道与价格。

**失败路径**：

1. 上游同组返回 `["GLM-5.2", "glm-5.2", ...]`（或其它仅大小写不同的名字）。
2. `normalizeModelNames` 认为二者不同，全部保留。
3. `buildSiteModels` / `syncSiteModelsByGroup` 把两条都放进 `snapshot.models`。
4. `preparePersistedSyncModels` / `mergePersistedSiteModelsByGroup` / `compactPersistedSiteModels` 仍按**大小写敏感**键去重，两条都留下。
5. `tx.Create(&finalModels)` 在 MySQL 上因唯一索引 `idx_site_account_group_model` 将二者视为同一键 → 插入失败。
6. `SyncAccount` 返回错误，同步状态失败；用户看到主键 / 唯一约束冲突。

**分叉点**：

`internal/sitesync/storage.go:442-456`（及上游 `normalizeModelNames`）——应用层「大小写不同 = 不同模型」与 MySQL utf8mb4 默认「大小写不敏感唯一约束」语义冲突。真正抛错在 `storage.go:138` 的批量 `Create`。

## 3. 根因

**根因类型**：data-format（应用层模型名身份语义与数据库唯一约束 / collation 语义不一致）+ missing-guard（入库前未按库端等价规则折叠）

**根因描述**：

站点模型表用 `(账号, 分组, model_name)` 做唯一约束。MySQL + `utf8mb4` 默认 collation 下，`model_name` 比较通常**不区分大小写**。但同步链路从抓取归一化到持久化压缩，全程用**大小写敏感**字符串当身份键，导致 `GLM-5.2` 与 `glm-5.2` 在应用层是两行、在数据库里却是同一唯一键，批量插入时触发 Duplicate entry。

**是否有多个根因**：是

1. **主因**：`site_models` 持久化去重与 MySQL 唯一索引大小写语义不一致（直接导致报错）。
2. **次因 / 放大器**：`normalizeModelNames`、`compactSiteModels` 同样敏感去重；即便只修一处，其它入口仍可能再引入变体。
3. **约束说明**：用户期望「两种大小写都保留」。在 MySQL 默认 CI collation + 当前唯一索引定义下，**物理上不能**在同一 `(account, group)` 下存两条仅大小写不同的 `model_name`，除非改列 collation / 唯一索引策略为大小写敏感（或改唯一键设计）。这是方案取舍的关键边界。

## 4. 影响面

- **影响范围**：所有走 `sitesync.SyncAccount` → `persistSyncSnapshot` 的站点 / Hub 同步；只要某组模型列表含大小写变体就会稳定失败。管理端「同步渠道」与定时批量同步同源。
- **潜在受害模块**：
  - `ProjectAccount` 投射渠道的 `channel.Model` 字符串（若硬保留双写，下游 diff/自动分组可能重复或混乱）
  - `LLMPriceAddToDB` / `llm.BatchCreate`（已小写化，当前不易炸，但与 `site_models` 展示名可能不一致）
  - 分组 `GroupItem.model_name` 唯一索引 `idx_group_channel_model` 在 MySQL 上同样可能 CI；若别处大小写双写，存在同类风险
  - SQLite / PostgreSQL 默认更接近大小写敏感：同一 bug 在非 MySQL 上可能「不报错但存两条」，行为跨库不一致
- **数据完整性风险**：有。同步事务失败时本次 snapshot 不落库；反复失败会让站点模型 / 投射渠道停在旧状态。若强行改成 CS 双存，又会与价格表小写主键、请求路由大小写策略产生分裂身份。
- **严重程度复核**：**维持 P1**。不是全站必现，但触发即阻断同步且无业务层可读绕过；与 report 一致。

## 5. 修复方案

### 方案 A：应用层按「大小写不敏感」折叠为一条（推荐）

- **做什么**：
  1. 在 `normalizeModelNames`、`compactPersistedSiteModels`、`compactSiteModels`（以及 `syncSiteModelsByGroup` / `buildGlobalSiteModels` 的 seen 键）统一用 `strings.ToLower(TrimSpace(name))` 作为身份键去重。
  2. 保留策略二选一（实现时定一种并单测固定）：**保留首次出现的原始大小写**，或 **统一存小写**（与 `llm.Create` 一致，更干净）。
  3. 补单测：同组 `GLM-5.2` + `glm-5.2` 同步不报错，最终只 1 行。
  4. 可选：日志里 `Warn` 记录被折叠的变体，方便排查上游脏数据。
- **优点**：改动集中在 sitesync 去重；三库行为一致；直接消掉 1062；与价格表小写身份靠拢。
- **缺点 / 风险**：不满足「两条都保留」的字面期望；展示名可能从 `GLM-5.2` 变成 `glm-5.2`（若选统一小写）。需确认转发时上游是否大小写敏感（多数 OpenAI 兼容模型 id 实际 case-sensitive，但上游若两套并存通常本就是脏数据 / 重复条目）。
- **影响面**：主要 `internal/sitesync/{http,storage,project,sync_fetch}.go` + 测试；不改表结构。

### 方案 B：MySQL 列改为大小写敏感唯一索引，允许双存

- **做什么**：
  1. 迁移 `site_models.model_name` 使用 `utf8mb4_bin`（或列级 binary collation），使唯一索引区分大小写。
  2. 应用层继续大小写敏感去重（现状），两行都能插入。
  3. 评估 SQLite / Postgres 是否需对齐；评估 `group_items` / `llm_infos` 是否同样改。
- **优点**：最贴近用户「都保留且不报错」；唯一键仍直接基于业务字段，可读性好。
- **缺点 / 风险**：
  - 迁移成本与存量数据冲突（已有 CI 语义下不可能已双存，但 collation 变更要锁表 / rebuild index）。
  - 跨库行为更分裂；价格表仍小写主键，会出现「站点有两条、价格一条」。
  - 路由 / 自动分组 / 统计若某处 `EqualFold`、某处严格相等，会出现半匹配半丢失。
  - 多数上游「同模型两套大小写」更像重复脏数据，双存会把脏数据产品化。
- **影响面**：DB migration + sitesync + 可能的 group/llm 语义梳理；回归面大。

### 方案 B2：唯一索引改为「大小写敏感派生键」（如 MD5 / SHA256 of 原 model_name）

> 来自 owner 提案：索引不直接建在 `model_name` 上，而建在 `md5(原始大小写 model_name)`（或等价 hash / binary 指纹）上，使 `GLM-5.2` 与 `glm-5.2` 的唯一键不同。

- **做什么（推荐落地形态，而非裸函数索引）**：
  1. 新增持久化列，例如 `model_name_key`（`char(32)` 存 hex MD5，或 `binary(16)` / `char(64)` 存 SHA256）。
  2. 写入前在应用层计算：`model_name_key = hex(md5(TrimSpace(model_name)))`（**不要**先 ToLower；必须对原始大小写哈希）。
  3. 唯一索引改为 `(site_account_id, group_key, model_name_key)`，**去掉**（或不再依赖）对 CI 的 `model_name` 唯一约束。
  4. `model_name` 列继续存原始展示值；查询 / 展示 / 投射仍读 `model_name`。
  5. 同步链路的 seen 键与 compact 键改为使用同一 `model_name_key`（或与哈希输入一致的原始 trim 串）。
  6. 三库 migration：SQLite / MySQL / PostgreSQL 都加列 + 回填 + 换唯一索引。

- **为什么技术上可行**：
  - MD5/SHA 输出是十六进制或二进制字节，在 MySQL 常见 collation 下比较等价于**大小写敏感 / 字节敏感**（hex 本身只有 `0-9a-f`）。
  - `md5("GLM-5.2") ≠ md5("glm-5.2")`，因此唯一索引不再把二者撞成同一键。
  - 比直接改 `model_name` 的 collation 更「局部」：展示列 collation 可保持不变；唯一语义由派生键承担。

- **比「直接在索引里写 MD5(model_name)」更稳的原因**：
  - GORM / AutoMigrate 对三库「表达式唯一索引」支持不一致；生成列 / 函数索引在 SQLite、MySQL、Postgres 语法各异。
  - 应用层维护 `model_name_key` 列可预测、可单测、可回填，与现有 GORM tag 模型一致。
  - 若坚持 DB 生成列：MySQL 8 可用 `GENERATED ALWAYS AS (MD5(model_name)) STORED` + unique；仍要写 dialect 分支 migration，且 SQLite/Postgres 要各自等价实现。

- **优点**：
  - 满足「`GLM-5.2` 与 `glm-5.2` 都保留且不报主键冲突」。
  - 不必把整列 `model_name` 改成 `utf8mb4_bin`（避免影响按名模糊搜等依赖 CI 的行为，若有）。
  - 哈希键固定长度，避开超长 `model_name` 的索引前缀问题。

- **缺点 / 风险（与方案 B 同类，甚至多一层间接）**：
  1. **仍只解决 `site_models` 入库**；投射后的 `channel.Model` 字符串、`group_items.model_name`、`llm_infos.name` 若继续大小写敏感双写或 CI 唯一，可能在下游再次冲突或身份分裂。
  2. **价格表 `llm.Create` 会 ToLower**：双存站点模型最终可能共享同一价格主键 → 「站点两条、价格一条」，计费/展示要对齐策略。
  3. **MD5 非加密用途可接受**，但理论碰撞存在；模型名场景极低，若在意可改 SHA256。
  4. **所有写入路径必须同步维护 `model_name_key`**，漏写会导致唯一约束失效或误伤。
  5. **迁移 + 回填 + 换索引**工作量不低于 B；比 A 重一个数量级。
  6. 若产品本意是「同一模型只应一条」，B2 会把上游脏数据合法化。

- **影响面**：`model.SiteModel` 结构、migration、`sitesync` 全链路 compact/seen、可能的备份/导入导出、相关测试。

### 方案 C：入库前检测并返回可读业务错误（治标）

- **做什么**：在 `persistSyncSnapshot` 前按 lower 键检测冲突，返回明确中文错误：「同组存在仅大小写不同的模型名：GLM-5.2 / glm-5.2」，不写库。
- **优点**：用户不再看到原生主键错误；实现最小。
- **缺点 / 风险**：同步仍然失败，P1 业务阻断未解除；不满足「不报错」。
- **影响面**：`storage.go` + 错误包装；几乎不改数据模型。

### 推荐方案

**推荐方案 A**，理由：

1. 根因是「应用身份键 vs 库唯一约束」语义不一致；在应用层按库端等价规则折叠，是最小且跨库一致的修法。
2. 方案 B / B2 虽可字面「都保留」，但把上游重复模型固化进核心表，并与 `llm` 小写主键、统计、分组多处语义冲突；B2 额外引入派生键维护成本，并不比 B 更省心。
3. 方案 C 只改善报错体验，不恢复同步能力。

若你坚持「必须物理保留两种大小写」：

- 想少动业务字段 collation → 选 **B2**（`model_name_key = md5(原名)` 唯一索引）。
- 想唯一键仍直接可读 → 选 **B**（`model_name` binary collation）。

否则实现 A 时建议 **统一存小写**（与 `internal/op/llm/llm.go` 一致），展示层如需美化再另做。

## 备注（给修复阶段）

- 验证重点：MySQL 下构造同组大小写变体 → `SyncAccount` 成功；`site_models` 仅 1 行；投射渠道 `Model` 字段无重复变体；相关 unit test。
- 不在本 issue 扩大做全库模型名规范化，除非修复时发现 `group_items` 已有同类生产故障。
