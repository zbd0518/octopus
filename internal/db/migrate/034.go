package migrate

import (
	"fmt"
	"strings"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	// 必须在 AutoMigrate 之前：否则 GORM 会先按新 tag 建
	// idx_site_account_group_model_key，而存量行的 model_name_key 仍为空，
	// 同账号同分组多行会立刻撞唯一约束。
	RegisterBeforeAutoMigration(Migration{
		Version: 34,
		Up:      migrateSiteModelNameKey,
	})
}

// 034: site_models 唯一键从 (site_account_id, group_key, model_name) 改为
// (site_account_id, group_key, model_name_key)，其中 model_name_key =
// md5(TrimSpace(原始 model_name)) 十六进制。
//
// 背景：MySQL utf8mb4 默认 collation 对大小写不敏感，同组 GLM-5.2 与 glm-5.2
// 会触发 unique 冲突；应用层希望保留两种原始大小写展示值。
//
// 步骤（幂等）：
// 1. 确保 model_name_key 列存在（AutoMigrate 通常已加；legacy 表再补）
// 2. 回填空的 model_name_key
// 3. 先创建新唯一索引 idx_site_account_group_model_key（若不存在）
// 4. 再删除旧唯一索引 idx_site_account_group_model（若存在）
//
// 步骤 3 必须在 4 之前：MySQL 上 site_models.site_account_id 的外键依赖
// 旧复合唯一索引作为引用列索引；若先删旧索引会报
// Error 1553 (HY000): Cannot drop index ... needed in a foreign key constraint。
// 新唯一索引同样以 site_account_id 为最左列，可承接外键索引需求。
func migrateSiteModelNameKey(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteModel{}) {
		return nil
	}

	if !db.Migrator().HasColumn(&model.SiteModel{}, "ModelNameKey") {
		if err := db.Migrator().AddColumn(&model.SiteModel{}, "ModelNameKey"); err != nil {
			return fmt.Errorf("add site_models.model_name_key: %w", err)
		}
	}

	if err := backfillSiteModelNameKeys(db); err != nil {
		return err
	}

	if !db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model_key") {
		if err := db.Migrator().CreateIndex(&model.SiteModel{}, "idx_site_account_group_model_key"); err != nil {
			return fmt.Errorf("create site_models idx_site_account_group_model_key: %w", err)
		}
	}

	if err := dropSiteModelOldUniqueIndexes(db); err != nil {
		return err
	}
	return nil
}

func backfillSiteModelNameKeys(db *gorm.DB) error {
	var rows []model.SiteModel
	// 只拉需要回填的行，避免大表全量进内存后无谓更新。
	if err := db.Where("model_name_key IS NULL OR model_name_key = ?", "").
		Select("id", "model_name", "model_name_key").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("list site_models missing model_name_key: %w", err)
	}
	for i := range rows {
		key := model.SiteModelNameKey(rows[i].ModelName)
		if key == "" {
			// 空名极少见；写占位避免 not null 违约，后续同步会清掉。
			key = model.SiteModelNameKey("_empty_")
		}
		if err := db.Model(&model.SiteModel{}).
			Where("id = ?", rows[i].ID).
			Update("model_name_key", key).Error; err != nil {
			return fmt.Errorf("backfill site_models.id=%d model_name_key: %w", rows[i].ID, err)
		}
	}
	return nil
}

func dropSiteModelOldUniqueIndexes(db *gorm.DB) error {
	// 先保证 site_account_id 上仍有可用索引（新唯一索引已建时自然满足；
	// 若 CreateIndex 因某种原因未建立，再补一个非唯一索引供 MySQL FK 使用）。
	if err := ensureSiteModelAccountIDIndexForFK(db); err != nil {
		return err
	}

	// GORM 旧标签名
	oldNames := []string{
		"idx_site_account_group_model",
		"uni_site_models_site_account_id_group_key_model_name",
	}
	for _, name := range oldNames {
		if db.Migrator().HasIndex(&model.SiteModel{}, name) {
			if err := db.Migrator().DropIndex(&model.SiteModel{}, name); err != nil {
				return fmt.Errorf("drop site_models index %s: %w", name, err)
			}
		}
	}

	// dialect 兜底：按列组合探测并删除「仅 account+group+model_name」的唯一索引
	switch db.Dialector.Name() {
	case "sqlite":
		return dropSQLiteSiteModelNameUniqueIndexes(db)
	case "mysql":
		return dropMySQLSiteModelNameUniqueIndexes(db)
	case "postgres", "postgresql":
		return dropPostgresSiteModelNameUniqueIndexes(db)
	default:
		return nil
	}
}

// ensureSiteModelAccountIDIndexForFK 确保删除旧复合唯一索引后，
// site_account_id 仍有索引可服务 site_models -> site_accounts 外键。
// 新唯一索引 idx_site_account_group_model_key 已以 site_account_id 为最左列时无需额外操作。
func ensureSiteModelAccountIDIndexForFK(db *gorm.DB) error {
	if db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model_key") {
		return nil
	}
	// 兜底：建独立非唯一索引（正常路径不应走到这里）
	if db.Dialector.Name() == "mysql" {
		var count int64
		if err := db.Raw(`
SELECT COUNT(1) FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'site_models'
  AND COLUMN_NAME = 'site_account_id' AND SEQ_IN_INDEX = 1`).Scan(&count).Error; err != nil {
			return fmt.Errorf("check site_models site_account_id index: %w", err)
		}
		if count > 0 {
			return nil
		}
		if err := db.Exec(`CREATE INDEX idx_site_models_site_account_id ON site_models (site_account_id)`).Error; err != nil {
			return fmt.Errorf("create site_models site_account_id index for fk: %w", err)
		}
	}
	return nil
}

func dropSQLiteSiteModelNameUniqueIndexes(db *gorm.DB) error {
	type indexRow struct {
		Name   string
		Unique int
	}
	var indexes []indexRow
	if err := db.Raw(`PRAGMA index_list('site_models')`).Scan(&indexes).Error; err != nil {
		return fmt.Errorf("list sqlite site_models indexes: %w", err)
	}
	for _, idx := range indexes {
		if idx.Unique != 1 {
			continue
		}
		if idx.Name == "idx_site_account_group_model_key" {
			continue
		}
		var columns []struct{ Name string }
		if err := db.Raw(fmt.Sprintf("PRAGMA index_info(%q)", idx.Name)).Scan(&columns).Error; err != nil {
			return fmt.Errorf("inspect sqlite site_models index %s: %w", idx.Name, err)
		}
		if isSiteAccountGroupModelNameIndex(columnsToNames(columns)) {
			if err := db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %q", idx.Name)).Error; err != nil {
				return fmt.Errorf("drop sqlite site_models index %s: %w", idx.Name, err)
			}
		}
	}
	return nil
}

func dropMySQLSiteModelNameUniqueIndexes(db *gorm.DB) error {
	type indexRow struct {
		IndexName  string `gorm:"column:INDEX_NAME"`
		NonUnique  int    `gorm:"column:NON_UNIQUE"`
		ColumnName string `gorm:"column:COLUMN_NAME"`
		SeqInIndex int    `gorm:"column:SEQ_IN_INDEX"`
	}
	var rows []indexRow
	if err := db.Raw(`
SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'site_models'
ORDER BY INDEX_NAME, SEQ_IN_INDEX`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("list mysql site_models indexes: %w", err)
	}
	grouped := make(map[string][]string)
	unique := make(map[string]bool)
	for _, row := range rows {
		if row.IndexName == "PRIMARY" || row.IndexName == "idx_site_account_group_model_key" {
			continue
		}
		grouped[row.IndexName] = append(grouped[row.IndexName], row.ColumnName)
		unique[row.IndexName] = row.NonUnique == 0
	}
	for name, cols := range grouped {
		if !unique[name] {
			continue
		}
		if isSiteAccountGroupModelNameIndex(cols) {
			if err := db.Exec(fmt.Sprintf("ALTER TABLE site_models DROP INDEX `%s`", name)).Error; err != nil {
				return fmt.Errorf("drop mysql site_models index %s: %w", name, err)
			}
		}
	}
	return nil
}

func dropPostgresSiteModelNameUniqueIndexes(db *gorm.DB) error {
	type indexRow struct {
		IndexName string
		Columns   string
	}
	var rows []indexRow
	// pg_get_indexdef 较复杂；用 indkey 列名聚合
	if err := db.Raw(`
SELECT i.relname AS index_name,
       string_agg(a.attname, ',' ORDER BY x.n) AS columns
FROM pg_class t
JOIN pg_index ix ON t.oid = ix.indrelid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS x(attnum, n) ON true
JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = x.attnum
WHERE t.relname = 'site_models' AND ix.indisunique AND NOT ix.indisprimary
GROUP BY i.relname`).Scan(&rows).Error; err != nil {
		// 某些 PG 版本/权限下查询失败时，退回按已知名字删除
		for _, name := range []string{"idx_site_account_group_model"} {
			_ = db.Exec(fmt.Sprintf(`DROP INDEX IF EXISTS %q`, name)).Error
		}
		return nil
	}
	for _, row := range rows {
		if row.IndexName == "idx_site_account_group_model_key" {
			continue
		}
		cols := strings.Split(row.Columns, ",")
		if isSiteAccountGroupModelNameIndex(cols) {
			if err := db.Exec(fmt.Sprintf(`DROP INDEX IF EXISTS %q`, row.IndexName)).Error; err != nil {
				return fmt.Errorf("drop postgres site_models index %s: %w", row.IndexName, err)
			}
		}
	}
	return nil
}

func columnsToNames(columns []struct{ Name string }) []string {
	out := make([]string, 0, len(columns))
	for _, c := range columns {
		out = append(out, c.Name)
	}
	return out
}

func isSiteAccountGroupModelNameIndex(cols []string) bool {
	if len(cols) != 3 {
		return false
	}
	// 允许列顺序固定为 account, group, model_name
	return strings.EqualFold(cols[0], "site_account_id") &&
		strings.EqualFold(cols[1], "group_key") &&
		strings.EqualFold(cols[2], "model_name")
}
