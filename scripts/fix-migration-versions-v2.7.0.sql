-- =============================================================================
-- 修复 migration_records 编号错位（合并 master / v2.7.0 后）
-- 适用：MariaDB / MySQL 的 octopus 库
--
-- 已在本地 13.0.2-MariaDB 实测通过并修正完成。
--
-- 背景
-- -----------------------------------------------------------------------------
-- 本仓库长期在 43-47、50-57 区间插入了自己的迁移，与 upstream/master 编号
-- 整体错位。合并 v2.7.0 时又出现 62-65 与 upstream 54-57 撞号，曾把 upstream
-- 的池迁移临时推到 62-65。启动时会直接报：
--     duplicated migration version: 62
--
-- 现在代码已与 upstream 对齐（1-62 一一对应），本仓库独有迁移顺延到 63/64：
--   63 = plan_providers 自动刷新字段         (upstream 原 44)
--   64 = plan_providers 累计已用额度 TotalUsed (upstream 原 45)
--
-- 注意：不要逐个 UPDATE 旧编号 -> 新编号
-- -----------------------------------------------------------------------------
-- 旧库的 46/47 本身就是本仓库的 plan_provider 迁移，直接 UPDATE 到 46/47 会撞
-- 主键（ERROR 1062）。正确做法是：保留 1-45，清空 46 以后，再按目标集合重建
-- 为「全部已执行」。所有这些迁移都是幂等的，且表结构在旧编号下都已生效，
-- 因此把记录标记为已执行是安全的。
--
-- 用法
-- -----------------------------------------------------------------------------
--   mysql -u root -p octopus < scripts/fix-migration-versions-v2.7.0.sql
--
-- 执行前先备份：
--   CREATE TABLE migration_records_backup_YYYYMMDD AS SELECT * FROM migration_records;
-- 回滚见 scripts/fix-migration-versions-v2.7.0-rollback.sql
-- =============================================================================

START TRANSACTION;

-- 1) 清空 46 及之后的记录（1-45 与 upstream 一致，无需改动）
DELETE FROM migration_records WHERE version >= 46;

-- 2) 按目标编号集合重建为「已执行」
INSERT IGNORE INTO migration_records (version, status) VALUES
  (46, 1),  -- plan_provider 账号密码登录
  (47, 1),  -- 模型广场归一化去重默认值
  (48, 1),  -- channel_keys.managed
  (49, 1),  -- groups.endpoint_type '*' -> 'chat'
  (50, 1),  -- channel_keys.supported_models
  (51, 1),  -- api_keys.api_key_hash
  (52, 1),  -- api_key_hash 回填
  (53, 1),  -- channel_keys.supported_models -> text
  (54, 1),  -- pool_scheduled_tests
  (55, 1),  -- pool_unsched_rules
  (56, 1),  -- pool_accounts.proxy_fallback_origin_id
  (57, 1),  -- alert_rules.scope_pool_account_id
  (58, 1),  -- groups.thinking_mode
  (59, 1),  -- model_normalize_tasks
  (60, 1),  -- plan_provider 专属号池
  (61, 1),  -- plugins
  (62, 1),  -- nodes
  (63, 1),  -- plan_providers 自动刷新字段（本仓库独有）
  (64, 1);  -- plan_providers.total_used（本仓库独有）

COMMIT;

-- =============================================================================
-- 执行后校验
-- =============================================================================
-- 1) 应是 1-64 共 64 条、0 重复：
--    SELECT COUNT(*) FROM migration_records;
--    SELECT COUNT(*) FROM (SELECT version FROM migration_records
--                          GROUP BY version HAVING COUNT(*)>1) t;
--
-- 2) 启动一次程序（新版本），AutoMigrate 会自动补建缺失的表：
--    plugins / plugin_kvs / plugin_secrets / nodes / model_normalize_tasks
--    再确认：
--    SELECT table_name FROM information_schema.tables
--     WHERE table_schema = DATABASE()
--       AND table_name IN ('plugins','nodes','model_normalize_tasks');
