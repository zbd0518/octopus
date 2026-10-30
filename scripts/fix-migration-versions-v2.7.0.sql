-- =============================================================================
-- 修复 migration_records 编号错位（合并 master / v2.7.0 后）
-- 适用：MariaDB / MySQL 的 octopus 库
--
-- 背景
-- -----------------------------------------------------------------------------
-- 本仓库长期在 43-47、50-57 区间插入了自己的迁移，与 upstream/master 的
-- 编号整体错位。合并 v2.7.0 时又出现 62-65 与 upstream 54-57 撞号，曾经把
-- upstream 的池迁移临时推到了 62-65。
--
-- 现在代码已重新对齐 upstream（1-62 与 upstream 一一对应），本仓库独有的
-- 两个迁移顺延到 63/64：
--   63 = plan_providers 自动刷新字段         (upstream 原 44)
--   64 = plan_providers 累计已用额度 TotalUsed (upstream 原 45)
--
-- 存量库的 migration_records 记录的还是旧编号，必须修正，否则：
--   * 旧记录里的 62-65 会顶替新的 62(nodes) 与 53-57，导致真正该跑的迁移被跳过；
--   * 新编号 63/64 会被当成未执行而重复跑（虽幂等，但记录混乱）。
--
-- 旧的 -> 新的 映射
-- -----------------------------------------------------------------------------
--   54 -> 46   plan_provider 账号密码登录字段
--   55 -> 47   模型广场归一化去重默认值 true
--   56 -> 48   channel_keys.managed
--   57 -> 49   groups.endpoint_type '*' -> 'chat'
--   62 -> 54   pool_scheduled_tests
--   63 -> 55   pool_unsched_rules
--   64 -> 56   pool_accounts.proxy_fallback_origin_id
--   65 -> 57   alert_rules.scope_pool_account_id
--   新增 50,51,52,53,58,59,60,61,62   标记为已执行
--   新增 63,64                         本仓库独有迁移，标记为已执行
--
-- 用法
-- -----------------------------------------------------------------------------
--   mysql -u root -p octopus < scripts/fix-migration-versions-v2.7.0.sql
--
-- 脚本幂等，可重复执行。建议执行前先备份 migration_records：
--   CREATE TABLE migration_records_backup_<日期> AS SELECT * FROM migration_records;
-- 回滚见 scripts/fix-migration-versions-v2.7.0-rollback.sql
-- =============================================================================

START TRANSACTION;

-- ---------------------------------------------------------------------------
-- 步骤 1：旧 54-57 先搬到临时高位，避免与目标 54-57 占位冲突
-- ---------------------------------------------------------------------------
UPDATE migration_records SET version = 900 WHERE version = 54 AND status = 1;
UPDATE migration_records SET version = 901 WHERE version = 55 AND status = 1;
UPDATE migration_records SET version = 902 WHERE version = 56 AND status = 1;
UPDATE migration_records SET version = 903 WHERE version = 57 AND status = 1;

-- ---------------------------------------------------------------------------
-- 步骤 2：旧 62-65（池相关迁移）降到 54-57
-- ---------------------------------------------------------------------------
UPDATE migration_records SET version = 54 WHERE version = 62 AND status = 1;
UPDATE migration_records SET version = 55 WHERE version = 63 AND status = 1;
UPDATE migration_records SET version = 56 WHERE version = 64 AND status = 1;
UPDATE migration_records SET version = 57 WHERE version = 65 AND status = 1;

-- ---------------------------------------------------------------------------
-- 步骤 3：临时高位落回 46-49
-- ---------------------------------------------------------------------------
UPDATE migration_records SET version = 46 WHERE version = 900;
UPDATE migration_records SET version = 47 WHERE version = 901;
UPDATE migration_records SET version = 48 WHERE version = 902;
UPDATE migration_records SET version = 49 WHERE version = 903;

-- ---------------------------------------------------------------------------
-- 步骤 4：补齐 upstream 50-53、58-62 的已执行记录
--   这些迁移对应的表/列在旧编号下都已经建好，这里只补记录。
-- ---------------------------------------------------------------------------
INSERT IGNORE INTO migration_records (version, status) VALUES
  (50, 1),  -- channel_keys.supported_models
  (51, 1),  -- api_keys.api_key_hash
  (52, 1),  -- api_key_hash 回填
  (53, 1),  -- channel_keys.supported_models -> text
  (58, 1),  -- groups.thinking_mode
  (59, 1),  -- model_normalize_tasks
  (60, 1),  -- plan_provider 专属号池
  (61, 1),  -- plugins
  (62, 1);  -- nodes

-- ---------------------------------------------------------------------------
-- 步骤 5：本仓库独有的两个迁移顺延到 63/64
-- ---------------------------------------------------------------------------
INSERT INTO migration_records (version, status) VALUES
  (63, 1),  -- plan_providers 自动刷新字段
  (64, 1)   -- plan_providers.total_used
ON DUPLICATE KEY UPDATE status = VALUES(status);

COMMIT;

-- =============================================================================
-- 执行后校验
-- =============================================================================
-- 1) 不应有重复 version：
--    SELECT version, COUNT(*) c FROM migration_records GROUP BY version HAVING c > 1;
--
-- 2) 应看到 1-64 中除历史空号外的连续记录：
--    SELECT version, status FROM migration_records ORDER BY version;
--
-- 3) 抽查关键表/列确实存在：
--    SELECT table_name FROM information_schema.tables
--     WHERE table_schema = DATABASE()
--       AND table_name IN ('plugins','nodes','model_normalize_tasks',
--                          'pool_scheduled_tests','pool_unsched_rules');
--    SELECT table_name, column_name FROM information_schema.columns
--     WHERE table_schema = DATABASE()
--       AND ((table_name='channel_keys' AND column_name='managed')
--         OR (table_name='api_keys' AND column_name='api_key_hash')
--         OR (table_name='groups' AND column_name='thinking_mode'));
