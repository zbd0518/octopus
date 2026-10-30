-- =============================================================================
-- fix-migration-versions-v2.7.0.sql 的回滚脚本
-- 把 migration_records 恢复成"合并前旧编号"的形态。
--
-- 注意：本脚本只回滚 migration_records 记录，不触碰任何业务表结构
--       （迁移本身都是幂等的，表结构不受编号影响）。
-- =============================================================================

START TRANSACTION;

-- 1) 删除修复时补出来的记录
DELETE FROM migration_records WHERE version IN (50, 51, 52, 53, 58, 59, 60, 61, 62);
DELETE FROM migration_records WHERE version IN (63, 64);

-- 2) 46-49 搬回临时高位
UPDATE migration_records SET version = 900 WHERE version = 46 AND status = 1;
UPDATE migration_records SET version = 901 WHERE version = 47 AND status = 1;
UPDATE migration_records SET version = 902 WHERE version = 48 AND status = 1;
UPDATE migration_records SET version = 903 WHERE version = 49 AND status = 1;

-- 3) 54-57 搬回旧位置 62-65
UPDATE migration_records SET version = 62 WHERE version = 54 AND status = 1;
UPDATE migration_records SET version = 63 WHERE version = 55 AND status = 1;
UPDATE migration_records SET version = 64 WHERE version = 56 AND status = 1;
UPDATE migration_records SET version = 65 WHERE version = 57 AND status = 1;

-- 4) 临时高位落回旧 54-57
UPDATE migration_records SET version = 54 WHERE version = 900;
UPDATE migration_records SET version = 55 WHERE version = 901;
UPDATE migration_records SET version = 56 WHERE version = 902;
UPDATE migration_records SET version = 57 WHERE version = 903;

COMMIT;

-- 校验：
-- SELECT version, status FROM migration_records ORDER BY version;
