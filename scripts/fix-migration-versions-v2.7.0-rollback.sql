-- =============================================================================
-- fix-migration-versions-v2.7.0.sql 的回滚脚本
-- 从备份表恢复 migration_records 的原始内容。
--
-- 前提：执行修复前创建过备份表（脚本里假设名为 migration_records_backup_20261030，
--       如用了别的名字请自行替换）。
-- =============================================================================

START TRANSACTION;

DELETE FROM migration_records;
INSERT INTO migration_records (version, status)
SELECT version, status FROM migration_records_backup_20261030;

COMMIT;

-- 校验：
-- SELECT version, status FROM migration_records ORDER BY version;
--
-- 说明：本脚本只恢复记录表，不触碰业务表结构。
--       所有迁移均为幂等实现，表结构不受编号影响。
